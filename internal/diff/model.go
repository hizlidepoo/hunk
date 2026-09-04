// Package diff turns unified diff text into a structure the UI can render,
// and turns a selection of hunks back into a patch git can apply.
package diff

import "github.com/bluekeyes/go-gitdiff/gitdiff"

// Kind is what a line does: it stays, it arrives, or it leaves.
type Kind int

// The three things a line can do in a diff.
const (
	Context Kind = iota
	Added
	Removed
)

// Line is one line of a hunk. OldNum/NewNum are 1-based line numbers in the
// old and new file; they are 0 when the line does not exist on that side.
type Line struct {
	Kind   Kind
	Text   string
	OldNum int
	NewNum int
}

// Hunk is one @@ block.
type Hunk struct {
	Header   string // the full "@@ -1,4 +1,6 @@ func foo()" line
	OldStart int
	NewStart int
	Lines    []Line

	frag *gitdiff.TextFragment // source fragment, for verbatim patch emission
}

// File is one file's worth of changes.
type File struct {
	OldPath  string
	NewPath  string
	IsNew    bool
	IsDelete bool
	IsRename bool
	IsBinary bool
	Hunks    []Hunk

	Added   int
	Removed int
}

// Path is the name to show for a file: the new name, except for deletions.
func (f File) Path() string {
	if f.IsDelete || f.NewPath == "" {
		return f.OldPath
	}
	return f.NewPath
}
