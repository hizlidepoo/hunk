package ui

import (
	"testing"

	"github.com/wmarquardt/hunk/internal/diff"
)

// pairs renders a hunk's paired rows as "left|right" strings, which is close
// enough to what the screen shows to assert on directly.
func pairs(t *testing.T, unified string) []string {
	t.Helper()
	files, err := diff.ParseString(unified)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || len(files[0].Hunks) != 1 {
		t.Fatalf("fixture should hold exactly one hunk, got %d files", len(files))
	}

	var out []string
	for _, r := range hunkRows(files[0].Hunks[0], true) {
		out = append(out, r.Left.Text+"|"+r.Right.Text)
	}
	return out
}

func TestHunkRowsPairing(t *testing.T) {
	tests := []struct {
		name string
		diff string
		want []string
	}{
		{
			name: "context lines sit on both sides",
			diff: "--- a/x\n+++ b/x\n@@ -1,3 +1,3 @@\n one\n two\n-x\n+y\n",
			want: []string{"one|one", "two|two", "x|y"},
		},
		{
			name: "a removal and an addition pair up",
			diff: "--- a/x\n+++ b/x\n@@ -1,2 +1,2 @@\n one\n-two\n+2\n",
			want: []string{"one|one", "two|2"},
		},
		{
			name: "an unbalanced run pads the short side",
			diff: "--- a/x\n+++ b/x\n@@ -1,1 +1,3 @@\n-a\n+1\n+2\n+3\n",
			want: []string{"a|1", "|2", "|3"},
		},
		{
			name: "a pure deletion leaves the right side empty",
			diff: "--- a/x\n+++ b/x\n@@ -1,3 +1,1 @@\n keep\n-gone\n-also gone\n",
			want: []string{"keep|keep", "gone|", "also gone|"},
		},
		{
			name: "a second removal run starts a new pairing",
			diff: "--- a/x\n+++ b/x\n@@ -1,2 +1,2 @@\n-a\n+1\n-b\n+2\n",
			want: []string{"a|1", "b|2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pairs(t, tt.diff)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d rows %q, want %d %q", len(got), got, len(tt.want), tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("row %d = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestHunkRowsLineNumbers(t *testing.T) {
	files, err := diff.ParseString("--- a/x\n+++ b/x\n@@ -10,2 +20,2 @@\n keep\n-old\n+new\n")
	if err != nil {
		t.Fatal(err)
	}
	rows := hunkRows(files[0].Hunks[0], true)

	if rows[0].Left.Num != 10 || rows[0].Right.Num != 20 {
		t.Errorf("context row numbers = %d/%d, want 10/20", rows[0].Left.Num, rows[0].Right.Num)
	}
	if rows[1].Left.Num != 11 || rows[1].Right.Num != 21 {
		t.Errorf("changed row numbers = %d/%d, want 11/21", rows[1].Left.Num, rows[1].Right.Num)
	}
}

func TestHunkRowsEmptySideHasNoLineNumber(t *testing.T) {
	files, err := diff.ParseString("--- a/x\n+++ b/x\n@@ -1,1 +1,2 @@\n keep\n+added\n")
	if err != nil {
		t.Fatal(err)
	}
	rows := hunkRows(files[0].Hunks[0], true)

	added := rows[len(rows)-1]
	if !added.Left.Empty {
		t.Error("left side of a pure addition should be empty")
	}
	if added.Left.Num != 0 {
		t.Errorf("empty side has line number %d, want 0", added.Left.Num)
	}
}

func TestBuildIndexes(t *testing.T) {
	unified := "" +
		"--- a/one.txt\n+++ b/one.txt\n@@ -1,1 +1,1 @@\n-a\n+b\n" +
		"--- a/two.txt\n+++ b/two.txt\n@@ -1,1 +1,1 @@\n-c\n+d\n@@ -9,1 +9,1 @@\n-e\n+f\n"

	files, err := diff.ParseString(unified)
	if err != nil {
		t.Fatal(err)
	}
	v := Build(files, true)

	if len(v.FileRows) != 2 {
		t.Fatalf("FileRows = %v, want 2 entries", v.FileRows)
	}
	if len(v.HunkRows) != 3 {
		t.Fatalf("HunkRows = %v, want 3 entries", v.HunkRows)
	}
	for _, i := range v.FileRows {
		if v.Rows[i].Kind != RowFile {
			t.Errorf("row %d is not a file header", i)
		}
	}
	for _, i := range v.HunkRows {
		if v.Rows[i].Kind != RowHunk {
			t.Errorf("row %d is not a hunk header", i)
		}
	}
	// Indexes must be ascending, or next/prev navigation walks backwards.
	for i := 1; i < len(v.HunkRows); i++ {
		if v.HunkRows[i] <= v.HunkRows[i-1] {
			t.Errorf("HunkRows not ascending: %v", v.HunkRows)
		}
	}
}

func TestBuildBinaryAndEmptyFiles(t *testing.T) {
	files, err := diff.ParseString(
		"diff --git a/x.bin b/x.bin\n--- a/x.bin\n+++ b/x.bin\nBinary files a/x.bin and b/x.bin differ\n")
	if err != nil {
		t.Fatal(err)
	}
	v := Build(files, true)

	if len(v.HunkRows) != 0 {
		t.Errorf("a binary file should contribute no hunks, got %v", v.HunkRows)
	}
	var notice bool
	for _, r := range v.Rows {
		if r.Kind == RowNotice {
			notice = true
		}
	}
	if !notice {
		t.Error("a binary file should show a notice row rather than nothing")
	}
}

func TestBuildEmptyInput(t *testing.T) {
	v := Build(nil, true)
	if len(v.Rows) != 0 || len(v.FileRows) != 0 || len(v.HunkRows) != 0 {
		t.Errorf("empty input produced %+v", v)
	}
}

func TestNextPrevIndex(t *testing.T) {
	idx := []int{5, 10, 20}

	tests := []struct {
		name       string
		cur        int
		next, prev int
	}{
		{name: "before the first", cur: 0, next: 5, prev: 5},
		{name: "on the first", cur: 5, next: 10, prev: 5},
		{name: "between", cur: 7, next: 10, prev: 5},
		{name: "on the last", cur: 20, next: 20, prev: 10},
		{name: "past the last", cur: 99, next: 20, prev: 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NextIndex(idx, tt.cur); got != tt.next {
				t.Errorf("NextIndex(%d) = %d, want %d", tt.cur, got, tt.next)
			}
			if got := PrevIndex(idx, tt.cur); got != tt.prev {
				t.Errorf("PrevIndex(%d) = %d, want %d", tt.cur, got, tt.prev)
			}
		})
	}
}

func TestNextPrevIndexEmpty(t *testing.T) {
	if got := NextIndex(nil, 3); got != 3 {
		t.Errorf("NextIndex on an empty index = %d, want the cursor unmoved", got)
	}
	if got := PrevIndex(nil, 3); got != 3 {
		t.Errorf("PrevIndex on an empty index = %d, want the cursor unmoved", got)
	}
}
