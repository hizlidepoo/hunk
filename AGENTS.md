# AGENTS.md

Working notes for AI coding agents in this repository. Keep this file accurate:
if you change the layout, the conventions, or a command below, update it in the
same commit. Do not add advice here that is not specific to hunk.

## What hunk is

A themeable terminal diff viewer, single Go binary. Three ways in — stdin, two
paths, or the working tree — and one of them (the working tree) can stage what
you approve. Module: `github.com/wmarquardt/hunk`. Go 1.26.

## Commands

Every command here has been run in this repository and works. Verify before
adding another.

```sh
go build ./...
go vet ./...
go test ./...
go test -race ./...          # what CI runs
gofmt -l .                   # prints nothing when clean
go build -o hunk .
```

One package, or one test:

```sh
go test ./internal/diff/
go test ./internal/diff/ -run TestIntraline -v
go test ./internal/ui/ -run TestNavigationKeys -v
```

Lint (not installed by default; CI installs it):

```sh
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
golangci-lint run ./...      # must report "0 issues"
```

Run it:

```sh
cat testdata/multi.diff | go run .    # stdin path
go run . testdata/tree_a testdata/tree_b
go run . --theme paper                # the light theme
```

`go run .` with a terminal on stdin enters working-tree review mode, which can
write to the index. **Never run that against this repository while working in
it.** Use `mktemp -d` and a throwaway `git init` — that is what the tests do.

## Layout

```
main.go                       flags, mode dispatch, non-TTY passthrough
internal/diff/                everything about diffs, no terminal code
  model.go                    File, Hunk, Line, Kind, Range
  parse.go                    unified diff text -> []File   (go-gitdiff)
  generate.go                 two paths -> unified diff text (go-udiff)
  intraline.go                word-level ranges for a paired line
  patch.go                    File.Patch: selected hunks -> a patch git can apply
internal/theme/               TOML themes, local and remote
  theme.go                    schema, Decode, Validate
  resolve.go                  reference resolution, GitHub fetch, cache
  builtin/*.toml              the two shipped themes, //go:embed-ed
internal/git/git.go           os/exec wrappers; the only place hunk shells out
internal/ui/                  everything about the terminal, no git logic
  layout.go                   []File -> []Row + navigation indexes (pure)
  style.go                    theme -> Lip Gloss styles, tab expansion, fitting
  model.go                    Bubble Tea model: state, keys, rendering
  stage.go                    marks, staging, working-tree reading
testdata/*.diff               parser fixtures (binary.diff came from real git)
testdata/tree_a, tree_b       fixture trees for directory diffing
```

## How it fits together

**Everything becomes unified diff text, then is parsed once.**

```
stdin ──────────────────────────────┐
two paths ─> diff.Generate ─────────┼─> diff.ParseString ─> []diff.File ─> ui
git diff + untracked ─> ui.GitSource┘
```

`diff.Generate` produces text and hands it straight back to the parser. That is
deliberate: one interpretation of a diff instead of two, and the file/directory
path is covered by the same parser tests as the stdin path.

`ui.Build(files, split)` flattens every file into one `[]Row`, so scrolling,
windowing, and next/prev are index arithmetic. Split and unified are *different
row lists* — a changed line is one row in split, two in unified — so toggling
`s` rebuilds the view and re-seeks to the same file.

Only the visible rows are ever rendered (`renderBody`). Do not replace this with
`bubbles/viewport`: it renders the whole diff into one string, which is exactly
the wrong shape for the 20k-line diffs hunk exists for.

## Conventions

- **`// ponytail:` comments** mark deliberate simplifications and name their
  ceiling and upgrade path. Grep for them to see what has been consciously
  deferred. Add one when you take a shortcut on purpose.
- **Comments say why, not what.**
- Everything is under `internal/`. No public API yet.
- Colors are hex strings in the theme layer; only `internal/ui/style.go` turns
  them into Lip Gloss styles. `internal/theme` does not import Lip Gloss.
- `internal/diff` must not import `internal/ui` or `internal/git`.

## Rules that are not negotiable

**Staging only ever writes the index.** `git apply --cached` and `git add` are
the only mutating commands in the codebase, both in `internal/git/git.go`. hunk
has no code path that writes to a working-tree file. Any change near staging
needs a test asserting the working tree is byte-identical afterwards —
`TestStageOneHunkOfThree` is the pattern.

**Hunks are emitted verbatim.** `File.Patch` concatenates the fragments exactly
as parsed and lets `git apply --recount` fix the line counts. Do not recompute
`@@` headers: arithmetic there can silently stage the wrong lines.

**Licenses stay permissive.** No GPL code, not even vendored for reference.
Current dependencies: Bubble Tea and Lip Gloss (MIT), go-gitdiff (MIT), go-udiff
(BSD-3 + MIT), BurntSushi/toml (MIT), charmbracelet/x (MIT).

**Tests ship in the same commit as the code.** No test may hit the network —
remote theme tests use `httptest.Server`. Git tests build throwaway repos in
`t.TempDir()` and skip with `t.Skip` when git is missing.

**Both built-in themes must set every key.** `TestBuiltinsAreCompleteAndValid`
decodes them into a *zero* theme (not over the default) so a missing key fails
instead of silently inheriting. Adding a color to the schema means editing
`hunk-dark.toml` and `paper.toml`.

## Gotchas found the hard way

- Hand-written diff fixtures need correct `@@` counts, and a hunk with only
  context lines is rejected by the parser. Prefer generating fixtures with real
  git over writing them by hand.
- Bubble Tea reports the space key as `"space"`, not `" "`.
- A binary patch needs `---`/`+++` lines before the "Binary files … differ" line,
  or the parser cannot recover the filenames when the two sides are named
  differently. See `filePatch` in `generate.go`.
- Absolute paths in a `diff --git` header must have the leading `/` stripped, or
  the header reads `a//tmp/x` and cannot be split.
- Tabs must be expanded before anything is measured or sliced, and highlight
  ranges remapped with them (`expandTabs`).
- Slice styled strings with `ansi.Cut` / `ansi.Truncate`, never by byte or rune
  index: escape sequences and wide runes both break naive slicing.
