package masker

import "testing"

func TestFoldMatching(t *testing.T) {
	cases := []struct {
		s, sub    string
		replaced  string
		replaceTo string
		count     int
		contains  bool
	}{
		{s: "Lorraine Freddie", sub: "lorraine freddie", contains: true, count: 1, replaced: "[M]", replaceTo: "[M]"},
		{s: "Lorraine Freddie", sub: "LORRAINE FREDDIE", contains: true, count: 1, replaced: "[M]", replaceTo: "[M]"},
		{s: "Lorraine Freddie", sub: "Lorraine", contains: true, count: 1, replaced: "[M] Freddie", replaceTo: "[M]"},
		{s: "aAaA", sub: "a", contains: true, count: 4, replaced: "xxxx", replaceTo: "x"},
		{s: "hello world", sub: "xyz", contains: false, count: 0, replaced: "hello world", replaceTo: "x"},
		{s: "café CAFÉ", sub: "café", contains: true, count: 2, replaced: "X X", replaceTo: "X"},
		{s: "anything", sub: "", contains: false, count: 0, replaced: "anything", replaceTo: "x"},
	}
	for _, c := range cases {
		if got := containsFold(c.s, c.sub); got != c.contains {
			t.Errorf("containsFold(%q,%q)=%v want %v", c.s, c.sub, got, c.contains)
		}
		if got := countFold(c.s, c.sub); got != c.count {
			t.Errorf("countFold(%q,%q)=%d want %d", c.s, c.sub, got, c.count)
		}
		if got := replaceAllFold(c.s, c.sub, c.replaceTo); got != c.replaced {
			t.Errorf("replaceAllFold(%q,%q,%q)=%q want %q", c.s, c.sub, c.replaceTo, got, c.replaced)
		}
	}
}
