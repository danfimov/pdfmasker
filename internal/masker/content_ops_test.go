package masker

import "testing"

// TestMatchFlexAt covers the whitespace-flexible, boundary-aware matcher: a space
// in the pattern matches real whitespace or an operator boundary, but not the
// inside of a word. ("HERMIONE" is 8 bytes, so the boundary sits at offset 8.)
func TestMatchFlexAt(t *testing.T) {
	cases := []struct {
		name       string
		full       string
		boundaries map[int]bool
		start      int
		pattern    string
		wantEnd    int // expected end offset, or -1 for no match
	}{
		{
			name:       "space matches operator boundary (positioning gap)",
			full:       "HERMIONEGRANGER",
			boundaries: map[int]bool{8: true},
			pattern:    "HERMIONE GRANGER",
			wantEnd:    15,
		},
		{
			name:       "space matches boundary, case-insensitive across it",
			full:       "HERMIONEgranger",
			boundaries: map[int]bool{8: true},
			pattern:    "hermione GRANGER",
			wantEnd:    15,
		},
		{
			name:       "no space in pattern flows across the boundary too",
			full:       "HERMIONEGRANGER",
			boundaries: map[int]bool{8: true},
			pattern:    "HERMIONEGRANGER",
			wantEnd:    15,
		},
		{
			name:       "space inside a word does not match (no boundary, no whitespace)",
			full:       "income",
			boundaries: map[int]bool{},
			pattern:    "inco me",
			wantEnd:    -1,
		},
		{
			name:       "space matches a real space",
			full:       "Lorraine Freddie",
			boundaries: map[int]bool{},
			pattern:    "lorraine freddie",
			wantEnd:    16,
		},
		{
			name:       "space matches a run of whitespace",
			full:       "A  B",
			boundaries: map[int]bool{},
			pattern:    "a b",
			wantEnd:    4,
		},
		{
			name:       "match at non-zero offset",
			full:       "xxHERMIONEGRANGER",
			boundaries: map[int]bool{10: true},
			start:      2,
			pattern:    "HERMIONE GRANGER",
			wantEnd:    17,
		},
		{
			name:       "plain mismatch",
			full:       "hello",
			boundaries: map[int]bool{},
			pattern:    "world",
			wantEnd:    -1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := matchFlexAt(c.full, c.boundaries, c.start, c.pattern)
			if got != c.wantEnd {
				t.Errorf("matchFlexAt(%q, %v, %d, %q) = %d, want %d",
					c.full, c.boundaries, c.start, c.pattern, got, c.wantEnd)
			}
		})
	}
}
