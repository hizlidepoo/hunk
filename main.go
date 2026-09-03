// Command hunk is a themeable diff viewer for the terminal.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/x/term"

	"github.com/wmarquardt/hunk/internal/diff"
	"github.com/wmarquardt/hunk/internal/git"
	"github.com/wmarquardt/hunk/internal/theme"
	"github.com/wmarquardt/hunk/internal/ui"
)

const usage = `hunk — a diff viewer for the terminal

usage:
  hunk                     review the working tree, and stage what you approve
  git diff | hunk          review a diff from stdin
  hunk old.txt new.txt     diff two files
  hunk old/ new/           diff two directories

options:
`

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "hunk:", err)
		os.Exit(1)
	}
}

func run() error {
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		flag.PrintDefaults()
	}
	themeRef := flag.String("theme", os.Getenv("HUNK_THEME"),
		"theme name, path, or github.com/user/repo/name reference")
	themeUpdate := flag.Bool("theme-update", false, "re-download remote themes instead of using the cache")
	flag.Parse()

	th := loadTheme(*themeRef, *themeUpdate)

	repo, text, err := source(flag.Args())
	if err != nil {
		return err
	}
	files, err := diff.ParseString(text)
	if err != nil {
		return err
	}

	// Piped or redirected output means hunk is part of a pipeline, not a
	// viewer: hand over the diff and stay out of the way.
	if !term.IsTerminal(os.Stdout.Fd()) {
		_, err := io.WriteString(os.Stdout, text)
		return err
	}

	if repo != nil {
		return ui.RunGit(repo, files, th)
	}
	return ui.Run(files, th)
}

// loadTheme never fails the run: a broken theme costs you colors, not your diff.
func loadTheme(ref string, refresh bool) *theme.Theme {
	th, warnings, err := (&theme.Loader{Refresh: refresh}).Load(ref)
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "hunk:", w)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "hunk:", err)
		fmt.Fprintln(os.Stderr, "hunk: falling back to the default theme")
		return theme.Default()
	}
	return th
}

// source produces unified diff text from wherever this invocation gets it. A
// non-nil repo means the diff came from a working tree hunk may stage into.
func source(args []string) (*git.Repo, string, error) {
	switch len(args) {
	case 0:
		// A pipe is a diff to read; a terminal means the user ran hunk on its
		// own, which is a request to review the repo they are standing in.
		if !term.IsTerminal(os.Stdin.Fd()) {
			b, err := io.ReadAll(os.Stdin)
			return nil, string(b), err
		}

		repo := &git.Repo{}
		if !git.Available() || !repo.IsRepo() {
			flag.Usage()
			return nil, "", fmt.Errorf("not in a git repository: pipe a diff in, or name two paths")
		}
		text, err := ui.GitSource(repo)
		return repo, text, err

	case 2:
		text, err := diff.Generate(args[0], args[1])
		return nil, text, err

	default:
		flag.Usage()
		return nil, "", fmt.Errorf("expected two paths, got %d", len(args))
	}
}
