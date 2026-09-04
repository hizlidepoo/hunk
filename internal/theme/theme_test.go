package theme

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinsAreCompleteAndValid(t *testing.T) {
	names := Builtins()
	if len(names) < 2 {
		t.Fatalf("got %d built-in themes (%v), want at least 2", len(names), names)
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			data, err := builtinFS.ReadFile("builtin/" + name + ".toml")
			if err != nil {
				t.Fatal(err)
			}

			// Decode into a zero Theme, not over the default: this checks the
			// file specifies every key itself, so adding a color to the schema
			// and forgetting it in a built-in fails here instead of silently
			// inheriting the default theme's value.
			var raw Theme
			if _, _, err := decodeInto(&raw, data, name); err != nil {
				t.Fatalf("does not parse: %v", err)
			}
			for _, c := range colors(&raw) {
				if c.value == "" {
					t.Errorf("%s is unset", c.key)
				}
			}
			if err := raw.Validate(name); err != nil {
				t.Error(err)
			}
			if raw.Name != name {
				t.Errorf("name = %q, want %q (it should match the filename)", raw.Name, name)
			}
		})
	}
}

func TestDefaultIsACopy(t *testing.T) {
	a, b := Default(), Default()
	a.Diff.AddedFg = "#000000"
	if b.Diff.AddedFg == "#000000" {
		t.Error("Default() handed out a shared theme; mutating one leaked into the other")
	}
}

func TestDecode(t *testing.T) {
	tests := []struct {
		name        string
		toml        string
		wantErr     string
		wantWarning string
		check       func(t *testing.T, th *Theme)
	}{
		{
			name: "full override of one key",
			toml: `[diff]
added_fg = "#123456"`,
			check: func(t *testing.T, th *Theme) {
				if th.Diff.AddedFg != "#123456" {
					t.Errorf("added_fg = %q", th.Diff.AddedFg)
				}
			},
		},
		{
			name: "unspecified keys inherit the default",
			toml: `name = "mine"
[diff]
added_fg = "#123456"`,
			check: func(t *testing.T, th *Theme) {
				def := Default()
				if th.Name != "mine" {
					t.Errorf("name = %q, want mine", th.Name)
				}
				for _, pair := range [][2]string{
					{th.Diff.RemovedFg, def.Diff.RemovedFg},
					{th.UI.Background, def.UI.Background},
					{th.UI.Accent, def.UI.Accent},
					{th.Diff.AddedWordBg, def.Diff.AddedWordBg},
				} {
					if pair[0] != pair[1] {
						t.Errorf("got %q, want inherited %q", pair[0], pair[1])
					}
				}
			},
		},
		{
			name: "empty file is the default theme",
			toml: "",
			check: func(t *testing.T, th *Theme) {
				if *th != *Default() {
					t.Error("an empty theme file should decode to the default theme")
				}
			},
		},
		{
			name: "three-digit hex is allowed",
			toml: `[ui]
background = "#abc"`,
			check: func(t *testing.T, th *Theme) {
				if th.UI.Background != "#abc" {
					t.Errorf("background = %q", th.UI.Background)
				}
			},
		},
		{
			name: "unknown key warns but still loads",
			toml: `[diff]
added_fg = "#123456"
adde_fg = "#654321"`,
			wantWarning: "adde_fg",
			check: func(t *testing.T, th *Theme) {
				if th.Diff.AddedFg != "#123456" {
					t.Errorf("the valid key next to the typo was dropped: %q", th.Diff.AddedFg)
				}
			},
		},
		{
			name: "unknown section warns but still loads",
			toml: `[colours]
added = "#123456"`,
			wantWarning: "colours",
		},
		{
			name:    "malformed TOML errors",
			toml:    `[diff` + "\n" + `added_fg = `,
			wantErr: "test.toml",
		},
		{
			name: "non-hex color errors and names the key",
			toml: `[diff]
added_fg = "green"`,
			wantErr: "diff.added_fg",
		},
		{
			name: "hex without a leading # errors",
			toml: `[ui]
background = "1a1b26"`,
			wantErr: "ui.background",
		},
		{
			name: "wrong-length hex errors",
			toml: `[ui]
border = "#12345"`,
			wantErr: "ui.border",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			th, warnings, err := Decode([]byte(tt.toml), "test.toml")

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("want an error mentioning %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not mention %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantWarning != "" {
				joined := strings.Join(warnings, "\n")
				if !strings.Contains(joined, tt.wantWarning) {
					t.Errorf("warnings %q do not mention %q", joined, tt.wantWarning)
				}
			} else if len(warnings) != 0 {
				t.Errorf("unexpected warnings: %v", warnings)
			}

			if tt.check != nil {
				tt.check(t, th)
			}
		})
	}
}

