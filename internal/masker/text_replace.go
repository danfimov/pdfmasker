package masker

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"unicode/utf16"

	"github.com/benoitkugler/pdf/fonts/cmaps"
	benoitModel "github.com/benoitkugler/pdf/model"
	tokenizer "github.com/benoitkugler/pstokenizer"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	pdfcpuModel "github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// ReplaceOptions describes how text replacement should be executed.
type ReplaceOptions struct {
	InputPath    string
	OutputPath   string
	SearchText   string
	ReplaceText  string
	StopOnErrors bool
}

// ReplaceText runs a pdfcpu powered find/replace flow and returns the number of replacements made.
func ReplaceText(opts ReplaceOptions) (int, error) {
	if err := opts.validate(); err != nil {
		return 0, err
	}

	conf := pdfcpuModel.NewDefaultConfiguration()
	conf.ValidationMode = pdfcpuModel.ValidationRelaxed

	f, err := os.Open(opts.InputPath)
	if err != nil {
		return 0, fmt.Errorf("open pdf: %w", err)
	}
	defer func() { _ = f.Close() }()

	ctx, err := api.ReadContext(f, conf)
	if err != nil {
		return 0, fmt.Errorf("read pdf: %w", err)
	}

	cache := newFontCache()
	reps := []replacement{{search: opts.SearchText, replace: opts.ReplaceText}}
	counts := make([]int, len(reps))
	totalReplacements, err := replaceTextInContext(ctx, cache, reps, counts, opts.StopOnErrors, func(page int, err error) {
		log.Printf("⚠️  skipping page %d due to error: %v", page, err)
	})
	if err != nil {
		return totalReplacements, err
	}

	if totalReplacements == 0 {
		log.Printf("ℹ️  no occurrences of %q were replaced", opts.SearchText)
	} else {
		log.Printf("✅ replaced %d occurrences of %q", totalReplacements, opts.SearchText)
	}

	if err := api.WriteContextFile(ctx, opts.OutputPath); err != nil {
		return totalReplacements, fmt.Errorf("write pdf: %w", err)
	}

	return totalReplacements, nil
}

// replacement pairs a search string with the value it should be masked with.
// A batch of replacements is applied to each content stream in a single decode/
// tokenize/encode pass, instead of re-parsing every stream once per target.
type replacement struct {
	search  string
	replace string
}

// replaceTextInContext applies all reps to every page. counts must be the same
// length as reps; counts[i] accumulates the number of matches for reps[i].
func replaceTextInContext(ctx *pdfcpuModel.Context, cache *fontCache, reps []replacement, counts []int, stopOnErrors bool, onSkip func(page int, err error)) (int, error) {
	totalReplacements := 0

	for page := 1; page <= ctx.PageCount; page++ {
		pageDict, _, inherited, err := ctx.PageDict(page, true)
		if err != nil {
			return totalReplacements, fmt.Errorf("page %d: %w", page, err)
		}

		contentObj, found := pageDict.Find("Contents")
		if !found || contentObj == nil {
			continue
		}

		resDict := extractResourceDict(ctx, pageDict, inherited)
		fonts, err := collectFonts(ctx, cache, resDict)
		if err != nil {
			return totalReplacements, fmt.Errorf("page %d: load fonts: %w", page, err)
		}

		replaced, err := replaceInObject(ctx, contentObj, fonts, reps, counts)
		if err != nil {
			if stopOnErrors {
				return totalReplacements, fmt.Errorf("page %d: %w", page, err)
			}
			if onSkip != nil {
				onSkip(page, err)
			}
			continue
		}

		totalReplacements += replaced
	}

	return totalReplacements, nil
}

func (o ReplaceOptions) validate() error {
	if o.InputPath == "" {
		return errors.New("input path is required")
	}
	if o.OutputPath == "" {
		return errors.New("output path is required")
	}
	if strings.TrimSpace(o.SearchText) == "" {
		return errors.New("search text must not be empty")
	}
	return nil
}

