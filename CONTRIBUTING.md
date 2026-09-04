# Contributing to hunk

Thanks for helping out. Bug reports, themes, and patches are all welcome.

## Building and testing

You need Go 1.26 or newer. Git is needed for the working-tree review mode and
for the tests that cover it.

```sh
go build ./...          # compile
go vet ./...            # the standard vet checks
go test ./...           # the full suite
go test -race ./...     # what CI runs
go build -o hunk .      # a binary you can actually run
```

Run a single package's tests while iterating:

```sh
go test ./internal/diff/
go test ./internal/diff/ -run TestIntraline -v
```

Linting uses [golangci-lint](https://golangci-lint.run):

```sh
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
golangci-lint run ./...
```

Formatting is plain `gofmt`:

```sh
gofmt -l .    # lists anything unformatted
gofmt -w .
```

## Trying your change

```sh
git diff | go run .            # the pager path
go run . testdata/tree_a/main.go testdata/tree_b/main.go
go run . testdata/tree_a testdata/tree_b
go run . --theme paper         # the alternate theme
```

For the staging path, use a scratch repo rather than your own working tree:

```sh
cd $(mktemp -d) && git init -q && echo hello > a.txt && git add -A && git commit -qm init
echo changed > a.txt
go run github.com/wmarquardt/hunk@latest    # or point at your build
```

## Tests are not optional

Every change ships with its tests, in the same commit. Concretely:

- **Diff logic** (`internal/diff`) is pure and table-driven. New behavior gets a
  new table entry, including the awkward cases: empty files, single-line files,
  no trailing newline, CRLF, binary, multibyte text.
- **Theme loading** (`internal/theme`) covers valid, invalid, partial, and
  unknown-key files. A partial theme must inherit, never crash.
- **Layout and navigation** (`internal/ui`) is tested through pure functions —
  row pairing, index math, width budgeting — not by driving a terminal.
- **Staging** (`internal/git`) is tested against real throwaway repositories in
  `t.TempDir()`. These tests assert on `git diff --cached` *and* that the working
  tree is byte-identical afterwards. Anything touching staging needs that second
  assertion: hunk must never modify a file you did not ask it to.
- No test may reach the network. Remote theme tests run against an
  `httptest.Server`.

## Pull requests

- Branch off `main`, one topic per PR.
- `go build ./... && go vet ./... && go test -race ./... && golangci-lint run ./...`
  must pass before you open it. CI runs exactly these on Linux and macOS.
- `main` is protected: PRs need the `test (ubuntu-latest)`, `test (macos-latest)`
  and `lint` checks green plus one approving review before they can merge.
- Say what you changed and why. If it changes what the user sees, paste the
  before and after.

### Commit style

Short imperative subject, no trailing period, under ~70 characters:

```
add intra-line highlighting to unified view
fix line numbers after an unbalanced hunk
```

The body explains *why*, if that is not obvious from the subject. Conventional
Commit prefixes are fine but not required.

## Code conventions

- Comments explain **why**, not what. If the code needs a comment to say what it
  does, the code is the problem.
- Deliberate simplifications are marked `// ponytail:` and name their ceiling and
  the upgrade path — e.g. `// ponytail: no soft-wrap in v1.` Grep for them to see
  what has been consciously deferred.
- Everything lives under `internal/`. There is no public API to keep stable yet.
- New dependencies need a reason in the PR description. Permissively licensed
  only: **no GPL code enters this repository**, not even as reference material.

## Adding a theme

Built-in themes live in `internal/theme/builtin/`. A built-in must specify
*every* key — there is a test that fails if one is missing, so adding a color to
the schema forces you to add it to both built-ins.

If you just want to share a theme, you do not need a PR at all: put it in a
`themes/` directory in a public GitHub repo and people can use it with
`hunk --theme github.com/you/your-repo/name`.

## Code of conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md).
