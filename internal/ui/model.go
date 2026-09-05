package ui

import (
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/wmarquardt/hunk/internal/diff"
	"github.com/wmarquardt/hunk/internal/git"
	"github.com/wmarquardt/hunk/internal/theme"
)

// markRepeat is how long after marking one hunk a space press on a *different*
// hunk is read as the key repeating rather than as a second decision. Marking
// moves the cursor on, so without this a held space bar walks the file and
// marks all of it. Pressing space again on the same hunk is never blocked, so
// taking a mark straight back off still works.
const markRepeat = time.Second

const (
	sidebarWidth = 28
	// minSidebarWidth is the total terminal width below which the sidebar is
	// hidden: past this point it costs more than it tells you.
	minSidebarWidth = 100
	// minSplitWidth is the content width below which side-by-side collapses to
	// a unified view rather than showing two unreadable columns.
	minSplitWidth = 80
	numWidth      = 5
)

// Model is the whole TUI state.
type Model struct {
	files []diff.File
	view  *View
	st    styles

	width, height int

	cur     int // cursor row, the row navigation acts on
	top     int // first visible row
	hscroll int

	// wantSplit and wantSidebar are what the user asked for; the terminal's
	// width decides what they actually get.
	wantSplit   bool
	wantSidebar bool
	builtSplit  bool

	showHelp bool

	// Git review mode. repo is nil for a plain diff, in which case marking and
	// staging are not offered at all.
	repo  *git.Repo
	marks marks
	msg   string

	// lastPatch and lastWhole record the most recent stage so "u" can take it
	// straight back out of the index — the undo for w.
	lastPatch string
	lastWhole []string

	// staged and unstaged are the paths git reports as having index and
	// working-tree changes, so the sidebar can show what is already approved.
	staged   map[string]bool
	unstaged map[string]bool

	// Live-follow: watch reports working-tree changes and live is whether hunk
	// currently acts on them. Both are zero for a plain diff, which never
	// follows anything.
	watch *watcher
	live  bool

	// ignoreWS re-diffs with git's -w, hiding whitespace-only changes. Only the
	// git review mode can honour it, since it is the only source hunk can re-run.
	ignoreWS bool

	// Search, vim-style. searchInput is true while the / prompt is open; typed
	// is the query being edited; search is the confirmed query that n / N repeat
	// and esc clears.
	searchInput bool
	typed       string
	search      string

	// context is how many unchanged lines surround each hunk (git's -U). + and -
	// re-diff with more or less. Git review mode only, since it re-sources.
	context int

	// showWS renders tabs and trailing spaces as visible marks. It is purely a
	// rendering choice — no re-diff — so it works in every mode.
	showWS bool

	// clock, lastMark and lastMarkAt tell a held space bar from a deliberate
	// second press. clock is a field so tests do not have to sleep.
	clock      func() time.Time
	lastMark   [2]int
	lastMarkAt time.Time

	// hints are the clickable option zones on the status bar, rebuilt on every
	// render so mouse hit-testing matches exactly what is on screen.
	hints []hintZone
}

// hintZone maps a horizontal span of the status bar to the key its label
// stands for, so a click on the label does what the key does.
type hintZone struct {
	x0, x1 int // inclusive column range on the status row
	key    string
}

// Options are the startup preferences the command-line flags set. Every one
// has an in-app key that still toggles it during a session; these just pick the
// state hunk opens in.
type Options struct {
	IgnoreWS  bool // -w: open with whitespace-only changes hidden
	Unified   bool // -u: open unified instead of side-by-side
	NoSidebar bool // --no-sidebar: open with the file sidebar hidden
	NoFollow  bool // --no-follow: open with live-follow paused
	Context   int  // --context: unchanged lines around each hunk (0 falls back to the default)
	ShowWS    bool // --show-whitespace: render tabs and trailing spaces as marks
}

// New builds a model over an already-parsed diff.
func New(files []diff.File, t *theme.Theme, opts Options) *Model {
	context := opts.Context
	if context <= 0 {
		context = diff.DefaultContext
	}
	m := &Model{
		files:       files,
		st:          newStyles(t),
		wantSplit:   !opts.Unified,
		wantSidebar: !opts.NoSidebar,
		builtSplit:  !opts.Unified,
		ignoreWS:    opts.IgnoreWS,
		context:     context,
		showWS:      opts.ShowWS,
		clock:       time.Now,
		lastMark:    [2]int{-1, -1},
	}
	m.rebuildView()
	return m
}

// rebuildView re-flattens the files into rows. Everything that changes the diff
// or the layout goes through here.
func (m *Model) rebuildView() {
	m.view = Build(m.files, m.builtSplit)
}

// NewGit is New for a working tree hunk can stage into. It also starts
// following the working tree, so edits made while hunk is open show up on their
// own. A watcher that fails to start just leaves live-follow off.
func NewGit(repo *git.Repo, files []diff.File, t *theme.Theme, opts Options) *Model {
	m := New(files, t, opts)
	m.repo = repo
	m.marks = marks{}
	m.refreshGitState()

	dir := repo.Dir
	if dir == "" {
		if wd, err := os.Getwd(); err == nil {
			dir = wd
		}
	}
	// The watcher still starts when following is off, so f can resume it later.
	if w, err := newWatcher(dir); err == nil {
		m.watch, m.live = w, !opts.NoFollow
	}
	return m
}

