package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/wmarquardt/hunk/internal/diff"
	"github.com/wmarquardt/hunk/internal/theme"
)

const sample = "" +
	"diff --git a/alpha.go b/alpha.go\n--- a/alpha.go\n+++ b/alpha.go\n" +
	"@@ -1,3 +1,3 @@ func main()\n one\n \ttwo\n-three\n+THREE\n" +
	"diff --git a/beta.go b/beta.go\n--- a/beta.go\n+++ b/beta.go\n" +
	"@@ -10,1 +10,2 @@\n keep\n+added\n"

// screen renders the model at a given size and returns the plain-text lines a
// user would see, with styling stripped.
func screen(t *testing.T, m *Model, w, h int) []string {
	t.Helper()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = updated.(*Model)

	lines := strings.Split(m.render(), "\n")
	for i, l := range lines {
		lines[i] = ansi.Strip(l)
	}
	return lines
}

func newTestModel(t *testing.T, unified string) *Model {
	t.Helper()
	files, err := diff.ParseString(unified)
	if err != nil {
		t.Fatal(err)
	}
	return New(files, theme.Default())
}

func TestRenderFillsTheScreenExactly(t *testing.T) {
	m := newTestModel(t, sample)

	for _, size := range [][2]int{{120, 30}, {80, 24}, {200, 50}, {40, 10}} {
		w, h := size[0], size[1]
		lines := screen(t, m, w, h)

		if len(lines) != h {
			t.Errorf("%dx%d: got %d lines, want %d", w, h, len(lines), h)
		}
		for i, l := range lines {
			if got := ansi.StringWidth(l); got > w {
				t.Errorf("%dx%d: line %d is %d cells wide, want at most %d: %q", w, h, i, got, w, l)
			}
		}
	}
}

func TestRenderShowsBothSides(t *testing.T) {
	m := newTestModel(t, sample)
	out := strings.Join(screen(t, m, 140, 20), "\n")

	for _, want := range []string{"alpha.go", "three", "THREE", "func main()"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered screen is missing %q:\n%s", want, out)
		}
	}
}

func TestNarrowTerminalDropsTheSidebar(t *testing.T) {
	m := newTestModel(t, sample)

	screen(t, m, 140, 20)
	if !m.sidebar() {
		t.Error("a wide terminal should show the sidebar")
	}
	if !m.split() {
		t.Error("a wide terminal should show a split view")
	}

	screen(t, m, 60, 20)
	if m.sidebar() {
		t.Error("a narrow terminal should hide the sidebar")
	}
	if m.split() {
		t.Error("a narrow terminal should fall back to unified")
	}
}

func TestUnifiedModeSplitsPairsIntoTwoRows(t *testing.T) {
	files, err := diff.ParseString(sample)
	if err != nil {
		t.Fatal(err)
	}

	split := Build(files, true)
	unified := Build(files, false)

	if len(unified.Rows) <= len(split.Rows) {
		t.Errorf("unified has %d rows, split has %d: a changed line should take two rows unified",
			len(unified.Rows), len(split.Rows))
	}
	// A context line still occupies both sides (it renders once, unchanged);
	// a changed line must not, or unified would show only half of the change.
	for _, r := range unified.Rows {
		if r.Kind != RowPair || r.Left.Kind == diff.Context {
			continue
		}
		if !r.Left.Empty && !r.Right.Empty {
			t.Errorf("unified row has both sides filled: %+v", r)
		}
	}
}

func TestToggleSplitKeepsTheCurrentFile(t *testing.T) {
	m := newTestModel(t, sample)
	screen(t, m, 140, 20)

	m.moveTo(m.view.FileRows[1])
	if got := m.view.Rows[m.cur].FileIdx; got != 1 {
		t.Fatalf("cursor is on file %d, want 1", got)
	}

	m.handleKey(keyPress("s"))
	if m.builtSplit {
		t.Fatal("pressing s did not switch to unified")
	}
	if got := m.view.Rows[m.cur].FileIdx; got != 1 {
		t.Errorf("after toggling, cursor is on file %d, want to stay on 1", got)
	}
}

func TestNavigationKeys(t *testing.T) {
	m := newTestModel(t, sample)
	screen(t, m, 140, 20)

	m.handleKey(keyPress("G"))
	if m.cur != len(m.view.Rows)-1 {
		t.Errorf("G left the cursor at %d, want the last row %d", m.cur, len(m.view.Rows)-1)
	}

	m.handleKey(keyPress("g"))
	if m.cur != 0 {
		t.Errorf("g left the cursor at %d, want 0", m.cur)
	}

	m.handleKey(keyPress("n"))
	if m.cur != m.view.HunkRows[0] && m.cur != m.view.HunkRows[1] {
		t.Errorf("n left the cursor at %d, want a hunk row from %v", m.cur, m.view.HunkRows)
	}

	m.handleKey(keyPress("]"))
	if m.view.Rows[m.cur].FileIdx != 1 {
		t.Errorf("] should jump to the next file, cursor is on file %d", m.view.Rows[m.cur].FileIdx)
	}

	m.handleKey(keyPress("["))
	if m.view.Rows[m.cur].FileIdx != 0 {
		t.Errorf("[ should jump back to the first file, cursor is on file %d", m.view.Rows[m.cur].FileIdx)
	}
}

