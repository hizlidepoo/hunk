package diff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// write creates dir/name with content, making parent directories as needed.
func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// generateAndParse exercises the real pipeline: text in, []File out.
func generateAndParse(t *testing.T, a, b string) []File {
	t.Helper()
	text, err := Generate(a, b)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	files, err := ParseString(text)
	if err != nil {
		t.Fatalf("Parse of generated text failed: %v\n---\n%s", err, text)
	}
	return files
}

func TestGenerateFiles(t *testing.T) {
	tests := []struct {
		name       string
		old, new   string
		wantFiles  int
		wantAdded  int
		wantRemove int
	}{
		{name: "identical", old: "same\n", new: "same\n", wantFiles: 0},
		{name: "both empty", old: "", new: "", wantFiles: 0},
		{name: "single line change", old: "a\n", new: "b\n", wantFiles: 1, wantAdded: 1, wantRemove: 1},
		{name: "empty to content", old: "", new: "hi\n", wantFiles: 1, wantAdded: 1},
		{name: "content to empty", old: "hi\n", new: "", wantFiles: 1, wantRemove: 1},
		{name: "no trailing newline", old: "a", new: "b", wantFiles: 1, wantAdded: 1, wantRemove: 1},
		{name: "append only", old: "a\n", new: "a\nb\n", wantFiles: 1, wantAdded: 1},
		{name: "crlf change", old: "a\r\n", new: "b\r\n", wantFiles: 1, wantAdded: 1, wantRemove: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			a := write(t, dir, "a.txt", tt.old)
			b := write(t, dir, "b.txt", tt.new)

			files := generateAndParse(t, a, b)
			if len(files) != tt.wantFiles {
				t.Fatalf("got %d files, want %d", len(files), tt.wantFiles)
			}
			if tt.wantFiles == 0 {
				return
			}
			if files[0].Added != tt.wantAdded || files[0].Removed != tt.wantRemove {
				t.Errorf("+%d -%d, want +%d -%d",
					files[0].Added, files[0].Removed, tt.wantAdded, tt.wantRemove)
			}
		})
	}
}

func TestGenerateBinaryFile(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.bin", "\x00\x01\x02")
	b := write(t, dir, "b.bin", "\x00\x01\x03")

	files := generateAndParse(t, a, b)
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	if !files[0].IsBinary {
		t.Error("IsBinary = false, want true")
	}
	if len(files[0].Hunks) != 0 {
		t.Errorf("got %d hunks, want 0", len(files[0].Hunks))
	}
}

func TestGenerateIdenticalBinaryFiles(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.bin", "\x00\x01\x02")
	b := write(t, dir, "b.bin", "\x00\x01\x02")

	if files := generateAndParse(t, a, b); len(files) != 0 {
		t.Errorf("got %d files, want 0 for identical binaries", len(files))
	}
}

func TestGenerateDirs(t *testing.T) {
	oldDir, newDir := t.TempDir(), t.TempDir()

	write(t, oldDir, "same.txt", "unchanged\n")
	write(t, newDir, "same.txt", "unchanged\n")

	write(t, oldDir, "changed.txt", "one\ntwo\n")
	write(t, newDir, "changed.txt", "one\nTWO\n")

	write(t, oldDir, "gone.txt", "delete me\n")
	write(t, newDir, "nested/added.txt", "brand new\n")

	files := generateAndParse(t, oldDir, newDir)

	byPath := map[string]File{}
	for _, f := range files {
		byPath[f.Path()] = f
	}

	if len(files) != 3 {
		t.Fatalf("got %d files (%v), want 3: changed, gone, added", len(files), byPath)
	}
	if _, ok := byPath["same.txt"]; ok {
		t.Error("unchanged file should not appear in the diff")
	}

	changed, ok := byPath["changed.txt"]
	if !ok {
		t.Fatal("changed.txt missing")
	}
	if changed.Added != 1 || changed.Removed != 1 {
		t.Errorf("changed.txt +%d -%d, want +1 -1", changed.Added, changed.Removed)
	}

	gone, ok := byPath["gone.txt"]
	if !ok {
		t.Fatal("gone.txt missing")
	}
	if !gone.IsDelete {
		t.Error("gone.txt IsDelete = false, want true")
	}

	added, ok := byPath["nested/added.txt"]
	if !ok {
		t.Fatal("nested/added.txt missing")
	}
	if !added.IsNew {
		t.Error("nested/added.txt IsNew = false, want true")
	}
}

func TestGenerateDirsIdentical(t *testing.T) {
	oldDir, newDir := t.TempDir(), t.TempDir()
	write(t, oldDir, "a/b/c.txt", "hello\n")
	write(t, newDir, "a/b/c.txt", "hello\n")

	text, err := Generate(oldDir, newDir)
	if err != nil {
		t.Fatal(err)
	}
	if text != "" {
		t.Errorf("want empty diff, got %q", text)
	}
}

func TestGenerateDirsSkipsGitDir(t *testing.T) {
	oldDir, newDir := t.TempDir(), t.TempDir()
	write(t, oldDir, ".git/config", "old\n")
	write(t, newDir, ".git/config", "new\n")

	text, err := Generate(oldDir, newDir)
	if err != nil {
		t.Fatal(err)
	}
	if text != "" {
		t.Errorf(".git contents leaked into the diff: %q", text)
	}
}

func TestGenerateMismatchedKinds(t *testing.T) {
	dir := t.TempDir()
	f := write(t, dir, "file.txt", "x\n")

	_, err := Generate(dir, f)
	if err == nil {
		t.Fatal("want an error diffing a directory against a file")
	}
	if !strings.Contains(err.Error(), "cannot diff a file against a directory") {
		t.Errorf("unhelpful error: %v", err)
	}
}

func TestGenerateMissingPath(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.txt", "x\n")

	if _, err := Generate(a, filepath.Join(dir, "nope.txt")); err == nil {
		t.Fatal("want an error for a missing path")
	}
}
