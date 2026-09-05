package diff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateContextLines(t *testing.T) {
	dir := t.TempDir()
	var a, b strings.Builder
	for i := 1; i <= 21; i++ {
		a.WriteString("line\n")
		if i == 11 {
			b.WriteString("CHANGED\n")
		} else {
			b.WriteString("line\n")
		}
	}
	pa := filepath.Join(dir, "a")
	pb := filepath.Join(dir, "b")
	if err := os.WriteFile(pa, []byte(a.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pb, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	wide, err := Generate(pa, pb, 5)
	if err != nil {
		t.Fatal(err)
	}
	narrow, err := Generate(pa, pb, 1)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(wide, "\n") <= strings.Count(narrow, "\n") {
		t.Errorf("more context should mean more lines: wide=%d narrow=%d",
			strings.Count(wide, "\n"), strings.Count(narrow, "\n"))
	}
}
