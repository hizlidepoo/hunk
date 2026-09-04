package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/wmarquardt/hunk/internal/diff"
	"github.com/wmarquardt/hunk/internal/git"
	"github.com/wmarquardt/hunk/internal/theme"
)

func TestMarksBookkeeping(t *testing.T) {
	m := marks{}

	m.set(0, 1, true)
	m.set(0, 2, true)
	m.set(3, 0, true)

	if !m.has(0, 1) || !m.has(3, 0) {
		t.Error("marks did not stick")
	}
	if m.has(0, 5) || m.has(9, 0) {
		t.Error("marks reports hunks that were never marked")
	}
	if hunks, files := m.total(); hunks != 3 || files != 2 {
		t.Errorf("total() = %d hunks in %d files, want 3 in 2", hunks, files)
	}

	m.set(0, 1, false)
	m.set(0, 2, false)
	if _, ok := m[0]; ok {
		t.Error("a file with no marks left should be dropped, not kept empty")
	}
	if hunks, files := m.total(); hunks != 1 || files != 1 {
		t.Errorf("total() = %d hunks in %d files, want 1 in 1", hunks, files)
	}
}

func TestFileState(t *testing.T) {
	f := diff.File{Hunks: make([]diff.Hunk, 3)}
	binary := diff.File{IsBinary: true}

	m := marks{}
	if got := m.state(0, f); got != fileUnmarked {
		t.Errorf("no marks: got %v, want unmarked", got)
	}

	m.set(0, 0, true)
	if got := m.state(0, f); got != filePartial {
		t.Errorf("one of three marked: got %v, want partial", got)
	}

	m.set(0, 1, true)
	m.set(0, 2, true)
	if got := m.state(0, f); got != fileAllMarked {
		t.Errorf("all marked: got %v, want fully marked", got)
	}

	m.set(1, wholeFile, true)
	if got := m.state(1, binary); got != fileAllMarked {
		t.Errorf("a marked hunkless file: got %v, want fully marked", got)
	}
}

// gitModel builds a model over a throwaway repo with the given working-tree
// edits already applied.
func gitModel(t *testing.T, committed, edited map[string]string) (*Model, *git.Repo) {
	t.Helper()
	if !git.Available() {
		t.Skip("git is not on PATH")
	}

	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "hunk test")
	for name, content := range committed {
		write(name, content)
	}
	run("add", "-A")
	run("commit", "-qm", "baseline")

	for name, content := range edited {
		write(name, content)
	}

	repo := &git.Repo{Dir: dir}
	text, err := GitSource(repo)
	if err != nil {
		t.Fatal(err)
	}
	files, err := diff.ParseString(text)
	if err != nil {
		t.Fatalf("%v\n%s", err, text)
	}

	m := NewGit(repo, files, theme.Default())
	u, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 24})
	return u.(*Model), repo
}

func gitOut(t *testing.T, r *git.Repo, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func lines(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		b.WriteString("line ")
		b.WriteString(strconvItoa(i))
		b.WriteString("\n")
	}
	return b.String()
}

