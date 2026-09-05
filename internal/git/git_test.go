package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wmarquardt/hunk/internal/diff"
	"github.com/wmarquardt/hunk/internal/git"
)

// repo builds a throwaway git repository with one committed file per entry.
func repo(t *testing.T, files map[string]string) *git.Repo {
	t.Helper()
	if !git.Available() {
		t.Skip("git is not on PATH")
	}

	dir := t.TempDir()
	r := &git.Repo{Dir: dir}

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "hunk test")

	for name, content := range files {
		writeFile(t, dir, name, content)
	}
	if len(files) > 0 {
		run("add", "-A")
		run("commit", "-qm", "baseline")
	}
	return r
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// gitOut runs a read-only git command and returns its output.
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

// stage parses the repo's unstaged diff, picks hunks with the given predicate,
// and stages exactly those — the same path the TUI takes when you press w.
func stage(t *testing.T, r *git.Repo, pick func(path string, hunk int) bool) {
	t.Helper()

	text, err := r.Diff(false, 3)
	if err != nil {
		t.Fatal(err)
	}
	files, err := diff.ParseString(text)
	if err != nil {
		t.Fatal(err)
	}

	var patch strings.Builder
	for _, f := range files {
		var selected []int
		for i := range f.Hunks {
			if pick(f.Path(), i) {
				selected = append(selected, i)
			}
		}
		patch.WriteString(f.Patch(selected))
	}

	if err := r.ApplyCached(patch.String()); err != nil {
		t.Fatalf("staging failed: %v\npatch:\n%s", err, patch.String())
	}
}

func all(string, int) bool  { return true }
func none(string, int) bool { return false }

func only(n int) func(string, int) bool {
	return func(_ string, i int) bool { return i == n }
}

// numbered builds a file of n lines, so a change to line k is easy to describe.
func numbered(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		b.WriteString("line ")
		b.WriteString(strings.Repeat("x", 0))
		b.WriteString(itoa(i))
		b.WriteString("\n")
	}
	return b.String()
}

