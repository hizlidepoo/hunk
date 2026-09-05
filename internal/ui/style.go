package ui

import (
	"image/color"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/wmarquardt/hunk/internal/diff"
	"github.com/wmarquardt/hunk/internal/theme"
)

// tabWidth is how wide a tab renders. Tabs must become spaces before anything
// is measured or sliced, or every column after one drifts.
const tabWidth = 4

// styles is a theme turned into the concrete Lip Gloss styles the renderer
// uses. Building them once per theme keeps the per-row work to string joins.
type styles struct {
	base       lipgloss.Style
	fileHeader lipgloss.Style
	hunkHeader lipgloss.Style
	notice     lipgloss.Style
	lineNum    lipgloss.Style
	gutter     lipgloss.Style
	statusbar  lipgloss.Style
	help       lipgloss.Style
	sidebar    lipgloss.Style
	sidebarSel lipgloss.Style
	modal      lipgloss.Style
	modalTitle lipgloss.Style
	// focusRail draws the vertical "current hunk" accent in the left margin;
	// focusHeader lights up that hunk's @@ line so the active block is obvious.
	focusRail   lipgloss.Style
	focusHeader lipgloss.Style
	// railMarked is the green left-margin bar down a hunk that is marked for
	// staging.
	railMarked lipgloss.Style

	// stagedFg / partialFg color the sidebar check: green when a file is fully
	// staged, gray when only some of it is.
	stagedFg    color.Color
	partialFg   color.Color
	context     lipgloss.Style
	added       lipgloss.Style
	removed     lipgloss.Style
	addedWord   lipgloss.Style
	removedWord lipgloss.Style
	filler      lipgloss.Style
	cursor      lipgloss.Style
	// box draws a change block's outline, in the same accent as everything else
	// hunk points with.
	box lipgloss.Style
}

func newStyles(t *theme.Theme) styles {
	c := lipgloss.Color
	base := lipgloss.NewStyle().Background(c(t.UI.Background)).Foreground(c(t.UI.Foreground))

	return styles{
		base:        base,
		fileHeader:  base.Foreground(c(t.UI.FileHeader)).Bold(true),
		hunkHeader:  base.Foreground(c(t.UI.Accent)),
		notice:      base.Foreground(c(t.UI.LineNumber)).Italic(true),
		lineNum:     base.Foreground(c(t.UI.LineNumber)),
		gutter:      base.Foreground(c(t.UI.Border)),
		statusbar:   lipgloss.NewStyle().Background(c(t.UI.Accent)).Foreground(c(t.UI.Background)),
		help:        base.Foreground(c(t.UI.Foreground)),
		sidebar:     base.Foreground(c(t.UI.SidebarFg)),
		sidebarSel:  lipgloss.NewStyle().Background(c(t.UI.Accent)).Foreground(c(t.UI.Background)),
		modal:       base.Border(lipgloss.RoundedBorder()).BorderBackground(c(t.UI.Background)).BorderForeground(c(t.UI.Border)).Padding(0, 2),
		modalTitle:  base.Foreground(c(t.UI.FileHeader)).Bold(true),
		focusRail:   base.Foreground(c(t.UI.Accent)).Bold(true),
		focusHeader: base.Background(c(t.UI.Accent)).Foreground(c(t.UI.Background)).Bold(true),
		railMarked:  base.Foreground(c(t.Diff.AddedFg)).Bold(true),
		stagedFg:    c(t.Diff.AddedFg),
		partialFg:   c(t.UI.LineNumber),
		context:     base.Foreground(c(t.Diff.ContextFg)),
		added:       base.Background(c(t.Diff.AddedBg)).Foreground(c(t.Diff.AddedFg)),
		removed:     base.Background(c(t.Diff.RemovedBg)).Foreground(c(t.Diff.RemovedFg)),
		addedWord:   base.Background(c(t.Diff.AddedWordBg)).Foreground(c(t.Diff.AddedFg)).Bold(true),
		removedWord: base.Background(c(t.Diff.RemovedWordBg)).Foreground(c(t.Diff.RemovedFg)).Bold(true),
		filler:      base.Foreground(c(t.UI.Border)),
		cursor:      base.Foreground(c(t.UI.Accent)).Bold(true),
		box:         base.Foreground(c(t.UI.Accent)),
	}
}

// lineStyles picks the pair of styles a side is painted with: the whole-line
// color, and the stronger color for the words that actually changed.
func (s styles) lineStyles(k diff.Kind) (line, word lipgloss.Style) {
	switch k {
	case diff.Added:
		return s.added, s.addedWord
	case diff.Removed:
		return s.removed, s.removedWord
	default:
		return s.context, s.context
	}
}

