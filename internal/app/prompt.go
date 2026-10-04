package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/ui/components"
)

// promptAction is what a submitted prompt does
type promptAction int

const (
	promptRename promptAction = iota
	promptNewFile
	promptNewDir
	promptTool // Permissions and archive names; the prompt's submit does the work
)

// prompt is the text input shown in place of the status bar
type prompt struct {
	action promptAction
	label  string
	input  components.TextInput
	target string // File being renamed
	dir    string // Directory shown when the prompt opened, where new files go
	err    string // Shown next to the input until the text changes

	// For promptTool: the mode's name in the status bar, and what Enter does
	badge  string
	submit func(m Model, value string) (tea.Model, tea.Cmd)
}

// openPrompt switches to input mode. The directory shown now is kept, as
// the list can move on while the name is typed: a watcher reload moves up
// from a directory that was deleted.
func (m Model) openPrompt(p prompt) (tea.Model, tea.Cmd) {
	if p.dir == "" {
		p.dir = m.tab().CurrentPath
	}
	m.prompt = p
	m.mode = ModeInput
	return m, nil
}

// promptGone says why the open prompt can no longer do what it was opened
// for, or returns "": the file being renamed isn't listed any more, or the
// directory for a new file has gone or is no longer the one shown
func (m *Model) promptGone() string {
	p := m.prompt
	switch p.action {
	case promptRename:
		for _, f := range m.tab().Files {
			if f.Path == p.target {
				return ""
			}
		}
		return filepath.Base(p.target) + " is gone, so it wasn't renamed"
	case promptNewFile, promptNewDir:
		if info, err := os.Stat(p.dir); err != nil || !info.IsDir() {
			return filepath.Base(p.dir) + " is gone, so nothing was created"
		}
		if m.tab().CurrentPath != p.dir {
			return "The folder shown changed, so nothing was created"
		}
	}
	return ""
}

// checkPrompt closes a rename or new-file prompt, with a message, once the
// active tab's list shows that what it was opened on has gone, and keeps
// the cursor, and so the rename field, on the file being renamed
func (m *Model) checkPrompt() tea.Cmd {
	if m.mode != ModeInput || m.prompt.submit != nil {
		return nil
	}
	if gone := m.promptGone(); gone != "" {
		m.mode = ModeNormal
		return m.setStatus(gone)
	}
	if m.prompt.action == promptRename {
		tab := m.tab()
		for i, f := range tab.Files {
			if f.Path == m.prompt.target {
				tab.Cursor = i
			}
		}
	}
	return nil
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
	dir := m.prompt.dir
	if m.prompt.submit != nil {
		return m.prompt.submit(m, value)
	}
	// Not into whatever the list shows now, such as the parent of a
	// directory deleted meanwhile
	if m.prompt.action != promptRename {
		if gone := m.promptGone(); gone != "" {
			m.mode = ModeNormal
			cmd := m.setStatus(gone)
			return m, cmd
		}
	}

	var path, status string
	var undo *undoEntry
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
			undo = renameUndo(old, path)
		}
	case promptNewFile:
		created := createdRoot(dir, value)
		// A trailing separator asks for a directory, as in "build/"
		if strings.HasSuffix(value, "/") || strings.HasSuffix(value, string(filepath.Separator)) {
			path, err = fs.CreateDir(dir, value)
		} else {
			path, err = fs.CreateFile(dir, value)
		}
		status = "Created " + value
		undo = createUndo("create "+value, created)
	case promptNewDir:
		created := createdRoot(dir, value)
		path, err = fs.CreateDir(dir, value)
		status = "Created " + value
		undo = createUndo("create "+value, created)
	}

	if err != nil {
		m.prompt.err = err.Error()
		return m, nil
	}

	m.mode = ModeNormal
	m.pushUndo(undo)
	m.tab().focusPath = topLevelEntry(dir, path)
	cmd := tea.Batch(m.setStatus(status), m.reloadAll())
	return m, cmd
}

// createdRoot returns the first part of name, a new entry in dir, that
// doesn't exist yet: what undoing its creation removes. Creating
// "src/main.go" creates src too if it is new.
func createdRoot(dir, name string) string {
	path := dir
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r < utf8.RuneSelf && (r == '/' || os.IsPathSeparator(uint8(r)))
	})
	for _, part := range parts {
		path = filepath.Join(path, part)
		if !fs.Exists(path) {
			break
		}
	}
	return path
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
	// A job finishing may move what an open prompt is about
	m.prompt.target = move(m.prompt.target)
	m.prompt.dir = move(m.prompt.dir)
	for _, tab := range m.panes() {
		tab.CurrentPath = move(tab.CurrentPath)
		tab.otherDir = move(tab.otherDir)
		tab.nav.retarget(move)
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

// reloadAll reloads every tab, since any of them may show what changed:
// a folder deleted, the nearest one above it that is still there. That
// is said after what the status bar says, as what the operation did.
func (m *Model) reloadAll() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(m.tabs)+1)
	note := ""
	for _, tab := range m.panes() {
		load, gone := m.reloadOrUp(tab)
		cmds = append(cmds, load)
		if note == "" {
			note = gone
		}
	}
	if note != "" {
		if m.statusMsg != "" {
			note = m.statusMsg + ". " + note
		}
		cmds = append(cmds, m.setStatus(note))
	}
	return tea.Batch(cmds...)
}
