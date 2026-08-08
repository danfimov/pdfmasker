package masker

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/benoitkugler/pdf/fonts/cmaps"
	benoitModel "github.com/benoitkugler/pdf/model"
	"github.com/benoitkugler/pdf/reader"
	tokenizer "github.com/benoitkugler/pstokenizer"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	pdfcpuModel "github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// hybridFontInfo holds font mapping info for the hybrid approach
type hybridFontInfo struct {
	lookup   map[benoitModel.CID][]rune
	reverse  map[rune]benoitModel.CID
	fallback benoitModel.CID
	isCID    bool
}

// inlineImagePlaceholder is used to mark inline image positions during tokenization
const inlineImagePlaceholder = "\x00INLINE_IMG_%d\x00"

// inlineImageInfo stores extracted inline image data
type inlineImageInfo struct {
	original []byte // The complete BI...EI block
	index    int    // Position index
}

// extractInlineImages removes inline images from content and returns placeholders
// Inline image format: BI <dict> ID <binary data> EI
func extractInlineImages(content []byte) ([]byte, []inlineImageInfo) {
	var images []inlineImageInfo
	result := make([]byte, 0, len(content))

	i := 0
	imgIndex := 0
	for i < len(content) {
		// Look for BI operator (must be preceded by whitespace or start of content)
		if i+3 < len(content) && isWhitespace(content, i) {
			if content[i+1] == 'B' && content[i+2] == 'I' && isWhitespace(content, i+3) {
				// Found potential BI operator
				biStart := i + 1

				// Find ID operator
				idPos := findIDOperator(content, biStart+2)
				if idPos < 0 {
					result = append(result, content[i])
					i++
					continue
				}

				// Find EI operator after ID
				eiPos := findEIOperator(content, idPos+3)
				if eiPos < 0 {
					result = append(result, content[i])
					i++
					continue
				}

				// Extract the complete inline image block
				imgEnd := eiPos + 2 // Include "EI"
				imgData := content[biStart:imgEnd]

				images = append(images, inlineImageInfo{
					original: imgData,
					index:    imgIndex,
				})

				// Add whitespace and placeholder
				result = append(result, content[i]) // Keep the whitespace before BI
				placeholder := fmt.Sprintf(inlineImagePlaceholder, imgIndex)
				result = append(result, []byte(placeholder)...)
				imgIndex++

				i = imgEnd
				continue
			}
		}
		result = append(result, content[i])
		i++
	}

	return result, images
}

