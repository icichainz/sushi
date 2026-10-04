package app

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/opener"
)

// externalDoneMsg is sent when an editor, opener or plugin finishes
type externalDoneMsg struct {
	label  string // What ran, for the status message
	err    error
	reload bool // Whether files may have changed
	exec   bool // It had the terminal, which sushi has back; see dispatch.go
}

// openFile opens a file as the opener setting says: "editor", "system", or
// "auto", which edits text files and hands everything else to the system
func (m Model) openFile(file fs.FileInfo) (tea.Model, tea.Cmd) {
	if m.useEditor(file) {
		return m.edit([]string{file.Path})
	}
	return m.openWithSystem([]string{file.Path})
}

// useEditor reports whether Enter should open file in the editor
func (m Model) useEditor(file fs.FileInfo) bool {
	switch m.config.Opener {
	case "editor":
		return true
	case "system":
		return false
	}
	binary, err := fs.IsBinary(file.Path)
	return err == nil && !binary
}

// edit opens paths in the editor, suspending sushi until it exits
func (m Model) edit(paths []string) (tea.Model, tea.Cmd) {
	if len(paths) == 0 {
		return m, nil
	}
	cmd := tea.ExecProcess(opener.EditorCommand(paths...), func(err error) tea.Msg {
		// The editor may have changed or created files
		return externalDoneMsg{label: "Editor", err: err, reload: true, exec: true}
	})
	return m, cmd
}

// openWithSystem opens paths with their default applications, without
// leaving sushi
func (m Model) openWithSystem(paths []string) (tea.Model, tea.Cmd) {
	if len(paths) == 0 {
		return m, nil
	}
	status := m.setStatus("Opening " + describe(paths))
	run := func() tea.Msg {
		for _, path := range paths {
			// Output is captured so it can't draw over the interface
			if out, err := opener.SystemCommand(path).CombinedOutput(); err != nil {
				return externalDoneMsg{label: "Open " + filepath.Base(path), err: withOutput(err, out)}
			}
		}
		return externalDoneMsg{label: "Open"}
	}
	return m, tea.Batch(status, run)
}

// withOutput adds the last line a command printed to its error
func withOutput(err error, out []byte) error {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if last := strings.TrimSpace(lines[len(lines)-1]); last != "" {
		return fmt.Errorf("%w: %s", err, last)
	}
	return err
}

// handleExternalDone reports how an editor, opener or plugin finished
func (m Model) handleExternalDone(msg externalDoneMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	if msg.err != nil {
		cmds = append(cmds, m.setStatus(fmt.Sprintf("%s failed: %v", msg.label, msg.err)))
	}
	if msg.reload {
		cmds = append(cmds, m.reloadAll())
	}
	cmd := tea.Batch(cmds...)
	return m, cmd
}
