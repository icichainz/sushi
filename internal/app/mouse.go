package app

import (
	"path/filepath"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	// doubleClickTime is the longest gap between the clicks of a double-click
	doubleClickTime = 400 * time.Millisecond
	// wheelStep is how many rows one turn of the wheel moves
	wheelStep = 3
)

// clock tells the time of a click; tests replace it to control double-clicks
var clock = time.Now

// click is the last left click, kept to recognise a double-click
type click struct {
	at     time.Time
	area   area
	y      int
	target string // The file path, bookmark or plugin the click picked
}

// isDouble reports whether a click at row y of a picking target follows
// the last one closely enough to be a double-click. The list may have
// scrolled to center the first pick, so what counts is the same screen
// row and the same pick, not what is under the pointer now.
func (m Model) isDouble(a area, y int, target string) bool {
	last := m.lastClick
	return last.area == a && last.y == y && last.target == target && target != "" &&
		clock().Sub(last.at) <= doubleClickTime
}

// remember records a left click for isDouble
func (m *Model) remember(a area, y int, target string) {
	m.lastClick = click{at: clock(), area: a, y: y, target: target}
}

// enter does what the Enter key does in the current mode, so a
// double-click opens things exactly as the keyboard does
func (m Model) enter() (tea.Model, tea.Cmd) {
	return m.handleKeyPress(tea.KeyMsg{Type: tea.KeyEnter})
}

// wheelDelta returns how far a wheel event moves, or 0 for other events
func wheelDelta(msg tea.MouseMsg, step int) int {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return -step
	case tea.MouseButtonWheelDown:
		return step
	}
	return 0
}

// handleMouse handles clicks and the wheel. Only presses count: releases
// and drags do nothing.
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if !m.config.Mouse || msg.Action != tea.MouseActionPress {
		return m, nil
	}
	switch m.mode {
	case ModeNormal, ModeSearch:
		return m.mouseBrowse(msg)
	case ModeBookmarks:
		return m.mouseBookmarks(msg)
	case ModePlugins:
		return m.mouseRun(msg)
	case ModeHelp:
		return m.mouseHelp(msg)
	}
	// Prompts, confirmations and anything else ignore the mouse, so a stray
	// click can't change what they act on
	return m, nil
}

// mouseBrowse handles the mouse over the panes and the tab bar
func (m Model) mouseBrowse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	s := m.spotAt(msg.X, msg.Y)
	tab := m.tab()

	if delta := wheelDelta(msg, wheelStep); delta != 0 {
		switch s.area {
		case areaList:
			return m.moveCursorBy(delta)
		case areaPreview:
			tab.PreviewScroll = max(min(tab.PreviewScroll+delta, tab.Preview.MaxScroll(m.previewRows())), 0)
		}
		return m, nil
	}

	// Right-click, or Ctrl-click where the terminal reports it, selects
	toggle := msg.Button == tea.MouseButtonRight || (msg.Button == tea.MouseButtonLeft && msg.Ctrl)
	if toggle {
		if s.area != areaList || s.index < 0 {
			return m, nil
		}
		path := tab.Files[s.index].Path
		if tab.Selected[path] {
			delete(tab.Selected, path)
		} else {
			tab.Selected[path] = true
		}
		cmd := m.moveCursorTo(s.index)
		return m, cmd
	}
	if msg.Button != tea.MouseButtonLeft {
		return m, nil
	}

	switch s.area {
	case areaList:
		if s.index < 0 {
			return m, nil
		}
		picked := tab.Files[tab.Cursor].Path
		if m.isDouble(areaList, msg.Y, picked) {
			m.lastClick = click{}
			if m.mode == ModeSearch {
				// Enter keeps the match and ends the search, then opens it
				updated, _ := m.enter()
				m = updated.(Model)
			}
			return m.enter()
		}
		cmd := m.moveCursorTo(s.index)
		m.remember(areaList, msg.Y, tab.Files[s.index].Path)
		return m, cmd

	case areaParent:
		// While searching, keys only move between matches, and so does the mouse
		if m.mode == ModeSearch {
			return m, nil
		}
		return m.clickParent(s)

	case areaTabs:
		if m.mode == ModeNormal && s.index >= 0 {
			m.activeTabIdx = s.index
		}
	}
	return m, nil
}