// refreshGitState reads which files git considers staged and unstaged, so the
// sidebar can mark approved files without re-deriving it from the diff text.
func (m *Model) refreshGitState() {
	m.staged, m.unstaged = map[string]bool{}, map[string]bool{}
	if m.repo == nil {
		return
	}
	if paths, err := m.repo.StagedPaths(); err == nil {
		for _, p := range paths {
			m.staged[p] = true
		}
	}
	if paths, err := m.repo.UnstagedPaths(); err == nil {
		for _, p := range paths {
			m.unstaged[p] = true
		}
	}
	if paths, err := m.repo.Untracked(); err == nil {
		for _, p := range paths {
			m.unstaged[p] = true
		}
	}
}

// Run puts the model on screen and blocks until the user quits.
func Run(files []diff.File, t *theme.Theme, opts Options) error {
	_, err := tea.NewProgram(New(files, t, opts)).Run()
	return err
}

// RunGit is Run with staging enabled.
func RunGit(repo *git.Repo, files []diff.File, t *theme.Theme, opts Options) error {
	m := NewGit(repo, files, t, opts)
	defer m.watch.Close() // nil-safe; stops the follow goroutine on quit
	_, err := tea.NewProgram(m).Run()
	return err
}

// staging reports whether this session can write to the index.
func (m *Model) staging() bool { return m.repo != nil }

// Init starts following the working tree when live-follow is on; otherwise
// hunk has nothing to do before its first render.
func (m *Model) Init() tea.Cmd {
	if m.live && m.watch != nil {
		return m.watch.wait()
	}
	return nil
}

func (m *Model) split() bool   { return m.wantSplit && m.contentWidth() >= minSplitWidth }
func (m *Model) sidebar() bool { return m.wantSidebar && m.width >= minSidebarWidth }

func (m *Model) contentWidth() int {
	w := m.width
	if m.wantSidebar && m.width >= minSidebarWidth {
		w -= sidebarWidth + 1
	}
	if w < 1 {
		return 1
	}
	return w
}

// bodyHeight is the number of diff rows on screen, leaving a line for the
// status bar.
func (m *Model) bodyHeight() int {
	h := m.height - 1
	if h < 1 {
		return 1
	}
	return h
}

// Update handles resizes and key presses.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.rebuildIfNeeded()
		m.ensureVisible()
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.MouseClickMsg:
		return m.handleClick(msg)

	case tea.MouseWheelMsg:
		return m.handleWheel(msg)

	case fsDirtyMsg:
		// The tree changed. Reload while preserving marks and cursor, then wait
		// for the next change. If following was paused since this fired, drop it.
		if !m.live || m.watch == nil {
			return m, nil
		}
		m.liveReload()
		return m, m.watch.wait()
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.showHelp {
		// Any key closes help; there is nothing else to do while it is up.
		m.showHelp = false
		return m, nil
	}

	// While the / prompt is open, keys edit the query, not the diff.
	if m.searchInput {
		m.searchKey(msg)
		return m, nil
	}

	// A keystroke means the last result has been read.
	m.msg = ""

	return m, m.command(msg.String())
}

// command runs the action bound to a key. It is shared by the keyboard and by
// clicks on the status-bar option labels, so both do exactly the same thing.
func (m *Model) command(key string) tea.Cmd {
	switch key {
	case "q", "ctrl+c":
		return tea.Quit

	case "j", "down":
		m.step(1)
	case "k", "up":
		m.step(-1)
	case "ctrl+d", "pgdown":
		m.step(m.bodyHeight() / 2)
	case "ctrl+u", "pgup":
		m.step(-m.bodyHeight() / 2)
	case "g", "home":
		m.moveTo(0)
	case "G", "end":
		m.moveTo(len(m.view.Rows) - 1)

	case "/":
		m.searchInput, m.typed = true, ""
	case "n":
		// When a search is active, n repeats it (vim); otherwise it is the next
		// hunk, as always.
		if m.search != "" {
			m.jumpMatch(1)
		} else {
			m.moveTo(NextIndex(m.view.HunkRows, m.cur))
		}
	case "N":
		if m.search != "" {
			m.jumpMatch(-1)
		}
	case "esc":
		m.search = ""
	case "p":
		m.moveTo(PrevIndex(m.view.HunkRows, m.cur))
	case "]":
		m.moveTo(NextIndex(m.view.FileRows, m.cur))
	case "[":
		m.moveTo(PrevIndex(m.view.FileRows, m.cur))

	// ponytail: horizontal scrolling instead of soft-wrap. Wrapping a
	// side-by-side view means rows stop being one screen line each, which the
	// windowing and navigation both assume. Revisit only if people ask.
	case "l", "right":
		m.hscroll += 8
	case "h", "left":
		m.hscroll -= 8
		if m.hscroll < 0 {
			m.hscroll = 0
		}

	case "s":
		m.wantSplit = !m.wantSplit
		m.rebuildIfNeeded()
	case "b":
		m.wantSidebar = !m.wantSidebar
		m.rebuildIfNeeded()
	case "f":
		return m.toggleFollow()
	case "i":
		m.toggleIgnoreWS()
	case "+", "=":
		m.changeContext(1)
	case "-":
		m.changeContext(-1)
	case "W":
		m.showWS = !m.showWS
		if m.showWS {
			m.msg = "showing whitespace"
		} else {
			m.msg = "hiding whitespace"
		}
	case "?":
		m.showHelp = true

	case "space", "a", "d", "A", "D", "w", "u":
		if m.staging() {
			m.handleMarkKey(key)
		}
	}
	return nil
}

