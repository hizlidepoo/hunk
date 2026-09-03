// Package ui renders parsed diffs into a terminal view.
package ui

import (
	"fmt"

	"github.com/wmarquardt/hunk/internal/diff"
)

// RowKind says what a row in the flattened view is.
type RowKind int

// The kinds of row the flattened view is made of.
const (
	RowFile   RowKind = iota // a file's header line
	RowHunk                  // an @@ line
	RowPair                  // one line of the diff, on one or both sides
	RowNotice                // "Binary file changed", "no changes", etc.
	RowSpacer                // blank separator between files
)

// Side is one half of a paired row. Empty means this side has no line here,
// which is what a pure addition or deletion looks like side by side.
type Side struct {
	Kind   diff.Kind
	Num    int // 1-based line number; 0 when Empty
	Text   string
	Ranges []diff.Range // byte ranges within Text that actually changed
	Empty  bool
}

// Row is one visual line of the whole session: every file, flattened into a
// single list. Flattening keeps scrolling, windowing, and next/prev navigation
// to simple index arithmetic instead of nested cursors.
type Row struct {
	Kind        RowKind
	FileIdx     int
	HunkIdx     int // -1 outside a hunk
	Text        string
	Left, Right Side
}

// View is the whole render-ready diff plus the indexes navigation needs.
type View struct {
	Rows []Row
	// FileRows[i] is the row index of file i's header.
	FileRows []int
	// HunkRows[i] is the row index of the i-th hunk across all files, in order.
	HunkRows []int
	Files    []diff.File
}

// Build flattens parsed files into rows. In split mode a changed line and its
// replacement share one row; in unified mode they become two rows, so a row is
// always exactly one line on screen either way.
func Build(files []diff.File, split bool) *View {
	v := &View{Files: files}

	for fi, f := range files {
		v.FileRows = append(v.FileRows, len(v.Rows))
		v.Rows = append(v.Rows, Row{
			Kind:    RowFile,
			FileIdx: fi,
			HunkIdx: -1,
			Text:    fileHeaderText(f),
		})

		switch {
		case f.IsBinary:
			v.Rows = append(v.Rows, Row{
				Kind: RowNotice, FileIdx: fi, HunkIdx: -1,
				Text: "Binary file — contents not shown",
			})
		case len(f.Hunks) == 0:
			v.Rows = append(v.Rows, Row{
				Kind: RowNotice, FileIdx: fi, HunkIdx: -1,
				Text: "No content changes",
			})
		}

		for hi, h := range f.Hunks {
			v.HunkRows = append(v.HunkRows, len(v.Rows))
			v.Rows = append(v.Rows, Row{
				Kind: RowHunk, FileIdx: fi, HunkIdx: hi, Text: h.Header,
			})
			for _, r := range hunkRows(h, split) {
				r.FileIdx, r.HunkIdx = fi, hi
				v.Rows = append(v.Rows, r)
			}
		}

		if fi < len(files)-1 {
			v.Rows = append(v.Rows, Row{Kind: RowSpacer, FileIdx: fi, HunkIdx: -1})
		}
	}
	return v
}

func fileHeaderText(f diff.File) string {
	name := f.Path()
	switch {
	case f.IsRename:
		name = fmt.Sprintf("%s → %s", f.OldPath, f.NewPath)
	case f.IsNew:
		name += "  (new)"
	case f.IsDelete:
		name += "  (deleted)"
	}
	return fmt.Sprintf("%s  +%d -%d", name, f.Added, f.Removed)
}

// hunkRows pairs a hunk's lines for side-by-side display: context lines sit on
// both sides, and a run of removals is zipped with the run of additions that
// follows it, so a changed line lines up with what it changed into.
func hunkRows(h diff.Hunk, split bool) []Row {
	var rows []Row
	var removed, added []diff.Line

	flush := func() {
		n := len(removed)
		if len(added) > n {
			n = len(added)
		}
		for i := 0; i < n; i++ {
			var row Row
			row.Kind = RowPair
			if i < len(removed) {
				row.Left = leftSide(removed[i])
			} else {
				row.Left = Side{Empty: true}
			}
			if i < len(added) {
				row.Right = rightSide(added[i])
			} else {
				row.Right = Side{Empty: true}
			}
			// Only a row with a line on both sides is a rewrite of one line into
			// another; anything else has nothing to compare against.
			if !row.Left.Empty && !row.Right.Empty {
				row.Left.Ranges, row.Right.Ranges = diff.Intraline(row.Left.Text, row.Right.Text)
			}
			if split {
				rows = append(rows, row)
				continue
			}
			// Unified: the removed line and the added line are stacked, not
			// paired, so each gets a row of its own.
			if !row.Left.Empty {
				rows = append(rows, Row{Kind: RowPair, Left: row.Left, Right: Side{Empty: true}})
			}
			if !row.Right.Empty {
				rows = append(rows, Row{Kind: RowPair, Left: Side{Empty: true}, Right: row.Right})
			}
		}
		removed, added = nil, nil
	}

	for _, l := range h.Lines {
		switch l.Kind {
		case diff.Removed:
			if len(added) > 0 {
				flush() // a new run starts here
			}
			removed = append(removed, l)
		case diff.Added:
			added = append(added, l)
		default:
			flush()
			rows = append(rows, Row{
				Kind:  RowPair,
				Left:  leftSide(l),
				Right: rightSide(l),
			})
		}
	}
	flush()
	return rows
}

func leftSide(l diff.Line) Side  { return Side{Kind: l.Kind, Num: l.OldNum, Text: l.Text} }
func rightSide(l diff.Line) Side { return Side{Kind: l.Kind, Num: l.NewNum, Text: l.Text} }

// NextIndex returns the first value in sorted that is greater than cur, or the
// last value when there is none. Used for next-hunk and next-file jumps.
func NextIndex(sorted []int, cur int) int {
	for _, v := range sorted {
		if v > cur {
			return v
		}
	}
	if len(sorted) == 0 {
		return cur
	}
	return sorted[len(sorted)-1]
}

// PrevIndex is NextIndex in reverse.
func PrevIndex(sorted []int, cur int) int {
	for i := len(sorted) - 1; i >= 0; i-- {
		if sorted[i] < cur {
			return sorted[i]
		}
	}
	if len(sorted) == 0 {
		return cur
	}
	return sorted[0]
}
