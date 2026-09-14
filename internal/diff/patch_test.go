package diff

import (
	"strings"
	"testing"
)

// patchFile parses text and returns its single file, so tests get hunks with a
// real fragment behind them. A File built by hand has a nil frag and Patch
// would panic on it.
func patchFile(t *testing.T, text string) File {
	t.Helper()
	files, err := ParseString(text)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	return files[0]
}

// threeHunks is one file with three separated changes, so hunk selection has
// something to choose between.
const threeHunks = `diff --git a/a.txt b/a.txt
--- a/a.txt
+++ b/a.txt
@@ -1,3 +1,3 @@
-one
+ONE
 two
 three
@@ -10,3 +10,3 @@
 nine
-ten
+TEN
 eleven
@@ -20,3 +20,3 @@
 nineteen
-twenty
+TWENTY
 twentyone
`

func TestPatchSelectsOnlyTheNamedHunks(t *testing.T) {
	f := patchFile(t, threeHunks)

	patch := f.Patch([]int{1})
	if !strings.Contains(patch, "+TEN") {
		t.Errorf("patch is missing the selected hunk:\n%s", patch)
	}
	for _, unwanted := range []string{"+ONE", "+TWENTY"} {
		if strings.Contains(patch, unwanted) {
			t.Errorf("patch contains %q, which was not selected:\n%s", unwanted, patch)
		}
	}
	if !strings.HasPrefix(patch, "diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n") {
		t.Errorf("patch does not start with the file header:\n%s", patch)
	}
}

func TestPatchOfNoHunksIsEmpty(t *testing.T) {
	f := patchFile(t, threeHunks)
	if got := f.Patch(nil); got != "" {
		t.Errorf("Patch(nil) = %q, want empty", got)
	}
	if got := f.Patch([]int{}); got != "" {
		t.Errorf("Patch(empty) = %q, want empty", got)
	}
}

// Out-of-range indices are skipped, so a selection that names only bad indices
// leaves no hunks at all. Emitting the header on its own would hand git apply a
// patch with nothing in it; an empty string is what ApplyCached already treats
// as a no-op.
func TestPatchWithOnlyOutOfRangeHunks(t *testing.T) {
	f := patchFile(t, threeHunks)
	for _, hunks := range [][]int{{99}, {-1}, {-1, 99}} {
		if got := f.Patch(hunks); got != "" {
			t.Errorf("Patch(%v) = %q, want empty: no hunk survived the range check", hunks, got)
		}
	}
}

// A valid index alongside a bad one still produces that hunk's patch.
func TestPatchSkipsBadIndicesButKeepsGoodOnes(t *testing.T) {
	f := patchFile(t, threeHunks)
	patch := f.Patch([]int{99, 0})
	if !strings.Contains(patch, "+ONE") {
		t.Errorf("patch lost the valid hunk:\n%s", patch)
	}
}

// A new file's old side is /dev/null, which is what tells git apply to create
// it rather than look for a file that is not there.
func TestPatchOfANewFileUsesDevNull(t *testing.T) {
	f := patchFile(t, `diff --git a/new.txt b/new.txt
new file mode 100644
--- /dev/null
+++ b/new.txt
@@ -0,0 +1,2 @@
+one
+two
`)
	patch := f.Patch([]int{0})
	if !strings.Contains(patch, "--- /dev/null\n") {
		t.Errorf("old side is not /dev/null:\n%s", patch)
	}
	if !strings.Contains(patch, "new file mode 100644\n") {
		t.Errorf("patch is missing the new file mode line:\n%s", patch)
	}
}

func TestPatchOfADeletedFileUsesDevNull(t *testing.T) {
	f := patchFile(t, `diff --git a/gone.txt b/gone.txt
deleted file mode 100644
--- a/gone.txt
+++ /dev/null
@@ -1,2 +0,0 @@
-one
-two
`)
	patch := f.Patch([]int{0})
	if !strings.Contains(patch, "+++ /dev/null\n") {
		t.Errorf("new side is not /dev/null:\n%s", patch)
	}
	if !strings.Contains(patch, "deleted file mode 100644\n") {
		t.Errorf("patch is missing the deleted file mode line:\n%s", patch)
	}
}

// Hunks are emitted in the order given, and the fragments come through exactly
// as parsed — no @@ arithmetic of ours, which is what keeps a stage honest.
func TestPatchEmitsHunksVerbatimInTheOrderAsked(t *testing.T) {
	f := patchFile(t, threeHunks)
	patch := f.Patch([]int{2, 0})
	twenty, one := strings.Index(patch, "+TWENTY"), strings.Index(patch, "+ONE")
	if twenty < 0 || one < 0 {
		t.Fatalf("patch is missing a selected hunk:\n%s", patch)
	}
	if twenty > one {
		t.Errorf("hunks came out sorted rather than in the order asked:\n%s", patch)
	}
	if !strings.Contains(patch, "@@ -20,3 +20,3 @@") {
		t.Errorf("the original @@ header was not preserved:\n%s", patch)
	}
}