// toggleFollow pauses or resumes live-follow. Resuming reloads once right away
// so the screen catches up on whatever changed while it was paused, then re-arms
// the watcher.
func (m *Model) toggleFollow() tea.Cmd {
	if m.watch == nil {
		return nil
	}
	m.live = !m.live
	if !m.live {
		m.msg = "following paused — f to resume"
		return nil
	}
	m.liveReload()
	if m.msg == "" {
		m.msg = "following resumed"
	}
	return m.watch.wait()
}

// maxContext caps how far + will widen the context. Past this a "diff" is just
// the whole file twice.
const maxContext = 20

// changeContext widens or narrows the unchanged lines around each hunk and
// re-diffs. Git review mode only, since it is the only source hunk can re-run.
func (m *Model) changeContext(delta int) {
	if !m.staging() {
		return
	}
	next := clamp(m.context+delta, 0, maxContext)
	if next == m.context {
		return
	}
	m.context = next
	m.liveReload()
	m.msg = "context: " + plural(m.context, "line")
}

// toggleIgnoreWS flips whitespace-only changes on and off by re-running the
// diff. Only git review mode can re-source, so it is a no-op elsewhere.
func (m *Model) toggleIgnoreWS() {
	if !m.staging() {
		return
	}
	m.ignoreWS = !m.ignoreWS
	m.liveReload()
	if m.ignoreWS {
		m.msg = "ignoring whitespace"
	} else {
		m.msg = "showing whitespace"
	}
}

// searchKey edits the / prompt. Enter confirms and jumps to the first match,
// esc abandons the edit (the previous search stays), backspace deletes.
func (m *Model) searchKey(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "esc":
		m.searchInput, m.typed = false, ""
	case "enter":
		m.searchInput = false
		m.search = m.typed
		if m.search != "" {
			m.jumpMatchFrom(m.cur, 1, true)
		}
	case "backspace":
		if r := []rune(m.typed); len(r) > 0 {
			m.typed = string(r[:len(r)-1])
		}
	default:
		// Key.Text is non-empty only for printable input, so control keys are
		// ignored here without a list of names to maintain.
		m.typed += msg.Key().Text
	}
}

// jumpMatch moves to the next (dir 1) or previous (dir -1) row matching the
// active search, wrapping around the diff.
func (m *Model) jumpMatch(dir int) { m.jumpMatchFrom(m.cur, dir, false) }

func (m *Model) jumpMatchFrom(from, dir int, inclusive bool) {
	n := len(m.view.Rows)
	if n == 0 || m.search == "" {
		return
	}
	start := from
	if !inclusive {
		start = from + dir
	}
	for i := 0; i < n; i++ {
		idx := ((start+dir*i)%n + n) % n
		if matchText(m.rowSearchText(idx), m.search) {
			m.moveTo(idx)
			return
		}
	}
	m.msg = "no match: " + m.search
}

// rowSearchText is everything on a row a search can hit: its header text and
// both panes.
func (m *Model) rowSearchText(idx int) string {
	r := m.view.Rows[idx]
	return r.Text + " " + r.Left.Text + " " + r.Right.Text
}

// matchText is smartcase: a lowercase query matches case-insensitively, a query
// with any uppercase is matched exactly — the same rule vim uses.
func matchText(hay, needle string) bool {
	if needle == "" {
		return false
	}
	for _, r := range needle {
		if unicode.IsUpper(r) {
			return strings.Contains(hay, needle)
		}
	}
	return strings.Contains(strings.ToLower(hay), needle)
}

// handleClick routes a left click to whatever is under the pointer: an option
// on the status bar, a file in the sidebar, or a row in the diff body.
func (m *Model) handleClick(e tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if e.Button != tea.MouseLeft {
		return m, nil
	}
	// A click, like a keystroke, dismisses the help overlay and does nothing
	// else while it is up.
	if m.showHelp {
		m.showHelp = false
		return m, nil
	}
	m.msg = ""

	x, y := e.X, e.Y

	// Status bar: the bottom line. A click on an option label runs its key.
	if y == m.bodyHeight() {
		for _, h := range m.hints {
			if x >= h.x0 && x <= h.x1 {
				return m, m.command(h.key)
			}
		}
		return m, nil
	}

	// Sidebar: the left column, when shown. A click jumps to that file.
	if m.sidebar() && x < sidebarWidth {
		if idx := m.sidebarFileAt(y); idx >= 0 {
			m.moveTo(m.view.FileRows[idx])
		}
		return m, nil
	}

	// Body: put the cursor on the clicked row so the next mark or jump acts on
	// what the user pointed at.
	if row := m.top + y; row < len(m.view.Rows) {
		m.moveTo(row)
	}
	return m, nil
}

// sidebarFileAt maps a body-row y to the file index drawn there, or -1 for a
// blank line past the end. It mirrors the windowing in renderSidebar.
func (m *Model) sidebarFileAt(y int) int {
	cur := 0
	if m.cur < len(m.view.Rows) {
		cur = m.view.Rows[m.cur].FileIdx
	}
	start := 0
	if h := m.bodyHeight(); cur >= h {
		start = cur - h + 1
	}
	idx := start + y
	if idx < 0 || idx >= len(m.files) {
		return -1
	}
	return idx
}

// handleWheel scrolls the diff a few lines per notch, the shape people expect
// from a pager.
func (m *Model) handleWheel(e tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	const rows = 3
	switch e.Button {
	case tea.MouseWheelUp:
		m.step(-rows)
	case tea.MouseWheelDown:
		m.step(rows)
	}
	return m, nil
}

