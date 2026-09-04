package ui

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/wmarquardt/hunk/internal/diff"
	"github.com/wmarquardt/hunk/internal/git"
	"github.com/wmarquardt/hunk/internal/theme"
)

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

// New builds a model over an already-parsed diff.
func New(files []diff.File, t *theme.Theme) *Model {
	m := &Model{
		files:       files,
		st:          newStyles(t),
		wantSplit:   true,
		wantSidebar: true,
		builtSplit:  true,
	}
	m.view = Build(files, true)
	return m
}

// NewGit is New for a working tree hunk can stage into. It also starts
// following the working tree, so edits made while hunk is open show up on their
// own. A watcher that fails to start just leaves live-follow off.
func NewGit(repo *git.Repo, files []diff.File, t *theme.Theme) *Model {
	m := New(files, t)
	m.repo = repo
	m.marks = marks{}
	m.refreshGitState()

	dir := repo.Dir
	if dir == "" {
		if wd, err := os.Getwd(); err == nil {
			dir = wd
		}
	}
	if w, err := newWatcher(dir); err == nil {
		m.watch, m.live = w, true
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
func Run(files []diff.File, t *theme.Theme) error {
	_, err := tea.NewProgram(New(files, t)).Run()
	return err
}

// RunGit is Run with staging enabled.
func RunGit(repo *git.Repo, files []diff.File, t *theme.Theme) error {
	m := NewGit(repo, files, t)
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
		m.moveTo(m.cur + 1)
	case "k", "up":
		m.moveTo(m.cur - 1)
	case "ctrl+d", "pgdown":
		m.moveTo(m.cur + m.bodyHeight()/2)
	case "ctrl+u", "pgup":
		m.moveTo(m.cur - m.bodyHeight()/2)
	case "g", "home":
		m.moveTo(0)
	case "G", "end":
		m.moveTo(len(m.view.Rows) - 1)

	case "n":
		m.moveTo(NextIndex(m.view.HunkRows, m.cur))
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
	const step = 3
	switch e.Button {
	case tea.MouseWheelUp:
		m.moveTo(m.cur - step)
	case tea.MouseWheelDown:
		m.moveTo(m.cur + step)
	}
	return m, nil
}

// handleMarkKey applies the marking keys, which only exist in git review mode.
func (m *Model) handleMarkKey(key string) {
	file, hunk := m.currentTarget()

	switch key {
	case "space":
		m.marks.set(file, hunk, !m.marks.has(file, hunk))
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

// cursorIdentity describes where the cursor sits by content, not row number, so
// its place can be found again after the view is rebuilt. key is empty when the
// cursor is on a file header rather than a hunk.
func (m *Model) cursorIdentity() (path, key string) {
	if m.cur >= len(m.view.Rows) {
		return "", ""
	}
	r := m.view.Rows[m.cur]
	if r.FileIdx >= len(m.files) {
		return "", ""
	}
	f := m.files[r.FileIdx]
	if r.HunkIdx >= 0 && r.HunkIdx < len(f.Hunks) {
		key = f.Hunks[r.HunkIdx].Key()
	}
	return f.Path(), key
}

// restoreCursor puts the cursor back on the same file and hunk after a rebuild,
// falling back to the file's header, then to a clamped position, when the exact
// hunk is gone.
func (m *Model) restoreCursor(path, key string) {
	fi := m.fileIndexByPath(path)
	if fi < 0 {
		m.moveTo(m.cur) // path gone; just clamp and stay near where we were
		return
	}
	target := m.view.FileRows[fi]
	if key != "" {
		globalHunk := 0
		for i := 0; i < fi; i++ {
			globalHunk += len(m.files[i].Hunks)
		}
		for hi, h := range m.files[fi].Hunks {
			if h.Key() == key {
				if g := globalHunk + hi; g < len(m.view.HunkRows) {
					target = m.view.HunkRows[g]
				}
				break
			}
		}
	}
	m.moveTo(target)
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
	m.view = Build(m.files, m.builtSplit)

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

	if !m.builtSplit {
		return m.renderUnifiedRow(r, w)
	}

	half := (w - 1) / 2
	left := m.st.renderSide(r.Left, "", numWidth, half-numWidth-1, m.hscroll)
	right := m.st.renderSide(r.Right, "", numWidth, w-half-1-numWidth-1, m.hscroll)
	return left + m.st.gutter.Render("│") + right
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
	return m.st.renderSide(side, sign, numWidth, w-numWidth-1, m.hscroll)
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

	// A message — a confirmation prompt, or the result of staging — replaces the
	// status line while it is relevant.
	if m.msg != "" {
		return fit(m.st.statusbar.Render(" "+m.msg), 0, m.width, m.st.statusbar)
	}
	if len(m.view.Rows) == 0 {
		if m.staging() {
			return fit(m.st.statusbar.Render(" working tree clean  ·  q quit"), 0, m.width, m.st.statusbar)
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
	opts := []hintZone{
		{key: "n"}, {key: "]"}, {key: "s"}, {key: "?"}, {key: "q"},
	}
	labels := []string{"n/p hunk", "]/[ file", "s split", "? help", "q quit"}
	if m.staging() {
		if hunks, files := m.marks.total(); hunks > 0 {
			left += fmt.Sprintf("  ·  %s marked in %s", plural(hunks, "hunk"), plural(files, "file"))
		}
		if m.watch != nil {
			if m.live {
				left += "  ·  ● live"
			} else {
				left += "  ·  ○ paused"
			}
		}
		opts = []hintZone{{key: "space"}, {key: "A"}, {key: "w"}, {key: "u"}, {key: "f"}, {key: "?"}, {key: "q"}}
		labels = []string{"space mark", "A file", "w stage", "u undo", "f follow", "? help", "q quit"}
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
			[2]string{"space", "mark / unmark the current hunk"},
			[2]string{"a / d", "mark / unmark, then jump to the next hunk"},
			[2]string{"A / D", "mark / unmark every hunk in this file"},
			[2]string{"w", "stage what is marked"},
			[2]string{"u", "undo the last stage"},
			[2]string{"f", "pause / resume following file changes"},
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
