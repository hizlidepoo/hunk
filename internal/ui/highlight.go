package ui

import (
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
)

// synSpan is a byte range of a line and the foreground hex color syntax
// highlighting wants for it. An empty fg means "leave the line's own color".
type synSpan struct {
	start, end int
	fg         string
}

// highlighter turns a line of code into colored spans using a chroma style. It
// caches the lexer per file path and the spans per line, so re-rendering while
// scrolling costs nothing.
type highlighter struct {
	style  *chroma.Style
	lexers map[string]chroma.Lexer // path -> lexer (nil cached for "no lexer")
	lines  map[string][]synSpan    // lexerName\x00text -> spans
}

func newHighlighter(styleName string) *highlighter {
	return &highlighter{
		style:  chromastyles.Get(styleName), // falls back to a default when unknown
		lexers: map[string]chroma.Lexer{},
		lines:  map[string][]synSpan{},
	}
}

func (h *highlighter) lexerFor(path string) chroma.Lexer {
	if lx, ok := h.lexers[path]; ok {
		return lx
	}
	lx := lexers.Match(path)
	h.lexers[path] = lx
	return lx
}

// spans returns the colored spans for one line of the file at path, or nil when
// no lexer matches (the line then keeps the diff's own colors).
func (h *highlighter) spans(path, text string) []synSpan {
	if text == "" {
		return nil
	}
	lx := h.lexerFor(path)
	if lx == nil {
		return nil
	}
	key := lx.Config().Name + "\x00" + text
	if s, ok := h.lines[key]; ok {
		return s
	}
	s := tokenSpans(h.style, lx, text)
	h.lines[key] = s
	return s
}

// tokenSpans tokenises a single line and maps each token to a foreground span.
// Tokenising a line on its own can miscolor multi-line constructs (a block
// comment, a string across lines); that is an accepted v1 limit.
func tokenSpans(style *chroma.Style, lx chroma.Lexer, text string) []synSpan {
	it, err := lx.Tokenise(nil, text)
	if err != nil {
		return nil
	}
	var spans []synSpan
	off := 0
	for _, t := range it.Tokens() {
		n := len(t.Value)
		if c := style.Get(t.Type).Colour; c.IsSet() {
			spans = append(spans, synSpan{off, off + n, c.String()})
		}
		off += n
	}
	return spans
}
