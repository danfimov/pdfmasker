package masker

import (
	"testing"

	tokenizer "github.com/benoitkugler/pstokenizer"
)

// tok builds a token slice from a compact spec: "[" open, "]" close, "s" string,
// "o" other. Lets us assert the backward array-start scan, including nesting.
func toks(spec string) []tokenizer.Token {
	out := make([]tokenizer.Token, 0, len(spec))
	for _, c := range spec {
		var k tokenizer.Kind
		switch c {
		case '[':
			k = tokenizer.StartArray
		case ']':
			k = tokenizer.EndArray
		case 's':
			k = tokenizer.String
		default:
			k = tokenizer.Other
		}
		out = append(out, tokenizer.Token{Kind: k})
	}
	return out
}

func TestFindTJArrayStart(t *testing.T) {
	cases := []struct {
		spec string
		end  int
		want int
	}{
		{"[ss]", 3, 0},        // simple array
		{"o[s]", 3, 1},        // preceded by an operator
		{"[s[s]s]", 6, 0},     // nested: outer close matches outer open
		{"[s[s]s]", 4, 2},     // nested: inner close matches inner open
		{"[s][s]", 5, 3},      // second array's close -> second array's open
		{"ss", 1, -1},         // no array at all
		{"[s]s]", 4, -1},      // unbalanced close has no matching open
	}
	for _, c := range cases {
		got := findTJArrayStart(toks(c.spec), c.end)
		if got != c.want {
			t.Errorf("findTJArrayStart(%q, %d) = %d, want %d", c.spec, c.end, got, c.want)
		}
	}
}
