package ui

import (
	"strings"
	"testing"

	"github.com/wmarquardt/hunk/internal/theme"
)

func spanCovering(spans []synSpan, pos int) (synSpan, bool) {
	for _, s := range spans {
		if s.start <= pos && pos < s.end {
			return s, true
		}
	}
	return synSpan{}, false
}

func TestHighlighterColorsTokens(t *testing.T) {
	h := newHighlighter("github-dark")

	spans := h.spans("main.go", "func main() {")
	kw, ok := spanCovering(spans, 0) // the "func" keyword
	if !ok || kw.fg == "" {
		t.Fatalf("keyword not colored: %+v", spans)
	}

	comment := h.spans("main.go", "// a note")
	c, ok := spanCovering(comment, 0)
	if !ok || c.fg == "" {
		t.Fatal("comment not colored")
	}
	if c.fg == kw.fg {
		t.Errorf("keyword and comment share a color %q — style not applied", c.fg)
	}

	// An unknown extension has no lexer, so nothing is colored.
	if h.spans("mystery.zzz", "func main() {") != nil {
		t.Error("unknown file type should return no spans")
	}
}

const goDiff = "diff --git a/m.go b/m.go\n--- a/m.go\n+++ b/m.go\n" +
	"@@ -1,1 +1,2 @@\n package main\n+func added() {}\n"

func TestSyntaxToggleChangesRendering(t *testing.T) {
	m := newTestModel(t, goDiff)
	screen(t, m, 100, 20) // give the model a size

	// The code text is on screen either way (screen() strips the color codes).
	if lineWith(screen(t, m, 100, 20), "func added") == "" {
		t.Fatal("added line not on screen")
	}

	on := m.render() // raw, with color codes
	m.handleKey(keyPress("H"))
	if m.syntax {
		t.Fatal("H did not turn syntax off")
	}
	off := m.render()

	if on == off {
		t.Error("syntax highlighting did not change how the frame renders")
	}
}

func TestSyntaxDefaultsOnAndOption(t *testing.T) {
	if m := New(nil, theme.Default(), Options{}); !m.syntax {
		t.Error("syntax should default on")
	}
	if m := New(nil, theme.Default(), Options{NoSyntax: true}); m.syntax {
		t.Error("--no-syntax should open with syntax off")
	}
	if !strings.HasPrefix(theme.Default().Syntax, "github") {
		t.Errorf("default theme syntax style = %q", theme.Default().Syntax)
	}
}
