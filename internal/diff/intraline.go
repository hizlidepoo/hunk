package diff

import (
	"unicode"
	"unicode/utf8"

	"github.com/aymanbagabas/go-udiff"
)

// Range is a half-open byte range [Start, End) inside a line.
type Range struct {
	Start, End int
}

// changeLimit is the share of a line that may differ before intra-line
// highlighting is dropped. Past this, the two lines are not a rewrite of each
// other, and painting every other word emphasizes nothing.
const changeLimit = 0.5

// Intraline finds the parts of two paired lines that actually differ, so a
// one-word change reads as a one-word change instead of a whole red line next
// to a whole green one.
//
// Ranges are byte ranges into before and after respectively. It returns nothing when
// the lines are too different to be worth comparing.
func Intraline(before, after string) (beforeRanges, afterRanges []Range) {
	if before == after {
		return nil, nil
	}

	// udiff reports each edit as "replace old[Start:End] with New". The same
	// edit sits at a different offset in the new line, shifted by every edit
	// before it, so track that drift as we go.
	var shift, changedBefore, changedAfter int
	for _, e := range udiff.Strings(before, after) {
		if e.End > e.Start {
			r := snap(before, Range{e.Start, e.End})
			beforeRanges = append(beforeRanges, r)
			changedBefore += r.End - r.Start
		}
		if len(e.New) > 0 {
			start := e.Start + shift
			r := snap(after, Range{start, start + len(e.New)})
			afterRanges = append(afterRanges, r)
			changedAfter += r.End - r.Start
		}
		shift += len(e.New) - (e.End - e.Start)
	}

	if tooDifferent(changedBefore, len(before)) || tooDifferent(changedAfter, len(after)) {
		return nil, nil
	}
	return beforeRanges, afterRanges
}

func tooDifferent(changed, total int) bool {
	if total == 0 {
		return false
	}
	return float64(changed)/float64(total) > changeLimit
}

// snap widens a range to whole words, so highlighting lands on "value" rather
// than on the "alu" in the middle of it. It also keeps range edges on rune
// boundaries, which matters the moment a line contains anything non-ASCII.
func snap(s string, r Range) Range {
	r.Start = clamp(r.Start, 0, len(s))
	r.End = clamp(r.End, r.Start, len(s))

	for r.Start > 0 && isWord(prevRune(s, r.Start)) && isWord(nextRune(s, r.Start)) {
		_, size := utf8.DecodeLastRuneInString(s[:r.Start])
		r.Start -= size
	}
	for r.End < len(s) && isWord(prevRune(s, r.End)) && isWord(nextRune(s, r.End)) {
		_, size := utf8.DecodeRuneInString(s[r.End:])
		r.End += size
	}
	return r
}

func prevRune(s string, i int) rune {
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return r
}

func nextRune(s string, i int) rune {
	r, _ := utf8.DecodeRuneInString(s[i:])
	return r
}

func isWord(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
