package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeWorking overwrites a working-tree file in the repo under test.
func writeWorking(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// markedText concatenates the text of every marked hunk, so a test can assert
// which change is still marked without caring about indices.
func markedText(m *Model) string {
	var b strings.Builder
	for fi, hunks := range m.marks {
		for hi := range hunks {
			if hi == wholeFile {
				continue
			}
			for _, l := range m.files[fi].Hunks[hi].Lines {
				b.WriteString(l.Text)
			}
		}
	}
	return b.String()
}

func TestLiveReloadKeepsUnchangedMarkAndCursor(t *testing.T) {
	base := lines(60)
	edited := replaceLine(base, 5, "FIRST")
	edited = replaceLine(edited, 30, "SECOND")

	m, repo := gitModel(t, map[string]string{"a.txt": base}, map[string]string{"a.txt": edited})

	// Mark the first hunk and leave the cursor on it.
	m.moveTo(m.view.HunkRows[0] + 1)
	m.handleKey(keyPress(" "))

	// The AI edits an unrelated part of the file: the first hunk is untouched.
	writeWorking(t, repo.Dir, "a.txt", replaceLine(edited, 50, "THIRD"))
	m.liveReload()

	if hunks, _ := m.marks.total(); hunks != 1 {
		t.Fatalf("after an unrelated edit, %d hunks marked, want 1", hunks)
	}
	if got := markedText(m); !strings.Contains(got, "FIRST") {
		t.Errorf("the wrong hunk stayed marked: %q", got)
	}
	if strings.Contains(m.msg, "reset") {
		t.Errorf("an unrelated edit should not reset marks, got msg %q", m.msg)
	}
	// The cursor is still on the file, on a hunk.
	if p := m.files[m.view.Rows[m.cur].FileIdx].Path(); p != "a.txt" {
		t.Errorf("cursor drifted to %q", p)
	}
	if _, hunk := m.currentTarget(); hunk == wholeFile {
		t.Error("cursor no longer on a hunk after reload")
	}
}

func TestLiveReloadResetsEditedMark(t *testing.T) {
	base := lines(60)
	edited := replaceLine(base, 5, "FIRST")
	edited = replaceLine(edited, 30, "SECOND")

	m, repo := gitModel(t, map[string]string{"a.txt": base}, map[string]string{"a.txt": edited})

	// Mark the first hunk.
	m.moveTo(m.view.HunkRows[0] + 1)
	m.handleKey(keyPress(" "))

	// The AI re-edits that same region: the marked hunk's content changes.
	changed := replaceLine(base, 5, "FIRST-REWRITTEN-AGAIN")
	changed = replaceLine(changed, 30, "SECOND")
	writeWorking(t, repo.Dir, "a.txt", changed)
	m.liveReload()

	if hunks, _ := m.marks.total(); hunks != 0 {
		t.Fatalf("an edited hunk kept its mark: %d marked", hunks)
	}
	if !strings.Contains(m.msg, "reset") {
		t.Errorf("editing a marked hunk should report a reset, got %q", m.msg)
	}
}

func TestLiveReloadWholeFileMarkStaysWhole(t *testing.T) {
	base := lines(60)
	edited := replaceLine(base, 5, "FIRST")
	edited = replaceLine(edited, 30, "SECOND")

	m, repo := gitModel(t, map[string]string{"a.txt": base}, map[string]string{"a.txt": edited})

	// Mark the whole file (two hunks).
	m.handleKey(keyPress("A"))
	if got := m.marks.inFile(0); got != 2 {
		t.Fatalf("A marked %d hunks, want 2", got)
	}

	// The AI adds a third change: a whole-file mark should adopt it too.
	more := replaceLine(edited, 50, "THIRD")
	writeWorking(t, repo.Dir, "a.txt", more)
	m.liveReload()

	if len(m.files) != 1 || len(m.files[0].Hunks) != 3 {
		t.Fatalf("expected one file with three hunks after the edit, got %v", m.files)
	}
	if got := m.marks.inFile(0); got != 3 {
		t.Errorf("whole-file mark covered %d of 3 hunks after a new edit", got)
	}
}

func TestWatcherSignalsOnChange(t *testing.T) {
	dir := t.TempDir()
	w, err := newWatcher(dir)
	if err != nil {
		t.Skip("fsnotify unavailable:", err)
	}
	defer w.Close()

	writeWorking(t, dir, "x.txt", "hello\n")
	writeWorking(t, dir, "y.txt", "world\n")

	select {
	case <-w.dirty:
		// A burst of writes coalesced into at least one dirty signal.
	case <-time.After(3 * time.Second):
		t.Fatal("watcher never reported the change")
	}
}

// A clean tree is a valid place to open hunk: it shows what it is waiting for
// and fills in once the tree is edited.
func TestEmptyTreeRendersWaitingThenFillsIn(t *testing.T) {
	base := lines(20)
	m, repo := gitModel(t, map[string]string{"a.txt": base}, nil)

	if got := m.renderScreen(); !strings.Contains(got, "watching for edits") {
		t.Fatalf("a clean tree should say what it is waiting for, got:\n%s", got)
	}

	writeWorking(t, repo.Dir, "a.txt", replaceLine(base, 5, "EDIT"))
	m.liveReload()

	if len(m.view.Rows) == 0 {
		t.Fatal("the first edit should populate the empty viewer")
	}
	if got := m.renderScreen(); !strings.Contains(got, "EDIT") {
		t.Errorf("the new edit is not on screen:\n%s", got)
	}
}

// A reload used to yank the cursor back to the top of its hunk, which made
// scrolling through a long change impossible while an agent kept writing: every
// line you scrolled down was undone by the next reload.
func TestLiveReloadKeepsTheReadingPosition(t *testing.T) {
	base := lines(200)
	edited := base
	for i := 10; i < 60; i++ {
		edited = replaceLine(edited, i, "CHANGED")
	}
	m, repo := gitModel(t, map[string]string{"a.txt": base, "b.txt": base},
		map[string]string{"a.txt": edited, "b.txt": replaceLine(base, 5, "X")})

	for i := 0; i < 40; i++ {
		m.handleKey(keyPress("j"))
	}
	cur, top := m.cur, m.top
	if cur == 0 || top == 0 {
		t.Fatalf("the fixture did not scroll: cur=%d top=%d", cur, top)
	}

	// Something else in the tree changes, which is what live-follow reacts to.
	writeWorking(t, repo.Dir, "b.txt", replaceLine(base, 90, "Y"))
	m.liveReload()

	if m.cur != cur || m.top != top {
		t.Errorf("reload moved the view to cur=%d top=%d, want cur=%d top=%d",
			m.cur, m.top, cur, top)
	}
}
