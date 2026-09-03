package diff

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func load(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(b)
}

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		input   string // used when fixture is empty
		wantErr bool
		files   int
		check   func(t *testing.T, files []File)
	}{
		{
			name:  "empty input",
			input: "",
			files: 0,
		},
		{
			name:  "no diff, just prose",
			input: "this is not a diff at all\nnot even close\n",
			files: 0,
		},
		{
			name:    "simple change",
			fixture: "simple.diff",
			files:   1,
			check: func(t *testing.T, files []File) {
				f := files[0]
				if f.Path() != "hello.txt" {
					t.Errorf("Path() = %q, want hello.txt", f.Path())
				}
				if f.Added != 2 || f.Removed != 1 {
					t.Errorf("+%d -%d, want +2 -1", f.Added, f.Removed)
				}
				if len(f.Hunks) != 1 {
					t.Fatalf("got %d hunks, want 1", len(f.Hunks))
				}
				h := f.Hunks[0]
				if !strings.HasPrefix(h.Header, "@@ -1,4 +1,5 @@") {
					t.Errorf("header = %q", h.Header)
				}
				want := []Line{
					{Context, "one", 1, 1},
					{Removed, "two", 2, 0},
					{Added, "two changed", 0, 2},
					{Added, "two and a half", 0, 3},
					{Context, "three", 3, 4},
					{Context, "four", 4, 5},
				}
				if len(h.Lines) != len(want) {
					t.Fatalf("got %d lines, want %d", len(h.Lines), len(want))
				}
				for i, w := range want {
					if h.Lines[i] != w {
						t.Errorf("line %d = %+v, want %+v", i, h.Lines[i], w)
					}
				}
			},
		},
		{
			name:    "new file",
			fixture: "newfile.diff",
			files:   1,
			check: func(t *testing.T, files []File) {
				f := files[0]
				if !f.IsNew {
					t.Error("IsNew = false, want true")
				}
				if f.Path() != "new.txt" {
					t.Errorf("Path() = %q", f.Path())
				}
				if f.Added != 2 || f.Removed != 0 {
					t.Errorf("+%d -%d, want +2 -0", f.Added, f.Removed)
				}
				if got := f.Hunks[0].Lines[0].NewNum; got != 1 {
					t.Errorf("first NewNum = %d, want 1", got)
				}
			},
		},
		{
			name:    "deleted file",
			fixture: "delete.diff",
			files:   1,
			check: func(t *testing.T, files []File) {
				f := files[0]
				if !f.IsDelete {
					t.Error("IsDelete = false, want true")
				}
				if f.Path() != "gone.txt" {
					t.Errorf("Path() = %q, want gone.txt (deletes show the old name)", f.Path())
				}
			},
		},
		{
			name:    "rename with edit",
			fixture: "rename.diff",
			files:   1,
			check: func(t *testing.T, files []File) {
				f := files[0]
				if !f.IsRename {
					t.Error("IsRename = false, want true")
				}
				if f.OldPath != "old/name.txt" || f.NewPath != "new/name.txt" {
					t.Errorf("paths = %q -> %q", f.OldPath, f.NewPath)
				}
			},
		},
		{
			name:    "binary file",
			fixture: "binary.diff",
			files:   1,
			check: func(t *testing.T, files []File) {
				f := files[0]
				if !f.IsBinary {
					t.Error("IsBinary = false, want true")
				}
				if len(f.Hunks) != 0 {
					t.Errorf("got %d hunks, want 0 for a binary file", len(f.Hunks))
				}
			},
		},
		{
			name:    "no newline at end of file",
			fixture: "noeol.diff",
			files:   1,
			check: func(t *testing.T, files []File) {
				h := files[0].Hunks[0]
				if len(h.Lines) != 2 {
					t.Fatalf("got %d lines, want 2", len(h.Lines))
				}
				if h.Lines[0].Text != "old line" || h.Lines[1].Text != "new line" {
					t.Errorf("lines = %q, %q", h.Lines[0].Text, h.Lines[1].Text)
				}
			},
		},
		{
			name:    "CRLF line endings",
			fixture: "crlf.diff",
			files:   1,
			check: func(t *testing.T, files []File) {
				h := files[0].Hunks[0]
				// The \r belongs to the file's content, not to the diff format,
				// so it must survive parsing.
				if h.Lines[0].Text != "keep\r" {
					t.Errorf("context line = %q, want %q", h.Lines[0].Text, "keep\r")
				}
			},
		},
		{
			name:    "multiple files",
			fixture: "multi.diff",
			files:   2,
			check: func(t *testing.T, files []File) {
				if files[0].Path() != "hello.txt" || files[1].Path() != "new/name.txt" {
					t.Errorf("paths = %q, %q", files[0].Path(), files[1].Path())
				}
			},
		},
		{
			name:    "truncated hunk",
			input:   "diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1,9 +1,9 @@\n one\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := tt.input
			if tt.fixture != "" {
				in = load(t, tt.fixture)
			}

			files, err := ParseString(in)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(files) != tt.files {
				t.Fatalf("got %d files, want %d", len(files), tt.files)
			}
			if tt.check != nil {
				tt.check(t, files)
			}
		})
	}
}

// A single-line file and a very large hunk are the two shapes most likely to
// trip off-by-one line numbering.
func TestParseSingleLineFile(t *testing.T) {
	in := "--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n+b\n"
	files, err := ParseString(in)
	if err != nil {
		t.Fatal(err)
	}
	h := files[0].Hunks[0]
	if len(h.Lines) != 2 || h.Lines[0].OldNum != 1 || h.Lines[1].NewNum != 1 {
		t.Errorf("lines = %+v", h.Lines)
	}
}

func TestParseHugeHunk(t *testing.T) {
	const n = 20000
	var b strings.Builder
	b.WriteString("--- a/big.txt\n+++ b/big.txt\n")
	b.WriteString("@@ -1," + strconv.Itoa(n/2) + " +1," + strconv.Itoa(n/2) + " @@\n")
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			b.WriteString("-line " + strconv.Itoa(i) + "\n")
		} else {
			b.WriteString("+line " + strconv.Itoa(i) + "\n")
		}
	}

	files, err := ParseString(b.String())
	if err != nil {
		t.Fatal(err)
	}
	h := files[0].Hunks[0]
	if len(h.Lines) != n {
		t.Fatalf("got %d lines, want %d", len(h.Lines), n)
	}
	if files[0].Added != n/2 || files[0].Removed != n/2 {
		t.Errorf("+%d -%d, want +%d -%d", files[0].Added, files[0].Removed, n/2, n/2)
	}
	// Lines alternate, so the last removed line is at index n-2 and is old line n/2.
	if last := h.Lines[n-2]; last.Kind != Removed || last.OldNum != n/2 {
		t.Errorf("last removed = %+v", last)
	}
}
