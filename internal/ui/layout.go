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
	RowFile        RowKind = iota // a file's header line
	RowHunk                       // an @@ line
	RowPair                       // one line of the diff, on one or both sides
	RowNotice                     // "Binary file changed", "no changes", etc.
	RowSpacer                     // blank separator between files
	RowBlockTop                   // the rule above a changed block
	RowBlockBottom                // the rule below a changed block
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
	// BoxLeft and BoxRight say what a change block's outline does in each pane
	// on this row. Each pane closes on its own last changed line, so a one-line
	// removal that became seven lines shows a short box facing a tall one.
	BoxLeft, BoxRight BoxPart
	// Arrow marks the row that carries the direction marker between the panes.
	Arrow bool
}

// BoxPart is one pane's share of a change block's outline on a single row.
type BoxPart int

// The parts of an outline a row can carry.
const (
	BoxNone   BoxPart = iota // this pane is outside the block here
	BoxTop                   // the rule above it
	BoxMid                   // enclosed: the box's sides run down this row
	BoxBottom                // the rule below it
)

// View is the whole render-ready diff plus the indexes navigation needs.
type View struct {
	Rows []Row
	// FileRows[i] is the row index of file i's header.
	FileRows []int
	// HunkRows[i] is the row index of the i-th hunk across all files, in order.
	HunkRows []int
}

// Build flattens parsed files into rows. In split mode a changed line and its
// replacement share one row; in unified mode they become two rows, so a row is
// always exactly one line on screen either way.
func Build(files []diff.File, split bool) *View {
	v := &View{}

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
			for _, r := range wrapBlocks(hunkRows(h, split), split) {
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
		for i := 0; i < max(len(removed), len(added)); i++ {
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

// wrapBlocks outlines every run of changed rows. The outline is one shape
// crossing both panes, but each pane closes on its own last changed line, so a
// one-line removal that became seven additions draws a short box on the left
// facing a tall one on the right — the outline itself shows the shape of the
// change. Unified view has a single column, so there both panes move together.
//
// It runs on a hunk's rows before they are appended to the view, so the row
// indexes Build records for files and hunks stay correct.
//
// ponytail: the outline's rule rows are ordinary rows, so j/k stops on them and
// the cursor can rest on a rule. They carry the hunk's identity, so marking and
// staging from there still act on the right hunk. Skipping them means giving
// moveTo a direction at every call site; do that if it grates in use.
func wrapBlocks(rows []Row, split bool) []Row {
	out := make([]Row, 0, len(rows))
	for i := 0; i < len(rows); {
		if !changedRow(rows[i]) {
			out = append(out, rows[i])
			i++
			continue
		}

		// The run is every changed row from here on; context ends it.
		j := i
		lastLeft, lastRight := -1, -1
		for j < len(rows) && changedRow(rows[j]) {
			if !rows[j].Left.Empty {
				lastLeft = j
			}
			if !rows[j].Right.Empty {
				lastRight = j
			}
			j++
		}
		if !split {
			// One column: both edges belong to the same box, which ends with
			// the run.
			lastLeft, lastRight = j-1, j-1
		}

		edge := Row{FileIdx: rows[i].FileIdx, HunkIdx: rows[i].HunkIdx, Kind: RowBlockTop}
		edge.BoxLeft, edge.BoxRight = part(lastLeft >= 0, BoxTop), part(lastRight >= 0, BoxTop)
		out = append(out, edge)

		for k := i; k < j; k++ {
			rows[k].BoxLeft = spanPart(k, lastLeft)
			rows[k].BoxRight = spanPart(k, lastRight)
			// The direction marker goes on the first enclosed row of a change
			// that crosses panes; a pure addition or deletion has no direction.
			rows[k].Arrow = k == i && split && lastLeft >= 0 && lastRight >= 0
			out = append(out, rows[k])
		}

		edge.Kind = RowBlockBottom
		edge.BoxLeft, edge.BoxRight = part(lastLeft == j-1, BoxBottom), part(lastRight == j-1, BoxBottom)
		out = append(out, edge)
		i = j
	}
	return out
}

// spanPart places a row inside one pane's box: enclosed while the pane still
// has changed lines coming, the closing rule on the row right after its last
// one, and nothing at all once the box is shut.
func spanPart(row, last int) BoxPart {
	switch {
	case last < 0 || row > last+1:
		return BoxNone
	case row == last+1:
		return BoxBottom
	default:
		return BoxMid
	}
}

func part(ok bool, p BoxPart) BoxPart {
	if ok {
		return p
	}
	return BoxNone
}

// changedRow reports whether a row carries a change on either side, which is
// what a block is made of.
func changedRow(r Row) bool {
	if r.Kind != RowPair {
		return false
	}
	return r.Left.Kind == diff.Added || r.Left.Kind == diff.Removed ||
		r.Right.Kind == diff.Added || r.Right.Kind == diff.Removed
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
