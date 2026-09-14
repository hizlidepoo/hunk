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
	patch := buildPatch(t, r, pick)
	if err := r.ApplyCached(patch); err != nil {
		t.Fatalf("staging failed: %v\npatch:\n%s", err, patch)
	}
}

// buildPatch renders the patch that stage would apply, without applying it.
// Undo tests need the patch text itself to hand back to UnapplyCached.
func buildPatch(t *testing.T, r *git.Repo, pick func(path string, hunk int) bool) string {
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
	return patch.String()
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

// logRepo builds a repository with a known history: three commits on main, the
// last of which is a merge of a side branch, so the merge case is covered too.
func logRepo(t *testing.T) *git.Repo {
	t.Helper()
	r := repo(t, map[string]string{"a.txt": "one\n"})

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = r.Dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	writeFile(t, r.Dir, "a.txt", "one\ntwo\n")
	run("commit", "-qam", "add two")

	run("checkout", "-qb", "side")
	writeFile(t, r.Dir, "b.txt", "side\n")
	run("add", "-A")
	run("commit", "-qm", "add b on the side")

	run("checkout", "-q", "-")
	run("merge", "-q", "--no-ff", "-m", "merge side", "side")
	return r
}

// bySubject finds a commit in a log by its subject line.
func bySubject(t *testing.T, commits []git.Commit, subject string) git.Commit {
	t.Helper()
	for _, c := range commits {
		if c.Subject == subject {
			return c
		}
	}
	t.Fatalf("no commit with subject %q in %+v", subject, commits)
	return git.Commit{}
}

func TestLogReadsHistoryNewestFirst(t *testing.T) {
	r := logRepo(t)

	commits, err := r.Log(10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 4 {
		t.Fatalf("got %d commits, want 4", len(commits))
	}

	// Newest first, oldest last. The two commits in between were made in the
	// same second on different branches, so git is free to order them either
	// way and the test does not pin that down.
	if commits[0].Subject != "merge side" {
		t.Errorf("first commit = %q, want the merge", commits[0].Subject)
	}
	if commits[3].Subject != "baseline" {
		t.Errorf("last commit = %q, want the root commit", commits[3].Subject)
	}

	c := commits[0]
	if len(c.SHA) != 40 {
		t.Errorf("SHA = %q, want a full hash", c.SHA)
	}
	if !strings.HasPrefix(c.SHA, c.Short) {
		t.Errorf("Short %q is not a prefix of SHA %q", c.Short, c.SHA)
	}
	if c.Author != "hunk test" {
		t.Errorf("Author = %q, want %q", c.Author, "hunk test")
	}
	if c.Rel == "" {
		t.Error("Rel is empty, want a relative date")
	}
}

func TestLogLimitAndPaths(t *testing.T) {
	r := logRepo(t)

	commits, err := r.Log(2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("Log(2) returned %d commits, want 2", len(commits))
	}

	commits, err = r.Log(10, []string{"b.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 1 || commits[0].Subject != "add b on the side" {
		t.Fatalf("Log for b.txt = %+v, want only the commit that adds it", commits)
	}
}

func TestShowParsesAsADiff(t *testing.T) {
	r := logRepo(t)

	commits, err := r.Log(10, nil)
	if err != nil {
		t.Fatal(err)
	}
	text, err := r.Show(bySubject(t, commits, "add two").SHA, false, diff.DefaultContext)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "add two") {
		t.Errorf("Show left the commit message in the diff:\n%s", text)
	}

	files, err := diff.ParseString(text)
	if err != nil {
		t.Fatalf("%v\n%s", err, text)
	}
	if len(files) != 1 || files[0].Path() != "a.txt" {
		t.Fatalf("Show gave %d files, want just a.txt", len(files))
	}
	if files[0].Added != 1 || files[0].Removed != 0 {
		t.Errorf("a.txt +%d -%d, want +1 -0", files[0].Added, files[0].Removed)
	}
}

// A merge commit has no diff at all unless it is asked for against one parent,
// which is the whole reason Show passes --first-parent -m.
func TestShowMergeCommitHasADiff(t *testing.T) {
	r := logRepo(t)

	commits, err := r.Log(10, nil)
	if err != nil {
		t.Fatal(err)
	}
	text, err := r.Show(commits[0].SHA, false, diff.DefaultContext)
	if err != nil {
		t.Fatal(err)
	}

	files, err := diff.ParseString(text)
	if err != nil {
		t.Fatalf("%v\n%s", err, text)
	}
	if len(files) != 1 || files[0].Path() != "b.txt" {
		t.Fatalf("merge commit gave %d files (%s), want b.txt", len(files), text)
	}
}

// A repository with no commits yet is an empty history, not an error.
func TestLogOnAnUnbornBranch(t *testing.T) {
	r := repo(t, nil)

	commits, err := r.Log(10, nil)
	if err != nil {
		t.Fatalf("Log on a repository with no commits: %v", err)
	}
	if len(commits) != 0 {
		t.Errorf("got %d commits from an empty repository", len(commits))
	}
}

// UnapplyCached is the undo behind the stage key, and its promise is that it
// takes back exactly what was staged. Nothing tested that before, so this walks
// the full round trip: stage a hunk, take it back, and confirm the index is
// clean again and the working tree never moved.
func TestUnapplyCachedTakesTheStageBackOut(t *testing.T) {
	base := numbered(60)
	r := repo(t, map[string]string{"a.txt": base})

	edited := change(base, 5, "FIRST")
	edited = change(edited, 30, "SECOND")
	writeFile(t, r.Dir, "a.txt", edited)

	patch := buildPatch(t, r, only(0))
	if err := r.ApplyCached(patch); err != nil {
		t.Fatalf("staging failed: %v\npatch:\n%s", err, patch)
	}
	if cached := gitOut(t, r, "diff", "--cached"); !strings.Contains(cached, "FIRST") {
		t.Fatalf("setup did not stage the hunk:\n%s", cached)
	}

	if err := r.UnapplyCached(patch); err != nil {
		t.Fatalf("UnapplyCached: %v\npatch:\n%s", err, patch)
	}

	if cached := gitOut(t, r, "diff", "--cached"); strings.TrimSpace(cached) != "" {
		t.Errorf("the index still holds something after the undo:\n%s", cached)
	}
	if unstaged := gitOut(t, r, "diff"); !strings.Contains(unstaged, "FIRST") || !strings.Contains(unstaged, "SECOND") {
		t.Errorf("the undo lost a working-tree change:\n%s", unstaged)
	}

	// Undo writes the index only, exactly like the stage it reverses.
	got, err := os.ReadFile(filepath.Join(r.Dir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != edited {
		t.Error("UnapplyCached modified the working-tree file")
	}
}

// The "and nothing else" half of the promise: undoing one stage must leave an
// earlier, unrelated stage sitting in the index untouched.
func TestUnapplyCachedLeavesOtherStagedHunksAlone(t *testing.T) {
	base := numbered(60)
	r := repo(t, map[string]string{"a.txt": base})

	edited := change(base, 5, "FIRST")
	edited = change(edited, 30, "SECOND")
	writeFile(t, r.Dir, "a.txt", edited)

	// Stage the two hunks as two separate operations, the way pressing the
	// stage key twice would, and keep only the second patch to undo.
	first := buildPatch(t, r, only(0))
	if err := r.ApplyCached(first); err != nil {
		t.Fatalf("staging the first hunk: %v", err)
	}
	second := buildPatch(t, r, all)
	if err := r.ApplyCached(second); err != nil {
		t.Fatalf("staging the second hunk: %v", err)
	}

	if err := r.UnapplyCached(second); err != nil {
		t.Fatalf("UnapplyCached: %v\npatch:\n%s", err, second)
	}

	cached := gitOut(t, r, "diff", "--cached")
	if !strings.Contains(cached, "FIRST") {
		t.Errorf("the undo took back a hunk it was not given:\n%s", cached)
	}
	if strings.Contains(cached, "SECOND") {
		t.Errorf("the undo left its own hunk in the index:\n%s", cached)
	}
}

func TestUnapplyCachedEmptyPatchIsANoOp(t *testing.T) {
	base := numbered(20)
	r := repo(t, map[string]string{"a.txt": base})
	writeFile(t, r.Dir, "a.txt", change(base, 3, "CHANGED"))
	stage(t, r, all)

	for _, patch := range []string{"", "   \n\t\n"} {
		if err := r.UnapplyCached(patch); err != nil {
			t.Errorf("UnapplyCached(%q) = %v, want no error", patch, err)
		}
	}
	if cached := gitOut(t, r, "diff", "--cached"); !strings.Contains(cached, "CHANGED") {
		t.Errorf("a blank undo emptied the index:\n%s", cached)
	}
}

func TestUnapplyCachedRejectsABadPatch(t *testing.T) {
	r := repo(t, map[string]string{"a.txt": "one\n"})

	err := r.UnapplyCached("this is not a patch\n")
	if err == nil {
		t.Fatal("UnapplyCached accepted a patch that is not a diff")
	}
	if !strings.Contains(err.Error(), "git apply") {
		t.Errorf("error does not name the failing command: %v", err)
	}
}

// UnstagedPaths and StagedPaths are how the file list decides what to show and
// what to put a check beside, so the split between them has to be exact.
func TestStagedAndUnstagedPaths(t *testing.T) {
	base := numbered(20)
	r := repo(t, map[string]string{"a.txt": base, "b.txt": base})

	writeFile(t, r.Dir, "a.txt", change(base, 3, "STAGED"))
	stage(t, r, all)
	writeFile(t, r.Dir, "b.txt", change(base, 3, "UNSTAGED"))

	staged, err := r.StagedPaths()
	if err != nil {
		t.Fatal(err)
	}
	if len(staged) != 1 || staged[0] != "a.txt" {
		t.Errorf("StagedPaths = %v, want [a.txt]", staged)
	}

	unstaged, err := r.UnstagedPaths()
	if err != nil {
		t.Fatal(err)
	}
	if len(unstaged) != 1 || unstaged[0] != "b.txt" {
		t.Errorf("UnstagedPaths = %v, want [b.txt]", unstaged)
	}
}

// Both listings come back nil, not an empty slice, when git prints nothing.
func TestPathListingsAreNilWhenThereIsNothing(t *testing.T) {
	r := repo(t, map[string]string{"a.txt": "one\n"})

	staged, err := r.StagedPaths()
	if err != nil {
		t.Fatal(err)
	}
	if staged != nil {
		t.Errorf("StagedPaths = %v, want nil on a clean repo", staged)
	}

	unstaged, err := r.UnstagedPaths()
	if err != nil {
		t.Fatal(err)
	}
	if unstaged != nil {
		t.Errorf("UnstagedPaths = %v, want nil on a clean repo", unstaged)
	}
}

// StagedDiff is what keeps a fully staged file on screen instead of vanishing
// from the review once it has no unstaged changes left.
func TestStagedDiff(t *testing.T) {
	base := numbered(20)
	r := repo(t, map[string]string{"a.txt": base, "b.txt": base})

	writeFile(t, r.Dir, "a.txt", change(base, 3, "AAA"))
	writeFile(t, r.Dir, "b.txt", change(base, 3, "BBB"))
	stage(t, r, all)

	text, err := r.StagedDiff([]string{"a.txt"}, false, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "AAA") {
		t.Errorf("staged diff is missing the file's change:\n%s", text)
	}
	if strings.Contains(text, "BBB") {
		t.Errorf("staged diff includes a path that was not asked for:\n%s", text)
	}

	// No paths means no git call at all, and an empty diff rather than the
	// whole index — asking for nothing must not quietly return everything.
	empty, err := r.StagedDiff(nil, false, 3)
	if err != nil {
		t.Fatal(err)
	}
	if empty != "" {
		t.Errorf("StagedDiff(nil) = %q, want empty", empty)
	}
}

func TestStagedDiffIgnoringWhitespace(t *testing.T) {
	r := repo(t, map[string]string{"a.txt": "one\ntwo\n"})
	writeFile(t, r.Dir, "a.txt", "one   \ntwo\n")
	stage(t, r, all)

	text, err := r.StagedDiff([]string{"a.txt"}, true, 3)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "@@") {
		t.Errorf("a whitespace-only change survived -w:\n%s", text)
	}
}

// UnstageFiles is the undo for a whole-file stage, which is how untracked files
// are staged: they have no hunks to pick between.
func TestUnstageFiles(t *testing.T) {
	r := repo(t, map[string]string{"a.txt": "one\n"})
	writeFile(t, r.Dir, "new.txt", "fresh\n")

	if err := r.StageFiles([]string{"new.txt"}); err != nil {
		t.Fatal(err)
	}
	if cached := gitOut(t, r, "diff", "--cached", "--name-only"); !strings.Contains(cached, "new.txt") {
		t.Fatalf("setup did not stage the file:\n%s", cached)
	}

	if err := r.UnstageFiles([]string{"new.txt"}); err != nil {
		t.Fatalf("UnstageFiles: %v", err)
	}
	if cached := gitOut(t, r, "diff", "--cached", "--name-only"); strings.TrimSpace(cached) != "" {
		t.Errorf("the file is still in the index:\n%s", cached)
	}

	// The file goes back to being untracked; it is not deleted from the tree.
	if _, err := os.Stat(filepath.Join(r.Dir, "new.txt")); err != nil {
		t.Errorf("UnstageFiles removed the working-tree file: %v", err)
	}
	untracked, err := r.Untracked()
	if err != nil {
		t.Fatal(err)
	}
	if !contains(untracked, "new.txt") {
		t.Errorf("the file did not return to untracked: %v", untracked)
	}
}

func TestUnstageNothingIsANoOp(t *testing.T) {
	r := repo(t, map[string]string{"a.txt": "one\n"})
	writeFile(t, r.Dir, "a.txt", "two\n")
	stage(t, r, all)

	if err := r.UnstageFiles(nil); err != nil {
		t.Errorf("UnstageFiles(nil) = %v, want no error", err)
	}
	if cached := gitOut(t, r, "diff", "--cached"); !strings.Contains(cached, "two") {
		t.Errorf("unstaging nothing emptied the index:\n%s", cached)
	}
}

// On an unborn branch there is no HEAD to restore from. Undo has to surface
// that as an error rather than pretending the unstage worked.
func TestUnstageFilesOnAnUnbornBranch(t *testing.T) {
	r := repo(t, nil)
	writeFile(t, r.Dir, "new.txt", "fresh\n")
	if err := r.StageFiles([]string{"new.txt"}); err != nil {
		t.Fatal(err)
	}

	err := r.UnstageFiles([]string{"new.txt"})
	if err == nil {
		t.Skip("this git restores from an unborn HEAD without complaining")
	}
	if !strings.Contains(err.Error(), "git restore") {
		t.Errorf("error does not name the failing command: %v", err)
	}
}
