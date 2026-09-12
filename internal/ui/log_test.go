package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wmarquardt/hunk/internal/diff"
	"github.com/wmarquardt/hunk/internal/git"
	"github.com/wmarquardt/hunk/internal/theme"
)

// logModel builds a model over a throwaway repository with three commits, the
// newest on screen. The history is linear, so the log order is the order the
// commits were made in: add b, add two to a, add a.
func logModel(t *testing.T) (*Model, *git.Repo) {
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

	write("a.txt", "one\n")
	run("add", "-A")
	run("commit", "-qm", "add a")

	write("a.txt", "one\ntwo\n")
	run("commit", "-qam", "add two to a")

	write("b.txt", "hello\n")
	run("add", "-A")
	run("commit", "-qm", "add b")

	repo := &git.Repo{Dir: dir}
	commits, err := repo.Log(10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 3 {
		t.Fatalf("got %d commits, want 3", len(commits))
	}
	text, err := repo.Show(commits[0].SHA, false, diff.DefaultContext)
	if err != nil {
		t.Fatal(err)
	}
	files, err := diff.ParseString(text)
	if err != nil {
		t.Fatalf("%v\n%s", err, text)
	}
	return NewLog(repo, commits, files, theme.Default(), Options{}), repo
}

// gitState is everything about a repository hunk must not change while reading
// history: the working tree and the index.
func gitState(t *testing.T, r *git.Repo) string {
	t.Helper()
	var out []string
	for _, args := range [][]string{{"status", "--porcelain"}, {"diff", "--cached"}, {"diff"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = r.Dir
		b, err := cmd.Output()
		if err != nil {
			t.Fatalf("git %s: %v", strings.Join(args, " "), err)
		}
		out = append(out, string(b))
	}
	return strings.Join(out, "\x00")
}

func TestLogWalksCommits(t *testing.T) {
	m, _ := logModel(t)
	screen(t, m, 140, 24)

	if m.commitIdx != 0 || m.files[0].Path() != "b.txt" {
		t.Fatalf("opened on commit %d showing %s, want the newest commit and b.txt",
			m.commitIdx, m.files[0].Path())
	}

	// } walks back in time, the direction git log prints.
	m.command("}")
	if m.commitIdx != 1 {
		t.Fatalf("} left commitIdx at %d, want 1", m.commitIdx)
	}
	if m.files[0].Path() != "a.txt" || m.files[0].Added != 1 {
		t.Fatalf("older commit shows %s +%d, want a.txt +1",
			m.files[0].Path(), m.files[0].Added)
	}

	// { comes back to the newer one.
	m.command("{")
	if m.commitIdx != 0 || m.files[0].Path() != "b.txt" {
		t.Fatalf("{ gave commit %d showing %s, want 0 and b.txt", m.commitIdx, m.files[0].Path())
	}
}

func TestLogClampsAtBothEnds(t *testing.T) {
	m, _ := logModel(t)
	screen(t, m, 140, 24)

	for i := 0; i < 5; i++ {
		m.command("{")
	}
	if m.commitIdx != 0 {
		t.Errorf("{ past the newest commit landed on %d, want 0", m.commitIdx)
	}

	for i := 0; i < 5; i++ {
		m.command("}")
	}
	if m.commitIdx != len(m.commits)-1 {
		t.Errorf("} past the oldest commit landed on %d, want %d", m.commitIdx, len(m.commits)-1)
	}
	if len(m.view.Rows) == 0 {
		t.Error("the oldest commit rendered nothing")
	}
}

// Reading history never writes. The mark and stage keys are not bound at all in
// log mode, and pressing them must leave the tree and the index byte-identical.
func TestLogIsReadOnly(t *testing.T) {
	m, repo := logModel(t)
	screen(t, m, 140, 24)

	if m.staging() {
		t.Fatal("log mode reports itself as able to stage")
	}

	before := gitState(t, repo)
	for _, key := range []string{"space", "a", "d", "A", "D", "w", "u"} {
		m.command(key)
	}
	if after := gitState(t, repo); after != before {
		t.Errorf("the repository changed after pressing the mark and stage keys:\n%q\n%q", before, after)
	}
	if hunks, _ := m.marks.total(); hunks != 0 {
		t.Errorf("%d hunks got marked in log mode", hunks)
	}
}

func TestLogSidebarStacksCommitsOverFiles(t *testing.T) {
	m, _ := logModel(t)
	lines := screen(t, m, 140, 24)
	out := strings.Join(lines, "\n")

	if !strings.Contains(out, "Commits") || !strings.Contains(out, "Files") {
		t.Fatalf("sidebar is missing one of its two headers:\n%s", out)
	}
	for _, c := range m.commits {
		if !strings.Contains(out, c.Short) {
			t.Errorf("commit %s (%s) is not listed in the sidebar", c.Short, c.Subject)
		}
	}
	if !strings.Contains(out, "add b") {
		t.Error("commit subjects are not shown in the sidebar")
	}
	if !strings.Contains(out, "b.txt") {
		t.Error("the selected commit's files are not shown in the sidebar")
	}
}

func TestLogStatusNamesTheCommit(t *testing.T) {
	m, _ := logModel(t)
	status := screen(t, m, 140, 24)[23]

	for _, want := range []string{m.commits[0].Short, "commit 1/3", "}/{ commit"} {
		if !strings.Contains(status, want) {
			t.Errorf("status bar is missing %q:\n%s", want, status)
		}
	}
	for _, gone := range []string{"w stage", "f follow"} {
		if strings.Contains(status, gone) {
			t.Errorf("status bar offers %q while reading history:\n%s", gone, status)
		}
	}
}

func TestLogHelpListsCommitKeys(t *testing.T) {
	m, _ := logModel(t)
	screen(t, m, 140, 24)
	m.command("?")
	out := strings.Join(screen(t, m, 140, 24), "\n")

	if !strings.Contains(out, "older / newer commit") {
		t.Errorf("help does not mention commit navigation:\n%s", out)
	}
	if strings.Contains(out, "stage what is marked") {
		t.Errorf("help offers staging while reading history:\n%s", out)
	}
}

// + / - and i re-run git show for the commit on screen, the same way they
// re-run git diff in review mode.
func TestLogReSourcesForContextAndWhitespace(t *testing.T) {
	m, _ := logModel(t)
	screen(t, m, 140, 24)
	m.command("}") // "add two to a" has a line of context to widen

	before := len(m.view.Rows)
	m.command("+")
	if m.context != diff.DefaultContext+1 {
		t.Fatalf("+ left the context at %d, want %d", m.context, diff.DefaultContext+1)
	}
	if len(m.view.Rows) < before {
		t.Errorf("widening the context shrank the view from %d to %d rows", before, len(m.view.Rows))
	}

	m.command("i")
	if !m.ignoreWS {
		t.Error("i did not turn whitespace-ignoring on in log mode")
	}
	if len(m.view.Rows) == 0 {
		t.Error("re-sourcing with -w emptied the view")
	}
}

func TestLogClickSelectsACommit(t *testing.T) {
	m, _ := logModel(t)
	screen(t, m, 140, 24)

	// Row 0 is the "Commits" header, so row 2 is the second commit listed.
	m = clickAt(t, m, 2, 2)
	if m.commitIdx != 1 {
		t.Fatalf("clicking the second commit row gave commitIdx %d, want 1", m.commitIdx)
	}
	if m.files[0].Path() != "a.txt" {
		t.Errorf("the click did not load the commit's diff: showing %s", m.files[0].Path())
	}
}

// An empty commit is a dead end unless the screen still says which commit it is
// and how to leave it.
func TestLogEmptyCommitStaysNavigable(t *testing.T) {
	m, repo := logModel(t)

	cmd := exec.Command("git", "commit", "-q", "--allow-empty", "-m", "nothing at all")
	cmd.Dir = repo.Dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit --allow-empty: %v\n%s", err, out)
	}
	commits, err := repo.Log(10, nil)
	if err != nil {
		t.Fatal(err)
	}
	m.commits, m.commitIdx = commits, 1
	m.loadCommit(0)

	if len(m.files) != 0 {
		t.Fatalf("an empty commit produced %d files", len(m.files))
	}
	out := strings.Join(screen(t, m, 140, 24), "\n")
	if !strings.Contains(out, commits[0].Short) || !strings.Contains(out, "}/{ commit") {
		t.Errorf("an empty commit leaves nothing to navigate by:\n%s", out)
	}

	// And } still walks off it.
	m.command("}")
	if m.commitIdx != 1 || len(m.files) == 0 {
		t.Errorf("} off an empty commit landed on %d with %d files", m.commitIdx, len(m.files))
	}
}

// Nothing in history mode follows the working tree: the past does not change.
func TestLogDoesNotFollowTheWorkingTree(t *testing.T) {
	m, _ := logModel(t)
	if m.watch != nil || m.live {
		t.Fatal("history mode started a working-tree watcher")
	}
	m.command("f")
	if m.live {
		t.Error("f started following in history mode")
	}
}
