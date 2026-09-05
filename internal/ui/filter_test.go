package ui

import (
	"regexp"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/wmarquardt/hunk/internal/diff"
	"github.com/wmarquardt/hunk/internal/theme"
)

// twoHunkDiff: hunk 1 changes only a "version =" line (noise), hunk 2 changes
// real code.
const twoHunkDiff = "diff --git a/x b/x\n--- a/x\n+++ b/x\n" +
	"@@ -1,2 +1,2 @@\n keep\n-version = 1\n+version = 2\n" +
	"@@ -20,2 +20,2 @@\n keep2\n-real code\n+real code changed\n"

func hunkCount(files []diff.File) int {
	n := 0
	for _, f := range files {
		n += len(f.Hunks)
	}
	return n
}

func TestFilterFilesDropsAllMatchingHunks(t *testing.T) {
	files, err := diff.ParseString(twoHunkDiff)
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`version =`)

	got := filterFiles(files, re)
	if hunkCount(got) != 1 {
		t.Fatalf("kept %d hunks, want 1 (the version hunk should drop)", hunkCount(got))
	}
	// The surviving hunk is the real one, and the file's counts reflect only it.
	if got[0].Added != 1 || got[0].Removed != 1 {
		t.Errorf("file counts not recomputed: +%d -%d", got[0].Added, got[0].Removed)
	}
	// A nil filter is a passthrough.
	if hunkCount(filterFiles(files, nil)) != 2 {
		t.Error("nil filter should show every hunk")
	}
	// A filter that matches every changed line drops the whole file.
	if len(filterFiles(files, regexp.MustCompile(`.`))) != 0 {
		t.Error("a filter matching everything should leave no files")
	}
}

// runFilter drives the F prompt.
func runFilter(t *testing.T, m *Model, pattern string) {
	t.Helper()
	m.handleKey(keyPress("F"))
	if !m.filterInput {
		t.Fatal("F did not open the filter prompt")
	}
	for _, r := range pattern {
		m.handleKey(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
}

func TestFilterPromptHidesAndClears(t *testing.T) {
	m := newTestModel(t, twoHunkDiff)
	if hunkCount(m.files) != 2 {
		t.Fatalf("start with %d hunks, want 2", hunkCount(m.files))
	}

	runFilter(t, m, "version =")
	if hunkCount(m.files) != 1 {
		t.Errorf("filter left %d hunks, want 1", hunkCount(m.files))
	}
	if m.filter == nil || m.raw == nil || hunkCount(m.raw) != 2 {
		t.Error("raw diff must be kept intact behind the filter")
	}

	// Empty pattern clears the filter, restoring every hunk.
	runFilter(t, m, "")
	if hunkCount(m.files) != 2 {
		t.Errorf("clearing the filter left %d hunks, want 2", hunkCount(m.files))
	}
}

func TestBadFilterKeepsPreviousAndReports(t *testing.T) {
	m := newTestModel(t, twoHunkDiff)
	runFilter(t, m, "version =")

	runFilter(t, m, "(") // invalid regex
	if m.filter == nil || m.filterSrc != "version =" {
		t.Error("a bad pattern should keep the previous filter")
	}
	if hunkCount(m.files) != 1 {
		t.Errorf("bad pattern changed the view: %d hunks", hunkCount(m.files))
	}
}

func TestFilterOptionAtStartup(t *testing.T) {
	files, err := diff.ParseString(twoHunkDiff)
	if err != nil {
		t.Fatal(err)
	}
	m := New(files, theme.Default(), Options{Filter: "version ="})
	if hunkCount(m.files) != 1 {
		t.Errorf("--filter did not apply at startup: %d hunks", hunkCount(m.files))
	}
}
