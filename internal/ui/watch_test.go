package ui

import (
	"os"
	"path/filepath"
	"testing"
)

// mkdirs creates each directory under root, so a .gitignore entry has something
// real to point at — ignoredDirs only keeps entries that are directories on disk.
func mkdirs(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(n)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func writeGitignore(t *testing.T, root, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ignoredDirs is what keeps the watcher off node_modules and dist. It reads
// only the top-level .gitignore and only the entries that name a directory —
// the cases below pin that deliberately narrow behaviour down so a future
// change to it is a decision rather than an accident.
func TestIgnoredDirs(t *testing.T) {
	tests := []struct {
		name      string
		dirs      []string
		files     []string
		gitignore string
		want      []string // directories expected in the map
		notWant   []string
	}{
		{
			name: "no gitignore ignores nothing",
			dirs: []string{"node_modules"},
			want: nil,
		},
		{
			name:      "a named directory is ignored",
			dirs:      []string{"node_modules", "src"},
			gitignore: "node_modules\n",
			want:      []string{"node_modules"},
			notWant:   []string{"src"},
		},
		{
			name:      "a trailing slash is stripped",
			dirs:      []string{"dist"},
			gitignore: "dist/\n",
			want:      []string{"dist"},
		},
		{
			name:      "a leading slash is stripped",
			dirs:      []string{"build"},
			gitignore: "/build\n",
			want:      []string{"build"},
		},
		{
			name:      "comments and blank lines are skipped",
			dirs:      []string{"dist"},
			gitignore: "# a comment\n\n   \ndist\n",
			want:      []string{"dist"},
		},
		{
			name:      "a negation is skipped",
			dirs:      []string{"keep"},
			gitignore: "!keep\n",
			notWant:   []string{"keep"},
		},
		{
			name:      "a glob is skipped",
			dirs:      []string{"logs"},
			gitignore: "*.log\nlogs?\nlogs[0-9]\n",
			notWant:   []string{"logs"},
		},
		{
			name:      "an entry naming a file is not a directory to skip",
			files:     []string{"secrets.env"},
			gitignore: "secrets.env\n",
			notWant:   []string{"secrets.env"},
		},
		{
			name:      "an entry that does not exist is dropped",
			gitignore: "vanished\n",
			notWant:   []string{"vanished"},
		},
		{
			name:      "a nested path is resolved against the root",
			dirs:      []string{"a/b"},
			gitignore: "a/b\n",
			want:      []string{"a/b"},
		},
		{
			name:      "surrounding whitespace is trimmed",
			dirs:      []string{"dist"},
			gitignore: "  dist  \n",
			want:      []string{"dist"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			mkdirs(t, root, tt.dirs...)
			for _, f := range tt.files {
				if err := os.WriteFile(filepath.Join(root, f), []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if tt.gitignore != "" {
				writeGitignore(t, root, tt.gitignore)
			}

			got := ignoredDirs(root)
			for _, w := range tt.want {
				key := filepath.Clean(filepath.Join(root, filepath.FromSlash(w)))
				if !got[key] {
					t.Errorf("%q is not ignored, but .gitignore names it: %v", w, got)
				}
			}
			for _, w := range tt.notWant {
				key := filepath.Clean(filepath.Join(root, filepath.FromSlash(w)))
				if got[key] {
					t.Errorf("%q is ignored, but should not be: %v", w, got)
				}
			}
			if tt.want == nil && tt.notWant == nil && len(got) != 0 {
				t.Errorf("got %v, want an empty map", got)
			}
		})
	}
}

// An unreadable .gitignore is not worth failing a review over: the watcher just
// watches everything.
func TestIgnoredDirsWithNoGitignore(t *testing.T) {
	if got := ignoredDirs(t.TempDir()); len(got) != 0 {
		t.Errorf("got %v, want an empty map", got)
	}
}

// isIgnored is the per-event check, and .git is the one subtree it must always
// refuse: every git command hunk runs writes there, and following those writes
// would reload the view in a loop.
func TestIsIgnored(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, "node_modules/pkg", ".git/objects", "src/lib", "dist")
	ignore := map[string]bool{
		filepath.Clean(filepath.Join(root, "node_modules")): true,
		filepath.Clean(filepath.Join(root, "dist")):         true,
	}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "the root itself", path: ".", want: false},
		{name: "a watched file", path: "src/lib/main.go", want: false},
		{name: "a file directly under .git", path: ".git/index", want: true},
		{name: "a file deep under .git", path: ".git/objects/ab/cdef", want: true},
		{name: "an ignored directory", path: "node_modules", want: true},
		{name: "a file inside an ignored directory", path: "node_modules/pkg/index.js", want: true},
		{name: "another ignored directory", path: "dist/bundle.js", want: true},
		{name: "a name that merely starts the same", path: "node_modules_extra/a.js", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := filepath.Join(root, filepath.FromSlash(tt.path))
			if got := isIgnored(root, p, ignore); got != tt.want {
				t.Errorf("isIgnored(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// A path outside the root cannot be made relative to it on some platforms, and
// an unrelated path is not something to skip on any of them.
func TestIsIgnoredOutsideTheRoot(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	if isIgnored(root, filepath.Join(other, "a.txt"), map[string]bool{}) {
		t.Error("a path outside the root was reported as ignored")
	}
}

// wait turns the watcher's dirty signal into the message that drives a reload.
func TestWatcherWaitDeliversTheDirtyMessage(t *testing.T) {
	w := &watcher{dirty: make(chan struct{}, 1)}
	w.dirty <- struct{}{}

	if msg := w.wait()(); msg != (fsDirtyMsg{}) {
		t.Errorf("wait() delivered %#v, want fsDirtyMsg{}", msg)
	}
}