// isWhitespace checks if position is at whitespace (or start/end boundary)
func isWhitespace(content []byte, pos int) bool {
	if pos < 0 || pos >= len(content) {
		return true // Treat boundaries as whitespace
	}
	c := content[pos]
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// findIDOperator finds the ID operator after BI dict
func findIDOperator(content []byte, start int) int {
	// ID must be preceded and followed by whitespace
	for i := start; i < len(content)-2; i++ {
		if content[i] == 'I' && content[i+1] == 'D' {
			// Check whitespace before and after
			if i > 0 && isWhitespace(content, i-1) && isWhitespace(content, i+2) {
				return i
			}
		}
	}
	return -1
}

// findEIOperator finds the EI operator after binary image data
// EI must be followed by whitespace (or end of content)
// Note: The byte before EI may be null (end of binary data) or whitespace
func findEIOperator(content []byte, start int) int {
	// EI detection is tricky because binary data can contain "EI"
	// We look for EI that is followed by whitespace or end of content
	// Per PDF spec, EI should be preceded by whitespace, but in practice
	// the binary data may end with any byte including null
	for i := start; i < len(content)-1; i++ {
		if content[i] == 'E' && content[i+1] == 'I' {
			// Must be followed by whitespace or end of content
			if i+2 >= len(content) || isWhitespace(content, i+2) {
				// Additional check: the byte before should not be alphanumeric
				// (to avoid matching "...xEI " in binary data where x is a letter)
				if i > 0 {
					prev := content[i-1]
					// Accept whitespace, null bytes, or non-alphanumeric bytes
					isAlphaNum := (prev >= 'A' && prev <= 'Z') || (prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9')
					if !isAlphaNum {
						return i
					}
				} else {
					return i
				}
			}
		}
	}
	return -1
}

// restoreInlineImages replaces placeholders with original inline image data
func restoreInlineImages(content []byte, images []inlineImageInfo) []byte {
	result := content
	for _, img := range images {
		placeholder := fmt.Sprintf(inlineImagePlaceholder, img.index)
		result = bytes.Replace(result, []byte(placeholder), img.original, 1)
	}
	return result
}

// MaskStreamHybrid applies text masking using a hybrid approach:
// - Uses pdfcpu to extract font ToUnicode CMaps
// - Uses benoitkugler/pdf for document structure and writing
// This preserves PDF 1.5+ features (object streams, xref streams) correctly.
func MaskStreamHybrid(ctx *pdfcpuModel.Context, data []byte, targets []string, maskWith string) ([]byte, map[string]int, error) {
	applied := make(map[string]int, len(targets))

	// Build replacements map and initialize applied with all targets
	replacements := make(map[string]string, len(targets))
	for _, target := range targets {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		mask := maskWith
		if mask == "" {
			mask = strings.Repeat(DefaultMaskChar, len([]rune(target)))
		}
		replacements[target] = mask
		applied[target] = 0 // Initialize all targets with 0
	}

	if len(replacements) == 0 {
		return data, applied, nil
	}

	// Step 1: Extract font info from the already-parsed pdfcpu context.
	fonts, err := extractFontInfoFromCtx(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("extract font info: %w", err)
	}

	// Step 2: Read document with benoitkugler/pdf
	doc, _, err := reader.ParsePDFReader(bytes.NewReader(data), reader.Options{})
	if err != nil {
		return nil, nil, fmt.Errorf("parse pdf: %w", err)
	}

	// Step 3: Process each page
	pages := doc.Catalog.Pages.Flatten()
	for pageIdx, page := range pages {
		if len(page.Contents) == 0 {
			continue
		}

		for contentIdx, content := range page.Contents {
			decoded, err := content.Decode()
			if err != nil {
				continue
			}

			// Extract inline images before tokenizing (they contain binary data that breaks tokenizer)
			cleanedContent, inlineImages := extractInlineImages(decoded)

			tokens, err := tokenizer.Tokenize(cleanedContent)
			if err != nil {
				continue
			}

			modified, counts := processTokensHybrid(tokens, fonts, replacements)
			if modified || len(inlineImages) > 0 {
				// Merge counts
				for target, count := range counts {
					applied[target] += count
				}

				// Serialize tokens
				newContent := serializeTokens(tokens)

				// Restore inline images
				if len(inlineImages) > 0 {
					newContent = restoreInlineImages(newContent, inlineImages)
				}

				// Update content stream
				pages[pageIdx].Contents[contentIdx] = benoitModel.ContentStream{
					Stream: benoitModel.Stream{Content: newContent},
				}
			}
		}
	}

	// Step 4: Write using benoitkugler/pdf
	var buf bytes.Buffer
	if err := doc.Write(&buf, nil); err != nil {
		return nil, nil, fmt.Errorf("write pdf: %w", err)
	}

	return buf.Bytes(), applied, nil
}

// extractFontInfoFromCtx extracts ToUnicode CMaps from an already-parsed pdfcpu
// context. The context is reused from the object-stream detection step so the PDF
// is not parsed (and flate-decompressed) a second time.
func extractFontInfoFromCtx(ctx *pdfcpuModel.Context) (map[string]*hybridFontInfo, error) {
	// Dereference lazy objects
	for i, entry := range ctx.Table {
		if entry == nil || entry.Object == nil {
			continue
		}
		if _, ok := entry.Object.(types.LazyObjectStreamObject); ok {
			if _, err := ctx.Dereference(types.IndirectRef{
				ObjectNumber:     types.Integer(i),
				GenerationNumber: types.Integer(0),
			}); err != nil {
				continue
			}
		}
	}

	fonts := make(map[string]*hybridFontInfo)

	// Find page dicts and extract font info
	for _, entry := range ctx.Table {
		if entry == nil || entry.Object == nil {
			continue
		}
		dict, ok := entry.Object.(types.Dict)
		if !ok {
			continue
		}
		t := dict.Type()
		if t == nil || *t != "Page" {
			continue
		}

		resDict, _ := ctx.DereferenceDict(dict["Resources"])
		if resDict == nil {
			continue
		}
		fontDict, _ := ctx.DereferenceDict(resDict["Font"])
		if fontDict == nil {
			continue
		}

		for name, fobj := range fontDict {
			fontName := "/" + name
			if _, exists := fonts[fontName]; exists {
				continue
			}

			fd, _ := ctx.DereferenceDict(fobj)
			if fd == nil {
				continue
			}

			info := &hybridFontInfo{
				lookup:  make(map[benoitModel.CID][]rune),
				reverse: make(map[rune]benoitModel.CID),
			}

			// Check if CID font
			if subtype := fd.NameEntry("Subtype"); subtype != nil && *subtype == "Type0" {
				info.isCID = true
			}

			// Extract ToUnicode CMap
			if toUnicodeRef := fd["ToUnicode"]; toUnicodeRef != nil {
				sd, _, _ := ctx.DereferenceStreamDict(toUnicodeRef)
				if sd != nil {
					if err := sd.Decode(); err != nil {
						continue
					}
					cmap, err := cmaps.ParseUnicodeCMap(sd.Content)
					if err == nil {
						info.lookup = cmap.ProperLookupTable()
						for cid, runes := range info.lookup {
							if len(runes) > 0 {
								if _, exists := info.reverse[runes[0]]; !exists {
									info.reverse[runes[0]] = cid
								}
							}
						}
						info.fallback = findFallbackCIDHybrid(info.reverse)
						fonts[fontName] = info
					}
				}
			}
		}
	}

	return fonts, nil
}

// findFallbackCIDHybrid finds a suitable fallback character for masking
func findFallbackCIDHybrid(reverse map[rune]benoitModel.CID) benoitModel.CID {
	// Preferred fallback characters
	preferred := []rune{'X', 'x', '*', '#', '-', '_', '.', '0', 'o', 'n', 'a', 'e'}
	for _, r := range preferred {
		if cid, ok := reverse[r]; ok {
			return cid
		}
	}
	// Use any available character
	for _, cid := range reverse {
		return cid
	}
	return 0
}

// processTokensHybrid processes tokens and applies replacements
func processTokensHybrid(tokens []tokenizer.Token, fonts map[string]*hybridFontInfo, replacements map[string]string) (bool, map[string]int) {
	counts := make(map[string]int)
	modified := false

	var currentFontName string

	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if tok.Kind != tokenizer.Other {
			continue
		}

		op := string(tok.Value)

		// Track font changes
		if op == "Tf" && i >= 2 {
			if tokens[i-2].Kind == tokenizer.Name {
				currentFontName = "/" + string(tokens[i-2].Value)
			}
		}

		// Handle TJ operator
		if op == "TJ" {
			fontInfo := fonts[currentFontName] // May be nil for simple fonts

			// Find array bounds
			end := i - 1
			if tokens[end].Kind != tokenizer.EndArray {
				continue
			}
			start := findTJArrayStart(tokens, end)
			if start < 0 {
				continue
			}

			// Collect string tokens info: index, decoded text, slot count
			type stringToken struct {
				text      string
				idx       int
				charCount int // number of decoded characters (for redistribution)
				slotCount int // number of byte slots (CID pairs or bytes, for encoding)
			}
			var stringTokens []stringToken
			var fullText strings.Builder

			for idx := start + 1; idx < end; idx++ {
				if tokens[idx].Kind == tokenizer.String || tokens[idx].Kind == tokenizer.StringHex {
					data := tokens[idx].Value
					var decoded strings.Builder
					var slotCount int

					if fontInfo != nil && fontInfo.isCID {
						// CID font: each slot is 2 bytes
						slotCount = len(data) / 2
						for j := 0; j+1 < len(data); j += 2 {
							cid := benoitModel.CID(uint16(data[j])<<8 | uint16(data[j+1]))
							if runes, ok := fontInfo.lookup[cid]; ok && len(runes) > 0 {
								decoded.WriteRune(runes[0])
							}
							// Note: we don't write anything for unmapped CIDs,
							// but we still count the slot
						}
					} else {
						// Simple font: each slot is 1 byte
						slotCount = len(data)
						decoded.Write(data)
					}

					text := decoded.String()
					charCount := len([]rune(text))
					stringTokens = append(stringTokens, stringToken{
						idx:       idx,
						text:      text,
						charCount: charCount,
						slotCount: slotCount,
					})
					fullText.WriteString(text)
				}
			}

			text := fullText.String()
			newText := text
			for search, replace := range replacements {
				if strings.Contains(newText, search) {
					count := strings.Count(newText, search)
					newText = strings.ReplaceAll(newText, search, replace)
					counts[search] += count
				}
			}

			if newText != text {
				modified = true

				// Redistribute the new text back to original tokens preserving structure
				// Use charCount for redistribution (how many chars originally decoded),
				// then use slotCount for encoding (to maintain same byte structure)
				newRunes := []rune(newText)
				runeIdx := 0

				for _, st := range stringTokens {
					// Take the same number of CHARACTERS as original token decoded to
					endIdx := runeIdx + st.charCount
					if endIdx > len(newRunes) {
						endIdx = len(newRunes)
					}

					var tokenText string
					if runeIdx < len(newRunes) {
						tokenText = string(newRunes[runeIdx:endIdx])
					}
					runeIdx = endIdx

					// Encode the text for this token, preserving original byte structure
					var newBytes []byte
					if fontInfo != nil && fontInfo.isCID {
						newBytes = encodeTextHybridWithSlots(tokenText, fontInfo, st.slotCount)
						tokens[st.idx].Kind = tokenizer.StringHex
					} else {
						// For simple fonts, pad or truncate to match slot count
						newBytes = make([]byte, st.slotCount)
						copy(newBytes, []byte(tokenText))
						tokens[st.idx].Kind = tokenizer.String
					}
					tokens[st.idx].Value = newBytes
				}
			}
		}

		// Handle Tj operator
		if (op == "Tj" || op == "'" || op == "\"") && i > 0 {
			fontInfo := fonts[currentFontName] // May be nil for simple fonts

			prevIdx := i - 1
			if tokens[prevIdx].Kind != tokenizer.String && tokens[prevIdx].Kind != tokenizer.StringHex {
				continue
			}

			// Decode text
			data := tokens[prevIdx].Value
			var origText string
			if fontInfo != nil && fontInfo.isCID {
				// Decode CID font
				var text strings.Builder
				for j := 0; j+1 < len(data); j += 2 {
					cid := benoitModel.CID(uint16(data[j])<<8 | uint16(data[j+1]))
					if runes, ok := fontInfo.lookup[cid]; ok && len(runes) > 0 {
						text.WriteRune(runes[0])
					}
				}
				origText = text.String()
			} else {
				// Simple font or no font info - treat bytes as ASCII
				origText = string(data)
			}
			newText := origText
			for search, replace := range replacements {
				if strings.Contains(newText, search) {
					count := strings.Count(newText, search)
					newText = strings.ReplaceAll(newText, search, replace)
					counts[search] += count
				}
			}

			if newText != origText {
				modified = true
				var newBytes []byte
				if fontInfo != nil && fontInfo.isCID {
					newBytes = encodeTextHybrid(newText, fontInfo)
					tokens[prevIdx].Kind = tokenizer.StringHex
				} else {
					// Simple font - just use ASCII bytes
					newBytes = []byte(newText)
					tokens[prevIdx].Kind = tokenizer.String
				}
				tokens[prevIdx].Value = newBytes
			}
		}
	}

	return modified, counts
}

// encodeTextHybrid encodes text using font mapping with fallback for missing chars
func encodeTextHybrid(text string, info *hybridFontInfo) []byte {
	var out []byte
	for _, r := range text {
		cid, ok := info.reverse[r]
		if !ok {
			cid = info.fallback
		}
		if cid == 0 {
			continue
		}
		if info.isCID {
			out = append(out, byte(cid>>8), byte(cid&0xFF))
		} else {
			out = append(out, byte(cid))
		}
	}
	return out
}

// encodeTextHybridWithSlots encodes text to exactly slotCount CID pairs (slotCount*2 bytes)
// Pads with fallback character or truncates if needed to maintain byte structure
func encodeTextHybridWithSlots(text string, info *hybridFontInfo, slotCount int) []byte {
	runes := []rune(text)
	out := make([]byte, slotCount*2) // Pre-allocate exact size for CID font

	for i := 0; i < slotCount; i++ {
		var cid benoitModel.CID
		if i < len(runes) {
			r := runes[i]
			var ok bool
			cid, ok = info.reverse[r]
			if !ok {
				cid = info.fallback
			}
		} else {
			// Pad with fallback character
			cid = info.fallback
		}

		if cid == 0 {
			cid = info.fallback
		}

		out[i*2] = byte(cid >> 8)
		out[i*2+1] = byte(cid & 0xFF)
	}

	return out
}

// hasObjectStreams checks if PDF uses object streams (PDF 1.5+ feature)
// PDFs with object streams have LazyObjectStreamObject entries that haven't been dereferenced yet
func hasObjectStreams(ctx *pdfcpuModel.Context) bool {
	for _, entry := range ctx.Table {
		if entry == nil || entry.Object == nil {
			continue
		}
		// LazyObjectStreamObject indicates an object that came from an object stream
		if _, ok := entry.Object.(types.LazyObjectStreamObject); ok {
			return true
		}
	}
	return false
}

// MaskStreamWithFallback applies masking using the hybrid approach for PDFs with object streams,
// and falls back to pdfcpu for simpler PDFs.
func MaskStreamWithFallback(data []byte, targets []string, maskWith string, stopOnErrors bool) (io.ReadSeeker, map[string]int, error) {
	// Try to detect if this PDF has object streams
	conf := pdfcpuModel.NewDefaultConfiguration()
	conf.ValidationMode = pdfcpuModel.ValidationRelaxed
	ctx, err := api.ReadContext(bytes.NewReader(data), conf)
	if err != nil {
		return nil, nil, fmt.Errorf("read pdf context: %w", err)
	}

	useHybrid := hasObjectStreams(ctx)

	if useHybrid {
		// The hybrid tokenizer path matches strings exactly, so it still relies on
		// expanding each target into case variations. Expand here, run, then fold
		// the per-variation counts back onto the original targets.
		variationToOriginal := make(map[string]string)
		var expanded []string
		for _, rawTarget := range targets {
			target := strings.TrimSpace(rawTarget)
			if target == "" {
				continue
			}
			for _, v := range generateCaseVariations(target) {
				if _, ok := variationToOriginal[v]; !ok {
					variationToOriginal[v] = target
					expanded = append(expanded, v)
				}
			}
		}

		result, appliedVar, err := MaskStreamHybrid(ctx, data, expanded, maskWith)
		if err != nil {
			return nil, nil, err
		}

		applied := make(map[string]int, len(targets))
		for _, rawTarget := range targets {
			if target := strings.TrimSpace(rawTarget); target != "" {
				applied[target] = 0
			}
		}
		for variation, count := range appliedVar {
			if original, ok := variationToOriginal[variation]; ok {
				applied[original] += count
			}
		}
		return bytes.NewReader(result), applied, nil
	}

	// Use original pdfcpu approach for simpler PDFs. The fallback path matches
	// case-insensitively, so no case-variation expansion is needed: all targets
	// are applied to each content stream in a single decode/tokenize/encode pass.
	if err := api.ValidateContext(ctx); err != nil {
		return nil, nil, fmt.Errorf("validate pdf: %w", err)
	}

	reps := make([]replacement, 0, len(targets))
	order := make([]string, 0, len(targets))
	for _, rawTarget := range targets {
		target := strings.TrimSpace(rawTarget)
		if target == "" {
			continue
		}
		maskValue := maskWith
		if maskValue == "" {
			maskValue = strings.Repeat(DefaultMaskChar, len([]rune(target)))
		}
		reps = append(reps, replacement{search: target, replace: maskValue})
		order = append(order, target)
	}

	cache := newFontCache()
	counts := make([]int, len(reps))
	if _, err := replaceTextInContext(ctx, cache, reps, counts, stopOnErrors, nil); err != nil {
		return nil, nil, fmt.Errorf("mask: %w", err)
	}

	applied := make(map[string]int, len(reps))
	for i, target := range order {
		applied[target] = counts[i]
	}

	buf := bytes.NewBuffer(nil)
	if err := api.WriteContext(ctx, buf); err != nil {
		return nil, nil, fmt.Errorf("write pdf: %w", err)
	}

	return bytes.NewReader(buf.Bytes()), applied, nil
}