func replaceInObject(ctx *pdfcpuModel.Context, obj types.Object, fonts fontMap, reps []replacement, counts []int) (int, error) {
	switch o := obj.(type) {
	case types.IndirectRef:
		return replaceInIndirectObject(ctx, o, fonts, reps, counts)
	case types.StreamDict:
		sd := o
		count, err := replaceInStreamDict(ctx, &sd, fonts, reps, counts)
		return count, err
	case types.Array:
		return replaceInArray(ctx, o, fonts, reps, counts)
	default:
		return 0, nil
	}
}

func replaceInIndirectObject(ctx *pdfcpuModel.Context, ref types.IndirectRef, fonts fontMap, reps []replacement, counts []int) (int, error) {
	entry, found := ctx.FindTableEntry(ref.ObjectNumber.Value(), ref.GenerationNumber.Value())
	if !found || entry == nil || entry.Object == nil {
		return 0, nil
	}

	switch obj := entry.Object.(type) {
	case types.StreamDict:
		sd := obj
		count, err := replaceInStreamDict(ctx, &sd, fonts, reps, counts)
		if err != nil || count == 0 {
			return count, err
		}
		entry.Object = sd
		return count, nil

	case types.Array:
		count, err := replaceInArray(ctx, obj, fonts, reps, counts)
		if err != nil || count == 0 {
			return count, err
		}
		entry.Object = obj
		return count, nil

	default:
		return 0, nil
	}
}

func replaceInArray(ctx *pdfcpuModel.Context, arr types.Array, fonts fontMap, reps []replacement, counts []int) (int, error) {
	total := 0
	for idx, item := range arr {
		switch v := item.(type) {
		case types.IndirectRef:
			count, err := replaceInIndirectObject(ctx, v, fonts, reps, counts)
			if err != nil {
				return total, err
			}
			total += count
		case types.StreamDict:
			sd := v
			count, err := replaceInStreamDict(ctx, &sd, fonts, reps, counts)
			if err != nil {
				return total, err
			}
			if count > 0 {
				arr[idx] = sd
				total += count
			}
		case types.Array:
			count, err := replaceInArray(ctx, v, fonts, reps, counts)
			if err != nil {
				return total, err
			}
			total += count
		}
	}
	return total, nil
}

func replaceInStreamDict(ctx *pdfcpuModel.Context, sd *types.StreamDict, fonts fontMap, reps []replacement, counts []int) (int, error) {
	if sd == nil {
		return 0, nil
	}
	if err := sd.Decode(); err != nil {
		return 0, fmt.Errorf("decode stream: %w", err)
	}

	tokens, err := tokenizer.Tokenize(sd.Content)
	if err != nil {
		return 0, fmt.Errorf("tokenize stream: %w", err)
	}

	// Apply every target to the same token slice. Each pattern mutates the tokens
	// in place, so later patterns see the already-masked text — identical semantics
	// to the previous "one full pass per target" approach, but the stream is
	// decoded, tokenized and (re-)encoded only once instead of once per target.
	total := 0
	for i := range reps {
		n, err := applyReplacement(ctx, tokens, fonts, reps[i].search, reps[i].replace)
		if err != nil {
			return total, err
		}
		counts[i] += n
		total += n
	}
	if total == 0 {
		return 0, nil
	}

	newContent := serializeTokens(tokens)
	sd.Content = newContent
	sd.Raw = nil

	if err := sd.Encode(); err != nil {
		return total, fmt.Errorf("encode stream: %w", err)
	}

	return total, nil
}

func applyReplacement(ctx *pdfcpuModel.Context, tokens []tokenizer.Token, fonts fontMap, search, replace string) (int, error) {
	if len(tokens) == 0 {
		return 0, nil
	}

	// First try: use text reconstruction to handle character-by-character PDFs
	total, err := applyReplacementWithReconstruction(ctx, tokens, fonts, search, replace)
	if err == nil && total > 0 {
		return total, nil
	}

	// Fallback to original token-by-token approach
	var currentFont *fontResource

	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if tok.Kind != tokenizer.Other {
			continue
		}

		op := string(tok.Value)
		switch op {
		case "Tf":
			if i >= 2 {
				if tokens[i-2].Kind == tokenizer.Name {
					fontName := "/" + string(tokens[i-2].Value)
					currentFont = fonts[fontName]
				}
			}

		case "Tj", "'", "\"":
			if i == 0 || currentFont == nil {
				continue
			}
			prev := &tokens[i-1]
			if prev.Kind == tokenizer.String || prev.Kind == tokenizer.StringHex {
				cnt, err := replaceInTextToken(ctx, prev, currentFont, search, replace)
				if err != nil {
					return total, err
				}
				total += cnt
			}

		case "TJ":
			cnt, err := processArrayForTJ(ctx, tokens, i, currentFont, search, replace)
			if err != nil {
				return total, err
			}
			total += cnt
		}
	}

	return total, nil
}

