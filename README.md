# hunk

**Your AI's code might be ugly. The diff won't be.**

A standalone, themeable diff viewer for the terminal. Side-by-side, word-level
highlighting, keyboard-driven — and when you run it inside a dirty git
repository, it turns into a review pass you can stage from, hunk by hunk.

Built for the diffs coding agents produce: large, spread across many files, and
miserable to read as raw `git diff` output.

```
┌ Files ────┬─ auth/token.go ──────────────────────────────────────────────────┐
│ ● auth/…  │ @@ -18,5 +18,5 @@ func Validate(tok string) error                │
│   +12 -4  │  18 │     if tok == "" {      18 │     if tok == "" {             │
│ ◐ ui/vie… │  19 │       return ErrEmpty    19 │       return fmt.Errorf("empt │
│   +1 -0   │  20 │     }                    20 │     }                         │
│ · main.go │  21 │     claims := parse(t    21 │     claims := parseWithLeeway │
└───────────┴─ space mark  A file  w stage  ? help  q quit ────────────────────┘
```

> Screenshots and a demo GIF: **TODO** — the ASCII sketch above is a stand-in.

## Install

```sh
go install github.com/wmarquardt/hunk@latest
```

Or build from a clone:

```sh
git clone https://github.com/wmarquardt/hunk
cd hunk
go build -o hunk .
```

Single static binary, no runtime dependencies. Git is only needed for the
working-tree review mode below.

## Usage

```sh
hunk                     # review the working tree, and stage what you approve
git diff | hunk          # read a diff from stdin
git show <sha> | hunk    # or any other diff-producing command
hunk old.txt new.txt     # diff two files, no git required
hunk old/ new/           # diff two directories
```

Output that is piped or redirected is passed through as plain unified diff, so
`hunk a b > patch.diff` does what you would expect.

### Keys

| Key | Action |
|---|---|
| `j` / `k`, ↑ / ↓ | scroll a line |
| `ctrl-d` / `ctrl-u` | scroll half a page |
| `n` / `p` | next / previous hunk |
| `]` / `[` | next / previous file |
| `g` / `G` | top / bottom |
| `h` / `l`, ← / → | scroll sideways |
| `s` | toggle side-by-side / unified |
| `b` | toggle the file sidebar |
| `?` | help |
| `q` | quit |

In working-tree review mode you also get:

| Key | Action |
|---|---|
| `space` | mark / unmark this hunk |
| `a` / `d` | mark / unmark, then jump to the next hunk |
| `A` / `D` | mark / unmark every hunk in this file |
| `w` | stage what is marked (asks first) |

Marking is `git add -p` without the one-hunk-at-a-time straitjacket: see the
whole change, jump around, mark as you go, then write it all at once.

Staging runs `git apply --cached`, so it **only ever writes the index** — no
working-tree file is created, modified, or deleted by hunk. If you stage
something you did not mean to, `git restore --staged .` puts it back.

The side-by-side layout falls back to unified on narrow terminals, and the
sidebar hides itself when there is no room for it.

## Theming

hunk ships with two themes: `hunk-dark` (the default) and `paper` (light).

```sh
hunk --theme paper
HUNK_THEME=paper hunk
```

### Writing your own

Themes are TOML files in `~/.config/hunk/themes/` (`$XDG_CONFIG_HOME` is
honoured). A file named `midnight.toml` there is available as `--theme midnight`.

**Every key is optional.** Anything you leave out keeps the default theme's
value, so a two-line theme is a perfectly good theme:

```toml
# ~/.config/hunk/themes/midnight.toml
name = "midnight"

[diff]
added_fg   = "#7ee787"
removed_fg = "#ff7b72"
```

The full schema, with every key and what it colors, is in
[`internal/theme/builtin/hunk-dark.toml`](internal/theme/builtin/hunk-dark.toml) —
copy it and edit. Values are hex colors (`#rgb` or `#rrggbb`). A typo'd key gets
a warning, a bad color gets an error naming the key, and neither costs you your
diff: hunk falls back to the default theme and carries on.

### Themes from GitHub

Reference a theme the way Go references a module:

```sh
hunk --theme github.com/someone/hunk-themes/dracula
hunk --theme github.com/someone/hunk-themes/dracula@v1.2.0   # pin it
hunk --theme-update                                          # re-fetch
```

It resolves to `themes/<name>.toml` (or `<name>.toml`) in that repo, and is
cached under `~/.config/hunk/themes/github.com/…`. A cache hit never touches the
network, so a theme is downloaded once and hunk keeps working offline.

To publish your own, put `.toml` files in a `themes/` directory in any public
GitHub repo. That is the whole protocol.

## Not in v1

Reverting hunks in the working tree, reviewing already-staged changes,
committing from inside hunk, watch mode, three-way merge, syntax highlighting.
See [the issues](https://github.com/wmarquardt/hunk/issues) or open one.

## Contributing

Bug reports, themes, and patches all welcome — see [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT © Will Marquardt. See [LICENSE](LICENSE).