// expandTabs replaces tabs with spaces and moves the highlight ranges to match.
func expandTabs(text string, ranges []diff.Range, showWS bool) (string, []diff.Range) {
	if !strings.ContainsRune(text, '\t') {
		if showWS {
			return markTrailingSpaces(text), ranges
		}
		return text, ranges
	}

	// shift[i] is where byte i of the original text lands in the expanded one.
	shift := make([]int, len(text)+1)
	var b strings.Builder
	col := 0
	for i, r := range text {
		shift[i] = b.Len()
		if r == '\t' {
			pad := tabWidth - col%tabWidth
			if showWS {
				// A tab is one arrow cell then spaces out to the tab stop, so it
				// still occupies exactly pad columns.
				b.WriteString(tabMarker)
				b.WriteString(strings.Repeat(" ", pad-1))
			} else {
				b.WriteString(strings.Repeat(" ", pad))
			}
			col += pad
			continue
		}
		b.WriteRune(r)
		col++
	}
	shift[len(text)] = b.Len()

	moved := make([]diff.Range, 0, len(ranges))
	for _, r := range ranges {
		if r.Start > len(text) || r.End > len(text) {
			continue
		}
		moved = append(moved, diff.Range{Start: shift[r.Start], End: shift[r.End]})
	}
	out := b.String()
	if showWS {
		out = markTrailingSpaces(out)
	}
	return out, moved
}

const (
	tabMarker = "→" // shown at the start of an expanded tab when whitespace is visible
	spaceMark = "·" // shown for each trailing space when whitespace is visible
)

// markTrailingSpaces replaces the run of spaces at the end of a line with a
// visible dot each, so trailing whitespace stops hiding. Widths are unchanged:
// one dot per space.
func markTrailingSpaces(s string) string {
	i := len(s)
	for i > 0 && s[i-1] == ' ' {
		i--
	}
	if i == len(s) {
		return s
	}
	return s[:i] + strings.Repeat(spaceMark, len(s)-i)
}

// styleText paints a line, with the changed ranges in the emphasis style.
func styleText(text string, ranges []diff.Range, line, word lipgloss.Style) string {
	if len(ranges) == 0 {
		return line.Render(text)
	}

	var b strings.Builder
	prev := 0
	for _, r := range ranges {
		if r.Start < prev || r.End > len(text) {
			continue // a stale range; drop it rather than slice out of bounds
		}
		b.WriteString(line.Render(text[prev:r.Start]))
		b.WriteString(word.Render(text[r.Start:r.End]))
		prev = r.End
	}
	b.WriteString(line.Render(text[prev:]))
	return b.String()
}

// fit slices a styled string to a horizontal window and pads it out to width,
// counting terminal cells rather than bytes so wide runes stay aligned.
func fit(styled string, hscroll, width int, pad lipgloss.Style) string {
	if width <= 0 {
		return ""
	}
	if hscroll > 0 {
		styled = ansi.Cut(styled, hscroll, hscroll+width)
	} else {
		styled = ansi.Truncate(styled, width, "")
	}
	if gap := width - ansi.StringWidth(styled); gap > 0 {
		styled += pad.Render(strings.Repeat(" ", gap))
	}
	return styled
}

// renderSide renders one column of a row: the line number gutter and the text.
func (s styles) renderSide(side Side, sign string, numWidth, textWidth, hscroll int, showWS bool) string {
	lineStyle, wordStyle := s.lineStyles(side.Kind)
	if textWidth <= 0 {
		return "" // no room for this column at all; the caller pads the row
	}

	if side.Empty {
		// An absent side is painted in the frame color, not in added/removed:
		// nothing was added or removed here, the line simply does not exist.
		return s.filler.Render(strings.Repeat(" ", numWidth+1)) +
			fit("", 0, textWidth, s.filler)
	}

	num := ""
	if side.Num > 0 {
		num = strconv.Itoa(side.Num)
	}
	// The number carries the line's own background on a changed line, so the
	// tint runs the full width of the block instead of starting mid-row.
	numStyle := s.lineNum
	if side.Kind != diff.Context {
		numStyle = numStyle.Background(lineStyle.GetBackground())
	}
	gutter := numStyle.Render(padLeft(num, numWidth) + " ")

	text, ranges := expandTabs(side.Text, side.Ranges, showWS)
	body := styleText(sign+text, shiftRanges(ranges, len(sign)), lineStyle, wordStyle)
	return gutter + fit(body, hscroll, textWidth, lineStyle)
}

func shiftRanges(ranges []diff.Range, by int) []diff.Range {
	if by == 0 || len(ranges) == 0 {
		return ranges
	}
	out := make([]diff.Range, len(ranges))
	for i, r := range ranges {
		out[i] = diff.Range{Start: r.Start + by, End: r.End + by}
	}
	return out
}

func padLeft(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return strings.Repeat(" ", w-len(s)) + s
}
