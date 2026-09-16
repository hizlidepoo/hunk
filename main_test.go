package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wmarquardt/hunk/internal/diff"
	"github.com/wmarquardt/hunk/internal/git"
)

// noUsage stands in for the FlagSet's usage printer where a test only cares
// about the error that follows it.
func noUsage() {}

// twoFiles writes an old and a new file and returns their paths. Two paths is
// the one invocation that needs neither git nor a terminal, so it is what the
// flag tests use to drive run() all the way through.
func twoFiles(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	old := filepath.Join(dir, "old.txt")
	newer := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(old, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, []byte("one\nCHANGED\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return old, newer
}

// runArgs drives run() with the given arguments and returns what it wrote.
// HUNK_THEME is cleared so a developer's environment cannot change the result.
func runArgs(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	t.Setenv("HUNK_THEME", "")
	var out, errBuf bytes.Buffer
	err = run(append([]string{"hunk"}, args...), &out, &errBuf)
	return out.String(), errBuf.String(), err
}

// tempRepo builds a throwaway repository, the same shape internal/git's tests
// use. Nothing here ever runs against the checkout being worked in.
func tempRepo(t *testing.T, commits []string) string {
	t.Helper()
	if !git.Available() {
		t.Skip("git is not on PATH")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "hunk test")

	for i, subject := range commits {
		name := filepath.Join(dir, "a.txt")
		if err := os.WriteFile(name, []byte(strings.Repeat("line\n", i+1)), 0o644); err != nil {
			t.Fatal(err)
		}
		run("add", "-A")
		run("commit", "-qm", subject)
	}
	return dir
}

// Both spellings of the version request print the same line and stop there,
// which is what lets "hunk version" work outside a repository.
func TestVersionIsReportedBothWays(t *testing.T) {
	old := version
	version = "v1.2.3"
	t.Cleanup(func() { version = old })

	for _, args := range [][]string{{"version"}, {"-version"}} {
		stdout, _, err := runArgs(t, args...)
		if err != nil {
			t.Errorf("run(%v) = %v, want no error", args, err)
		}
		if stdout != "hunk v1.2.3\n" {
			t.Errorf("run(%v) printed %q, want %q", args, stdout, "hunk v1.2.3\n")
		}
	}
}

// "version" is a subcommand, not a path, so it wins before flag parsing — the
// same reason "log" has to be stripped first.
func TestVersionSubcommandBeatsFlagParsing(t *testing.T) {
	stdout, _, err := runArgs(t, "version", "-bogus")
	if err != nil {
		t.Errorf("run = %v, want no error", err)
	}
	if !strings.HasPrefix(stdout, "hunk ") {
		t.Errorf("stdout = %q, want a version line", stdout)
	}
}

// With stdout redirected — which is always the case under go test — hunk is a
// pipeline stage and hands the diff over instead of opening the viewer.
func TestTwoPathsWriteTheDiffToStdout(t *testing.T) {
	old, newer := twoFiles(t)

	stdout, _, err := runArgs(t, old, newer)
	if err != nil {
		t.Fatalf("run = %v", err)
	}
	if !strings.Contains(stdout, "+CHANGED") {
		t.Errorf("diff is missing the change:\n%s", stdout)
	}
	if !strings.Contains(stdout, "diff --git") {
		t.Errorf("output is not a diff:\n%s", stdout)
	}
}

// Every flag has to reach the parser, including the short spellings that share
// a variable with their long form. Driving the real run() is what proves a flag
// is registered at all — a typo in the name would be invisible otherwise.
func TestFlagsAreAccepted(t *testing.T) {
	old, newer := twoFiles(t)

	for _, args := range [][]string{
		{"-ignore-whitespace"}, {"-w"},
		{"-unified"}, {"-u"},
		{"-no-sidebar"}, {"-no-follow"},
		{"-sidebar-width", "0"}, {"-sidebar-width", "20"}, {"-sidebar-width", "48"},
		{"-context", "1"}, {"-U", "1"},
		{"-show-whitespace"}, {"-no-syntax"},
		{"-filter", "CHANGED"},
		{"-max-count", "10"}, {"-n", "10"},
		{"-theme", "hunk-dark"},
		{"-theme-update"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, _, err := runArgs(t, append(append([]string{}, args...), old, newer)...)
			if err != nil {
				t.Errorf("run(%v) = %v, want no error", args, err)
			}
		})
	}
}

// -U is shorthand for -context, so both have to change how much surrounding
// code comes back. One line of context is visibly less than three.
func TestContextShorthandMatchesTheLongForm(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.txt")
	newer := filepath.Join(dir, "new.txt")
	body := strings.Repeat("keep\n", 10)
	if err := os.WriteFile(old, []byte(body+"tail\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, []byte(body+"TAIL\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	short, _, err := runArgs(t, "-U", "1", old, newer)
	if err != nil {
		t.Fatal(err)
	}
	long, _, err := runArgs(t, "-context", "1", old, newer)
	if err != nil {
		t.Fatal(err)
	}
	if short != long {
		t.Errorf("-U and -context disagree:\n%s\n---\n%s", short, long)
	}

	wide, _, err := runArgs(t, "-context", "5", old, newer)
	if err != nil {
		t.Fatal(err)
	}
	if len(short) >= len(wide) {
		t.Error("-context 1 did not produce a smaller diff than -context 5")
	}
}

func TestBadFilterPatternIsRejected(t *testing.T) {
	old, newer := twoFiles(t)

	_, _, err := runArgs(t, "-filter", "[", old, newer)
	if err == nil {
		t.Fatal("run accepted a filter that is not a regex")
	}
	if !strings.Contains(err.Error(), "bad -filter pattern") {
		t.Errorf("error = %v, want it to name the filter flag", err)
	}
}

// A sidebar width outside the range is refused up front, naming the flag,
// rather than silently clamped into something the user did not ask for.
func TestSidebarWidthOutOfRangeIsRejected(t *testing.T) {
	old, newer := twoFiles(t)
	for _, w := range []string{"19", "49", "-4"} {
		_, _, err := runArgs(t, "-sidebar-width", w, old, newer)
		if err == nil {
			t.Errorf("run accepted -sidebar-width %s", w)
			continue
		}
		if !strings.Contains(err.Error(), "bad -sidebar-width") {
			t.Errorf("-sidebar-width %s: error = %v, want it to name the flag", w, err)
		}
	}
}

// An unknown flag is a usage problem: the flag package prints the complaint and
// the usage text itself, and main turns errUsage into exit status 2.
func TestUnknownFlagIsAUsageError(t *testing.T) {
	_, stderr, err := runArgs(t, "-bogus")
	if err == nil {
		t.Fatal("run accepted an unknown flag")
	}
	if err != errUsage {
		t.Errorf("err = %v, want errUsage", err)
	}
	if !strings.Contains(stderr, "not defined: -bogus") {
		t.Errorf("stderr does not explain the bad flag:\n%s", stderr)
	}
	if !strings.Contains(stderr, "hunk — a diff viewer for the terminal") {
		t.Errorf("stderr is missing the usage text:\n%s", stderr)
	}
}

// The flags live on a local FlagSet, so run can be called more than once in a
// process. On the global CommandLine the second call would panic on a
// redefined flag.
func TestRunCanBeCalledTwice(t *testing.T) {
	old, newer := twoFiles(t)
	for i := range 2 {
		if _, _, err := runArgs(t, old, newer); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}
}

// HUNK_THEME supplies the default for -theme, so the environment picks the
// theme when the flag is absent and the flag wins when it is present.
func TestThemeComesFromTheEnvironment(t *testing.T) {
	old, newer := twoFiles(t)

	t.Setenv("HUNK_THEME", "no-such-theme")
	var out, errBuf bytes.Buffer
	if err := run([]string{"hunk", old, newer}, &out, &errBuf); err != nil {
		t.Fatalf("a bad theme must not fail the run: %v", err)
	}
	if !strings.Contains(errBuf.String(), "falling back to the default theme") {
		t.Errorf("stderr does not mention the fallback:\n%s", errBuf.String())
	}

	errBuf.Reset()
	if err := run([]string{"hunk", "-theme", "hunk-dark", old, newer}, &out, &errBuf); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(errBuf.String(), "falling back") {
		t.Errorf("the -theme flag did not override the environment:\n%s", errBuf.String())
	}
}

func TestStripLog(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantLog  bool
		wantArgs []string
	}{
		{name: "no arguments", args: []string{"hunk"}, wantArgs: []string{"hunk"}},
		{name: "the bare subcommand", args: []string{"hunk", "log"}, wantLog: true, wantArgs: []string{"hunk"}},
		{
			name:     "flags after the subcommand survive",
			args:     []string{"hunk", "log", "-n", "200", "a.txt"},
			wantLog:  true,
			wantArgs: []string{"hunk", "-n", "200", "a.txt"},
		},
		{
			// "log" only counts as the first argument; anywhere else it is a path.
			name:     "log after a flag is not the subcommand",
			args:     []string{"hunk", "-u", "log"},
			wantArgs: []string{"hunk", "-u", "log"},
		},
		{
			name:     "a path named log stays a path",
			args:     []string{"hunk", "a.txt", "log"},
			wantArgs: []string{"hunk", "a.txt", "log"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := append([]string{}, tt.args...)

			gotLog, gotArgs := stripLog(tt.args)
			if gotLog != tt.wantLog {
				t.Errorf("logMode = %v, want %v", gotLog, tt.wantLog)
			}
			if strings.Join(gotArgs, " ") != strings.Join(tt.wantArgs, " ") {
				t.Errorf("args = %v, want %v", gotArgs, tt.wantArgs)
			}
			// The caller's slice is left alone, unlike the in-place rewrite
			// this used to do to os.Args.
			if strings.Join(tt.args, " ") != strings.Join(original, " ") {
				t.Errorf("stripLog rewrote its input: %v, was %v", tt.args, original)
			}
		})
	}
}

func TestPrintVersion(t *testing.T) {
	old := version
	version = "v9.9.9"
	t.Cleanup(func() { version = old })

	var b bytes.Buffer
	printVersion(&b)
	if b.String() != "hunk v9.9.9\n" {
		t.Errorf("printVersion wrote %q", b.String())
	}
}

// A theme that cannot be loaded costs colors, never the diff: loadTheme always
// hands back something usable and says what went wrong on stderr.
func TestLoadThemeFallsBackAndExplains(t *testing.T) {
	var b bytes.Buffer
	th := loadTheme("no-such-theme", false, &b)
	if th == nil {
		t.Fatal("loadTheme returned nil")
	}
	if !strings.Contains(b.String(), "falling back to the default theme") {
		t.Errorf("stderr does not mention the fallback:\n%s", b.String())
	}
}

func TestLoadThemeAcceptsABuiltin(t *testing.T) {
	var b bytes.Buffer
	th := loadTheme("hunk-dark", false, &b)
	if th == nil {
		t.Fatal("loadTheme returned nil")
	}
	if th.Name != "hunk-dark" {
		t.Errorf("theme name = %q, want hunk-dark", th.Name)
	}
	if b.Len() != 0 {
		t.Errorf("a good theme wrote to stderr:\n%s", b.String())
	}
}

// An empty reference is the default theme, and it must not go looking on disk
// or on the network for it.
func TestLoadThemeWithNoReference(t *testing.T) {
	var b bytes.Buffer
	if th := loadTheme("", false, &b); th == nil {
		t.Fatal("loadTheme returned nil")
	}
	if b.Len() != 0 {
		t.Errorf("the default theme wrote to stderr:\n%s", b.String())
	}
}

func TestSourceWithTwoPaths(t *testing.T) {
	old, newer := twoFiles(t)

	repo, commits, text, err := source(false, []string{old, newer}, "", false, diff.DefaultContext, defaultLogCount, noUsage)
	if err != nil {
		t.Fatal(err)
	}
	if repo != nil || commits != nil {
		t.Error("a two-path diff is not a repository")
	}
	if !strings.Contains(text, "+CHANGED") {
		t.Errorf("diff is missing the change:\n%s", text)
	}
}

// One path, or three, is a mistyped two-path invocation, and the message says
// how many were counted so the mistake is obvious.
func TestSourceRejectsTheWrongNumberOfPaths(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{args: []string{"only.txt"}, want: "expected two paths, got 1"},
		{args: []string{"a", "b", "c"}, want: "expected two paths, got 3"},
	} {
		usageShown := false
		_, _, _, err := source(false, tc.args, "", false, diff.DefaultContext, defaultLogCount, func() { usageShown = true })
		if err == nil {
			t.Fatalf("source(%v) returned no error", tc.args)
		}
		if err.Error() != tc.want {
			t.Errorf("error = %q, want %q", err, tc.want)
		}
		if !usageShown {
			t.Errorf("source(%v) did not print the usage text", tc.args)
		}
	}
}

func TestSourceMissingPath(t *testing.T) {
	_, _, _, err := source(false, []string{"nope-a.txt", "nope-b.txt"}, "", false, diff.DefaultContext, defaultLogCount, noUsage)
	if err == nil {
		t.Fatal("source accepted two paths that do not exist")
	}
}

// Log mode reads a repository's history and never writes to it.
func TestSourceInLogMode(t *testing.T) {
	dir := tempRepo(t, []string{"first", "second"})

	repo, commits, text, err := source(true, nil, dir, false, diff.DefaultContext, defaultLogCount, noUsage)
	if err != nil {
		t.Fatal(err)
	}
	if repo == nil {
		t.Fatal("log mode returned no repository")
	}
	if len(commits) != 2 {
		t.Fatalf("got %d commits, want 2", len(commits))
	}
	// Newest first, so the opening diff is the most recent commit.
	if commits[0].Subject != "second" {
		t.Errorf("first commit = %q, want the newest", commits[0].Subject)
	}
	if !strings.Contains(text, "diff --git") {
		t.Errorf("log mode did not open on a diff:\n%s", text)
	}
}

func TestSourceLogModeHonoursMaxCount(t *testing.T) {
	dir := tempRepo(t, []string{"first", "second", "third"})

	_, commits, _, err := source(true, nil, dir, false, diff.DefaultContext, 2, noUsage)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Errorf("got %d commits, want the 2 that were asked for", len(commits))
	}
}

