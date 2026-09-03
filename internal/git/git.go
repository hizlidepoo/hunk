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
// is the same set of changes "git add -p" walks you through.
func (r *Repo) Diff() (string, error) {
	return r.run("", "diff", "--no-color", "--no-ext-diff", "-U3")
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

// Available reports whether the git binary is on PATH at all.
func Available() bool {
	_, err := exec.LookPath("git")
	return err == nil
}