func TestCursorStaysOnScreen(t *testing.T) {
	m := newTestModel(t, sample)
	screen(t, m, 140, 8)

	for i := 0; i < 50; i++ {
		m.handleKey(keyPress("j"))
	}
	if m.cur < m.top || m.cur >= m.top+m.bodyHeight() {
		t.Errorf("cursor %d is outside the window [%d, %d)", m.cur, m.top, m.top+m.bodyHeight())
	}
	if m.cur > len(m.view.Rows)-1 {
		t.Errorf("cursor %d ran past the last row %d", m.cur, len(m.view.Rows)-1)
	}

	for i := 0; i < 100; i++ {
		m.handleKey(keyPress("k"))
	}
	if m.cur != 0 || m.top != 0 {
		t.Errorf("scrolling up past the top left cur=%d top=%d, want 0/0", m.cur, m.top)
	}
}

func TestHelpOverlayAndDismissal(t *testing.T) {
	m := newTestModel(t, sample)
	screen(t, m, 140, 20)

	m.handleKey(keyPress("?"))
	out := strings.Join(screen(t, m, 140, 20), "\n")
	if !strings.Contains(out, "next / previous hunk") {
		t.Errorf("help overlay did not render:\n%s", out)
	}

	m.handleKey(keyPress("j"))
	if m.showHelp {
		t.Error("any key should dismiss the help overlay")
	}
}

func TestEmptyDiffRenders(t *testing.T) {
	m := New(nil, theme.Default())
	lines := screen(t, m, 80, 10)

	if len(lines) != 10 {
		t.Fatalf("got %d lines, want 10", len(lines))
	}
	if !strings.Contains(lines[len(lines)-1], "no changes") {
		t.Errorf("status bar = %q, want it to say there are no changes", lines[len(lines)-1])
	}
}

func TestHorizontalScroll(t *testing.T) {
	long := "--- a/x\n+++ b/x\n@@ -1,1 +1,1 @@\n-" +
		strings.Repeat("left", 40) + "\n+" + strings.Repeat("right", 40) + "\n"

	m := newTestModel(t, long)
	screen(t, m, 120, 10)

	before := strings.Join(screen(t, m, 120, 10), "\n")
	for i := 0; i < 4; i++ {
		m.handleKey(keyPress("l"))
	}
	after := strings.Join(screen(t, m, 120, 10), "\n")

	if before == after {
		t.Error("scrolling right changed nothing")
	}

	for i := 0; i < 20; i++ {
		m.handleKey(keyPress("h"))
	}
	if m.hscroll != 0 {
		t.Errorf("scrolling left past the start left hscroll at %d", m.hscroll)
	}
}

func keyPress(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

// The outline is one shape crossing both panes: inside it there is no divider
// between the columns, so the top rule sweeps straight across the seam and the
// direction marker sits on it.
func TestChangeBlockOutlineIsOneShape(t *testing.T) {
	m := newTestModel(t, sample)
	lines := screen(t, m, 140, 20)

	var top, ctx, arrow string
	for i, l := range lines {
		if top == "" && strings.Contains(l, "╭") && i > 0 {
			top, ctx = l, lines[i-1]
		}
		if arrow == "" && strings.Contains(l, "→") {
			arrow = l
		}
	}
	if top == "" {
		t.Fatalf("no change block outline on screen:\n%s", strings.Join(lines, "\n"))
	}

	// Compare cell columns, not byte offsets: box glyphs are three bytes each.
	seam := col(ctx, '│')
	if got := at(top, seam); got != '─' {
		t.Errorf("the top rule reads %q at the seam, want it to sweep across:\n%q\n%q",
			string(got), ctx, top)
	}
	if arrow == "" {
		t.Error("a change that crosses panes should carry a direction marker")
	} else if col(arrow, '→') != seam {
		t.Errorf("the direction marker is at column %d, want the seam at %d: %q",
			col(arrow, '→'), seam, arrow)
	}
	if !strings.HasSuffix(strings.TrimRight(top, " "), "╮") {
		t.Errorf("the top rule does not close on the right: %q", top)
	}
}

// at is the rune in cell column i.
func at(line string, i int) rune {
	r := []rune(line)
	if i < 0 || i >= len(r) {
		return 0
	}
	return r[i]
}

// col is the last cell column r appears at in a rendered line.
func col(line string, r rune) int {
	last := -1
	for i, c := range []rune(line) {
		if c == r {
			last = i
		}
	}
	return last
}
