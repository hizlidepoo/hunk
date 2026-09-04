package ui

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
)

// fsDirtyMsg says the working tree changed and the diff should be reloaded.
type fsDirtyMsg struct{}

// debounce is how long the watcher waits after the last event before it calls
// the tree dirty, so a burst of writes from a formatter or a codegen tool
// becomes a single reload instead of dozens.
const debounce = 200 * time.Millisecond

// watcher turns filesystem events under a repo into one coalesced "dirty"
// signal the UI can wait on. It watches the working tree only; .git and
// gitignored directories are skipped so an editor's churn and node_modules do
// not drown the diff in reloads.
type watcher struct {
	fs    *fsnotify.Watcher
	dirty chan struct{}
	done  chan struct{}
}

// newWatcher starts watching root, a repo working tree. A returned error means
// live-follow is unavailable; it never means hunk cannot run.
func newWatcher(root string) (*watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &watcher{
		fs:    fw,
		dirty: make(chan struct{}, 1),
		done:  make(chan struct{}),
	}
	ignore := ignoredDirs(root)
	addTree(fw, root, ignore)
	go w.loop(root, ignore)
	return w, nil
}

// loop coalesces events: it (re)arms a timer on every relevant change and only
// signals dirty once the writes stop for `debounce`.
func (w *watcher) loop(root string, ignore map[string]bool) {
	timer := time.NewTimer(debounce)
	if !timer.Stop() {
		<-timer.C
	}

	for {
		select {
		case <-w.done:
			return

		case ev, ok := <-w.fs.Events:
			if !ok {
				return
			}
			// A newly created directory needs its own watch, or edits inside it
			// are never seen.
			if ev.Op&fsnotify.Create != 0 && !isIgnored(root, ev.Name, ignore) {
				if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
					addTree(w.fs, ev.Name, ignore)
				}
			}
			if isIgnored(root, ev.Name, ignore) {
				continue
			}
			timer.Reset(debounce)

		case <-timer.C:
			select {
			case w.dirty <- struct{}{}:
			default: // a reload is already pending; it will see this change too
			}

		case _, ok := <-w.fs.Errors:
			if !ok {
				return
			}
		}
	}
}

// Close stops the watcher and releases its OS handles.
func (w *watcher) Close() {
	if w == nil {
		return
	}
	close(w.done)
	_ = w.fs.Close()
}

// wait blocks until the tree is dirty, then asks the UI to reload. It is
// re-issued after each reload to keep following.
func (w *watcher) wait() tea.Cmd {
	return func() tea.Msg {
		<-w.dirty
		return fsDirtyMsg{}
	}
}

// addTree adds a recursive watch on dir, skipping .git and ignored subtrees.
func addTree(fw *fsnotify.Watcher, dir string, ignore map[string]bool) {
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if d.Name() == ".git" || ignore[filepath.Clean(path)] {
			return fs.SkipDir
		}
		_ = fw.Add(path)
		return nil
	})
}

// ignoredDirs reads the top-level .gitignore and returns the directories it
// names, so those subtrees are never watched. Full gitignore semantics (nested
// files, globs, negations) are intentionally left out for now; this catches the
// common heavy hitters like node_modules/ and dist/.
func ignoredDirs(root string) map[string]bool {
	ignore := map[string]bool{}
	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		return ignore
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		name := strings.Trim(line, "/")
		if name == "" || strings.ContainsAny(name, "*?[") {
			continue
		}
		p := filepath.Join(root, filepath.FromSlash(name))
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			ignore[filepath.Clean(p)] = true
		}
	}
	return ignore
}

// isIgnored reports whether path lies under .git or an ignored directory.
func isIgnored(root, path string, ignore map[string]bool) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return false
	}
	acc := filepath.Clean(root)
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if part == ".git" {
			return true
		}
		acc = filepath.Join(acc, part)
		if ignore[acc] {
			return true
		}
	}
	return false
}
