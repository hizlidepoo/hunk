// Package diff turns unified diff text into a structure the UI can render,
// and turns a selection of hunks back into a patch git can apply.
package diff

import (
	"hash/fnv"
	"strconv"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
)

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

// Key is a content-stable identity for a hunk: a hash of what each line does
// and says, but not of its line numbers. Two hunks with the same body match
// even after edits elsewhere shift their positions, and a hunk stops matching
// the moment its own content changes — which is how live-follow tells an
// untouched mark from one that needs re-approving.
func (h Hunk) Key() string {
	sum := fnv.New64a()
	for _, l := range h.Lines {
		sum.Write([]byte(strconv.Itoa(int(l.Kind))))
		sum.Write([]byte{0})
		sum.Write([]byte(l.Text))
		sum.Write([]byte{'\n'})
	}
	return strconv.FormatUint(sum.Sum64(), 36)
}