// A repository with no commits has no history to browse, and saying so beats
// opening an empty viewer.
func TestSourceLogModeWithNoCommits(t *testing.T) {
	dir := tempRepo(t, nil)

	_, _, _, err := source(true, nil, dir, false, diff.DefaultContext, defaultLogCount, noUsage)
	if err == nil {
		t.Fatal("log mode accepted a repository with no commits")
	}
	if err.Error() != "no commits to show" {
		t.Errorf("error = %q", err)
	}
}

func TestSourceLogModeOutsideARepository(t *testing.T) {
	if !git.Available() {
		t.Skip("git is not on PATH")
	}
	_, _, _, err := source(true, nil, t.TempDir(), false, diff.DefaultContext, defaultLogCount, noUsage)
	if err == nil {
		t.Fatal("log mode accepted a directory that is not a repository")
	}
	if !strings.Contains(err.Error(), "not in a git repository") {
		t.Errorf("error = %q", err)
	}
}

// Log mode is read-only. Nothing it does may reach the index.
func TestSourceLogModeLeavesTheIndexAlone(t *testing.T) {
	dir := tempRepo(t, []string{"first"})

	if _, _, _, err := source(true, nil, dir, false, diff.DefaultContext, defaultLogCount, noUsage); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Errorf("log mode changed the repository:\n%s", out)
	}
}
