package diff

import (
	"fmt"
	"strings"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
)

// ParseString reads unified diff text. Every input to hunk becomes unified diff
// text first, so this is the only place a diff is interpreted.
func ParseString(s string) ([]File, error) {
	gfs, _, err := gitdiff.Parse(strings.NewReader(s))
	if err != nil {
		return nil, fmt.Errorf("parse diff: %w", err)
	}

	files := make([]File, 0, len(gfs))
	for _, gf := range gfs {
		files = append(files, convertFile(gf))
	}
	return files, nil
}

func convertFile(gf *gitdiff.File) File {
	f := File{
		OldPath:  gf.OldName,
		NewPath:  gf.NewName,
		IsNew:    gf.IsNew,
		IsDelete: gf.IsDelete,
		IsRename: gf.IsRename,
		IsBinary: gf.IsBinary,
	}
	for _, frag := range gf.TextFragments {
		h := convertHunk(frag)
		f.Added += int(frag.LinesAdded)
		f.Removed += int(frag.LinesDeleted)
		f.Hunks = append(f.Hunks, h)
	}
	return f
}

func convertHunk(frag *gitdiff.TextFragment) Hunk {
	h := Hunk{
		Header:   strings.TrimRight(frag.Header(), "\n"),
		OldStart: int(frag.OldPosition),
		NewStart: int(frag.NewPosition),
		Lines:    make([]Line, 0, len(frag.Lines)),
		frag:     frag,
	}

	oldNum, newNum := int(frag.OldPosition), int(frag.NewPosition)
	for _, gl := range frag.Lines {
		l := Line{Text: strings.TrimRight(gl.Line, "\n")}
		switch gl.Op {
		case gitdiff.OpAdd:
			l.Kind, l.NewNum = Added, newNum
			newNum++
		case gitdiff.OpDelete:
			l.Kind, l.OldNum = Removed, oldNum
			oldNum++
		default: // OpContext
			l.Kind, l.OldNum, l.NewNum = Context, oldNum, newNum
			oldNum++
			newNum++
		}
		h.Lines = append(h.Lines, l)
	}
	return h
}
