package diff

import (
	"fmt"
	"strings"
)

// Patch renders a patch containing only the named hunks of a file, ready for
// "git apply --cached".
//
// The hunks are emitted exactly as they were parsed, never recomputed: git is
// asked to recount the line numbers, so no arithmetic of ours can silently
// corrupt a patch and stage the wrong thing.
func (f File) Patch(hunks []int) string {
	if len(hunks) == 0 {
		return ""
	}

	// Resolve the selection before writing anything: if no index names a real
	// hunk there is nothing to stage, and a header on its own is a patch git
	// apply has no business seeing.
	selected := make([]int, 0, len(hunks))
	for _, i := range hunks {
		if i >= 0 && i < len(f.Hunks) {
			selected = append(selected, i)
		}
	}
	if len(selected) == 0 {
		return ""
	}

	oldName, newName := f.OldPath, f.NewPath
	if oldName == "" {
		oldName = newName
	}
	if newName == "" {
		newName = oldName
	}

	oldLabel, newLabel := "a/"+oldName, "b/"+newName
	if f.IsNew {
		oldLabel = "/dev/null"
	}
	if f.IsDelete {
		newLabel = "/dev/null"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "diff --git a/%s b/%s\n", oldName, newName)
	if f.IsNew {
		b.WriteString("new file mode 100644\n")
	}
	if f.IsDelete {
		b.WriteString("deleted file mode 100644\n")
	}
	fmt.Fprintf(&b, "--- %s\n+++ %s\n", oldLabel, newLabel)

	for _, i := range selected {
		b.WriteString(f.Hunks[i].frag.String())
	}
	return b.String()
}