// handleMarkKey applies the marking keys, which only exist in git review mode.
func (m *Model) handleMarkKey(key string) {
	file, hunk := m.currentTarget()

	switch key {
	case "space":
		if m.heldSpace(file, hunk) {
			return
		}
		// Marking moves on to the next hunk in this file: approving is a run of
		// decisions, and stopping on each one you just made costs a keystroke.
		// Unmarking stays put, so you can see what you took back.
		on := !m.marks.has(file, hunk)
		m.marks.set(file, hunk, on)
		if on {
			m.nextHunkInFile(file)
		}
	case "a":
		m.marks.set(file, hunk, true)
		m.moveTo(NextIndex(m.view.HunkRows, m.cur))
	case "d":
		m.marks.set(file, hunk, false)
		m.moveTo(NextIndex(m.view.HunkRows, m.cur))
	case "A":
		m.markWholeFile(file, true)
	case "D":
		m.markWholeFile(file, false)
	case "w":
		if hunks, _ := m.marks.total(); hunks == 0 {
			m.msg = "nothing marked — space or a to mark a hunk"
			return
		}
		m.stage()
	case "u":
		m.undo()
	}
}

// heldSpace reports whether this space press is the key repeating: it landed on
// a hunk the cursor was moved to by the previous press, too soon after it to be
// a decision. A press on the hunk just marked is always a decision, so undoing
// a mark stays instant.
func (m *Model) heldSpace(file, hunk int) bool {
	now := m.clock()
	target := [2]int{file, hunk}
	if target != m.lastMark && now.Sub(m.lastMarkAt) < markRepeat {
		return true
	}
	m.lastMark, m.lastMarkAt = target, now
	return false
}

// nextHunkInFile puts the cursor on this file's next hunk, or leaves it where
// it is when this was the file's last one — jumping into the next file would
// take the eye somewhere it did not ask to go.
func (m *Model) nextHunkInFile(file int) {
	for _, row := range m.view.HunkRows {
		if row > m.cur && m.view.Rows[row].FileIdx == file {
			m.moveTo(row)
			return
		}
	}
}

// place describes where the cursor sits by content, not row number, so it can
// be found again after the view is rebuilt. key is empty when the cursor is on
// a file header rather than a hunk; offset is how far into that hunk the cursor
// had scrolled, and screen how far down the window it was sitting, so a reload
// puts the same lines back under the same eyes instead of yanking the view up
// to the hunk's first row.
type place struct {
	path   string
	key    string
	offset int
	screen int
}

func (m *Model) cursorIdentity() place {
	if m.cur >= len(m.view.Rows) {
		return place{}
	}
	r := m.view.Rows[m.cur]
	if r.FileIdx >= len(m.files) {
		return place{}
	}
	f := m.files[r.FileIdx]
	p := place{path: f.Path(), screen: m.cur - m.top}
	if r.HunkIdx >= 0 && r.HunkIdx < len(f.Hunks) {
		p.key = f.Hunks[r.HunkIdx].Key()
		lo, _ := m.currentHunkSpan()
		if lo >= 0 {
			p.offset = m.cur - lo
		}
	}
	return p
}

// restoreCursor puts the cursor back where it was reading: same file, same
// hunk, same distance into that hunk, and the same distance down the window.
func (m *Model) restoreCursor(p place) {
	fi := m.fileIndexByPath(p.path)
	if fi < 0 {
		m.moveTo(m.cur) // path gone; just clamp and stay near where we were
		return
	}

	target := m.view.FileRows[fi]
	if p.key != "" {
		globalHunk := 0
		for i := 0; i < fi; i++ {
			globalHunk += len(m.files[i].Hunks)
		}
		for hi, h := range m.files[fi].Hunks {
			if h.Key() == p.key {
				if g := globalHunk + hi; g < len(m.view.HunkRows) {
					target = m.view.HunkRows[g] + p.offset
					// The hunk may have shrunk under the offset; never walk out
					// of it into the next one.
					if end := hunkEnd(m.view, m.view.HunkRows[g]); target >= end {
						target = end - 1
					}
				}
				break
			}
		}
	}

	m.moveTo(target)
	if p.screen > 0 {
		m.top = m.cur - p.screen
		m.ensureVisible()
	}
}

// hunkEnd is the row after the last one belonging to the hunk that starts at
// row start.
func hunkEnd(v *View, start int) int {
	r := v.Rows[start]
	end := start + 1
	for end < len(v.Rows) && v.Rows[end].FileIdx == r.FileIdx && v.Rows[end].HunkIdx == r.HunkIdx {
		end++
	}
	return end
}

func (m *Model) fileIndexByPath(path string) int {
	for i, f := range m.files {
		if f.Path() == path {
			return i
		}
	}
	return -1
}

// currentTarget is the file and hunk the cursor is on. A file with no hunks
// (a binary file) can only be marked whole.
func (m *Model) currentTarget() (file, hunk int) {
	if m.cur >= len(m.view.Rows) {
		return 0, wholeFile
	}
	row := m.view.Rows[m.cur]
	if row.HunkIdx < 0 || len(m.files[row.FileIdx].Hunks) == 0 {
		return row.FileIdx, wholeFile
	}
	return row.FileIdx, row.HunkIdx
}

func (m *Model) markWholeFile(file int, on bool) {
	f := m.files[file]
	if len(f.Hunks) == 0 {
		m.marks.set(file, wholeFile, on)
		return
	}
	for i := range f.Hunks {
		m.marks.set(file, i, on)
	}
}

