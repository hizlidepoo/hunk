// Package git is the thin layer between hunk and the git binary.
//
// ponytail: os/exec over a git library. This is eight commands; go-git is a large
// dependency, and manipulating the index by hand is exactly where a
// reimplementation of git goes wrong. hunk shells out only here — reading a
// diff and file/directory diffing stay git-free.
package git

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Repo is a git repository hunk is reviewing.
type Repo struct {
	// Dir is where git commands run. Empty means the current directory.
	Dir string
}

func (r *Repo) run(stdin string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.String(), nil
}

// IsRepo reports whether we are inside a git working tree.
func (r *Repo) IsRepo() bool {
	out, err := r.run("", "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}

// Diff returns the unstaged changes: the working tree against the index. This
// is the same set of changes "git add -p" walks you through. ignoreWS drops
// whitespace-only changes (git's -w); context is the lines shown around each
// hunk (git's -U).
func (r *Repo) Diff(ignoreWS bool, context int) (string, error) {
	return r.run("", diffArgs(ignoreWS, context, "diff")...)
}

// diffArgs builds a `git diff` argument list with the flags every diff hunk
// uses, plus -w when whitespace is being ignored and -U for the context.
func diffArgs(ignoreWS bool, context int, args ...string) []string {
	args = append(args, "--no-color", "--no-ext-diff", "-U"+strconv.Itoa(context))
	if ignoreWS {
		args = append(args, "-w")
	}
	return args
}

// Untracked lists files git does not know about yet, honouring .gitignore.
func (r *Repo) Untracked() ([]string, error) {
	out, err := r.run("", "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// UnstagedPaths lists files with changes in the working tree that are not yet
// staged — the files Diff would show, by name.
func (r *Repo) UnstagedPaths() ([]string, error) {
	return r.names("diff", "--name-only")
}

// StagedPaths lists files with content staged in the index against HEAD.
func (r *Repo) StagedPaths() ([]string, error) {
	return r.names("diff", "--cached", "--name-only")
}

// StagedDiff returns the staged diff (index against HEAD) for the given paths.
// It is what lets a fully staged file stay on screen with its changes visible.
func (r *Repo) StagedDiff(paths []string, ignoreWS bool, context int) (string, error) {
	if len(paths) == 0 {
		return "", nil
	}
	args := append(diffArgs(ignoreWS, context, "diff", "--cached"), "--")
	return r.run("", append(args, paths...)...)
}

// names runs a git command that prints one path per line and returns them.
func (r *Repo) names(args ...string) ([]string, error) {
	out, err := r.run("", args...)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// Commit is one entry of the history, as the log panel shows it.
type Commit struct {
	SHA     string // full hash, what Show is called with
	Short   string // abbreviated, what the sidebar shows
	Author  string
	Rel     string // relative to now, e.g. "3 days ago"
	Subject string
}

// logFormat prints one commit per line with NUL-separated fields, so a subject
// containing any printable separator still parses.
const logFormat = "--pretty=format:%H%x00%h%x00%an%x00%ar%x00%s"

// Log reads the n most recent commits, newest first. paths, when given, narrows
// the history to commits that touch them.
//
// A repository with no commits yet has an empty history, not a broken one, so
// an unborn HEAD returns no commits rather than git's "does not have any
// commits yet" error.
func (r *Repo) Log(n int, paths []string) ([]Commit, error) {
	if _, err := r.run("", "rev-parse", "--verify", "--quiet", "HEAD"); err != nil {
		return nil, nil
	}
	args := []string{"log", "--no-color", "-n", strconv.Itoa(n), logFormat}
	if len(paths) > 0 {
		args = append(append(args, "--"), paths...)
	}
	out, err := r.run("", args...)
	if err != nil {
		return nil, err
	}

	var commits []Commit
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\x00")
		if len(f) < 5 {
			continue // an empty history prints nothing at all
		}
		commits = append(commits, Commit{
			SHA: f[0], Short: f[1], Author: f[2], Rel: f[3], Subject: f[4],
		})
	}
	return commits, nil
}

// Show returns one commit's changes as unified diff text, with the commit
// message stripped so it parses like every other source hunk reads.
//
// --first-parent -m is what gives a merge commit a diff at all: without it,
// "git show" prints a merge's header and nothing else.
func (r *Repo) Show(sha string, ignoreWS bool, context int) (string, error) {
	args := diffArgs(ignoreWS, context, "show", "--format=", "--first-parent", "-m")
	return r.run("", append(args, sha)...)
}

// StageFiles stages whole files. Used for untracked files, which have no hunks
// to choose between.
func (r *Repo) StageFiles(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	args := append([]string{"add", "--"}, paths...)
	_, err := r.run("", args...)
	return err
}

// ApplyCached stages a patch.
//
// --cached means only the index is written: no working-tree file is created,
// modified, or removed by this call, so an unwanted stage is undone with
// "git restore --staged". --recount lets git fix up the @@ line counts, which
// is what makes it safe to emit selected hunks verbatim.
func (r *Repo) ApplyCached(patch string) error {
	if strings.TrimSpace(patch) == "" {
		return nil
	}
	_, err := r.run(patch, "apply", "--cached", "--recount", "--unidiff-zero", "-")
	return err
}

// UnapplyCached reverses a patch that was staged with ApplyCached, taking
// exactly those changes back out of the index and nothing else. It is the undo
// for a hunk stage.
func (r *Repo) UnapplyCached(patch string) error {
	if strings.TrimSpace(patch) == "" {
		return nil
	}
	_, err := r.run(patch, "apply", "--cached", "--reverse", "--recount", "--unidiff-zero", "-")
	return err
}

// UnstageFiles removes whole files from the index, the undo for StageFiles.
func (r *Repo) UnstageFiles(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	args := append([]string{"restore", "--staged", "--"}, paths...)
	_, err := r.run("", args...)
	return err
}

// Available reports whether the git binary is on PATH at all.
func Available() bool {
	_, err := exec.LookPath("git")
	return err == nil
}
