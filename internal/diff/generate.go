package diff

import (
	"bytes"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/aymanbagabas/go-udiff"
)

// DefaultContext is how many unchanged lines surround a hunk by default — git's
// -U3.
const DefaultContext = 3

// binarySniff is how much of a file we look at to decide it is binary. Same
// heuristic git uses: a NUL byte near the start means "not text".
const binarySniff = 8000

// Generate diffs two files or two directories and returns unified diff text.
//
// ponytail: generate-then-reparse. Producing text and handing it straight back
// to Parse keeps one interpretation of a diff in the codebase instead of two.
// Swap in a direct []File builder only if profiling says this matters.
func Generate(oldPath, newPath string, contextLines int) (string, error) {
	oldInfo, err := os.Stat(oldPath)
	if err != nil {
		return "", err
	}
	newInfo, err := os.Stat(newPath)
	if err != nil {
		return "", err
	}

	if oldInfo.IsDir() != newInfo.IsDir() {
		return "", fmt.Errorf("cannot diff a file against a directory: %s, %s", oldPath, newPath)
	}
	if oldInfo.IsDir() {
		return generateDirs(oldPath, newPath, contextLines)
	}

	oldData, err := os.ReadFile(oldPath)
	if err != nil {
		return "", err
	}
	newData, err := os.ReadFile(newPath)
	if err != nil {
		return "", err
	}
	return filePatch(diffName(oldPath), diffName(newPath), oldData, newData, true, true, contextLines), nil
}

// diffName turns a path into the name used inside diff headers. Git writes
// "diff --git a/tmp/x b/tmp/x" for /tmp/x, so the leading slash goes: with it,
// the header reads "a//tmp/x" and parsers cannot split the two filenames.
func diffName(path string) string {
	return strings.TrimPrefix(filepath.ToSlash(path), "/")
}

func generateDirs(oldRoot, newRoot string, contextLines int) (string, error) {
	oldFiles, err := walk(oldRoot)
	if err != nil {
		return "", err
	}
	newFiles, err := walk(newRoot)
	if err != nil {
		return "", err
	}

	var out strings.Builder
	for _, rel := range union(oldFiles, newFiles) {
		_, inOld := oldFiles[rel]
		_, inNew := newFiles[rel]

		var oldData, newData []byte
		if inOld {
			if oldData, err = os.ReadFile(filepath.Join(oldRoot, rel)); err != nil {
				return "", err
			}
		}
		if inNew {
			if newData, err = os.ReadFile(filepath.Join(newRoot, rel)); err != nil {
				return "", err
			}
		}
		out.WriteString(filePatch(rel, rel, oldData, newData, inOld, inNew, contextLines))
	}
	return out.String(), nil
}

// filePatch renders one file's changes with contextLines of context around each
// hunk. inOld/inNew say whether the file exists on that side, which is what
// turns a diff into an add or a delete.
func filePatch(oldName, newName string, oldData, newData []byte, inOld, inNew bool, contextLines int) string {
	if inOld && inNew && bytes.Equal(oldData, newData) {
		return ""
	}

	header := fmt.Sprintf("diff --git a/%s b/%s\n", oldName, newName)

	oldLabel, newLabel := "a/"+oldName, "b/"+newName
	if !inOld {
		oldLabel = "/dev/null"
		header += "new file mode 100644\n"
	}
	if !inNew {
		newLabel = "/dev/null"
		header += "deleted file mode 100644\n"
	}

	if isBinary(oldData) || isBinary(newData) {
		// The ---/+++ lines are redundant for git itself, but without them a
		// parser cannot recover the filenames when the two sides are named
		// differently, which is the normal case for `hunk old.bin new.bin`.
		return header +
			fmt.Sprintf("--- %s\n+++ %s\n", oldLabel, newLabel) +
			fmt.Sprintf("Binary files %s and %s differ\n", oldLabel, newLabel)
	}

	edits := udiff.Strings(string(oldData), string(newData))
	body, err := udiff.ToUnified(oldLabel, newLabel, string(oldData), edits, contextLines)
	if err != nil || body == "" {
		return ""
	}
	return header + body
}

func isBinary(data []byte) bool {
	return bytes.IndexByte(data[:min(len(data), binarySniff)], 0) >= 0
}

// walk collects every regular file under root, keyed by its path relative to root.
func walk(root string) (map[string]struct{}, error) {
	files := make(map[string]struct{})
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil // ponytail: symlinks, sockets and devices are not diffable content
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = struct{}{}
		return nil
	})
	return files, err
}

func union(a, b map[string]struct{}) []string {
	all := maps.Clone(a)
	maps.Copy(all, b)
	return slices.Sorted(maps.Keys(all))
}

// NewFilePatch renders an untracked file as an add-everything patch, so a file
// git has never seen can be reviewed and staged like any other change.
func NewFilePatch(path string, content []byte) string {
	// An untracked file is all additions; there are no context lines to size.
	return filePatch(path, path, nil, content, false, true, DefaultContext)
}
