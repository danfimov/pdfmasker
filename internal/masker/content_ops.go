package masker

import (
	"strings"
	"unicode"
	"unicode/utf8"

	cs "github.com/benoitkugler/pdf/contentstream"
	pdfFonts "github.com/benoitkugler/pdf/fonts"
)

// This file is the single content-stream masking core shared by both engines.
//
// It parses a decoded content stream with benoitkugler/pdf's operation parser
// (github.com/benoitkugler/pdf/reader/parser.ParseContent), which — unlike the raw
// PostScript tokenizer — understands inline images (BI ... ID <binary> EI) and
// skips their binary payload correctly. That removes the whole class of "text after
// an inline image is silently dropped" bugs and lets us delete the fragile
// extract-to-placeholder-and-splice-back workaround the old token path needed.
//
// The parser keeps the *encoded* font bytes of every show-text operator intact
// (OpShowText.Text and TextSpaced.CharCodes are raw, unescaped bytes, and TJ kerning
// is preserved), so the byte-slot preservation the maskers rely on still applies
// unchanged. Matching is Unicode case-insensitive (see fold.go) for every font.

// glyphFont maps a font's encoded bytes to/from runes. A nil *glyphFont, or one
// with a nil mapping, means the font has no ToUnicode CMap: bytes are treated as
// raw text (with UTF-16 BOM detection), matching the pre-migration behavior for
// the many paystub fonts that ship without a ToUnicode.
type glyphFont struct {
	mapping *fontMapping
	isCID   bool
}

func (f *glyphFont) decode(raw []byte) (string, textEncoding) {
	if f != nil && f.mapping != nil {
		return decodeWithMapping(raw, f.mapping, f.isCID), encodingRaw
	}
	return decodePDFText(raw)
}

// encode turns text back into font bytes. When preserveSlots is true (the text is
// the same length as the original, e.g. a default X mask) a CID operand is re-encoded
// to the original byte count so the visual layout and downstream offsets stay stable
// (see encodeWithMappingSlots). When it is false (a custom mask changed the length)
// the text is encoded at its natural length instead of being truncated to fit.
func (f *glyphFont) encode(text string, origByteCount int, enc textEncoding, preserveSlots bool) []byte {
	switch {
	case f != nil && f.mapping != nil && f.isCID:
		if preserveSlots {
			return encodeWithMappingSlots(text, f.mapping, origByteCount)
		}
		return encodeWithMapping(text, f.mapping, true)
	case f != nil && f.mapping != nil:
		return encodeWithMapping(text, f.mapping, f.isCID)
	default:
		return encodePDFText(text, enc)
	}
}

// opTextChunk is one editable run of decoded text inside a show-text operator,
// paired with a writeBack that re-encodes and stores new bytes into the ops slice.
// newOp marks the first chunk contributed by a show-text operator, i.e. an operator
// boundary within the segment (a TJ array's later elements are not boundaries).
type opTextChunk struct {
	font      *glyphFont
	text      string
	enc       textEncoding
	byteCount int
	newOp     bool
	writeBack func([]byte)
}

// maskOperations applies every replacement (in order, case-insensitively) to the
// text-showing operators in ops, mutating ops in place. counts accumulates the
// number of matches per search string. It returns whether anything changed.
//
// Text is frequently split character-by-character across many Tj/TJ operators, so
// matching works on a reconstructed segment — a run of consecutive show-text ops
// under one font, bounded by font changes and BT/ET — then the replacement is
// redistributed back, re-encoding only the chunks whose characters actually changed.
func maskOperations(ops []cs.Operation, fonts map[string]*glyphFont, reps []replacement, counts map[string]int) bool {
	var current *glyphFont
	var segment []opTextChunk
	modified := false

	flush := func() {
		if len(segment) > 0 && applySegmentReplacements(segment, reps, counts) {
			modified = true
		}
		segment = segment[:0]
	}

	addChunk := func(font *glyphFont, raw []byte, newOp bool, writeBack func([]byte)) {
		text, enc := font.decode(raw)
		segment = append(segment, opTextChunk{
			font:      font,
			text:      text,
			enc:       enc,
			byteCount: len(raw),
			newOp:     newOp,
			writeBack: writeBack,
		})
	}

	for i := range ops {
		switch op := ops[i].(type) {
		case cs.OpSetFont:
			flush()
			current = fonts[op.Font.String()]

		case cs.OpBeginText, cs.OpEndText:
			// A text block boundary ends the reconstructed run.
			flush()

		case cs.OpShowText: // Tj
			idx := i
			addChunk(current, []byte(op.Text), true, func(b []byte) {
				ops[idx] = cs.OpShowText{Text: string(b)}
			})

		case cs.OpMoveShowText: // '
			idx := i
			addChunk(current, []byte(op.Text), true, func(b []byte) {
				ops[idx] = cs.OpMoveShowText{Text: string(b)}
			})

		case cs.OpMoveSetShowText: // "
			idx := i
			ws, cw := op.WordSpacing, op.CharacterSpacing
			addChunk(current, []byte(op.Text), true, func(b []byte) {
				ops[idx] = cs.OpMoveSetShowText{Text: string(b), WordSpacing: ws, CharacterSpacing: cw}
			})

		case cs.OpShowSpaceText: // TJ — each element is its own chunk; kerning is preserved
			idx := i
			texts := make([]pdfFonts.TextSpaced, len(op.Texts))
			copy(texts, op.Texts)
			ops[idx] = cs.OpShowSpaceText{Texts: texts}
			for j := range texts {
				jj := j
				addChunk(current, texts[jj].CharCodes, jj == 0, func(b []byte) {
					texts[jj].CharCodes = b
					ops[idx] = cs.OpShowSpaceText{Texts: texts}
				})
			}
		}
	}
	flush()

	return modified
}

