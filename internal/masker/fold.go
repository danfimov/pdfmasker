package masker

import (
	"unicode"
	"unicode/utf8"
)

// runeEqualFold reports whether sr and tr are equal under Unicode simple folding.
// It mirrors the per-rune logic of strings.EqualFold and is the case-insensitivity
// primitive used by the content-stream matcher (matchFlexAt in content_ops.go).
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
