package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/wmarquardt/hunk/internal/diff"
	"github.com/wmarquardt/hunk/internal/theme"
)

func TestExpandTabs(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		ranges []diff.Range
		want   string
		// wantHighlight is the highlighted substring after expansion.
		wantHighlight string
	}{
		{name: "no tabs is left alone", in: "abc", want: "abc"},
		{name: "leading tab", in: "\tx", want: "    x"},
		{name: "tab stops, not fixed width", in: "ab\tc", want: "ab  c"},
		{name: "tab already at a stop", in: "abcd\te", want: "abcd    e"},
		{
			name: "ranges move with the text",
			in:   "\tvalue := 1", ranges: []diff.Range{{Start: 1, End: 6}},
			want: "    value := 1", wantHighlight: "value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ranges, _ := expandTabs(tt.in, tt.ranges, nil, false)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			if tt.wantHighlight != "" {
				if len(ranges) != 1 {
					t.Fatalf("got %d ranges, want 1", len(ranges))
				}
				if h := got[ranges[0].Start:ranges[0].End]; h != tt.wantHighlight {
					t.Errorf("highlight = %q, want %q", h, tt.wantHighlight)
				}
			}
		})
	}
}

func TestFitPadsAndTruncates(t *testing.T) {
	st := newStyles(theme.Default())

	tests := []struct {
		name    string
		text    string
		hscroll int
		width   int
	}{
		{name: "pads a short line", text: "abc", width: 10},
		{name: "truncates a long line", text: strings.Repeat("x", 50), width: 10},
		{name: "scrolls sideways", text: strings.Repeat("abcdefghij", 5), hscroll: 12, width: 10},
		{name: "handles wide runes", text: "日本語のテキストです", width: 9},
		{name: "zero width", text: "abc", width: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fit(st.context.Render(tt.text), tt.hscroll, tt.width, st.context)
			if w := ansi.StringWidth(got); w != tt.width {
				t.Errorf("width = %d, want exactly %d (%q)", w, tt.width, ansi.Strip(got))
			}
		})
	}
}

func TestFitScrollShowsLaterText(t *testing.T) {
	st := newStyles(theme.Default())
	text := "0123456789abcdefghij"

	got := ansi.Strip(fit(st.context.Render(text), 10, 5, st.context))
	if got != "abcde" {
		t.Errorf("scrolled window = %q, want %q", got, "abcde")
	}
}

// The point of the whole intra-line machinery is that the changed words reach
// the screen in a different color from the rest of the line.
func TestIntralineHighlightReachesTheScreen(t *testing.T) {
	th := theme.Default()
	files, err := diff.ParseString(
		"--- a/x\n+++ b/x\n@@ -1,1 +1,1 @@\n-result := compute(alpha, beta, gamma)\n+result := compute(alpha, delta, gamma)\n")
	if err != nil {
		t.Fatal(err)
	}

	m := New(files, th, Options{})
	u, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 12})
	out := u.(*Model).render()

	// Lip Gloss writes the theme's colors as truecolor escapes; the emphasis
	// background only appears if a range was highlighted.
	for _, want := range []string{
		rgbEscape(th.Diff.RemovedWordBg),
		rgbEscape(th.Diff.AddedWordBg),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered diff does not use the intra-line color %q", want)
		}
	}
}

// rgbEscape builds the background escape Lip Gloss emits for a #rrggbb color.
func rgbEscape(hex string) string {
	var r, g, b int
	for i, c := range []byte(hex[1:]) {
		v := int(c)
		switch {
		case c >= '0' && c <= '9':
			v -= '0'
		case c >= 'a' && c <= 'f':
			v = v - 'a' + 10
		case c >= 'A' && c <= 'F':
			v = v - 'A' + 10
		}
		switch i / 2 {
		case 0:
			r = r*16 + v
		case 1:
			g = g*16 + v
		case 2:
			b = b*16 + v
		}
	}
	return "48;2;" + itoa3(r) + ";" + itoa3(g) + ";" + itoa3(b)
}

func itoa3(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}

// Intraline ranges are computed against the line text, but the rendered row has
// a +/- sign in front of it. shiftRanges moves them over so the highlight lands
// on the changed words and not one column to their left.
func TestShiftRanges(t *testing.T) {
	ranges := []diff.Range{{Start: 0, End: 3}, {Start: 5, End: 9}}

	got := shiftRanges(ranges, 1)
	want := []diff.Range{{Start: 1, End: 4}, {Start: 6, End: 10}}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("range %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	// The input is left alone: the same ranges are reused for the other side.
	if ranges[0] != (diff.Range{Start: 0, End: 3}) {
		t.Errorf("shiftRanges modified its input: %+v", ranges[0])
	}
}

func TestShiftRangesWithNothingToDo(t *testing.T) {
	ranges := []diff.Range{{Start: 2, End: 4}}

	// A zero shift hands the same slice straight back rather than copying it.
	if got := shiftRanges(ranges, 0); len(got) != 1 || got[0] != ranges[0] {
		t.Errorf("shiftRanges(_, 0) = %+v, want the input unchanged", got)
	}
	if got := shiftRanges(nil, 3); got != nil {
		t.Errorf("shiftRanges(nil, 3) = %+v, want nil", got)
	}
	if got := shiftRanges([]diff.Range{}, 3); len(got) != 0 {
		t.Errorf("shiftRanges(empty, 3) = %+v, want empty", got)
	}
}