// applySegmentReplacements reconstructs the segment's full text, applies every
// replacement, and writes the result back across the chunks, re-encoding only the
// chunks whose characters changed so untouched glyphs keep their original CIDs.
//
// Matching is whitespace-flexible: a space in a target matches either real
// whitespace or a boundary between two show-text operators (see matchFlexAt), so a
// full name like "HERMIONE GRANGER" matches even when the visual space between the
// two words is a positioning jump rather than a space glyph.
func applySegmentReplacements(segment []opTextChunk, reps []replacement, counts map[string]int) bool {
	var b strings.Builder
	boundaries := make(map[int]bool) // byte offsets in full where a new show-text operator starts
	for i := range segment {
		if i > 0 && segment[i].newOp {
			boundaries[b.Len()] = true
		}
		b.WriteString(segment[i].text)
	}
	full := b.String()

	// Scan left-to-right for non-overlapping matches. At each position the reps are
	// tried in order, so an earlier target wins and a masked span is never re-matched.
	type match struct {
		start, end int
		rep        *replacement
	}
	var matches []match
	for i := 0; i < len(full); {
		var hit *replacement
		end := i
		for k := range reps {
			if reps[k].search == "" {
				continue
			}
			if e := matchFlexAt(full, boundaries, i, reps[k].search); e > i {
				hit = &reps[k]
				end = e
				break
			}
		}
		if hit == nil {
			_, sz := utf8.DecodeRuneInString(full[i:])
			i += sz
			continue
		}
		matches = append(matches, match{start: i, end: end, rep: hit})
		counts[hit.search]++
		i = end
	}
	if len(matches) == 0 {
		return false
	}

	// Rewrite each chunk from the matches, walking full and the matches together.
	//
	//   - A default mask (maskChar != 0) replaces each matched rune in place, so the
	//     chunk keeps its rune length and CID byte slots — layout is preserved.
	//   - A custom mask replaces the whole matched span as a single unit, emitted
	//     into the chunk that holds the match's *start*; the other chunks the match
	//     spans drop their matched text. This keeps a multi-word replacement like
	//     "HERMIONE GRANGER" contiguous instead of being split across the original
	//     operators' positions.
	//
	// Chunks that decoded to nothing (e.g. unmapped CIDs) contribute no bytes to
	// full and are left untouched, so their original glyphs are preserved.
	modified := false
	offset := 0
	mIdx := 0
	for ci := range segment {
		chunk := &segment[ci]
		cstart := offset
		cend := offset + len(chunk.text)
		offset = cend
		if len(chunk.text) == 0 {
			continue
		}

		var nb strings.Builder
		changed := false
		for pos := cstart; pos < cend; {
			for mIdx < len(matches) && matches[mIdx].end <= pos {
				mIdx++
			}
			r, sz := utf8.DecodeRuneInString(full[pos:])
			switch {
			case mIdx >= len(matches) || pos < matches[mIdx].start:
				nb.WriteRune(r) // outside any match — copy verbatim
			case matches[mIdx].rep.maskChar != 0:
				nb.WriteRune(matches[mIdx].rep.maskChar) // default mask: one rune in place
				changed = true
			default:
				if pos == matches[mIdx].start {
					nb.WriteString(matches[mIdx].rep.replace) // custom mask: whole span here
				}
				changed = true
			}
			pos += sz
		}
		if !changed {
			continue
		}

		newText := nb.String()
		preserveSlots := utf8.RuneCountInString(newText) == utf8.RuneCountInString(chunk.text)
		chunk.writeBack(chunk.font.encode(newText, chunk.byteCount, chunk.enc, preserveSlots))
		modified = true
	}

	return modified
}

// matchFlexAt reports the end byte offset of a case-insensitive match of pattern
// beginning at byte offset start in full, or a value <= start if there is none.
//
// A run of whitespace in the pattern matches either one-or-more whitespace runes in
// full, or — when there are none — a boundary between two show-text operators (zero
// width, from the boundaries set). This lets "HERMIONE GRANGER" match text whose
// visual space is a positioning jump between two Tj/TJ operators, while still
// refusing to match inside a single word (e.g. "in come" never matches "income",
// which has neither whitespace nor an operator boundary between "in" and "come").
// Non-space runes must match under Unicode simple folding.
func matchFlexAt(full string, boundaries map[int]bool, start int, pattern string) int {
	si := start
	for pi := 0; pi < len(pattern); {
		pr, psz := utf8.DecodeRuneInString(pattern[pi:])
		if unicode.IsSpace(pr) {
			// Collapse a run of pattern whitespace into a single flexible gap.
			for pi < len(pattern) {
				r, sz := utf8.DecodeRuneInString(pattern[pi:])
				if !unicode.IsSpace(r) {
					break
				}
				pi += sz
			}
			// Consume zero-or-more whitespace runes in the text.
			consumed := 0
			for si < len(full) {
				r, sz := utf8.DecodeRuneInString(full[si:])
				if !unicode.IsSpace(r) {
					break
				}
				si += sz
				consumed++
			}
			if consumed == 0 && !boundaries[si] {
				return -1
			}
			continue
		}
		if si >= len(full) {
			return -1
		}
		r, sz := utf8.DecodeRuneInString(full[si:])
		if !runeEqualFold(r, pr) {
			return -1
		}
		si += sz
		pi += psz
	}
	return si
}