// stage writes the marked hunks straight to the index — no confirmation, since
// nothing here touches the working tree and "u" takes it right back out. The
// staged hunks then drop off the diff, freeing the screen for what is left.
func (m *Model) stage() {
	result, err := m.stageMarked()
	if err != nil {
		// Keep the marks: the user can fix the problem and try again.
		m.msg = "staging failed: " + firstLine(err.Error())
		return
	}
	if err := m.reload(); err != nil {
		m.msg = result + " (could not re-read the working tree: " + firstLine(err.Error()) + ")"
		return
	}
	m.msg = result + "  ·  u to undo"
}

// undo reverses the most recent stage, putting those changes back into the
// working tree exactly as they were before w.
func (m *Model) undo() {
	if m.lastPatch == "" && len(m.lastWhole) == 0 {
		m.msg = "nothing to undo"
		return
	}
	if err := m.unstageLast(); err != nil {
		m.msg = "undo failed: " + firstLine(err.Error())
		return
	}
	m.lastPatch, m.lastWhole = "", nil
	if err := m.reload(); err != nil {
		m.msg = "undone (could not re-read the working tree: " + firstLine(err.Error()) + ")"
		return
	}
	m.msg = "undone — back to unstaged"
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// step scrolls within the current file. Only one file is on screen at a time,
// so running off its end should stop at the end rather than drag the view into
// a file the reader did not ask for — ] and [ are how you change file.
func (m *Model) step(delta int) {
	lo, hi := m.fileSpan(m.cur)
	m.moveTo(clamp(m.cur+delta, lo, hi-1))
}

// moveTo puts the cursor on a row, clamped to the diff, and scrolls to it.
func (m *Model) moveTo(row int) {
	if len(m.view.Rows) == 0 {
		m.cur, m.top = 0, 0
		return
	}
	m.cur = clamp(row, 0, len(m.view.Rows)-1)
	m.ensureVisible()
}

func (m *Model) ensureVisible() {
	// The viewport is scoped to the current file: only its rows are ever shown,
	// so scrolling can never mix two files on screen.
	lo, hi := m.fileSpan(m.cur)
	h := m.bodyHeight()
	if m.cur < m.top {
		m.top = m.cur
	}
	if m.cur >= m.top+h {
		m.top = m.cur - h + 1
	}
	maxTop := hi - h
	if maxTop < lo {
		maxTop = lo
	}
	m.top = clamp(m.top, lo, maxTop)
}

// fileSpan is the [lo, hi) row range of the file that owns row. Rows outside it
// belong to other files and are never drawn while this one is current.
func (m *Model) fileSpan(row int) (lo, hi int) {
	if len(m.view.Rows) == 0 {
		return 0, 0
	}
	if row >= len(m.view.Rows) {
		row = len(m.view.Rows) - 1
	}
	f := m.view.Rows[row].FileIdx
	lo = m.view.FileRows[f]
	if f+1 < len(m.view.FileRows) {
		hi = m.view.FileRows[f+1]
	} else {
		hi = len(m.view.Rows)
	}
	return lo, hi
}

// currentHunkSpan is the [lo, hi) row range of the hunk under the cursor, or
// (-1, -1) when the cursor is not on a hunk (a file header or a hunkless file).
func (m *Model) currentHunkSpan() (lo, hi int) {
	file, hunk := m.currentTarget()
	if hunk == wholeFile || m.cur >= len(m.view.Rows) {
		return -1, -1
	}
	match := func(i int) bool {
		r := m.view.Rows[i]
		return r.FileIdx == file && r.HunkIdx == hunk
	}
	lo, hi = m.cur, m.cur+1
	for lo-1 >= 0 && match(lo-1) {
		lo--
	}
	for hi < len(m.view.Rows) && match(hi) {
		hi++
	}
	return lo, hi
}

// rebuildIfNeeded re-flattens the diff when the split/unified choice changes,
// keeping the cursor on the same file rather than at the same row number.
func (m *Model) rebuildIfNeeded() {
	if m.split() == m.builtSplit {
		return
	}
	var file int
	if m.cur < len(m.view.Rows) {
		file = m.view.Rows[m.cur].FileIdx
	}

	m.builtSplit = m.split()
	m.rebuildView()

	m.cur = 0
	if file < len(m.view.FileRows) {
		m.cur = m.view.FileRows[file]
	}
	m.ensureVisible()
}

// View renders the current screen into the alternate screen buffer.
func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	// Cell-motion mouse tracking delivers clicks and wheel events so the sidebar
	// and status-bar options work by pointer, not only by keystroke.
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m *Model) render() string {
	if m.width == 0 || m.height == 0 {
		return "" // no size yet; the first WindowSizeMsg is on its way
	}

	screen := m.renderScreen()
	if m.showHelp {
		// The help is a modal: the diff stays visible behind a centered box.
		return m.overlay(screen, m.renderHelp())
	}
	return screen
}

// renderScreen draws the diff, sidebar, and status bar — the whole screen
// except any modal floating on top of it.
func (m *Model) renderScreen() string {
	body := m.renderBody()
	side := m.renderSidebar()

	lines := make([]string, 0, m.bodyHeight()+1)
	for i := 0; i < m.bodyHeight(); i++ {
		line := body[i]
		if side != nil {
			line = side[i] + m.st.gutter.Render("│") + line
		}
		lines = append(lines, line)
	}
	lines = append(lines, m.renderStatus())
	return strings.Join(lines, "\n")
}

