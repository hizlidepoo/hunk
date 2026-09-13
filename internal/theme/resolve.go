package theme

import (
	"embed"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

//go:embed builtin/*.toml
var builtinFS embed.FS

// DefaultName is the theme hunk uses when nothing else is asked for.
const DefaultName = "hunk-dark"

// maxThemeBytes caps a downloaded theme. A theme is a few hundred bytes of hex
// colors; anything near this limit is not one.
const maxThemeBytes = 256 << 10

var defaultTheme = mustLoadBuiltin(DefaultName)

// Default returns a copy of the built-in default theme. Every other theme is
// decoded on top of this copy, which is what makes partial theme files work.
func Default() *Theme {
	t := defaultTheme
	return &t
}

// Builtins lists the theme names that ship inside the binary.
func Builtins() []string {
	entries, err := builtinFS.ReadDir("builtin")
	if err != nil {
		panic(err) // embedded at build time; missing means a broken binary
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(e.Name(), ".toml"))
	}
	return names
}

func mustLoadBuiltin(name string) Theme {
	data, err := builtinFS.ReadFile("builtin/" + name + ".toml")
	if err != nil {
		panic(fmt.Sprintf("built-in theme %q missing: %v", name, err))
	}
	var t Theme
	if _, err := toml.Decode(string(data), &t); err != nil {
		panic(fmt.Sprintf("built-in theme %q does not parse: %v", name, err))
	}
	if err := t.Validate(name); err != nil {
		panic(fmt.Sprintf("built-in theme %q is invalid: %v", name, err))
	}
	return t
}

// ConfigDir is where hunk keeps user themes, including downloaded ones.
func ConfigDir() string {
	if d := os.Getenv("HUNK_CONFIG_DIR"); d != "" {
		return d
	}
	if runtime.GOOS == "windows" {
		if d, err := os.UserConfigDir(); err == nil {
			return filepath.Join(d, "hunk")
		}
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "hunk")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".hunk"
	}
	return filepath.Join(home, ".config", "hunk")
}

// Loader resolves a theme reference into a Theme.
type Loader struct {
	// ConfigDir holds themes/ ; defaults to ConfigDir().
	ConfigDir string
	// BaseURL is where github.com/... references are fetched from. Tests point
	// this at an httptest server so no test ever touches the network.
	BaseURL string
	// Refresh re-downloads remote themes instead of using the cached copy.
	Refresh bool
}

const defaultBaseURL = "https://raw.githubusercontent.com"

// httpClient fetches remote themes. The timeout is what keeps a hung server
// from hanging hunk's startup.
var httpClient = &http.Client{Timeout: 10 * time.Second}

func (l *Loader) configDir() string {
	if l.ConfigDir != "" {
		return l.ConfigDir
	}
	return ConfigDir()
}

func (l *Loader) baseURL() string {
	if l.BaseURL != "" {
		return l.BaseURL
	}
	return defaultBaseURL
}

// Load resolves a theme reference, in order: an existing file path, a theme in
// the user's themes directory, a built-in name, then a github.com/... reference.
func (l *Loader) Load(ref string) (*Theme, []string, error) {
	if ref == "" || ref == DefaultName {
		return Default(), nil, nil
	}

	if data, source, ok := l.readLocal(ref); ok {
		return Decode(data, source)
	}
	if data, err := builtinFS.ReadFile("builtin/" + ref + ".toml"); err == nil {
		return Decode(data, ref)
	}
	// ponytail: github.com only. Add gitlab/codeberg/sourcehut when someone asks
	// for them — each is another URL shape to get right and keep working.
	if strings.HasPrefix(ref, "github.com/") {
		data, source, err := l.fetchRemote(ref)
		if err != nil {
			return nil, nil, err
		}
		return Decode(data, source)
	}
	return nil, nil, fmt.Errorf("unknown theme %q: expected a file, one of %v, or a github.com/user/repo/name reference",
		ref, Builtins())
}

// readLocal tries the reference as a path on disk, then as a name in the user's
// themes directory.
func (l *Loader) readLocal(ref string) (data []byte, source string, ok bool) {
	candidates := []string{expandHome(ref)}
	if !strings.ContainsAny(ref, `/\`) {
		candidates = append(candidates, filepath.Join(l.configDir(), "themes", ref+".toml"))
	}
	for _, c := range candidates {
		if b, err := os.ReadFile(c); err == nil {
			return b, c, true
		}
	}
	return nil, "", false
}

func expandHome(p string) string {
	if !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, p[2:])
}

// remoteRef is a parsed github.com/user/repo/name[@gitref] theme reference.
type remoteRef struct {
	user, repo, name, gitRef string
}

func parseRemote(ref string) (remoteRef, error) {
	bad := func() (remoteRef, error) {
		return remoteRef{}, fmt.Errorf("bad theme reference %q: want github.com/user/repo/name[@ref]", ref)
	}
	if strings.Contains(ref, "..") {
		return bad()
	}

	rest := strings.TrimPrefix(ref, "github.com/")
	gitRef := "HEAD"
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		gitRef, rest = rest[at+1:], rest[:at]
		if gitRef == "" {
			return bad()
		}
	}

	parts := strings.Split(rest, "/")
	if len(parts) < 3 {
		return bad()
	}
	for _, p := range parts {
		if p == "" {
			return bad()
		}
	}
	return remoteRef{
		user:   parts[0],
		repo:   parts[1],
		name:   strings.TrimSuffix(path.Join(parts[2:]...), ".toml"),
		gitRef: gitRef,
	}, nil
}

// fetchRemote returns a theme from GitHub, using the on-disk cache when it can.
// A cache hit never touches the network, so a theme is downloaded once and hunk
// keeps working offline afterwards.
func (l *Loader) fetchRemote(ref string) (data []byte, source string, err error) {
	r, err := parseRemote(ref)
	if err != nil {
		return nil, "", err
	}

	cache := filepath.Join(l.configDir(), "themes", "github.com",
		r.user, r.repo, filepath.FromSlash(r.name)+"@"+r.gitRef+".toml")

	if !l.Refresh {
		if b, err := os.ReadFile(cache); err == nil {
			return b, cache, nil
		}
	}

	// Repos usually keep themes in themes/, but a single-theme repo may put the
	// file at the root; try both before giving up.
	urls := []string{
		fmt.Sprintf("%s/%s/%s/%s/themes/%s.toml", l.baseURL(), r.user, r.repo, r.gitRef, r.name),
		fmt.Sprintf("%s/%s/%s/%s/%s.toml", l.baseURL(), r.user, r.repo, r.gitRef, r.name),
	}

	var lastErr error
	for _, u := range urls {
		b, err := l.get(u)
		if err != nil {
			lastErr = err
			continue
		}
		if err := os.MkdirAll(filepath.Dir(cache), 0o755); err == nil {
			// A cache we cannot write is not fatal: the theme still loads, it
			// just costs a request next time.
			_ = os.WriteFile(cache, b, 0o644)
		}
		return b, ref, nil
	}

	// Network trouble should not cost you a theme you already downloaded.
	if b, readErr := os.ReadFile(cache); readErr == nil {
		return b, cache, nil
	}
	return nil, "", fmt.Errorf("fetch theme %s: %w", ref, lastErr)
}

func (l *Loader) get(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxThemeBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxThemeBytes {
		return nil, fmt.Errorf("%s: theme is larger than %d bytes", url, maxThemeBytes)
	}
	return b, nil
}
