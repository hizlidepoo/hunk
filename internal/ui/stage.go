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
	for _, path := range untracked {
		content, err := os.ReadFile(filepath.Join(r.Dir, path))
		if err != nil {
			// A file that vanished between listing and reading is not an error
			// worth aborting a review over.
			continue
		}
		b.WriteString(diff.NewFilePatch(path, content))
	}
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

	hunks, files := m.marks.total()
	return fmt.Sprintf("staged %s in %s", plural(hunks, "hunk"), plural(files, "file")), nil
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
	return nil
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}
