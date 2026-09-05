// Package git is the thin layer between hunk and the git binary.
//
// ponytail: os/exec over a git library. This is six commands; go-git is a large
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
