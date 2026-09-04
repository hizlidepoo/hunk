package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wmarquardt/hunk/internal/diff"
	"github.com/wmarquardt/hunk/internal/git"
)

// wholeFile is the hunk index used to mark a file that has no hunks to choose
// between — a binary file, which git can only stage whole.
const wholeFile = -1

// marks is the set of hunks the user has picked, keyed by file index.
type marks map[int]map[int]bool

func (m marks) has(file, hunk int) bool { return m[file][hunk] }

func (m marks) set(file, hunk int, on bool) {
	if !on {
		delete(m[file], hunk)
		if len(m[file]) == 0 {
			delete(m, file)
		}
		return
	}
	if m[file] == nil {
		m[file] = map[int]bool{}
	}
	m[file][hunk] = true
}

func (m marks) inFile(file int) int { return len(m[file]) }

func (m marks) total() (hunks, files int) {
	for _, h := range m {
		hunks += len(h)
		files++
	}
	return hunks, files
}

// fileState describes how much of a file is marked, for the sidebar.
type fileState int

const (
	fileUnmarked fileState = iota
	filePartial
	fileAllMarked
)

func (m marks) state(file int, f diff.File) fileState {
	n := m.inFile(file)
	switch {
	case n == 0:
		return fileUnmarked
	case len(f.Hunks) == 0 || n >= len(f.Hunks):
		return fileAllMarked
	default:
		return filePartial
	}
}

func (s fileState) symbol() string {
	switch s {
	case fileAllMarked:
		return "●"
	case filePartial:
		return "◐"
	default:
		return "·"
	}
}

// GitSource reads the working tree: unstaged changes plus untracked files,
// rendered as one unified diff so review mode has nothing special about it.
func GitSource(r *git.Repo) (string, error) {
	text, err := r.Diff()
	if err != nil {
		return "", err
	}

	untracked, err := r.Untracked()
	if err != nil {
		return "", err
	}
	sort.Strings(untracked)

	var b strings.Builder
	b.WriteString(text)

	included := map[string]bool{}
	for _, path := range untracked {
		content, err := os.ReadFile(filepath.Join(r.Dir, path))
		if err != nil {
			// A file that vanished between listing and reading is not an error
			// worth aborting a review over.
			continue
		}
		included[path] = true
		b.WriteString(diff.NewFilePatch(path, content))
	}

	// A fully staged file has no unstaged changes, so it would drop off the
	// list. Keep it on screen by including its staged diff, so staging never
	// makes a file vanish — it just gets a check beside it.
	unstaged, err := r.UnstagedPaths()
	if err != nil {
		return "", err
	}
	for _, path := range unstaged {
		included[path] = true
	}
	staged, err := r.StagedPaths()
	if err != nil {
		return "", err
	}
	var stagedOnly []string
	for _, path := range staged {
		if !included[path] {
			stagedOnly = append(stagedOnly, path)
		}
	}
	stagedDiff, err := r.StagedDiff(stagedOnly)
	if err != nil {
		return "", err
	}
	b.WriteString(stagedDiff)

	return b.String(), nil
}

// stageMarked writes the marked hunks to the index in one git call, then a
// second one for files that can only be staged whole.
//
// Only the index is written: no working-tree file is created, modified, or
// deleted, so an unwanted stage is undone with "git restore --staged".
func (m *Model) stageMarked() (string, error) {
	var patch strings.Builder
	var wholeFiles []string

	for fileIdx, hunks := range m.marks {
		f := m.files[fileIdx]

		if hunks[wholeFile] {
			wholeFiles = append(wholeFiles, f.Path())
			continue
		}

		selected := make([]int, 0, len(hunks))
		for h := range hunks {
			selected = append(selected, h)
		}
		sort.Ints(selected)
		patch.WriteString(f.Patch(selected))
	}
	sort.Strings(wholeFiles)

	if err := m.repo.ApplyCached(patch.String()); err != nil {
		return "", err
	}
	if err := m.repo.StageFiles(wholeFiles); err != nil {
		return "", err
	}

	// Remember exactly what went in so "u" can reverse this stage and nothing
	// else.
	m.lastPatch, m.lastWhole = patch.String(), wholeFiles

	hunks, files := m.marks.total()
	return fmt.Sprintf("staged %s in %s", plural(hunks, "hunk"), plural(files, "file")), nil
}

// unstageLast reverses the most recent stage: the hunk patch comes back out of
// the index, and any whole files staged alongside it are removed too.
func (m *Model) unstageLast() error {
	if err := m.repo.UnapplyCached(m.lastPatch); err != nil {
		return err
	}
	return m.repo.UnstageFiles(m.lastWhole)
}

// reload re-reads the working tree after staging, so what is on screen is what
// is still unstaged.
func (m *Model) reload() error {
	text, err := GitSource(m.repo)
	if err != nil {
		return err
	}
	files, err := diff.ParseString(text)
	if err != nil {
		return err
	}

	m.files = files
	m.marks = marks{}
	m.view = Build(files, m.builtSplit)
	m.cur, m.top = 0, 0
	m.refreshGitState()
	return nil
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}
