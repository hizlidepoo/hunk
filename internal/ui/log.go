package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/wmarquardt/hunk/internal/diff"
	"github.com/wmarquardt/hunk/internal/git"
	"github.com/wmarquardt/hunk/internal/theme"
)

// NewLog builds a model over a repository's history. It is NewGit without the
// index: a commit is something to read, so marking, staging and following the
// working tree are all off, and nothing here can write.
func NewLog(repo *git.Repo, commits []git.Commit, files []diff.File, t *theme.Theme, opts Options) *Model {
	m := New(files, t, opts)
	m.repo = repo
	m.commits = commits
	m.logMode = true
	return m
}

// RunLog puts the history on screen and blocks until the user quits.
func RunLog(repo *git.Repo, commits []git.Commit, files []diff.File, t *theme.Theme, opts Options) error {
	_, err := tea.NewProgram(NewLog(repo, commits, files, t, opts)).Run()
	return err
}

// commit is the commit currently on screen.
func (m *Model) commit() git.Commit {
	if m.commitIdx < 0 || m.commitIdx >= len(m.commits) {
		return git.Commit{}
	}
	return m.commits[m.commitIdx]
}

// loadCommit moves to another commit and shows it from the top. Indexes clamp
// instead of wrapping, so } on the oldest commit stays there.
func (m *Model) loadCommit(idx int) {
	if !m.logMode || len(m.commits) == 0 {
		return
	}
	idx = min(max(idx, 0), len(m.commits)-1)
	if idx == m.commitIdx {
		return
	}
	m.commitIdx = idx
	if m.showCommit() {
		m.cur, m.top, m.hscroll = 0, 0, 0
	}
}

// showCommit reads the current commit into the view. It reports whether the
// read worked; a failure leaves what is on screen alone and says so.
func (m *Model) showCommit() bool {
	text, err := m.repo.Show(m.commit().SHA, m.ignoreWS, m.context)
	var files []diff.File
	if err == nil {
		files, err = diff.ParseString(text)
	}
	if err != nil {
		m.msg = "show failed: " + firstLine(err.Error())
		return false
	}
	m.raw = files
	m.applyFilter()
	m.rebuildView()
	return true
}

// rediff re-runs whatever produced the diff on screen, keeping the user's
// place. It is what + / - and i do once they have changed what to ask for.
func (m *Model) rediff() {
	if m.logMode {
		where := m.cursorIdentity()
		if m.showCommit() {
			m.restoreCursor(where)
		}
		return
	}
	m.liveReload()
}

// logSplit is how many sidebar rows the commit list gets. Each list also costs
// a header row, and the file list below takes whatever is left over.
func (m *Model) logSplit(h int) int {
	avail := h - 2
	if avail < 2 {
		return 0
	}
	return min(avail/2, len(m.commits))
}

// renderLogSidebar draws the two stacked lists of history mode: the commits,
// then the files of the selected one.
func (m *Model) renderLogSidebar(h, w int) []string {
	cn := m.logSplit(h)
	out := make([]string, 0, h)

	out = append(out, m.sidebarHeader("Commits", w))
	start := listStart(m.commitIdx, cn)
	for i := 0; i < cn; i++ {
		out = append(out, m.commitLine(start+i, w))
	}

	out = append(out, m.sidebarHeader("Files", w))
	fh := h - cn - 2
	fstart := listStart(m.currentFile(), fh)
	for i := 0; i < fh; i++ {
		out = append(out, m.fileLine(fstart+i, w))
	}
	return out
}

// logSidebarAt maps a sidebar row to what is drawn there: a commit, a file, or
// neither. It mirrors renderLogSidebar so a click lands on what it points at.
func (m *Model) logSidebarAt(y int) (commit, file int) {
	h := m.bodyHeight()
	cn := m.logSplit(h)
	switch {
	case y == 0 || y == cn+1: // a header
	case y <= cn:
		if idx := listStart(m.commitIdx, cn) + y - 1; idx < len(m.commits) {
			return idx, -1
		}
	default:
		fh := h - cn - 2
		if idx := listStart(m.currentFile(), fh) + y - cn - 2; idx < len(m.files) {
			return -1, idx
		}
	}
	return -1, -1
}

// commitLine renders one row of the commit list, or a blank row past the end.
func (m *Model) commitLine(idx, w int) string {
	if idx < 0 || idx >= len(m.commits) {
		return m.st.sidebar.Render(strings.Repeat(" ", w))
	}
	c := m.commits[idx]
	style := m.st.sidebar
	if idx == m.commitIdx {
		style = m.st.sidebarSel
	}
	label := fmt.Sprintf(" %s  %s", c.Short, clip(c.Subject, w-lipgloss.Width(c.Short)-3))
	return fit(style.Render(label), 0, w, style)
}

// sidebarHeader titles one of the two lists, in the same border color as the
// rule that separates the sidebar from the diff.
func (m *Model) sidebarHeader(label string, w int) string {
	text := "── " + label + " "
	if pad := w - lipgloss.Width(text); pad > 0 {
		text += strings.Repeat("─", pad)
	}
	return fit(m.st.gutter.Render(text), 0, w, m.st.gutter)
}

// clip shortens text from the right, which is where a commit subject gets less
// informative. shortPath does the opposite for paths, where the tail names the
// file.
func clip(s string, w int) string { return ansi.Truncate(s, w, "…") }