func itoa(n int) string {
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

// change rewrites one 1-based line of a numbered file.
func change(content string, line int, to string) string {
	lines := strings.Split(content, "\n")
	lines[line-1] = to
	return strings.Join(lines, "\n")
}

func TestIsRepo(t *testing.T) {
	r := repo(t, map[string]string{"a.txt": "hello\n"})
	if !r.IsRepo() {
		t.Error("IsRepo() = false inside a git repo")
	}

	outside := &git.Repo{Dir: t.TempDir()}
	if outside.IsRepo() {
		t.Error("IsRepo() = true outside any git repo")
	}
}

func TestDiffSeesWorkingTreeChanges(t *testing.T) {
	base := numbered(30)
	r := repo(t, map[string]string{"a.txt": base})
	writeFile(t, r.Dir, "a.txt", change(base, 5, "CHANGED"))

	text, err := r.Diff(false, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "CHANGED") {
		t.Errorf("diff does not mention the change:\n%s", text)
	}
}

// Staging one hunk of several is the whole point of the mode, and the case most
// likely to go wrong: the hunks after it have shifted line numbers.
func TestStageOneHunkOfThree(t *testing.T) {
	base := numbered(60)
	r := repo(t, map[string]string{"a.txt": base})

	edited := change(base, 5, "FIRST")
	edited = change(edited, 30, "SECOND")
	edited = change(edited, 55, "THIRD")
	writeFile(t, r.Dir, "a.txt", edited)

	for _, tc := range []struct {
		name     string
		hunk     int
		staged   string
		unstaged []string
	}{
		{name: "first", hunk: 0, staged: "FIRST", unstaged: []string{"SECOND", "THIRD"}},
		{name: "middle", hunk: 1, staged: "SECOND", unstaged: []string{"FIRST", "THIRD"}},
		{name: "last", hunk: 2, staged: "THIRD", unstaged: []string{"FIRST", "SECOND"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := repo(t, map[string]string{"a.txt": base})
			writeFile(t, r.Dir, "a.txt", edited)

			stage(t, r, only(tc.hunk))

			cached := gitOut(t, r, "diff", "--cached")
			if !strings.Contains(cached, tc.staged) {
				t.Errorf("index is missing %q:\n%s", tc.staged, cached)
			}
			for _, u := range tc.unstaged {
				if strings.Contains(cached, u) {
					t.Errorf("index wrongly contains %q:\n%s", u, cached)
				}
			}

			unstaged := gitOut(t, r, "diff")
			for _, u := range tc.unstaged {
				if !strings.Contains(unstaged, u) {
					t.Errorf("unstaged diff lost %q:\n%s", u, unstaged)
				}
			}

			// The working tree is never touched by staging.
			got, err := os.ReadFile(filepath.Join(r.Dir, "a.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != edited {
				t.Error("staging modified the working-tree file")
			}
		})
	}
}

func TestStageNothingIsANoOp(t *testing.T) {
	base := numbered(20)
	r := repo(t, map[string]string{"a.txt": base})
	writeFile(t, r.Dir, "a.txt", change(base, 3, "CHANGED"))

	stage(t, r, none)

	if cached := gitOut(t, r, "diff", "--cached"); strings.TrimSpace(cached) != "" {
		t.Errorf("staging nothing wrote to the index:\n%s", cached)
	}
	if unstaged := gitOut(t, r, "diff"); !strings.Contains(unstaged, "CHANGED") {
		t.Error("the change went missing from the working tree")
	}
}

func TestStageAcrossMultipleFiles(t *testing.T) {
	base := numbered(20)
	r := repo(t, map[string]string{"a.txt": base, "b.txt": base})
	writeFile(t, r.Dir, "a.txt", change(base, 3, "IN_A"))
	writeFile(t, r.Dir, "b.txt", change(base, 3, "IN_B"))

	stage(t, r, func(path string, _ int) bool { return path == "a.txt" })

	cached := gitOut(t, r, "diff", "--cached")
	if !strings.Contains(cached, "IN_A") {
		t.Errorf("a.txt was not staged:\n%s", cached)
	}
	if strings.Contains(cached, "IN_B") {
		t.Errorf("b.txt was staged but was not selected:\n%s", cached)
	}
}

func TestStageWithChangesAlreadyInTheIndex(t *testing.T) {
	base := numbered(40)
	r := repo(t, map[string]string{"a.txt": base})

	// Stage one change, then make another on top of it.
	writeFile(t, r.Dir, "a.txt", change(base, 5, "ALREADY_STAGED"))
	if err := r.StageFiles([]string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	staged := change(base, 5, "ALREADY_STAGED")
	writeFile(t, r.Dir, "a.txt", change(staged, 35, "NEW_CHANGE"))

	stage(t, r, all)

	cached := gitOut(t, r, "diff", "--cached")
	for _, want := range []string{"ALREADY_STAGED", "NEW_CHANGE"} {
		if !strings.Contains(cached, want) {
			t.Errorf("index is missing %q:\n%s", want, cached)
		}
	}
	if unstaged := gitOut(t, r, "diff"); strings.TrimSpace(unstaged) != "" {
		t.Errorf("everything was staged, so nothing should be left:\n%s", unstaged)
	}
}

func TestStageDeletedFile(t *testing.T) {
	r := repo(t, map[string]string{"gone.txt": "delete me\n", "keep.txt": "keep\n"})
	if err := os.Remove(filepath.Join(r.Dir, "gone.txt")); err != nil {
		t.Fatal(err)
	}

	stage(t, r, all)

	cached := gitOut(t, r, "diff", "--cached", "--name-status")
	if !strings.HasPrefix(strings.TrimSpace(cached), "D") {
		t.Errorf("deletion was not staged:\n%s", cached)
	}
}

func TestStageFileWithoutTrailingNewline(t *testing.T) {
	r := repo(t, map[string]string{"a.txt": "one\ntwo\nthree"})
	writeFile(t, r.Dir, "a.txt", "one\ntwo\nTHREE")

	stage(t, r, all)

	cached := gitOut(t, r, "diff", "--cached")
	if !strings.Contains(cached, "THREE") {
		t.Errorf("change was not staged:\n%s", cached)
	}
	if unstaged := gitOut(t, r, "diff"); strings.TrimSpace(unstaged) != "" {
		t.Errorf("nothing should be left unstaged:\n%s", unstaged)
	}
}

func TestUntrackedAndStageFiles(t *testing.T) {
	r := repo(t, map[string]string{"a.txt": "hello\n"})
	writeFile(t, r.Dir, "new.txt", "brand new\n")
	writeFile(t, r.Dir, "ignored.txt", "nope\n")
	writeFile(t, r.Dir, ".gitignore", "ignored.txt\n")

	untracked, err := r.Untracked()
	if err != nil {
		t.Fatal(err)
	}
	if !contains(untracked, "new.txt") {
		t.Errorf("untracked = %v, want it to include new.txt", untracked)
	}
	if contains(untracked, "ignored.txt") {
		t.Errorf("untracked = %v, want .gitignore to be honoured", untracked)
	}

	if err := r.StageFiles([]string{"new.txt"}); err != nil {
		t.Fatal(err)
	}
	if cached := gitOut(t, r, "diff", "--cached", "--name-only"); !strings.Contains(cached, "new.txt") {
		t.Errorf("new.txt was not staged:\n%s", cached)
	}
}

func TestStageBinaryFileWholesale(t *testing.T) {
	r := repo(t, map[string]string{"logo.png": "\x00\x01\x02"})
	writeFile(t, r.Dir, "logo.png", "\x00\x01\x02\x03\x04")

	// Binary files have no hunks to choose between, so they are staged whole.
	if err := r.StageFiles([]string{"logo.png"}); err != nil {
		t.Fatal(err)
	}
	if cached := gitOut(t, r, "diff", "--cached", "--name-only"); !strings.Contains(cached, "logo.png") {
		t.Errorf("binary file was not staged:\n%s", cached)
	}
}

func TestApplyCachedRejectsABadPatch(t *testing.T) {
	r := repo(t, map[string]string{"a.txt": "one\ntwo\n"})

	err := r.ApplyCached("diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1,1 +1,1 @@\n-nonexistent line\n+replacement\n")
	if err == nil {
		t.Fatal("want an error for a patch that does not apply")
	}
	if !strings.Contains(err.Error(), "git apply") {
		t.Errorf("error should name the failing command: %v", err)
	}
	if cached := gitOut(t, r, "diff", "--cached"); strings.TrimSpace(cached) != "" {
		t.Errorf("a failed apply left changes in the index:\n%s", cached)
	}
}

func TestApplyCachedEmptyPatchIsANoOp(t *testing.T) {
	r := repo(t, map[string]string{"a.txt": "one\n"})
	if err := r.ApplyCached(""); err != nil {
		t.Errorf("an empty patch should do nothing, got %v", err)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
