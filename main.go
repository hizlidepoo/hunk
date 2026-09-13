// Command hunk is a themeable diff viewer for the terminal.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"

	"github.com/charmbracelet/x/term"

	"github.com/wmarquardt/hunk/internal/diff"
	"github.com/wmarquardt/hunk/internal/git"
	"github.com/wmarquardt/hunk/internal/theme"
	"github.com/wmarquardt/hunk/internal/ui"
)

const usage = `hunk — a diff viewer for the terminal

usage:
  hunk                     review the working tree, and stage what you approve
  hunk log                 browse the commit history, read only
  hunk log <path>...       only commits touching those paths
  hunk version             print the version and exit
  git diff | hunk          review a diff from stdin
  hunk old.txt new.txt     diff two files
  hunk old/ new/           diff two directories

options:
`

// version is the build's version string. The release build stamps it with the
// git tag via -ldflags; a plain `go build` leaves it as "dev".
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "hunk:", err)
		os.Exit(1)
	}
}

// defaultLogCount is how much history "hunk log" reads up front. Deep enough
// to browse, shallow enough to open instantly in a repository with a long past.
const defaultLogCount = 50

func run() error {
	// "hunk version" is checked before the flag package sees the arguments,
	// the same reason stripLog runs early: flag stops at the first non-flag.
	if len(os.Args) > 1 && os.Args[1] == "version" {
		printVersion()
		return nil
	}
	logMode := stripLog()

	flag.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		flag.PrintDefaults()
	}
	themeRef := flag.String("theme", os.Getenv("HUNK_THEME"),
		"theme name, path, or github.com/user/repo/name reference")
	themeUpdate := flag.Bool("theme-update", false, "re-download remote themes instead of using the cache")

	// View filters, each mirroring an in-app toggle. Short forms share the same
	// variable so -w and -ignore-whitespace are the same flag.
	ignoreWS := flag.Bool("ignore-whitespace", false, "hide whitespace-only changes (git review mode)")
	flag.BoolVar(ignoreWS, "w", false, "shorthand for -ignore-whitespace")
	unified := flag.Bool("unified", false, "open unified instead of side-by-side")
	flag.BoolVar(unified, "u", false, "shorthand for -unified")
	noSidebar := flag.Bool("no-sidebar", false, "open with the file sidebar hidden")
	noFollow := flag.Bool("no-follow", false, "open with live-follow paused (git review mode)")
	context := flag.Int("context", diff.DefaultContext, "unchanged lines shown around each hunk")
	flag.IntVar(context, "U", diff.DefaultContext, "shorthand for -context")
	showWS := flag.Bool("show-whitespace", false, "render tabs and trailing spaces as visible marks")
	filter := flag.String("filter", "", "hide hunks whose every changed line matches this regex")
	noSyntax := flag.Bool("no-syntax", false, "open with syntax highlighting off")
	maxCount := flag.Int("max-count", defaultLogCount, "commits to read (hunk log)")
	flag.IntVar(maxCount, "n", defaultLogCount, "shorthand for -max-count")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		printVersion()
		return nil
	}

	if *filter != "" {
		if _, err := regexp.Compile(*filter); err != nil {
			return fmt.Errorf("bad -filter pattern: %w", err)
		}
	}

	th := loadTheme(*themeRef, *themeUpdate)
	opts := ui.Options{
		IgnoreWS:  *ignoreWS,
		Unified:   *unified,
		NoSidebar: *noSidebar,
		NoFollow:  *noFollow,
		Context:   *context,
		ShowWS:    *showWS,
		Filter:    *filter,
		NoSyntax:  *noSyntax,
	}

	repo, commits, text, err := source(logMode, flag.Args(), opts.IgnoreWS, opts.Context, *maxCount)
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

	if commits != nil {
		return ui.RunLog(repo, commits, files, th, opts)
	}
	if repo != nil {
		return ui.RunGit(repo, files, th, opts)
	}
	return ui.Run(files, th, opts)
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

// printVersion writes the version string that -version and the "version"
// subcommand both report.
func printVersion() {
	fmt.Println("hunk", version)
}

// stripLog takes the "log" subcommand off the argument list. The flag package
// stops at the first non-flag argument, so it has to go before flags are parsed
// or "hunk log -n 200" would never see -n.
func stripLog() bool {
	if len(os.Args) > 1 && os.Args[1] == "log" {
		os.Args = append(os.Args[:1], os.Args[2:]...)
		return true
	}
	return false
}

// source produces unified diff text from wherever this invocation gets it. A
// non-nil repo means the diff came from a git repository; a non-nil commit list
// means it is that repository's history, which hunk only ever reads.
func source(logMode bool, args []string, ignoreWS bool, context, maxCommits int) (*git.Repo, []git.Commit, string, error) {
	if logMode {
		repo := &git.Repo{}
		if !git.Available() || !repo.IsRepo() {
			return nil, nil, "", fmt.Errorf("not in a git repository: hunk log reads a repository's history")
		}
		commits, err := repo.Log(maxCommits, args)
		if err != nil {
			return nil, nil, "", err
		}
		if len(commits) == 0 {
			return nil, nil, "", fmt.Errorf("no commits to show")
		}
		text, err := repo.Show(commits[0].SHA, ignoreWS, context)
		return repo, commits, text, err
	}

	switch len(args) {
	case 0:
		// A pipe is a diff to read; a terminal means the user ran hunk on its
		// own, which is a request to review the repo they are standing in.
		if !term.IsTerminal(os.Stdin.Fd()) {
			b, err := io.ReadAll(os.Stdin)
			return nil, nil, string(b), err
		}

		repo := &git.Repo{}
		if !git.Available() || !repo.IsRepo() {
			flag.Usage()
			return nil, nil, "", fmt.Errorf("not in a git repository: pipe a diff in, or name two paths")
		}
		// A clean working tree is not an error: hunk follows the tree live, so
		// it opens empty and fills in as soon as something is edited.
		text, err := ui.GitSource(repo, ignoreWS, context)
		return repo, nil, text, err

	case 2:
		text, err := diff.Generate(args[0], args[1], context)
		return nil, nil, text, err

	default:
		flag.Usage()
		return nil, nil, "", fmt.Errorf("expected two paths, got %d", len(args))
	}
}
