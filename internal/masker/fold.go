package masker

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// This file implements case-insensitive (Unicode simple-fold) substring matching.
// It replaces the previous approach of expanding each target into a handful of
// hard-coded case variations, which only matched a few casings and missed common
// ones (e.g. multi-word title case). Matching folds per rune like strings.EqualFold,
// and all offsets returned are byte offsets into the original string so the
// existing CID/byte-slot redistribution logic keeps working unchanged.

// runeEqualFold reports whether sr and tr are equal under Unicode simple folding.
// Mirrors the per-rune logic of strings.EqualFold.
func runeEqualFold(sr, tr rune) bool {
	if sr == tr {
		return true
	}
	if tr < sr {
		sr, tr = tr, sr
	}
	// Fast path for ASCII.
	if tr < utf8.RuneSelf {
		return 'A' <= sr && sr <= 'Z' && tr == sr+('a'-'A')
	}
	// General path: walk the SimpleFold orbit from sr until we reach or pass tr.
	r := unicode.SimpleFold(sr)
	for r != sr && r < tr {
		r = unicode.SimpleFold(r)
	}
	return r == tr
}

// foldMatchLen reports the number of bytes consumed in s starting at byte offset
// start if substr matches there under case folding, or -1 if it does not.
// substr must be non-empty; a successful match always returns a value > 0.
func foldMatchLen(s string, start int, substr string) int {
	si := start
	for _, sub := range substr {
		if si >= len(s) {
			return -1
		}
		cur, size := utf8.DecodeRuneInString(s[si:])
		if !runeEqualFold(cur, sub) {
			return -1
		}
		si += size
	}
	return si - start
}

// indexFold returns the byte offset of the first case-insensitive occurrence of
// substr in s, or -1 if absent or substr is empty.
func indexFold(s, substr string) int {
	if substr == "" {
		return -1
	}
	for i := 0; i < len(s); {
		if foldMatchLen(s, i, substr) >= 0 {
			return i
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return -1
}

// containsFold reports whether substr occurs in s under case folding.
func containsFold(s, substr string) bool {
	return indexFold(s, substr) >= 0
}

// countFold counts non-overlapping case-insensitive occurrences of substr in s.
func countFold(s, substr string) int {
	if substr == "" {
		return 0
	}
	count := 0
	for i := 0; i < len(s); {
		if n := foldMatchLen(s, i, substr); n > 0 {
			count++
			i += n
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return count
}

// replaceAllFold replaces every non-overlapping case-insensitive occurrence of
// old in s with replacement.
func replaceAllFold(s, old, replacement string) string {
	if old == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if n := foldMatchLen(s, i, old); n > 0 {
			b.WriteString(replacement)
			i += n
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		b.WriteString(s[i : i+size])
		i += size
	}
	return b.String()
}
