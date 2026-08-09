package masker

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	cs "github.com/benoitkugler/pdf/contentstream"
	"github.com/benoitkugler/pdf/fonts/cmaps"
	benoitModel "github.com/benoitkugler/pdf/model"
	"github.com/benoitkugler/pdf/reader"
	"github.com/benoitkugler/pdf/reader/parser"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	pdfcpuModel "github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// hybridFontInfo holds font mapping info extracted from a pdfcpu context.
type hybridFontInfo struct {
	lookup   map[benoitModel.CID][]rune
	reverse  map[rune]benoitModel.CID
	fallback benoitModel.CID
	isCID    bool
}

// MaskStreamHybrid masks a PDF that uses object streams (PDF 1.5+). It extracts the
// font ToUnicode CMaps with pdfcpu (which pdfcpu reads reliably) but parses, edits
// and rewrites the document with benoitkugler/pdf, because pdfcpu does not
// round-trip object/xref streams safely. Content streams are parsed into operations
// (inline images and all) and masked by the shared maskOperations core.
func MaskStreamHybrid(ctx *pdfcpuModel.Context, data []byte, reps []replacement) ([]byte, map[string]int, error) {
	counts := make(map[string]int, len(reps))
	for _, rep := range reps {
		counts[rep.search] = 0
	}
	if len(reps) == 0 {
		return data, counts, nil
	}

	// Step 1: Extract font info from the already-parsed pdfcpu context.
	hfonts, err := extractFontInfoFromCtx(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("extract font info: %w", err)
	}
	fonts := make(map[string]*glyphFont, len(hfonts))
	for name, hi := range hfonts {
		fonts[name] = &glyphFont{
			mapping: &fontMapping{lookup: hi.lookup, reverse: hi.reverse, fallback: hi.fallback},
			isCID:   hi.isCID,
		}
	}

	// Step 2: Read the document with benoitkugler/pdf.
	doc, _, err := reader.ParsePDFReader(bytes.NewReader(data), reader.Options{})
	if err != nil {
		return nil, nil, fmt.Errorf("parse pdf: %w", err)
	}

	// Step 3: Mask each page's content streams.
	pages := doc.Catalog.Pages.Flatten()
	for pageIdx, page := range pages {
		var resCS benoitModel.ResourcesColorSpace
		if page.Resources != nil {
			resCS = page.Resources.ColorSpace
		}
		for contentIdx, content := range page.Contents {
			decoded, err := content.Decode()
			if err != nil {
				continue
			}
			ops, err := parser.ParseContent(decoded, resCS)
			if err != nil {
				continue
			}
			if maskOperations(ops, fonts, reps, counts) {
				pages[pageIdx].Contents[contentIdx] = benoitModel.ContentStream{
					Stream: benoitModel.Stream{Content: cs.WriteOperations(ops...)},
				}
			}
		}
	}

	// Step 4: Write using benoitkugler/pdf.
	var buf bytes.Buffer
	if err := doc.Write(&buf, nil); err != nil {
		return nil, nil, fmt.Errorf("write pdf: %w", err)
	}

	return buf.Bytes(), counts, nil
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

// hasObjectStreams checks if PDF uses object streams (PDF 1.5+ feature).
// PDFs with object streams have LazyObjectStreamObject entries that haven't been dereferenced yet.
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

// MaskStreamWithFallback applies masking using the hybrid approach for PDFs with
// object streams, and the pdfcpu-driven path for simpler PDFs. Both engines share
// the same content-stream masking core (maskOperations) and match case-insensitively,
// so no case-variation expansion is needed and the returned counts are keyed by the
// original targets.
func MaskStreamWithFallback(data []byte, targets []string, maskWith string, stopOnErrors bool) (io.ReadSeeker, map[string]int, error) {
	conf := pdfcpuModel.NewDefaultConfiguration()
	conf.ValidationMode = pdfcpuModel.ValidationRelaxed
	ctx, err := api.ReadContext(bytes.NewReader(data), conf)
	if err != nil {
		return nil, nil, fmt.Errorf("read pdf context: %w", err)
	}

	// Build the ordered replacement list once; both engines consume it directly.
	// An empty maskWith selects the default mask: each match is filled with
	// DefaultMaskChar sized to the matched text (handled in the masking core), which
	// preserves layout and stays correct for whitespace-flexible matches.
	reps := make([]replacement, 0, len(targets))
	for _, rawTarget := range targets {
		target := strings.TrimSpace(rawTarget)
		if target == "" {
			continue
		}
		rep := replacement{search: target}
		if maskWith == "" {
			rep.maskChar = []rune(DefaultMaskChar)[0]
		} else {
			rep.replace = maskWith
		}
		reps = append(reps, rep)
	}

	if hasObjectStreams(ctx) {
		result, applied, err := MaskStreamHybrid(ctx, data, reps)
		if err != nil {
			return nil, nil, err
		}
		return bytes.NewReader(result), applied, nil
	}

	// pdfcpu path for simpler PDFs.
	if err := api.ValidateContext(ctx); err != nil {
		return nil, nil, fmt.Errorf("validate pdf: %w", err)
	}

	cache := newFontCache()
	counts := make([]int, len(reps))
	if _, err := replaceTextInContext(ctx, cache, reps, counts, stopOnErrors, nil); err != nil {
		return nil, nil, fmt.Errorf("mask: %w", err)
	}

	applied := make(map[string]int, len(reps))
	for i, rep := range reps {
		applied[rep.search] = counts[i]
	}

	buf := bytes.NewBuffer(nil)
	if err := api.WriteContext(ctx, buf); err != nil {
		return nil, nil, fmt.Errorf("write pdf: %w", err)
	}

	return bytes.NewReader(buf.Bytes()), applied, nil
}
