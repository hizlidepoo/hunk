package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	tea "charm.land/bubbletea/v2"
)

// editorDoneMsg arrives when the editor e opened has exited and hunk has the
// terminal back.
type editorDoneMsg struct{ err error }

// openEditor hands the terminal to the user's editor on the file under the
// cursor, at the line the cursor is on. Only the working tree is on disk as it
// is on screen, so history and plain diffs have nothing to open.
func (m *Model) openEditor() tea.Cmd {
	if !m.staging() || m.cur >= len(m.view.Rows) {
		return nil
	}
	f := m.files[m.currentFile()]
	if f.IsDelete {
		return m.toast(f.Path() + " was deleted — nothing to edit")
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		return m.toast("$EDITOR is not set — export EDITOR=vim (or your editor) to edit from hunk")
	}
	root, err := m.repo.Root()
	if err != nil {
		return m.toast("edit failed: " + firstLine(err.Error()))
	}
	cmd := editorCmd(editor, filepath.Join(root, f.Path()), m.editLine())
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return editorDoneMsg{err} })
}

// editorCmd runs editor on path at line. The editor goes through sh the same
// way git runs $EDITOR, so it can carry its own arguments ("code --wait").
//
// ponytail: +N is what vim, nvim, nano, emacs, micro and kakoune take. helix
// and VS Code want path:N instead; special-case them by name if anyone asks.
func editorCmd(editor, path string, line int) *exec.Cmd {
	return exec.Command("sh", "-c", editor+` "$@"`, editor, "+"+strconv.Itoa(line), path)
}

// editLine is the line of the new file the cursor points at. A removed line
// has no place in the new file, so it opens at the next line that does: where
// the change lands. A header opens at its first hunk the same way.
//
// ponytail: removed lines at the very end of a file, with nothing after them,
// open at line 1; fall back to the hunk's NewStart if that ever bothers anyone.
func (m *Model) editLine() int {
	file := m.view.Rows[m.cur].FileIdx
	for _, r := range m.view.Rows[m.cur:] {
		if r.FileIdx != file {
			break
		}
		if r.Right.Num > 0 {
			return r.Right.Num
		}
	}
	return 1
}