func TestLoadResolution(t *testing.T) {
	config := t.TempDir()
	themes := filepath.Join(config, "themes")
	if err := os.MkdirAll(themes, 0o755); err != nil {
		t.Fatal(err)
	}

	userTheme := filepath.Join(themes, "mine.toml")
	if err := os.WriteFile(userTheme, []byte("name = \"mine\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	loose := filepath.Join(config, "loose.toml")
	if err := os.WriteFile(loose, []byte("name = \"loose\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	l := &Loader{ConfigDir: config}

	tests := []struct {
		ref      string
		wantName string
		wantErr  bool
	}{
		{ref: "", wantName: DefaultName},
		{ref: "hunk-dark", wantName: "hunk-dark"},
		{ref: "paper", wantName: "paper"},
		{ref: "mine", wantName: "mine"},
		{ref: loose, wantName: "loose"},
		{ref: "nope", wantErr: true},
		{ref: "gitlab.com/user/repo/x", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			th, _, err := l.Load(tt.ref)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if th.Name != tt.wantName {
				t.Errorf("name = %q, want %q", th.Name, tt.wantName)
			}
		})
	}
}

// A user theme named like a built-in wins: it is the more specific choice, and
// it is the only way to override a built-in you dislike.
func TestLoadUserThemeShadowsBuiltin(t *testing.T) {
	config := t.TempDir()
	themes := filepath.Join(config, "themes")
	if err := os.MkdirAll(themes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(themes, "paper.toml"), []byte("name = \"my-paper\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	th, _, err := (&Loader{ConfigDir: config}).Load("paper")
	if err != nil {
		t.Fatal(err)
	}
	if th.Name != "my-paper" {
		t.Errorf("name = %q, want the user's file to win", th.Name)
	}
}

func TestParseRemote(t *testing.T) {
	tests := []struct {
		ref                      string
		user, repo, name, gitRef string
		wantErr                  bool
	}{
		{ref: "github.com/u/r/dracula", user: "u", repo: "r", name: "dracula", gitRef: "HEAD"},
		{ref: "github.com/u/r/dracula@v1.2.0", user: "u", repo: "r", name: "dracula", gitRef: "v1.2.0"},
		{ref: "github.com/u/r/dark/dracula", user: "u", repo: "r", name: "dark/dracula", gitRef: "HEAD"},
		{ref: "github.com/u/r/dracula.toml", user: "u", repo: "r", name: "dracula", gitRef: "HEAD"},
		{ref: "github.com/u/r", wantErr: true},
		{ref: "github.com/u/r/", wantErr: true},
		{ref: "github.com/u/r/x@", wantErr: true},
		{ref: "github.com/u/../../etc/passwd", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			got, err := parseRemote(tt.ref)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := remoteRef{tt.user, tt.repo, tt.name, tt.gitRef}
			if got != want {
				t.Errorf("got %+v, want %+v", got, want)
			}
		})
	}
}

func TestFetchRemote(t *testing.T) {
	const body = "name = \"dracula\"\n[diff]\nadded_fg = \"#50fa7b\"\n"

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		switch r.URL.Path {
		case "/u/r/HEAD/themes/dracula.toml", "/u/r/v1.0.0/themes/dracula.toml":
			_, _ = w.Write([]byte(body))
		case "/u/r/HEAD/rootonly.toml":
			_, _ = w.Write([]byte("name = \"rootonly\"\n"))
		case "/u/r/HEAD/themes/broken.toml":
			_, _ = w.Write([]byte("[diff]\nadded_fg = \"not-a-color\"\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	config := t.TempDir()
	l := &Loader{ConfigDir: config, BaseURL: srv.URL, Client: srv.Client()}

	t.Run("downloads and caches", func(t *testing.T) {
		th, _, err := l.Load("github.com/u/r/dracula")
		if err != nil {
			t.Fatal(err)
		}
		if th.Name != "dracula" || th.Diff.AddedFg != "#50fa7b" {
			t.Errorf("got %+v", th)
		}
		// Keys the remote theme omitted still come from the default.
		if th.UI.Background != Default().UI.Background {
			t.Error("remote theme did not inherit unspecified keys")
		}

		cache := filepath.Join(config, "themes", "github.com", "u", "r", "dracula@HEAD.toml")
		if _, err := os.Stat(cache); err != nil {
			t.Errorf("not cached at %s: %v", cache, err)
		}
	})

	t.Run("cache hit skips the network", func(t *testing.T) {
		before := hits
		if _, _, err := l.Load("github.com/u/r/dracula"); err != nil {
			t.Fatal(err)
		}
		if hits != before {
			t.Errorf("made %d request(s) despite a warm cache", hits-before)
		}
	})

	t.Run("refresh re-downloads", func(t *testing.T) {
		before := hits
		refresher := &Loader{ConfigDir: config, BaseURL: srv.URL, Client: srv.Client(), Refresh: true}
		if _, _, err := refresher.Load("github.com/u/r/dracula"); err != nil {
			t.Fatal(err)
		}
		if hits == before {
			t.Error("--theme-update did not hit the network")
		}
	})

	t.Run("pinned ref caches separately", func(t *testing.T) {
		if _, _, err := l.Load("github.com/u/r/dracula@v1.0.0"); err != nil {
			t.Fatal(err)
		}
		cache := filepath.Join(config, "themes", "github.com", "u", "r", "dracula@v1.0.0.toml")
		if _, err := os.Stat(cache); err != nil {
			t.Errorf("pinned theme not cached separately: %v", err)
		}
	})

	t.Run("falls back to the repo root", func(t *testing.T) {
		th, _, err := l.Load("github.com/u/r/rootonly")
		if err != nil {
			t.Fatal(err)
		}
		if th.Name != "rootonly" {
			t.Errorf("name = %q", th.Name)
		}
	})

	t.Run("404 errors", func(t *testing.T) {
		if _, _, err := l.Load("github.com/u/r/missing"); err == nil {
			t.Fatal("want an error for a theme that is not there")
		}
	})

	t.Run("malformed remote theme errors", func(t *testing.T) {
		if _, _, err := l.Load("github.com/u/r/broken"); err == nil {
			t.Fatal("want an error for a remote theme with a bad color")
		}
	})

	t.Run("cache serves the theme when the network is gone", func(t *testing.T) {
		offline := &Loader{
			ConfigDir: config,
			BaseURL:   "http://127.0.0.1:1", // nothing listens here
			Client:    srv.Client(),
		}
		th, _, err := offline.Load("github.com/u/r/dracula")
		if err != nil {
			t.Fatalf("cached theme should load with no network: %v", err)
		}
		if th.Name != "dracula" {
			t.Errorf("name = %q", th.Name)
		}
	})
}

func TestConfigDirRespectsEnv(t *testing.T) {
	t.Setenv("HUNK_CONFIG_DIR", "/tmp/hunk-test-config")
	if got := ConfigDir(); got != "/tmp/hunk-test-config" {
		t.Errorf("ConfigDir() = %q", got)
	}
}