// overlay floats box in the center of base, compositing so the base screen
// shows through around it.
func (m *Model) overlay(base, box string) string {
	x := (m.width - lipgloss.Width(box)) / 2
	y := (m.height - lipgloss.Height(box)) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return lipgloss.NewCompositor(
		lipgloss.NewLayer(base),
		lipgloss.NewLayer(box).X(x).Y(y).Z(1),
	).Render()
}

// renderBody renders exactly the visible window of rows. Everything off screen
// costs nothing, which is what keeps a 20k-line diff responsive.
func (m *Model) renderBody() []string {
	// One column is reserved for the cursor marker: without it, j/k move
	// something invisible and navigation feels broken.
	w := m.contentWidth() - 1
	out := make([]string, 0, m.bodyHeight())

	if len(m.view.Rows) == 0 {
		return m.renderEmpty(w + 1)
	}

	// Only the current file's rows are drawn; anything past its end is blank,
	// even when that leaves empty space, so files never mix on screen.
	_, hi := m.fileSpan(m.cur)
	hlo, hhi := m.currentHunkSpan()
	for i := 0; i < m.bodyHeight(); i++ {
		row := m.top + i
		if row >= hi || row >= len(m.view.Rows) {
			out = append(out, m.st.base.Render(strings.Repeat(" ", w+1)))
			continue
		}
		focus := hlo <= row && row < hhi
		out = append(out, m.railMark(row, hlo, hhi)+m.renderRow(m.view.Rows[row], w, focus))
	}
	return out
}

// renderEmpty fills the body when there is no diff to show. In a repo that is
// not "done" but "not yet": hunk keeps following the tree, so the message says
// what it is waiting for.
func (m *Model) renderEmpty(w int) []string {
	msg := "no changes to show"
	switch {
	case m.staging() && m.live:
		msg = "nothing to stage yet — watching for edits"
	case m.staging() && m.watch != nil:
		msg = "nothing to stage — following is paused, press f to resume"
	case m.staging():
		msg = "nothing to stage — the working tree is clean"
	}

	blank := m.st.base.Render(strings.Repeat(" ", w))
	out := make([]string, 0, m.bodyHeight())
	mid := m.bodyHeight() / 3
	for i := 0; i < m.bodyHeight(); i++ {
		if i != mid {
			out = append(out, blank)
			continue
		}
		pad := (w - lipgloss.Width(msg)) / 2
		if pad < 0 {
			pad = 0
		}
		out = append(out, fit(m.st.base.Render(strings.Repeat(" ", pad))+m.st.notice.Render(msg), 0, w, m.st.base))
	}
	return out
}

// railMark draws the left-margin column for a row: the cursor bar on the cursor
// row, a thin accent bar down the active hunk, and a green bar down any marked
// hunk so what will be staged is visible at a glance. Green always means marked.
func (m *Model) railMark(row, hlo, hhi int) string {
	cursor := row == m.cur
	current := hlo <= row && row < hhi
	marked := m.rowMarked(row)

	// A marked hunk gets a solid bar so it reads as a block; the current hunk
	// gets a thin accent. The cursor is always solid so its line is findable.
	glyph := "▎"
	if cursor || marked {
		glyph = "▌"
	}

	// Marked wins the color outright — a whole marked hunk reads green top to
	// bottom, including the cursor line, so "this will be staged" is never
	// masked by the cursor or the current-hunk accent.
	switch {
	case marked:
		return m.st.railMarked.Render(glyph)
	case cursor:
		return m.st.cursor.Render(glyph)
	case current:
		return m.st.focusRail.Render(glyph)
	default:
		return m.st.base.Render(" ")
	}
}

// rowMarked reports whether the hunk a row belongs to is currently marked.
func (m *Model) rowMarked(row int) bool {
	if row < 0 || row >= len(m.view.Rows) {
		return false
	}
	r := m.view.Rows[row]
	return m.marks.has(r.FileIdx, r.HunkIdx)
}

func (m *Model) renderRow(r Row, w int, focus bool) string {
	switch r.Kind {
	case RowFile:
		return fit(m.st.fileHeader.Render(" "+r.Text), 0, w, m.st.fileHeader)
	case RowHunk:
		style := m.st.hunkHeader
		if focus {
			style = m.st.focusHeader
		}
		return fit(style.Render(" "+r.Text), 0, w, style)
	case RowNotice:
		return fit(m.st.notice.Render("   "+r.Text), 0, w, m.st.base)
	case RowSpacer:
		return m.st.base.Render(strings.Repeat(" ", w))
	}

	// The two outermost columns belong to the change block's outline. They are
	// reserved on every row, boxed or not, or text would shift sideways as the
	// eye moves from a context line into a change.
	inner := w - 2
	lead := m.st.box.Render(edgeGlyph(r.BoxLeft, true))
	trail := m.st.box.Render(edgeGlyph(r.BoxRight, false))

	if !m.builtSplit {
		return lead + m.paneOr(r.BoxLeft, inner, func() string {
			return m.renderUnifiedRow(r, inner)
		}) + trail
	}

	half := (inner - 1) / 2
	left := m.paneOr(r.BoxLeft, half, func() string {
		return m.st.renderSide(r.Left, "", numWidth, half-numWidth-1, m.hscroll, m.showWS)
	})
	right := m.paneOr(r.BoxRight, inner-half-1, func() string {
		return m.st.renderSide(r.Right, "", numWidth, inner-half-1-numWidth-1, m.hscroll, m.showWS)
	})
	return lead + left + m.divider(r) + right + trail
}