func strconvItoa(n int) string {
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

func replaceLine(content string, line int, to string) string {
	parts := strings.Split(content, "\n")
	parts[line-1] = to
	return strings.Join(parts, "\n")
}

func TestGitSourceIncludesUntrackedFiles(t *testing.T) {
	m, _ := gitModel(t,
		map[string]string{"tracked.txt": lines(10)},
		map[string]string{"tracked.txt": replaceLine(lines(10), 3, "CHANGED"), "brand-new.txt": "hello\n"},
	)

	var paths []string
	for _, f := range m.files {
		paths = append(paths, f.Path())
	}
	if !strings.Contains(strings.Join(paths, " "), "brand-new.txt") {
		t.Errorf("untracked file missing from review: %v", paths)
	}
	for _, f := range m.files {
		if f.Path() == "brand-new.txt" && !f.IsNew {
			t.Error("untracked file should be shown as a new file")
		}
	}
}

func TestStagingOneMarkedHunk(t *testing.T) {
	base := lines(60)
	edited := replaceLine(base, 5, "FIRST")
	edited = replaceLine(edited, 30, "SECOND")

	m, repo := gitModel(t, map[string]string{"a.txt": base}, map[string]string{"a.txt": edited})

	if len(m.files) != 1 || len(m.files[0].Hunks) != 2 {
		t.Fatalf("expected one file with two hunks, got %d files", len(m.files))
	}

	// Land on the second hunk and mark it.
	m.moveTo(m.view.HunkRows[1])
	m.handleKey(keyPress(" "))
	if hunks, _ := m.marks.total(); hunks != 1 {
		t.Fatalf("space marked %d hunks, want 1", hunks)
	}

	// w stages immediately — no confirmation step.
	m.handleKey(keyPress("w"))
	if !strings.Contains(m.msg, "staged 1 hunk") {
		t.Errorf("stage message = %q", m.msg)
	}

	cached := gitOut(t, repo, "diff", "--cached")
	if !strings.Contains(cached, "SECOND") {
		t.Errorf("marked hunk was not staged:\n%s", cached)
	}
	if strings.Contains(cached, "FIRST") {
		t.Errorf("an unmarked hunk was staged:\n%s", cached)
	}
	if unstaged := gitOut(t, repo, "diff"); !strings.Contains(unstaged, "FIRST") {
		t.Errorf("the unmarked hunk should still be unstaged:\n%s", unstaged)
	}

	// The working tree is untouched: staging only ever writes the index.
	got, err := os.ReadFile(filepath.Join(repo.Dir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != edited {
		t.Error("staging modified the working-tree file")
	}
}

func TestStagingReloadsAndClearsMarks(t *testing.T) {
	base := lines(60)
	edited := replaceLine(base, 5, "FIRST")
	edited = replaceLine(edited, 30, "SECOND")

	m, _ := gitModel(t, map[string]string{"a.txt": base}, map[string]string{"a.txt": edited})

	m.moveTo(m.view.HunkRows[0])
	m.handleKey(keyPress(" "))
	m.handleKey(keyPress("w"))

	if hunks, _ := m.marks.total(); hunks != 0 {
		t.Errorf("%d marks survived staging", hunks)
	}
	if len(m.files) != 1 || len(m.files[0].Hunks) != 1 {
		t.Errorf("after staging one of two hunks, one should remain; got %d files", len(m.files))
	}
	if !strings.Contains(m.msg, "u to undo") {
		t.Errorf("the result should mention undo, got %q", m.msg)
	}
}

func TestFullyStagedFileStaysWithGreenCheck(t *testing.T) {
	base := lines(20)
	m, _ := gitModel(t,
		map[string]string{"a.txt": base},
		map[string]string{"a.txt": replaceLine(base, 3, "CHANGED")},
	)

	// Approve the whole file, then stage it.
	m.handleKey(keyPress("A"))
	m.handleKey(keyPress("w"))

	// The file has no unstaged changes left, yet it must not vanish.
	if len(m.files) != 1 || m.files[0].Path() != "a.txt" {
		t.Fatalf("fully staged file dropped off the list: %v", m.files)
	}
	if !m.staged["a.txt"] || m.unstaged["a.txt"] {
		t.Fatalf("git state wrong: staged=%v unstaged=%v", m.staged, m.unstaged)
	}
	symbol, style := m.fileGlyph(0, m.files[0], m.st.sidebar)
	if symbol != "✓" {
		t.Errorf("glyph = %q, want a check", symbol)
	}
	if style.GetForeground() != m.st.stagedFg {
		t.Errorf("a fully staged file's check should be green")
	}
}

func TestPartiallyStagedFileShowsGrayCheck(t *testing.T) {
	base := lines(60)
	edited := replaceLine(base, 5, "FIRST")
	edited = replaceLine(edited, 30, "SECOND")

	m, _ := gitModel(t, map[string]string{"a.txt": base}, map[string]string{"a.txt": edited})

	// Stage only the first of two hunks.
	m.moveTo(m.view.HunkRows[0])
	m.handleKey(keyPress(" "))
	m.handleKey(keyPress("w"))

	if !m.staged["a.txt"] || !m.unstaged["a.txt"] {
		t.Fatalf("expected both staged and unstaged content: staged=%v unstaged=%v", m.staged, m.unstaged)
	}
	symbol, style := m.fileGlyph(0, m.files[0], m.st.sidebar)
	if symbol != "✓" {
		t.Errorf("glyph = %q, want a check", symbol)
	}
	if style.GetForeground() != m.st.partialFg {
		t.Errorf("a partially staged file's check should be gray, not green")
	}
}

func TestMarkedHunkShowsGreenRail(t *testing.T) {
	base := lines(30)
	edited := replaceLine(base, 5, "FIRST")
	edited = replaceLine(edited, 25, "SECOND")

	m, _ := gitModel(t, map[string]string{"a.txt": base}, map[string]string{"a.txt": edited})

	// Mark the first hunk, then move the cursor onto the second one.
	m.moveTo(m.view.HunkRows[0] + 1)
	m.handleKey(keyPress(" "))
	m.moveTo(m.view.HunkRows[1] + 1)

	// A body row inside the marked (now non-current) hunk gets the green bar.
	row := m.view.HunkRows[0] + 1
	if row == m.cur {
		t.Fatal("test setup put the cursor on the row under test")
	}
	hlo, hhi := m.currentHunkSpan()
	if got := m.railMark(row, hlo, hhi); got != m.st.railMarked.Render("▌") {
		t.Errorf("marked hunk row rail = %q, want the green marked bar", got)
	}
}

func TestUndoUnstagesTheLastStage(t *testing.T) {
	base := lines(60)
	edited := replaceLine(base, 5, "FIRST")
	edited = replaceLine(edited, 30, "SECOND")

	m, repo := gitModel(t, map[string]string{"a.txt": base}, map[string]string{"a.txt": edited})

	// Stage the first hunk, then undo it.
	m.moveTo(m.view.HunkRows[0])
	m.handleKey(keyPress(" "))
	m.handleKey(keyPress("w"))
	if cached := gitOut(t, repo, "diff", "--cached"); !strings.Contains(cached, "FIRST") {
		t.Fatalf("w did not stage the hunk:\n%s", cached)
	}
	if !strings.Contains(m.msg, "u to undo") {
		t.Errorf("stage message should mention undo, got %q", m.msg)
	}

	m.handleKey(keyPress("u"))
	if cached := gitOut(t, repo, "diff", "--cached"); strings.TrimSpace(cached) != "" {
		t.Errorf("undo left something staged:\n%s", cached)
	}
	if !strings.Contains(m.msg, "undone") {
		t.Errorf("undo message = %q", m.msg)
	}
	// Both hunks are unstaged again, so the file shows both.
	if len(m.files) != 1 || len(m.files[0].Hunks) != 2 {
		t.Errorf("after undo the file should have both hunks back; got %v", m.files)
	}

	// A second undo has nothing to reverse.
	m.handleKey(keyPress("u"))
	if !strings.Contains(m.msg, "nothing to undo") {
		t.Errorf("second undo message = %q", m.msg)
	}
}

func TestStagingNothingMarkedAsksForAMark(t *testing.T) {
	base := lines(20)
	m, repo := gitModel(t,
		map[string]string{"a.txt": base},
		map[string]string{"a.txt": replaceLine(base, 3, "CHANGED")},
	)

	m.handleKey(keyPress("w"))
	if !strings.Contains(m.msg, "nothing marked") {
		t.Errorf("message = %q", m.msg)
	}
	if cached := gitOut(t, repo, "diff", "--cached"); strings.TrimSpace(cached) != "" {
		t.Errorf("nothing was marked but the index changed:\n%s", cached)
	}
}

func TestMarkWholeFileKeys(t *testing.T) {
	base := lines(60)
	edited := replaceLine(base, 5, "FIRST")
	edited = replaceLine(edited, 30, "SECOND")

	m, _ := gitModel(t, map[string]string{"a.txt": base}, map[string]string{"a.txt": edited})

	m.handleKey(keyPress("A"))
	if got := m.marks.inFile(0); got != 2 {
		t.Errorf("A marked %d hunks, want both", got)
	}
	if got := m.marks.state(0, m.files[0]); got != fileAllMarked {
		t.Errorf("file state = %v, want fully marked", got)
	}

	m.handleKey(keyPress("D"))
	if hunks, _ := m.marks.total(); hunks != 0 {
		t.Errorf("D left %d marks", hunks)
	}
}

func TestMarkKeysDoNothingWithoutARepo(t *testing.T) {
	m := newTestModel(t, sample)
	screen(t, m, 140, 20)

	for _, k := range []string{" ", "a", "A", "w", "u"} {
		m.handleKey(keyPress(k))
	}
	if m.marks != nil {
		t.Error("a plain diff should not accumulate marks")
	}
}

func TestStatusBarShowsMarkCount(t *testing.T) {
	base := lines(20)
	m, _ := gitModel(t,
		map[string]string{"a.txt": base},
		map[string]string{"a.txt": replaceLine(base, 3, "CHANGED")},
	)

	m.moveTo(m.view.HunkRows[0])
	m.handleKey(keyPress(" "))

	out := strings.Join(screen(t, m, 140, 20), "\n")
	if !strings.Contains(out, "1 hunk marked") {
		t.Errorf("status bar does not show the mark count:\n%s", out)
	}
}
