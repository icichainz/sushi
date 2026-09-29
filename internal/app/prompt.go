package app

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/plugins"
	"github.com/icichainz/sushi/internal/ui/components"
)

// promptAction is what a submitted prompt does
type promptAction int

const (
	promptRename promptAction = iota
	promptNewFile
	promptNewDir
	promptShell
)

// prompt is the text input shown in place of the status bar
type prompt struct {
	action promptAction
	label  string
	input  components.TextInput
	target string // File being renamed
	err    string // Shown next to the input until the text changes
}

// openPrompt switches to input mode
func (m Model) openPrompt(p prompt) (tea.Model, tea.Cmd) {
	m.prompt = p
	m.mode = ModeInput
	return m, nil
}

// startRename prompts for a new name for the file under the cursor
func (m Model) startRename() (tea.Model, tea.Cmd) {
	tab := m.tab()
	if len(tab.Files) == 0 {
		return m, nil
	}
	file := tab.Files[tab.Cursor]
	input := components.NewTextInput(file.Name)
	// Put the cursor before the extension, where edits usually go
	if ext := filepath.Ext(file.Name); !file.IsDir && ext != "" && ext != file.Name {
		input.SetCursor(len([]rune(strings.TrimSuffix(file.Name, ext))))
	}
	return m.openPrompt(prompt{action: promptRename, label: "Rename:", input: input, target: file.Path})
}

// handleInputMode handles key presses while a prompt is open
func (m Model) handleInputMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.mode = ModeNormal
		return m, nil
	case tea.KeyEnter:
		return m.submitPrompt()
	}
	if m.prompt.input.Update(msg) {
		m.prompt.err = ""
	}
	return m, nil
}

// submitPrompt carries out the prompt's action. On failure the prompt stays
// open with the error, so the name can be corrected.
func (m Model) submitPrompt() (tea.Model, tea.Cmd) {
	value := m.prompt.input.Value()
	dir := m.tab().CurrentPath

	// A shell command is an unnamed plugin: it gets the same arguments and
	// environment, and waits so its output can be read
	if m.prompt.action == promptShell {
		m.mode = ModeNormal
		if strings.TrimSpace(value) == "" {
			return m, nil
		}
		return m.runPlugin(plugins.Plugin{Name: "shell", Command: value, Mode: plugins.ModeWait})
	}

	var path, status string
	var err error
	switch m.prompt.action {
	case promptRename:
		old := m.prompt.target
		if path, err = fs.Rename(old, value); err == nil {
			if path == old {
				m.mode = ModeNormal
				return m, nil
			}
			m.retarget(old, path)
			status = fmt.Sprintf("Renamed %s → %s", filepath.Base(old), filepath.Base(path))
		}
	case promptNewFile:
		// A trailing separator asks for a directory, as in "build/"
		if strings.HasSuffix(value, "/") || strings.HasSuffix(value, string(filepath.Separator)) {
			path, err = fs.CreateDir(dir, value)
		} else {
			path, err = fs.CreateFile(dir, value)
		}
		status = "Created " + value
	case promptNewDir:
		path, err = fs.CreateDir(dir, value)
		status = "Created " + value
	}

	if err != nil {
		m.prompt.err = err.Error()
		return m, nil
	}

	m.mode = ModeNormal
	m.tab().focusPath = topLevelEntry(dir, path)
	cmd := tea.Batch(m.setStatus(status), m.reloadAll())
	return m, cmd
}

// topLevelEntry returns the entry of dir that contains path, so creating
// "src/main.go" focuses "src"
func topLevelEntry(dir, path string) string {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return path
	}
	return filepath.Join(dir, strings.Split(rel, string(filepath.Separator))[0])
}

// retarget updates everything that refers to oldPath, or to something
// inside it, after a rename: the clipboard, selections, tabs and bookmarks
func (m *Model) retarget(oldPath, newPath string) {
	move := func(p string) string {
		if p == oldPath {
			return newPath
		}
		if rest, ok := strings.CutPrefix(p, oldPath+string(filepath.Separator)); ok {
			return filepath.Join(newPath, rest)
		}
		return p
	}

	for i, p := range m.clipboard {
		m.clipboard[i] = move(p)
	}
	for i := range m.tabs {
		tab := &m.tabs[i]
		tab.CurrentPath = move(tab.CurrentPath)
		for p := range tab.Selected {
			if moved := move(p); moved != p {
				delete(tab.Selected, p)
				tab.Selected[moved] = true
			}
		}
	}

	changed := false
	for i := range m.bookmarks.Bookmarks {
		bm := &m.bookmarks.Bookmarks[i]
		if moved := move(bm.Path); moved != bm.Path {
			bm.Path = moved
			changed = true
		}
	}
	if changed {
		m.bookmarks.Save()
	}
}

// reloadAll reloads every tab, since any of them may show what changed
func (m *Model) reloadAll() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(m.tabs))
	for i := range m.tabs {
		cmds = append(cmds, m.loadDir(&m.tabs[i], m.tabs[i].CurrentPath))
	}
	return tea.Batch(cmds...)
}