// paneOr draws a pane's share of a rule row, or the pane's normal contents when
// the outline is not opening or closing here.
func (m *Model) paneOr(p BoxPart, width int, draw func() string) string {
	if p == BoxTop || p == BoxBottom {
		return m.st.box.Render(strings.Repeat(lipgloss.RoundedBorder().Top, width))
	}
	return draw()
}

// edgeGlyph is the outline's outer column for one pane: a corner where the box
// opens or closes, its side while it is open, nothing when the pane is outside.
func edgeGlyph(p BoxPart, left bool) string {
	b := lipgloss.RoundedBorder()
	switch p {
	case BoxTop:
		if left {
			return b.TopLeft
		}
		return b.TopRight
	case BoxMid:
		return b.Left
	case BoxBottom:
		if left {
			return b.BottomLeft
		}
		return b.BottomRight
	default:
		return " "
	}
}

// divider draws the column between the panes. Inside a block there is no
// divider: the outline is one shape, so the seam carries whatever the outline
// is doing on that row — the rule sweeping across, a corner where a box that
// covers only one pane turns, the turn down into the taller pane, that pane's
// wall, or the corner where it finally closes.
func (m *Model) divider(r Row) string {
	b := lipgloss.RoundedBorder()
	l, rt := r.BoxLeft, r.BoxRight

	if l == BoxNone && rt == BoxNone {
		return m.st.gutter.Render("│")
	}
	if r.Arrow {
		// The change reads left to right — old on the left, new on the right —
		// and the marker says so, floating in the gap inside the outline.
		return m.st.box.Bold(true).Render("→")
	}

	glyph := b.Left // one pane is enclosed and the other is not: its wall
	switch {
	case l == rt: // both panes do the same thing here
		switch l {
		case BoxTop, BoxBottom:
			glyph = b.Top // one rule sweeping across both panes
		default:
			glyph = " " // inside the box, nothing separates the panes
		}
	case l == BoxTop:
		glyph = b.TopRight // a box over the left pane only
	case rt == BoxTop:
		glyph = b.TopLeft
	case l == BoxBottom && rt == BoxNone:
		glyph = b.BottomRight
	case rt == BoxBottom && l == BoxNone:
		glyph = b.BottomLeft
	case l == BoxBottom:
		glyph = b.TopRight // the left pane closes; its rule turns down into the
	case rt == BoxBottom: // wall the taller pane leans on, and vice versa
		glyph = b.TopLeft
	}
	return m.st.box.Render(glyph)
}

// renderUnifiedRow shows a single column with +/-/space signs, the shape people
// already know from git diff.
func (m *Model) renderUnifiedRow(r Row, w int) string {
	side, sign := r.Left, " "
	if r.Left.Empty {
		side, sign = r.Right, "+"
	} else if r.Right.Empty && r.Left.Kind == diff.Removed {
		sign = "-"
	} else if r.Left.Kind == diff.Added {
		sign = "+"
	}
	return m.st.renderSide(side, sign, numWidth, w-numWidth-1, m.hscroll, m.showWS)
}

// renderSidebar lists the changed files, or nil when there is no room for it.
func (m *Model) renderSidebar() []string {
	if !m.sidebar() {
		return nil
	}

	cur := 0
	if m.cur < len(m.view.Rows) {
		cur = m.view.Rows[m.cur].FileIdx
	}

	// Keep the current file on screen when there are more files than lines.
	h := m.bodyHeight()
	start := 0
	if cur >= h {
		start = cur - h + 1
	}

	out := make([]string, 0, h)
	for i := 0; i < h; i++ {
		idx := start + i
		if idx >= len(m.files) {
			out = append(out, m.st.sidebar.Render(strings.Repeat(" ", sidebarWidth)))
			continue
		}
		f := m.files[idx]
		style := m.st.sidebar
		if idx == cur {
			style = m.st.sidebarSel
		}

		if !m.staging() {
			label := fmt.Sprintf(" %s  +%d -%d",
				shortPath(f.Path(), sidebarWidth-10), f.Added, f.Removed)
			out = append(out, fit(style.Render(label), 0, sidebarWidth, style))
			continue
		}

		symbol, symStyle := m.fileGlyph(idx, f, style)
		rest := fmt.Sprintf("%s  +%d -%d", shortPath(f.Path(), sidebarWidth-12), f.Added, f.Removed)
		line := style.Render(" ") + symStyle.Render(symbol) + style.Render(" "+rest)
		out = append(out, fit(line, 0, sidebarWidth, style))
	}
	return out
}

// fileGlyph is the sidebar symbol for a file and the style to draw it in.
// A pending selection shows the mark symbol; otherwise a staged file shows a
// check — green when fully staged, gray when only partly.
func (m *Model) fileGlyph(idx int, f diff.File, base lipgloss.Style) (string, lipgloss.Style) {
	if m.marks.inFile(idx) > 0 {
		return m.marks.state(idx, f).symbol(), base
	}
	path := f.Path()
	switch {
	case m.staged[path] && !m.unstaged[path]:
		return "✓", base.Foreground(m.st.stagedFg)
	case m.staged[path]:
		return "✓", base.Foreground(m.st.partialFg)
	default:
		return "·", base
	}
}

// shortPath trims a path from the left, keeping the filename, which is the part
// that identifies it.
func shortPath(p string, w int) string {
	if w < 4 || len(p) <= w {
		return p
	}
	return "…" + p[len(p)-w+1:]
}