// applyReplacementWithReconstruction handles PDFs where text is split character-by-character
func applyReplacementWithReconstruction(ctx *pdfcpuModel.Context, tokens []tokenizer.Token, fonts fontMap, search, replace string) (int, error) {
	if len(tokens) == 0 {
		return 0, nil
	}

	// Build segments of consecutive text operations
	type textSegment struct {
		font    *fontResource
		indices []int // indices into the tokens array
	}

	var segments []textSegment
	var currentFont *fontResource
	var currentSegment *textSegment

	for i := 0; i < len(tokens); i++ {
		tok := &tokens[i]
		if tok.Kind != tokenizer.Other {
			continue
		}

		op := string(tok.Value)
		switch op {
		case "Tf":
			if i >= 2 {
				if tokens[i-2].Kind == tokenizer.Name {
					fontName := "/" + string(tokens[i-2].Value)
					currentFont = fonts[fontName]
				}
			}
			// Font change ends current segment
			if currentSegment != nil && len(currentSegment.indices) > 0 {
				segments = append(segments, *currentSegment)
				currentSegment = nil
			}

		case "Tj", "'", "\"":
			if i == 0 || currentFont == nil {
				continue
			}
			prevIdx := i - 1
			if tokens[prevIdx].Kind == tokenizer.String || tokens[prevIdx].Kind == tokenizer.StringHex {
				if currentSegment == nil {
					currentSegment = &textSegment{
						indices: make([]int, 0),
						font:    currentFont,
					}
				}
				currentSegment.indices = append(currentSegment.indices, prevIdx)
			}

		case "TJ":
			// TJ operator: extract strings from the array
			if currentFont == nil {
				continue
			}

			// Find the array before this TJ operator
			if i > 0 && tokens[i-1].Kind == tokenizer.EndArray {
				end := i - 1
				start := findTJArrayStart(tokens, end)

				if start >= 0 {
					// Collect string tokens from the array
					if currentSegment == nil {
						currentSegment = &textSegment{
							indices: make([]int, 0),
							font:    currentFont,
						}
					}
					for idx := start + 1; idx < end; idx++ {
						if tokens[idx].Kind == tokenizer.String || tokens[idx].Kind == tokenizer.StringHex {
							currentSegment.indices = append(currentSegment.indices, idx)
						}
					}
				}
			}

			// TJ ends the current segment
			if currentSegment != nil && len(currentSegment.indices) > 0 {
				segments = append(segments, *currentSegment)
				currentSegment = nil
			}

		case "BT", "ET":
			// Text block boundaries
			if currentSegment != nil && len(currentSegment.indices) > 0 {
				segments = append(segments, *currentSegment)
				currentSegment = nil
			}
		}
	}

	// Add final segment
	if currentSegment != nil && len(currentSegment.indices) > 0 {
		segments = append(segments, *currentSegment)
	}

	// Now process each segment
	total := 0
	for _, seg := range segments {
		// Reconstruct text from segment
		var reconstructed strings.Builder
		tokenInfos := make([]decodedText, len(seg.indices))

		for idx, tokenIdx := range seg.indices {
			info, err := decodeText(ctx, tokens[tokenIdx].Value, seg.font)
			if err != nil {
				continue
			}
			tokenInfos[idx] = info
			reconstructed.WriteString(info.text)
		}

		fullText := reconstructed.String()
		if !containsFold(fullText, search) {
			continue
		}

		// Found a match! Now we need to replace it across the tokens
		cnt, err := replaceAcrossTokensInPlace(ctx, tokens, seg.indices, tokenInfos, seg.font, fullText, search, replace)
		if err != nil {
			return total, err
		}
		total += cnt
	}

	return total, nil
}

