package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// runSearch drives the / prompt: open, type the query, press enter.
func runSearch(t *testing.T, m *Model, query string) {
	t.Helper()
	m.handleKey(keyPress("/"))
	if !m.searchInput {
		t.Fatal("/ did not open the search prompt")
	}
	for _, r := range query {
		m.handleKey(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
}

func TestMatchTextSmartcase(t *testing.T) {
	if !matchText("Hello World", "world") {
		t.Error("lowercase query should match case-insensitively")
	}
	if matchText("Hello World", "World!") {
		t.Error("query with uppercase should match exactly (no match here)")
	}
	if !matchText("Hello World", "World") {
		t.Error("uppercase query should match the exact case")
	}
	if matchText("anything", "") {
		t.Error("empty query should never match")
	}
}

const searchDiff = "diff --git a/x b/x\n--- a/x\n+++ b/x\n" +
	"@@ -1,1 +1,4 @@\n keep\n+alpha needle\n+beta\n+gamma needle\n"

func TestSearchJumpsAndRepeats(t *testing.T) {
	m := newTestModel(t, searchDiff)
	screen(t, m, 120, 30)

	runSearch(t, m, "needle")
	if m.search != "needle" || m.searchInput {
		t.Fatalf("after enter: search=%q input=%v", m.search, m.searchInput)
	}
	first := m.cur
	if !strings.Contains(m.rowSearchText(first), "needle") {
		t.Fatalf("cursor did not land on a match: %q", m.rowSearchText(first))
	}

	m.command("n") // next match
	second := m.cur
	if second == first || !strings.Contains(m.rowSearchText(second), "needle") {
		t.Fatalf("n did not advance to the next match: %d -> %d", first, second)
	}

	m.command("n") // wraps back to the first
	if m.cur != first {
		t.Errorf("n did not wrap around: got %d, want %d", m.cur, first)
	}

	m.command("N") // previous match
	if m.cur != second {
		t.Errorf("N did not go back: got %d, want %d", m.cur, second)
	}
}

func TestSearchNoMatchKeepsCursor(t *testing.T) {
	m := newTestModel(t, searchDiff)
	screen(t, m, 120, 30)
	m.moveTo(2)
	before := m.cur

	runSearch(t, m, "zzzznope")
	if m.cur != before {
		t.Errorf("a failed search moved the cursor: %d -> %d", before, m.cur)
	}
	if !strings.Contains(m.msg, "no match") {
		t.Errorf("status = %q, want a no-match message", m.msg)
	}
}

func TestEscClearsSearchSoNReturnsToHunkNav(t *testing.T) {
	m := newTestModel(t, searchDiff)
	screen(t, m, 120, 30)
	runSearch(t, m, "needle")

	m.command("esc")
	if m.search != "" {
		t.Fatalf("esc did not clear the search: %q", m.search)
	}
	// With no active search, n is next-hunk again.
	m.moveTo(0)
	m.command("n")
	if m.cur == 0 {
		t.Error("with search cleared, n should move to the next hunk")
	}
}
