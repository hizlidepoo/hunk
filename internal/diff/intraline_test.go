package diff

import (
	"testing"
	"unicode/utf8"
)

// apply renders the highlighted parts of a line as «...» so a test asserts on
// what a reader would actually see.
func apply(s string, ranges []Range) string {
	out := ""
	prev := 0
	for _, r := range ranges {
		out += s[prev:r.Start] + "«" + s[r.Start:r.End] + "»"
		prev = r.End
	}
	return out + s[prev:]
}

func TestIntraline(t *testing.T) {
	tests := []struct {
		name             string
		old, new         string
		wantOld, wantNew string
	}{
		{
			name: "identical lines have nothing to highlight",
			old:  "x := 1", new: "x := 1",
			wantOld: "x := 1", wantNew: "x := 1",
		},
		{
			name: "one word changed",
			old:  "return oldValue, nil", new: "return newValue, nil",
			wantOld: "return «oldValue», nil", wantNew: "return «newValue», nil",
		},
		{
			name: "pure insertion",
			old:  "f(a, b)", new: "f(a, b, c)",
			wantOld: "f(a, b)", wantNew: "f(a, b«, c»)",
		},
		{
			name: "pure deletion",
			old:  "f(a, b, c)", new: "f(a, b)",
			wantOld: "f(a, b«, c»)", wantNew: "f(a, b)",
		},
		{
			name: "trailing whitespace only",
			old:  "value := 1  ", new: "value := 1",
			wantOld: "value := 1«  »", wantNew: "value := 1",
		},
		{
			name: "indentation change",
			old:  "\tif ok {", new: "\t\tif ok {",
			wantOld: "\tif ok {", wantNew: "«\t»\tif ok {",
		},
		{
			name: "a full rewrite highlights nothing",
			old:  "alpha beta gamma", new: "zeta eta theta iota",
			wantOld: "alpha beta gamma", wantNew: "zeta eta theta iota",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldRanges, newRanges := Intraline(tt.old, tt.new)

			if got := apply(tt.old, oldRanges); got != tt.wantOld {
				t.Errorf("old:\n got %q\nwant %q", got, tt.wantOld)
			}
			if got := apply(tt.new, newRanges); got != tt.wantNew {
				t.Errorf("new:\n got %q\nwant %q", got, tt.wantNew)
			}
		})
	}
}

// Highlighting must never cut a rune in half: a sliced rune renders as a
// replacement character and corrupts every column after it.
func TestIntralineRuneBoundaries(t *testing.T) {
	pairs := [][2]string{
		{"名前 := 1", "名前 := 2"},
		{"greeting := \"héllo\"", "greeting := \"hello\""},
		{"emoji := \"🎉\"", "emoji := \"🎊\""},
		{"日本語のテキスト", "日本語のテキストです"},
	}

	for _, p := range pairs {
		before, after := p[0], p[1]
		beforeRanges, afterRanges := Intraline(before, after)

		for _, c := range []struct {
			s      string
			ranges []Range
		}{{before, beforeRanges}, {after, afterRanges}} {
			for _, r := range c.ranges {
				if r.Start < 0 || r.End > len(c.s) || r.Start > r.End {
					t.Fatalf("range %+v out of bounds for %q", r, c.s)
				}
				if !utf8.ValidString(c.s[r.Start:r.End]) {
					t.Errorf("range %+v splits a rune in %q", r, c.s)
				}
				if !utf8.ValidString(c.s[:r.Start]) {
					t.Errorf("range %+v starts mid-rune in %q", r, c.s)
				}
			}
		}
	}
}

func TestIntralineEmptyLines(t *testing.T) {
	if o, n := Intraline("", ""); o != nil || n != nil {
		t.Errorf("two empty lines: got %v, %v", o, n)
	}
	// A blank line paired with a content line is a 100% change, so the guard
	// drops the highlighting: the row's own added/removed color already says
	// everything, and painting the entire line emphasizes nothing.
	if _, n := Intraline("", "added"); n != nil {
		t.Errorf("empty -> content: want no highlighting, got %v", n)
	}
}