// replaceAcrossTokensInPlace replaces search string that spans across multiple character tokens
func replaceAcrossTokensInPlace(ctx *pdfcpuModel.Context, tokens []tokenizer.Token, indices []int, infos []decodedText, font *fontResource, fullText, search, replace string) (int, error) {
	// Find all occurrences of search in fullText (case-insensitive)
	count := countFold(fullText, search)
	if count == 0 {
		return 0, nil
	}

	// Build new full text with replacements (case-insensitive)
	newFullText := replaceAllFold(fullText, search, replace)

	// Now redistribute the new text across the tokens
	// Strategy: only re-encode tokens where the character actually changed
	// This preserves original CIDs for unchanged characters (avoiding reverse mapping issues)
	newTextRunes := []rune(newFullText)
	origTextRunes := []rune(fullText)
	runeIdx := 0

	for i, idx := range indices {
		if i >= len(infos) {
			break
		}

		// Get the original character count for this token
		origCharCount := len([]rune(infos[i].text))
		origByteCount := len(tokens[idx].Value)

		// If this token originally had no decoded characters, preserve original bytes
		// (unmapped CIDs should not be replaced with fallback characters)
		if origCharCount == 0 {
			continue
		}

		// Get the range of characters for this token
		endIdx := runeIdx + origCharCount
		if endIdx > len(newTextRunes) {
			endIdx = len(newTextRunes)
		}
		if endIdx > len(origTextRunes) {
			endIdx = len(origTextRunes)
		}

		// Check if characters actually changed
		origSlice := origTextRunes[runeIdx:endIdx]
		newSlice := newTextRunes[runeIdx:endIdx]
		changed := false
		if len(origSlice) != len(newSlice) {
			changed = true
		} else {
			for j := 0; j < len(origSlice); j++ {
				if origSlice[j] != newSlice[j] {
					changed = true
					break
				}
			}
		}

		runeIdx = endIdx

		// Only re-encode if characters changed
		if !changed {
			continue
		}

		tokenText := string(newSlice)

		// Encode the text, preserving original byte structure for CID fonts
		var newBytes []byte
		var err error

		switch {
		case font != nil && font.mapping != nil && font.isCIDFont:
			// For CID fonts, maintain the original byte count (slot count)
			newBytes = encodeWithMappingSlots(tokenText, font.mapping, origByteCount)
		case font != nil && font.mapping != nil:
			newBytes = encodeWithMapping(tokenText, font.mapping, font.isCIDFont)
		default:
			newBytes, err = encodeText(font, tokenText, infos[i])
			if err != nil {
				continue
			}
		}

		tokens[idx].Kind = tokenizer.StringHex
		tokens[idx].Value = newBytes
	}

	return count, nil
}