// moveCursorTo puts the cursor on file index i and loads its preview
func (m *Model) moveCursorTo(i int) tea.Cmd {
	tab := m.tab()
	if i == tab.Cursor {
		return nil
	}
	tab.Cursor = i
	// Up and down in a search step through the matches from here
	for pos, idx := range tab.SearchResults {
		if idx == i {
			tab.SearchResultIdx = pos
		}
	}
	return m.previewCmd(tab)
}

// moveCursorBy moves the cursor delta rows through the files the list
// shows, stopping at either end
func (m Model) moveCursorBy(delta int) (tea.Model, tea.Cmd) {
	visible := m.visibleFiles()
	if len(visible) == 0 {
		return m, nil
	}
	pos := 0
	for i, idx := range visible {
		if idx == m.tab().Cursor {
			pos = i
		}
	}
	pos = max(min(pos+delta, len(visible)-1), 0)
	cmd := m.moveCursorTo(visible[pos])
	return m, cmd
}

// clickParent goes where a click in the parent pane points: its heading
// goes up, a directory is entered, and a file is shown in its directory
func (m Model) clickParent(s spot) (tea.Model, tea.Cmd) {
	tab := m.tab()
	if s.row < 0 {
		return m.handleKeyPress(tea.KeyMsg{Type: tea.KeyLeft})
	}
	if s.index < 0 {
		return m, nil
	}
	f := tab.ParentFiles[s.index]
	switch {
	case f.Path == tab.CurrentPath:
		return m, nil
	case f.IsDir:
		return m, m.loadDir(tab, f.Path)
	}
	tab.focusPath = f.Path
	return m, m.loadDir(tab, filepath.Dir(f.Path))
}

// mouseBookmarks handles the mouse over the bookmark list: a click picks a
// bookmark and a double-click goes there
func (m Model) mouseBookmarks(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if delta := wheelDelta(msg, 1); delta != 0 {
		m.bookmarkCursor = max(min(m.bookmarkCursor+delta, m.bookmarks.Len()-1), 0)
		return m, nil
	}
	if msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	r, inside := m.dialogRowAt(m.bookmarksBox(), msg.X, msg.Y)
	if !inside {
		// Clicking outside closes the list, as Esc does
		m.mode = ModeNormal
		return m, nil
	}
	row := r - dialogBodyRow
	if row < 0 || row >= m.bookmarks.Len() {
		return m, nil
	}
	target := strconv.Itoa(row)
	if m.isDouble(areaDialog, msg.Y, target) && m.bookmarkCursor == row {
		m.lastClick = click{}
		return m.enter()
	}
	m.bookmarkCursor = row
	m.remember(areaDialog, msg.Y, target)
	return m, nil
}

// mouseRun handles the mouse over the Run palette: a click on the command
// line starts typing, a click on a plugin picks it, and a double-click
// runs it
func (m Model) mouseRun(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if delta := wheelDelta(msg, 1); delta != 0 {
		if !m.runTyping {
			m.pluginCursor = max(min(m.pluginCursor+delta, len(m.plugins)-1), 0)
		}
		return m, nil
	}
	if msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	r, inside := m.dialogRowAt(m.runBox(), msg.X, msg.Y)
	if !inside {
		m.mode = ModeNormal
		return m, nil
	}
	// The body is the command line, a rule, then the plugins
	row := r - dialogBodyRow
	if row == 0 {
		m.runTyping = true
		return m, nil
	}
	i := row - 2
	if i < 0 || i >= len(m.plugins) {
		return m, nil
	}
	target := strconv.Itoa(i)
	if m.isDouble(areaDialog, msg.Y, target) && !m.runTyping && m.pluginCursor == i {
		m.lastClick = click{}
		return m.enter()
	}
	m.runTyping = false
	m.pluginCursor = i
	m.remember(areaDialog, msg.Y, target)
	return m, nil
}

// mouseHelp handles the mouse over the key panel: the wheel scrolls it and
// a click closes it
func (m Model) mouseHelp(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if delta := wheelDelta(msg, 1); delta != 0 {
		m.helpScroll = max(min(m.helpScroll+delta, m.maxHelpScroll()), 0)
		return m, nil
	}
	if msg.Button == tea.MouseButtonLeft {
		return m.handleKeyPress(tea.KeyMsg{Type: tea.KeyEsc})
	}
	return m, nil
}
