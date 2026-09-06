package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestCurrentHunkSpanCoversWholeHunk(t *testing.T) {
	m := newTestModel(t, sample) // alpha.go: one hunk of a few rows
	screen(t, m, 120, 30)

	// Land inside the first hunk.
	m.moveTo(m.view.HunkRows[0] + 1)
	lo, hi := m.currentHunkSpan()
	if lo != m.view.HunkRows[0] {
		t.Errorf("span starts at %d, want the @@ row %d", lo, m.view.HunkRows[0])
	}
	if lo > m.cur || m.cur >= hi {
		t.Errorf("cursor %d not inside span [%d,%d)", m.cur, lo, hi)
	}
	for r := lo; r < hi; r++ {
		if m.view.Rows[r].HunkIdx != m.view.Rows[m.cur].HunkIdx {
			t.Errorf("row %d is a different hunk than the cursor", r)
		}
	}

	// On a file header there is no current hunk.
	m.moveTo(m.view.FileRows[0])
	if lo, hi := m.currentHunkSpan(); lo != -1 || hi != -1 {
		t.Errorf("file header should have no hunk span, got [%d,%d)", lo, hi)
	}
}

func TestViewportShowsOnlyCurrentFile(t *testing.T) {
	m := newTestModel(t, sample) // alpha.go then beta.go

	// On the first file: its hunk shows, the second file's does not.
	body := ansi.Strip(strings.Join(screen(t, m, 120, 30), "\n"))
	if !strings.Contains(body, "@@ -1,3") {
		t.Fatal("current file's hunk missing")
	}
	if strings.Contains(body, "@@ -10,1") {
		t.Fatal("another file leaked into the current file's view")
	}

	// Jump to the second file: now the reverse holds.
	m = clickAt(t, m, 2, 1)
	body = ansi.Strip(strings.Join(screen(t, m, 120, 30), "\n"))
	if !strings.Contains(body, "@@ -10,1") {
		t.Fatal("second file's hunk missing after jump")
	}
	if strings.Contains(body, "@@ -1,3") {
		t.Fatal("first file still visible after jumping to the second")
	}
}

func TestHelpRendersAsModalOverDiff(t *testing.T) {
	m := newTestModel(t, sample)
	screen(t, m, 120, 30)
	m.showHelp = true

	out := ansi.Strip(m.render())
	if !strings.Contains(out, "┏") || !strings.Contains(out, "┗") {
		t.Fatal("help is not drawn as a bordered box")
	}
	// The diff behind the modal is still visible: the box does not fill the row.
	if !strings.Contains(out, "alpha.go") {
		t.Fatal("diff not visible behind the help modal")
	}
	if !strings.Contains(out, "press any key to close") {
		t.Fatal("modal missing its dismiss hint")
	}
}

// clickAt sends a left click and returns the updated model.
func clickAt(t *testing.T, m *Model, x, y int) *Model {
	t.Helper()
	updated, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	return updated.(*Model)
}

func TestClickStatusOptionRunsItsKey(t *testing.T) {
	m := newTestModel(t, sample)
	screen(t, m, 200, 30) // render so the hint zones are recorded

	var help *hintZone
	for i := range m.hints {
		if m.hints[i].key == "?" {
			help = &m.hints[i]
		}
	}
	if help == nil {
		t.Fatal("no ? help hint was recorded")
	}

	m = clickAt(t, m, help.x0, m.bodyHeight())
	if !m.showHelp {
		t.Fatalf("clicking the help option did not open help")
	}
}

func TestClickStatusOptionTogglesSplit(t *testing.T) {
	m := newTestModel(t, sample)
	screen(t, m, 200, 30)
	before := m.wantSplit

	var split *hintZone
	for i := range m.hints {
		if m.hints[i].key == "s" {
			split = &m.hints[i]
		}
	}
	if split == nil {
		t.Fatal("no s split hint was recorded")
	}

	m = clickAt(t, m, split.x1, m.bodyHeight())
	if m.wantSplit == before {
		t.Fatalf("clicking the split option did not toggle wantSplit")
	}
}

func TestClickSidebarJumpsToFile(t *testing.T) {
	m := newTestModel(t, sample) // sample has two files
	screen(t, m, 120, 30)
	if !m.sidebar() {
		t.Fatal("sidebar not shown at this width")
	}

	// The second file is drawn on sidebar row 1 while the cursor is at the top.
	m = clickAt(t, m, 2, 1)
	if got := m.view.Rows[m.cur].FileIdx; got != 1 {
		t.Fatalf("click on sidebar row 1 landed on file %d, want 1", got)
	}
}

func TestClickBodyMovesCursor(t *testing.T) {
	m := newTestModel(t, sample)
	screen(t, m, 120, 30)

	// A row a few lines down in the body column (past the sidebar + gutter).
	m = clickAt(t, m, sidebarWidth+2, 4)
	if m.cur != m.top+4 {
		t.Fatalf("body click put cursor at %d, want %d", m.cur, m.top+4)
	}
}

func TestWheelScrolls(t *testing.T) {
	m := newTestModel(t, sample)
	screen(t, m, 120, 30)
	m.moveTo(6)
	start := m.cur

	updated, _ := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	m = updated.(*Model)
	if m.cur >= start {
		t.Fatalf("wheel up did not move the cursor up: %d -> %d", start, m.cur)
	}
}
