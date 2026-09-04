// Package theme loads hunk's color themes: two built-ins, plus TOML files a
// user writes locally or pulls from a GitHub repo.
package theme

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// Theme is the full set of colors hunk can paint with. Every field is a hex
// color string ("#rgb" or "#rrggbb"); the UI converts them to terminal colors.
type Theme struct {
	Name string `toml:"name"`
	UI   UI     `toml:"ui"`
	Diff Diff   `toml:"diff"`
}

// UI colors the frame: panes, headers, sidebar, status bar.
//
// Accent is the one highlight color: every place hunk points at something —
// the @@ line, the current hunk, a change block's outline, the status bar, the
// selected file — is painted with it, so a theme changes its highlight once.
type UI struct {
	Background string `toml:"background"`
	Foreground string `toml:"foreground"`
	Border     string `toml:"border"`
	LineNumber string `toml:"line_number"`
	FileHeader string `toml:"file_header"`
	Accent     string `toml:"accent"`
	SidebarFg  string `toml:"sidebar_fg"`
}

// Diff colors the changes themselves, including the intra-line emphasis that
// marks which words on a line actually moved.
type Diff struct {
	AddedFg       string `toml:"added_fg"`
	AddedBg       string `toml:"added_bg"`
	RemovedFg     string `toml:"removed_fg"`
	RemovedBg     string `toml:"removed_bg"`
	ContextFg     string `toml:"context_fg"`
	AddedWordBg   string `toml:"added_word_bg"`
	RemovedWordBg string `toml:"removed_word_bg"`
}

var hexColor = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// Decode parses TOML on top of the default theme, so a file that specifies only
// a few keys is a valid theme: everything it leaves out is inherited.
//
// Unknown keys are reported as warnings rather than failing the load — a typo
// should cost you one color, not your diff.
func Decode(data []byte, source string) (t *Theme, warnings []string, err error) {
	return decodeInto(Default(), data, source)
}

// decodeInto decodes onto an existing theme, whatever it already holds. Decode
// starts from the default so unspecified keys are inherited; the built-in
// completeness test starts from a zero theme so unspecified keys stay empty and
// can be spotted.
func decodeInto(into *Theme, data []byte, source string) (t *Theme, warnings []string, err error) {
	meta, err := toml.Decode(string(data), into)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", source, err)
	}
	for _, key := range meta.Undecoded() {
		warnings = append(warnings, fmt.Sprintf("%s: unknown key %q", source, key.String()))
	}
	if err := into.Validate(source); err != nil {
		return nil, warnings, err
	}
	return into, warnings, nil
}

// Validate rejects a theme with a color hunk cannot paint. A bad color is worth
// an error: silently substituting one leaves a theme looking broken with no
// explanation of which key is wrong.
func (t *Theme) Validate(source string) error {
	for _, c := range colors(t) {
		if !hexColor.MatchString(c.value) {
			return fmt.Errorf("%s: %s = %q is not a hex color like \"#7aa2f7\"", source, c.key, c.value)
		}
	}
	return nil
}

type namedColor struct {
	key   string
	value string
}

// colors walks the theme's color fields by their TOML names, so adding a field
// to the schema automatically brings it under validation and under the built-in
// completeness test.
func colors(t *Theme) []namedColor {
	var out []namedColor
	v := reflect.ValueOf(t).Elem()
	for i := 0; i < v.NumField(); i++ {
		section := v.Field(i)
		if section.Kind() != reflect.Struct {
			continue // the Name field
		}
		sectionName := v.Type().Field(i).Tag.Get("toml")
		for j := 0; j < section.NumField(); j++ {
			key := section.Type().Field(j).Tag.Get("toml")
			out = append(out, namedColor{
				key:   strings.Join([]string{sectionName, key}, "."),
				value: section.Field(j).String(),
			})
		}
	}
	return out
}
