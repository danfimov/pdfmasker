package masker

import "testing"

func TestRuneEqualFold(t *testing.T) {
	cases := []struct {
		a, b rune
		want bool
	}{
		{'a', 'a', true},
		{'a', 'A', true},
		{'A', 'a', true},
		{'z', 'Z', true},
		{'a', 'b', false},
		{'é', 'É', true}, // U+00E9 / U+00C9
		{'é', 'e', false},
		{'ß', 'ẞ', true}, // U+00DF / U+1E9E, folds together
		{'5', '5', true},
		{'5', 't', false},
	}
	for _, c := range cases {
		if got := runeEqualFold(c.a, c.b); got != c.want {
			t.Errorf("runeEqualFold(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
