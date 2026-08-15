package masker

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"unicode/utf16"

	cs "github.com/benoitkugler/pdf/contentstream"
	"github.com/benoitkugler/pdf/fonts/cmaps"
	benoitModel "github.com/benoitkugler/pdf/model"
	"github.com/benoitkugler/pdf/reader/parser"
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
// A batch of replacements is applied to each content stream in a single
// parse/mask/serialize pass, instead of re-parsing every stream once per target.
//
// When maskChar is non-zero the match is replaced by that rune repeated to the
// matched length (the default masking mode, which preserves layout); otherwise the
// literal replace string is used (the explicit find/replace mode).
type replacement struct {
	search   string
	replace  string
	maskChar rune
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
		glyphFonts := toGlyphFonts(ctx, fonts)

		replaced, err := replaceInObject(ctx, contentObj, glyphFonts, reps, counts)
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

func replaceInObject(ctx *pdfcpuModel.Context, obj types.Object, fonts map[string]*glyphFont, reps []replacement, counts []int) (int, error) {
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

func replaceInIndirectObject(ctx *pdfcpuModel.Context, ref types.IndirectRef, fonts map[string]*glyphFont, reps []replacement, counts []int) (int, error) {
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

func replaceInArray(ctx *pdfcpuModel.Context, arr types.Array, fonts map[string]*glyphFont, reps []replacement, counts []int) (int, error) {
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

func replaceInStreamDict(ctx *pdfcpuModel.Context, sd *types.StreamDict, fonts map[string]*glyphFont, reps []replacement, counts []int) (int, error) {
	if sd == nil {
		return 0, nil
	}
	if err := sd.Decode(); err != nil {
		return 0, fmt.Errorf("decode stream: %w", err)
	}

	// ParseContent understands inline images (BI ... ID <binary> EI) and skips
	// their binary payload, so text after an inline image is parsed correctly —
	// the raw PostScript tokenizer used to choke on the binary and silently drop
	// every operator that followed.
	ops, err := parser.ParseContent(sd.Content, nil)
	if err != nil {
		return 0, fmt.Errorf("parse content stream: %w", err)
	}

	countMap := make(map[string]int, len(reps))
	if !maskOperations(ops, fonts, reps, countMap) {
		return 0, nil
	}

	total := 0
	for i := range reps {
		n := countMap[reps[i].search]
		counts[i] += n
		total += n
	}

	sd.Content = cs.WriteOperations(ops...)
	sd.Raw = nil

	if err := sd.Encode(); err != nil {
		return total, fmt.Errorf("encode stream: %w", err)
	}

	return total, nil
}

// toGlyphFonts resolves each collected font's ToUnicode CMap (best effort) and
// projects it onto the engine-agnostic glyphFont used by the masking core. A font
// without a ToUnicode ends up with a nil mapping, i.e. "raw bytes".
func toGlyphFonts(ctx *pdfcpuModel.Context, fonts fontMap) map[string]*glyphFont {
	out := make(map[string]*glyphFont, len(fonts))
	for name, fr := range fonts {
		if fr == nil {
			continue
		}
		_ = fr.ensureMapping(ctx)
		out[name] = &glyphFont{mapping: fr.mapping, isCID: fr.isCIDFont}
	}
	return out
}

type textEncoding int

const (
	encodingRaw textEncoding = iota
	encodingUTF16BE
	encodingUTF16LE
)

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
