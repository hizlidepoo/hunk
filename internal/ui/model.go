package ui

import (
	"fmt"
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
	repo    *git.Repo
	marks   marks
	confirm bool
	msg     string
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

// NewGit is New for a working tree hunk can stage into.
func NewGit(repo *git.Repo, files []diff.File, t *theme.Theme) *Model {
	m := New(files, t)
	m.repo = repo
	m.marks = marks{}
	return m
}

// Run puts the model on screen and blocks until the user quits.
func Run(files []diff.File, t *theme.Theme) error {
	_, err := tea.NewProgram(New(files, t)).Run()
	return err
}

// RunGit is Run with staging enabled.
func RunGit(repo *git.Repo, files []diff.File, t *theme.Theme) error {
	_, err := tea.NewProgram(NewGit(repo, files, t)).Run()
	return err
}

// staging reports whether this session can write to the index.
func (m *Model) staging() bool { return m.repo != nil }

// Init satisfies tea.Model; hunk has nothing to do before its first render.
func (m *Model) Init() tea.Cmd { return nil }

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
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.showHelp {
		// Any key closes help; there is nothing else to do while it is up.
		m.showHelp = false
		return m, nil
	}

	if m.confirm {
		return m.handleConfirm(msg.String())
	}

	// A keystroke means the last result has been read.
	m.msg = ""

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

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
	case "?":
		m.showHelp = true

	case "space", "a", "d", "A", "D", "w":
		if m.staging() {
			m.handleMarkKey(msg.String())
		}
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
		hunks, files := m.marks.total()
		if hunks == 0 {
			m.msg = "nothing marked — space or a to mark a hunk"
			return
		}
		m.msg = fmt.Sprintf("stage %s in %s?  y / n", plural(hunks, "hunk"), plural(files, "file"))
		m.confirm = true
	}
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

func (m *Model) handleConfirm(key string) (tea.Model, tea.Cmd) {
	m.confirm = false

	if key != "y" && key != "Y" {
		m.msg = "cancelled — nothing was staged"
		return m, nil
	}

	result, err := m.stageMarked()
	if err != nil {
		// Keep the marks: the user can fix the problem and try again.
		m.msg = "staging failed: " + firstLine(err.Error())
		return m, nil
	}
	if err := m.reload(); err != nil {
		m.msg = result + " (could not re-read the working tree: " + firstLine(err.Error()) + ")"
		return m, nil
	}
	m.msg = result + " — undo with: git restore --staged ."
	return m, nil
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
	h := m.bodyHeight()
	if m.cur < m.top {
		m.top = m.cur
	}
	if m.cur >= m.top+h {
		m.top = m.cur - h + 1
	}
	maxTop := len(m.view.Rows) - h
	if maxTop < 0 {
		maxTop = 0
	}
	m.top = clamp(m.top, 0, maxTop)
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
	return v
}

func (m *Model) render() string {
	if m.width == 0 || m.height == 0 {
		return "" // no size yet; the first WindowSizeMsg is on its way
	}
	if m.showHelp {
		return m.renderHelp()
	}

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

// renderBody renders exactly the visible window of rows. Everything off screen
// costs nothing, which is what keeps a 20k-line diff responsive.
func (m *Model) renderBody() []string {
	// One column is reserved for the cursor marker: without it, j/k move
	// something invisible and navigation feels broken.
	w := m.contentWidth() - 1
	out := make([]string, 0, m.bodyHeight())

	for i := 0; i < m.bodyHeight(); i++ {
		row := m.top + i
		if row >= len(m.view.Rows) {
			out = append(out, m.st.base.Render(strings.Repeat(" ", w+1)))
			continue
		}
		out = append(out, m.cursorMark(row)+m.renderRow(m.view.Rows[row], w))
	}
	return out
}

func (m *Model) cursorMark(row int) string {
	if row == m.cur {
		return m.st.cursor.Render("▌")
	}
	return m.st.base.Render(" ")
}

func (m *Model) renderRow(r Row, w int) string {
	switch r.Kind {
	case RowFile:
		return fit(m.st.fileHeader.Render(" "+r.Text), 0, w, m.st.fileHeader)
	case RowHunk:
		return fit(m.st.hunkHeader.Render(" "+r.Text), 0, w, m.st.hunkHeader)
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
		mark := ""
		if m.staging() {
			mark = m.marks.state(idx, f).symbol() + " "
		}
		label := fmt.Sprintf(" %s%s  +%d -%d",
			mark, shortPath(f.Path(), sidebarWidth-10-len(mark)), f.Added, f.Removed)

		style := m.st.sidebar
		if idx == cur {
			style = m.st.sidebarSel
		}
		out = append(out, fit(style.Render(label), 0, sidebarWidth, style))
	}
	return out
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
	right := "n/p hunk  ]/[ file  s split  ? help  q quit "
	if m.staging() {
		if hunks, files := m.marks.total(); hunks > 0 {
			left += fmt.Sprintf("  ·  %s marked in %s", plural(hunks, "hunk"), plural(files, "file"))
		}
		right = "space mark  A file  w stage  ? help  q quit "
	}

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return fit(m.st.statusbar.Render(left), 0, m.width, m.st.statusbar)
	}
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
	}
	if m.staging() {
		rows = append(rows,
			[2]string{"", ""},
			[2]string{"space", "mark / unmark this hunk"},
			[2]string{"a / d", "mark / unmark, then jump to the next hunk"},
			[2]string{"A / D", "mark / unmark every hunk in this file"},
			[2]string{"w", "stage what is marked (asks first)"},
		)
	}

	lines := []string{m.st.fileHeader.Render(" hunk — keys"), ""}
	for _, r := range rows {
		lines = append(lines, m.st.help.Render(fmt.Sprintf("  %-18s %s", r[0], r[1])))
	}
	lines = append(lines, "", m.st.notice.Render("  press any key to go back"))

	for len(lines) < m.height {
		lines = append(lines, m.st.base.Render(strings.Repeat(" ", m.width)))
	}
	return strings.Join(lines[:m.height], "\n")
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
