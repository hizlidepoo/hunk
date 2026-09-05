package ui

import (
	"strings"
	"testing"
)

func TestExpandTabsShowWhitespace(t *testing.T) {
	// Off: tab becomes spaces, trailing spaces stay spaces.
	if got, _ := expandTabs("\tx  ", nil, false); got != "    x  " {
		t.Errorf("showWS off = %q", got)
	}
	// On: tab is an arrow then spaces to the stop; trailing spaces become dots.
	got, _ := expandTabs("\tx  ", nil, true)
	if got != "→   x··" {
		t.Errorf("showWS on = %q, want %q", got, "→   x··")
	}
	// A line with no tabs and no trailing spaces is untouched either way.
	if g, _ := expandTabs("plain", nil, true); g != "plain" {
		t.Errorf("plain line changed: %q", g)
	}
}

const wsDiff = "diff --git a/x b/x\n--- a/x\n+++ b/x\n" +
	"@@ -1,1 +1,2 @@\n keep\n+\ttabbed trail   \n"

// lineWith returns the first rendered line containing sub.
func lineWith(lines []string, sub string) string {
	for _, l := range lines {
		if strings.Contains(l, sub) {
			return l
		}
	}
	return ""
}

func TestShowWhitespaceToggleRenders(t *testing.T) {
	m := newTestModel(t, wsDiff)

	// Before: the tabbed line has neither an arrow nor trailing dots.
	before := lineWith(screen(t, m, 90, 20), "tabbed trail")
	if before == "" {
		t.Fatal("the changed line is not on screen")
	}
	if strings.Contains(before, tabMarker) || strings.Contains(before, spaceMark) {
		t.Fatalf("whitespace marks shown before toggling W: %q", before)
	}

	m.handleKey(keyPress("W"))
	if !m.showWS {
		t.Fatal("W did not turn on show-whitespace")
	}
	after := lineWith(screen(t, m, 90, 20), "tabbed trail")
	if !strings.Contains(after, tabMarker) {
		t.Errorf("tab not shown as an arrow after W: %q", after)
	}
	if !strings.Contains(after, spaceMark) {
		t.Errorf("trailing spaces not shown as dots after W: %q", after)
	}
}