func (m *Model) renderStatus() string {
	// No hints are clickable unless the option row below actually draws them.
	m.hints = nil

	// The / prompt takes over the status line while it is open.
	if m.searchInput {
		return fit(m.st.statusbar.Render(" /"+m.typed+"█"), 0, m.width, m.st.statusbar)
	}

	// A message — a confirmation prompt, or the result of staging — replaces the
	// status line while it is relevant.
	if m.msg != "" {
		return fit(m.st.statusbar.Render(" "+m.msg), 0, m.width, m.st.statusbar)
	}
	if len(m.view.Rows) == 0 {
		if m.staging() {
			status := " working tree clean"
			if m.ignoreWS {
				status += "  ·  ≈ ws"
			}
			if m.watch != nil {
				live := "○ paused"
				if m.live {
					live = "● live"
				}
				status += "  ·  " + live + "  ·  f follow"
			}
			return fit(m.st.statusbar.Render(status+"  ·  q quit"), 0, m.width, m.st.statusbar)
		}
		return fit(m.st.statusbar.Render(" no changes  ·  q quit"), 0, m.width, m.st.statusbar)
	}

	fileIdx := m.view.Rows[m.cur].FileIdx
	f := m.files[fileIdx]

	mode := "split"
	if !m.builtSplit {
		mode = "unified"
	}

	left := fmt.Sprintf(" %s  +%d -%d  ·  file %d/%d  ·  %s",
		f.Path(), f.Added, f.Removed, fileIdx+1, len(m.files), mode)
	if m.search != "" {
		left += "  ·  /" + m.search
	}
	if m.showWS {
		left += "  ·  ·→"
	}
	opts := []hintZone{
		{key: "n"}, {key: "]"}, {key: "s"}, {key: "/"}, {key: "W"}, {key: "?"}, {key: "q"},
	}
	labels := []string{"n/p hunk", "]/[ file", "s split", "/ search", "W space", "? help", "q quit"}
	if m.staging() {
		if hunks, files := m.marks.total(); hunks > 0 {
			left += fmt.Sprintf("  ·  %s marked in %s", plural(hunks, "hunk"), plural(files, "file"))
		}
		if m.watch != nil {
			ind := "○ paused"
			if m.live {
				ind = "● live"
			}
			left += "  ·  " + ind
		}
		if m.ignoreWS {
			left += "  ·  ≈ ws"
		}
		if m.context != diff.DefaultContext {
			left += fmt.Sprintf("  ·  ⋯ %d", m.context)
		}
		opts = []hintZone{{key: "space"}, {key: "A"}, {key: "w"}, {key: "u"}, {key: "f"}, {key: "i"}, {key: "+"}, {key: "W"}, {key: "/"}, {key: "?"}, {key: "q"}}
		labels = []string{"space mark", "A file", "w stage", "u undo", "f follow", "i ws", "+/- ctx", "W space", "/ search", "? help", "q quit"}
	}

	right := strings.Join(labels, "  ") + " "
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return fit(m.st.statusbar.Render(left), 0, m.width, m.st.statusbar)
	}

	// Record where each label lands so a click there runs its key. The right
	// block starts after the left text and the gap that pushes it to the edge.
	x := lipgloss.Width(left) + gap
	for i, label := range labels {
		opts[i].x0 = x
		opts[i].x1 = x + lipgloss.Width(label) - 1
		x += lipgloss.Width(label) + 2 // labels are joined by two spaces
	}
	m.hints = opts

	return m.st.statusbar.Render(left + strings.Repeat(" ", gap) + right)
}

func (m *Model) renderHelp() string {
	rows := [][2]string{
		{"j / k, ↑ / ↓", "scroll a line"},
		{"ctrl-d / ctrl-u", "scroll half a page"},
		{"n / p", "next / previous hunk"},
		{"] / [", "next / previous file"},
		{"g / G", "top / bottom"},
		{"/", "search; enter jumps, esc clears"},
		{"n / N", "next / previous match (while searching)"},
		{"W", "show / hide whitespace (tabs, trailing spaces)"},
		{"h / l, ← / →", "scroll sideways"},
		{"s", "toggle side-by-side / unified"},
		{"b", "toggle the file sidebar"},
		{"?", "this help"},
		{"q", "quit"},
		{"", ""},
		{"mouse", "click a file, row, or status-bar option; wheel scrolls"},
	}
	if m.staging() {
		rows = append(rows,
			[2]string{"", ""},
			[2]string{"space", "mark this hunk and move to the next one in the file"},
			[2]string{"a / d", "mark / unmark, then jump to the next hunk"},
			[2]string{"A / D", "mark / unmark every hunk in this file"},
			[2]string{"w", "stage what is marked"},
			[2]string{"u", "undo the last stage"},
			[2]string{"f", "pause / resume following file changes"},
			[2]string{"i", "ignore / show whitespace-only changes"},
			[2]string{"+ / -", "more / less context around each hunk"},
		)
	}

	lines := []string{m.st.modalTitle.Render("hunk — keys"), ""}
	for _, r := range rows {
		if r[0] == "" && r[1] == "" {
			lines = append(lines, m.st.base.Render(""))
			continue
		}
		lines = append(lines, m.st.help.Render(fmt.Sprintf("%-18s %s", r[0], r[1])))
	}
	lines = append(lines, "", m.st.notice.Render("press any key to close"))

	return m.st.modal.Render(strings.Join(lines, "\n"))
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