// findTJArrayStart returns the index of the array-open token ("[") matching the
// array-close token at endIdx, scanning backward and honoring nested arrays.
// endIdx must reference an EndArray token. Returns -1 if no matching open is found.
func findTJArrayStart(tokens []tokenizer.Token, endIdx int) int {
	depth := 0
	for j := endIdx; j >= 0; j-- {
		switch tokens[j].Kind {
		case tokenizer.EndArray:
			depth++
		case tokenizer.StartArray:
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return -1
}

func processArrayForTJ(ctx *pdfcpuModel.Context, tokens []tokenizer.Token, opIndex int, font *fontResource, search, replace string) (int, error) {
	if opIndex == 0 || tokens[opIndex-1].Kind != tokenizer.EndArray {
		return 0, nil
	}

	end := opIndex - 1
	start := findTJArrayStart(tokens, end)
	if start < 0 {
		return 0, nil
	}

	total := 0
	for idx := start + 1; idx < end; idx++ {
		if tokens[idx].Kind == tokenizer.String || tokens[idx].Kind == tokenizer.StringHex {
			cnt, err := replaceInTextToken(ctx, &tokens[idx], font, search, replace)
			if err != nil {
				return total, err
			}
			total += cnt
		}
	}
	return total, nil
}

type textEncoding int

const (
	encodingRaw textEncoding = iota
	encodingUTF16BE
	encodingUTF16LE
)

type decodedText struct {
	text     string
	encoding textEncoding
	viaFont  bool
}

func replaceInTextToken(ctx *pdfcpuModel.Context, tok *tokenizer.Token, font *fontResource, search, replace string) (int, error) {
	info, err := decodeText(ctx, tok.Value, font)
	if err != nil {
		return 0, err
	}
	if info.text == "" || !containsFold(info.text, search) {
		return 0, nil
	}

	count := countFold(info.text, search)
	if count == 0 {
		return 0, nil
	}

	updated := replaceAllFold(info.text, search, replace)
	if updated == info.text {
		return 0, nil
	}

	newBytes, err := encodeText(font, updated, info)
	if err != nil {
		return 0, err
	}

	tok.Kind = tokenizer.StringHex
	tok.Value = newBytes

	return count, nil
}

func decodeText(ctx *pdfcpuModel.Context, data []byte, font *fontResource) (decodedText, error) {
	if font != nil {
		if err := font.ensureMapping(ctx); err != nil {
			return decodedText{}, err
		}
		if font.mapping != nil {
			txt := decodeWithMapping(data, font.mapping, font.isCIDFont)
			return decodedText{text: txt, viaFont: true}, nil
		}
	}

	text, enc := decodePDFText(data)
	return decodedText{text: text, encoding: enc}, nil
}

func encodeText(font *fontResource, text string, info decodedText) ([]byte, error) {
	if info.viaFont && font != nil && font.mapping != nil {
		return encodeWithMapping(text, font.mapping, font.isCIDFont), nil
	}
	return encodePDFText(text, info.encoding), nil
}

func decodePDFText(data []byte) (string, textEncoding) {
	if len(data) >= 2 {
		if data[0] == 0xFE && data[1] == 0xFF {
			return utf16BytesToString(data[2:], binary.BigEndian), encodingUTF16BE
		}
		if data[0] == 0xFF && data[1] == 0xFE {
			return utf16BytesToString(data[2:], binary.LittleEndian), encodingUTF16LE
		}
	}
	return string(data), encodingRaw
}

func encodePDFText(text string, enc textEncoding) []byte {
	switch enc {
	case encodingUTF16BE:
		return prependBOM(utf16Encode(text), binary.BigEndian, true)
	case encodingUTF16LE:
		return prependBOM(utf16Encode(text), binary.LittleEndian, false)
	default:
		return []byte(text)
	}
}

func utf16Encode(text string) []uint16 {
	return utf16.Encode([]rune(text))
}

func prependBOM(data []uint16, order binary.ByteOrder, bigEndian bool) []byte {
	out := make([]byte, 0, 2+len(data)*2)
	if bigEndian {
		out = append(out, 0xFE, 0xFF)
	} else {
		out = append(out, 0xFF, 0xFE)
	}
	var tmp [2]byte
	for _, v := range data {
		order.PutUint16(tmp[:], v)
		out = append(out, tmp[0], tmp[1])
	}
	return out
}

func utf16BytesToString(data []byte, order binary.ByteOrder) string {
	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}
	codeUnits := make([]uint16, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		codeUnits[i/2] = order.Uint16(data[i : i+2])
	}
	return string(utf16.Decode(codeUnits))
}

func serializeTokens(tokens []tokenizer.Token) []byte {
	// Estimate output size to avoid repeated buffer growth. Hex strings roughly
	// double their value length, so add a per-token allowance on top.
	size := 0
	for i := range tokens {
		size += len(tokens[i].Value)*2 + 2
	}
	var buf bytes.Buffer
	buf.Grow(size)
	for _, tok := range tokens {
		writeToken(&buf, tok)
	}
	return buf.Bytes()
}

func writeToken(buf *bytes.Buffer, tok tokenizer.Token) {
	switch tok.Kind {
	case tokenizer.Name:
		buf.WriteByte('/')
		buf.Write(tok.Value)
		buf.WriteByte(' ')
	case tokenizer.String, tokenizer.StringHex:
		writeHexString(buf, tok.Value)
	case tokenizer.Integer, tokenizer.Float:
		buf.Write(tok.Value)
		buf.WriteByte(' ')
	case tokenizer.Other:
		buf.Write(tok.Value)
		buf.WriteByte('\n')
	case tokenizer.StartArray:
		buf.WriteByte('[')
	case tokenizer.EndArray:
		buf.WriteByte(']')
		buf.WriteByte(' ')
	case tokenizer.StartDic:
		buf.WriteString("<<")
	case tokenizer.EndDic:
		buf.WriteString(">>")
		buf.WriteByte(' ')
	}
}

const hexUpper = "0123456789ABCDEF"

func writeHexString(buf *bytes.Buffer, data []byte) {
	buf.WriteByte('<')
	for _, b := range data {
		buf.WriteByte(hexUpper[b>>4])
		buf.WriteByte(hexUpper[b&0x0F])
	}
	buf.WriteByte('>')
	buf.WriteByte(' ')
}

// --- font handling helpers ---

type fontMap map[string]*fontResource

type fontResource struct {
	dict      types.Dict
	toUnicode types.Object
	cache     *fontCache
	mapping   *fontMapping
	isCIDFont bool
}

func (fr *fontResource) ensureMapping(ctx *pdfcpuModel.Context) error {
	if fr == nil || fr.mapping != nil || fr.toUnicode == nil {
		return nil
	}
	mapping, err := fr.cache.getMapping(ctx, fr.toUnicode)
	if err != nil {
		return err
	}
	fr.mapping = mapping
	return nil
}

type fontMapping struct {
	lookup   map[benoitModel.CID][]rune
	reverse  map[rune]benoitModel.CID
	fallback benoitModel.CID
}

type fontCache struct {
	mappings map[int]*fontMapping
}

func newFontCache() *fontCache {
	return &fontCache{
		mappings: map[int]*fontMapping{},
	}
}

func (fc *fontCache) getMapping(ctx *pdfcpuModel.Context, obj types.Object) (*fontMapping, error) {
	switch v := obj.(type) {
	case types.IndirectRef:
		objNr := v.ObjectNumber.Value()
		if m, ok := fc.mappings[objNr]; ok {
			return m, nil
		}
		sd, _, err := ctx.DereferenceStreamDict(v)
		if err != nil || sd == nil {
			return nil, err
		}
		if err := sd.Decode(); err != nil {
			return nil, err
		}
		m, err := parseCMap(sd.Content)
		if err != nil {
			return nil, err
		}
		fc.mappings[objNr] = m
		return m, nil
	case types.StreamDict:
		sd := v
		if err := sd.Decode(); err != nil {
			return nil, err
		}
		return parseCMap(sd.Content)
	default:
		return nil, nil
	}
}

func parseCMap(data []byte) (*fontMapping, error) {
	cmap, err := cmaps.ParseUnicodeCMap(data)
	if err != nil {
		return nil, err
	}
	lookup := cmap.ProperLookupTable()
	reverse := map[rune]benoitModel.CID{}
	var fallback benoitModel.CID

	for cid, runes := range lookup {
		if len(runes) == 0 {
			continue
		}
		if fallback == 0 {
			fallback = cid
		}
		r := runes[0]
		if _, ok := reverse[r]; !ok {
			reverse[r] = cid
		}
	}

	return &fontMapping{
		lookup:   lookup,
		reverse:  reverse,
		fallback: fallback,
	}, nil
}

func collectFonts(ctx *pdfcpuModel.Context, cache *fontCache, resources types.Dict) (fontMap, error) {
	fonts := fontMap{}
	if resources == nil {
		return fonts, nil
	}

	fontDictObj := resources["Font"]
	if fontDictObj == nil {
		return fonts, nil
	}
	fontDict, err := ctx.DereferenceDict(fontDictObj)
	if err != nil {
		return nil, err
	}

	for name, obj := range fontDict {
		dict, err := ctx.DereferenceDict(obj)
		if err != nil || dict == nil {
			continue
		}
		fr := &fontResource{
			dict:      dict,
			toUnicode: dict["ToUnicode"],
			cache:     cache,
		}
		if subtype := dict.NameEntry("Subtype"); subtype != nil && *subtype == "Type0" {
			fr.isCIDFont = true
		}
		fonts["/"+name] = fr
	}
	return fonts, nil
}

func extractResourceDict(ctx *pdfcpuModel.Context, pageDict types.Dict, inherited *pdfcpuModel.InheritedPageAttrs) types.Dict {
	if inherited != nil && inherited.Resources != nil {
		return inherited.Resources
	}
	obj, found := pageDict.Find("Resources")
	if !found || obj == nil {
		return nil
	}
	res, err := ctx.DereferenceDict(obj)
	if err != nil {
		return nil
	}
	return res
}

func decodeWithMapping(data []byte, mapping *fontMapping, isCID bool) string {
	if mapping == nil || len(data) == 0 {
		return ""
	}
	var sb strings.Builder
	for i := 0; i < len(data); {
		var cid benoitModel.CID

		if isCID {
			// For CID-encoded data, only use 2-byte lookups
			if i+1 < len(data) {
				cid = benoitModel.CID(uint16(data[i])<<8 | uint16(data[i+1]))
				if runes, ok := mapping.lookup[cid]; ok && len(runes) > 0 {
					sb.WriteRune(runes[0])
				}
				// Always advance by 2 for CID streams
				i += 2
			} else {
				// Incomplete CID pair, skip remaining byte
				break
			}
		} else {
			// For non-CID data, try 2-byte lookup first, then fall back to 1-byte
			if i+1 < len(data) {
				cid = benoitModel.CID(uint16(data[i])<<8 | uint16(data[i+1]))
				if runes, ok := mapping.lookup[cid]; ok && len(runes) > 0 {
					sb.WriteRune(runes[0])
					i += 2
					continue
				}
			}

			// Fall back to single-byte lookup
			cid = benoitModel.CID(data[i])
			if runes, ok := mapping.lookup[cid]; ok && len(runes) > 0 {
				sb.WriteRune(runes[0])
			}
			i += 1
		}
	}
	return sb.String()
}

func encodeWithMapping(text string, mapping *fontMapping, isCID bool) []byte {
	if mapping == nil {
		return nil
	}

	// Find a good fallback character that exists in the font
	fallbackCID := findBestFallbackCID(mapping)

	var out []byte
	for _, r := range text {
		cid, ok := mapping.reverse[r]
		if !ok {
			cid = fallbackCID
			if cid == 0 {
				continue
			}
		}
		if cid > 0xFF || isCID {
			out = append(out, byte(cid>>8), byte(cid&0xFF))
		} else {
			out = append(out, byte(cid))
		}
	}
	return out
}

// encodeWithMappingSlots encodes text to exactly the specified byte count
// This preserves the original token's byte structure in CID fonts
// Pads with fallback character or returns empty if origByteCount is 0
func encodeWithMappingSlots(text string, mapping *fontMapping, origByteCount int) []byte {
	if origByteCount == 0 {
		return []byte{}
	}
	if mapping == nil {
		// Non-CID font: pad/truncate to original byte count
		out := make([]byte, origByteCount)
		copy(out, []byte(text))
		return out
	}

	// CID font: each slot is 2 bytes
	slotCount := origByteCount / 2
	if slotCount == 0 {
		return []byte{}
	}

	fallbackCID := findBestFallbackCID(mapping)
	runes := []rune(text)
	out := make([]byte, slotCount*2)

	for i := 0; i < slotCount; i++ {
		var cid benoitModel.CID
		if i < len(runes) {
			r := runes[i]
			var ok bool
			cid, ok = mapping.reverse[r]
			if !ok {
				cid = fallbackCID
			}
		} else {
			// Pad with fallback character
			cid = fallbackCID
		}

		if cid == 0 {
			cid = fallbackCID
		}

		out[i*2] = byte(cid >> 8)
		out[i*2+1] = byte(cid & 0xFF)
	}

	return out
}

// findBestFallbackCID finds a suitable fallback character for masking
// Prefers: X, *, #, - or other common masking characters
func findBestFallbackCID(mapping *fontMapping) benoitModel.CID {
	if mapping == nil {
		return 0
	}

	// Preferred fallback characters in order of preference
	preferredChars := []rune{'X', 'x', '*', '#', '-', '_', '.', '0'}

	for _, r := range preferredChars {
		if cid, ok := mapping.reverse[r]; ok {
			return cid
		}
	}

	// If none of the preferred chars exist, use the original fallback
	return mapping.fallback
}
