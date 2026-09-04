package diff

import "testing"

func TestHunkKeyIsContentStable(t *testing.T) {
	h := Hunk{Lines: []Line{
		{Kind: Context, Text: "keep"},
		{Kind: Removed, Text: "old"},
		{Kind: Added, Text: "new"},
	}}

	// The same content in a hunk at a different position (different line
	// numbers) keeps the same key.
	shifted := Hunk{OldStart: 99, NewStart: 99, Lines: h.Lines}
	if h.Key() != shifted.Key() {
		t.Errorf("key changed with line numbers: %s vs %s", h.Key(), shifted.Key())
	}

	// Changing what a line says changes the key.
	edited := Hunk{Lines: []Line{
		{Kind: Context, Text: "keep"},
		{Kind: Removed, Text: "old"},
		{Kind: Added, Text: "NEWER"},
	}}
	if h.Key() == edited.Key() {
		t.Error("key did not change when a line changed")
	}

	// Changing what a line does (kind) changes the key too.
	rekinded := Hunk{Lines: []Line{
		{Kind: Context, Text: "keep"},
		{Kind: Added, Text: "old"},
		{Kind: Added, Text: "new"},
	}}
	if h.Key() == rekinded.Key() {
		t.Error("key did not change when a line's kind changed")
	}
}
