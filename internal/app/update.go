package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/ui/components"
)

// statusDuration is how long transient status messages stay visible
const statusDuration = 3 * time.Second

// Update handles all state updates
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyPress(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case dirLoadedMsg:
		tab := m.tabByID(msg.tabID)
		// Drop results for closed tabs and for loads a newer one has superseded
		if tab == nil || msg.seq != tab.loadSeq {
			return m, nil
		}
		tab.Loading = false
		if msg.err != nil {
			// Keep showing the previous directory rather than an empty one
			cmd := m.setStatus(fmt.Sprintf("Error: %v", msg.err))
			return m, cmd
		}

		// Keep the cursor on the same file when reloading, and on the
		// directory we came from when going up
		samePath := msg.path == tab.CurrentPath
		focus := ""
		if samePath && len(tab.Files) > 0 {
			focus = tab.Files[tab.Cursor].Path
		} else if filepath.Dir(tab.CurrentPath) == msg.path {
			focus = tab.CurrentPath
		}

		oldCursor := tab.Cursor
		tab.setFiles(msg.files)
		tab.CurrentPath = msg.path
		tab.Cursor = 0
		if samePath {
			tab.Cursor = min(oldCursor, max(len(tab.Files)-1, 0))
		}
		for i, f := range tab.Files {
			if f.Path == focus {
				tab.Cursor = i
				break
			}
		}

		if m.mode == ModeSearch && tab.ID == m.tab().ID {
			m.updateSearchResults()
		}
		return m, m.previewCmd(tab)

	case previewLoadedMsg:
		tab := m.tabByID(msg.tabID)
		// Drop previews that arrive after the cursor has moved on
		if tab != nil && len(tab.Files) > 0 && tab.Files[tab.Cursor].Path == msg.preview.Path {
			tab.Preview = msg.preview
		}
		return m, nil

	case fileOperationMsg:
		status := msg.message
		if msg.err != nil {
			status = fmt.Sprintf("Error: %v", msg.err)
		} else if msg.operation == "cut" {
			// Clear clipboard after successful cut operation
			m.clipboard = ""
			m.clipboardMode = ""
		}

		// Any tab may be showing the source or destination, so reload them all
		cmds := []tea.Cmd{m.setStatus(status)}
		for i := range m.tabs {
			cmds = append(cmds, m.loadDir(&m.tabs[i], m.tabs[i].CurrentPath))
		}
		return m, tea.Batch(cmds...)

	case clearStatusMsg:
		if msg.id == m.statusID {
			m.statusMsg = ""
		}
		return m, nil
	}

	return m, nil
}

// setStatus shows a status message and schedules it to clear. It changes m,
// so call it before "return m, ...": Go doesn't specify whether m is read
// before or after other calls in the same return statement.
func (m *Model) setStatus(msg string) tea.Cmd {
	return m.setStatusFor(msg, statusDuration)
}

// setStatusFor shows a status message for the given duration
func (m *Model) setStatusFor(msg string, d time.Duration) tea.Cmd {
	m.statusID++
	m.statusMsg = msg
	id := m.statusID
	return tea.Tick(d, func(time.Time) tea.Msg {
		return clearStatusMsg{id: id}
	})
}

// loadDir starts loading path into tab, superseding any load already in flight
func (m *Model) loadDir(tab *Tab, path string) tea.Cmd {
	tab.Loading = true
	tab.loadSeq++
	return loadDirectory(tab.ID, tab.loadSeq, path, m.scanOptions())
}

// previewCmd loads the preview for the file under the tab's cursor, if shown
func (m *Model) previewCmd(tab *Tab) tea.Cmd {
	if !tab.PreviewEnabled || len(tab.Files) == 0 {
		return nil
	}
	return loadPreview(tab.ID, tab.Files[tab.Cursor], m.theme.Syntax)
}

// loadPreviewNow loads a preview synchronously using the theme's syntax style
func (m *Model) loadPreviewNow(file fs.FileInfo) components.PreviewContent {
	return components.LoadPreviewWithConfig(file, previewConfig(m.theme.Syntax))
}

// previewConfig returns the preview settings for the given syntax style
func previewConfig(syntax string) components.PreviewConfig {
	cfg := components.DefaultPreviewConfig()
	cfg.SyntaxTheme = syntax
	return cfg
}

// handleKeyPress processes keyboard input
func (m Model) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle help mode separately
	if m.mode == ModeHelp {
		// Any key exits help mode
		m.mode = ModeNormal
		return m, nil
	}

	// Handle confirmation mode
	if m.mode == ModeConfirm {
		return m.handleConfirmMode(msg)
	}

	// Handle search mode
	if m.mode == ModeSearch {
		return m.handleSearchMode(msg)
	}

	// Handle bookmarks mode
	if m.mode == ModeBookmarks {
		return m.handleBookmarkMode(msg)
	}

	tab := &m.tabs[m.activeTabIdx]

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keys.Help):
		m.mode = ModeHelp
		return m, nil

	case key.Matches(msg, m.keys.Preview):
		tab.PreviewEnabled = !tab.PreviewEnabled
		if !tab.PreviewEnabled {
			cmd := m.setStatus("Preview off")
			return m, cmd
		}
		// Previews aren't loaded while hidden, so fetch the current file's now
		tab.Preview = components.PreviewContent{}
		cmd := tea.Batch(m.previewCmd(tab), m.setStatus("Preview on"))
		return m, cmd

	case key.Matches(msg, m.keys.Hidden):
		m.showHidden = !m.showHidden
		status := "Hidden files hidden"
		if m.showHidden {
			status = "Hidden files shown"
		}
		// The setting is global, so refresh every tab
		cmds := []tea.Cmd{m.setStatus(status)}
		for i := range m.tabs {
			cmds = append(cmds, m.loadDir(&m.tabs[i], m.tabs[i].CurrentPath))
		}
		return m, tea.Batch(cmds...)

	case key.Matches(msg, m.keys.Up):
		if tab.Cursor > 0 {
			tab.Cursor--
			return m, m.previewCmd(tab)
		}

	case key.Matches(msg, m.keys.Down):
		if tab.Cursor < len(tab.Files)-1 {
			tab.Cursor++
			return m, m.previewCmd(tab)
		}

	case key.Matches(msg, m.keys.PageUp):
		if len(tab.Files) > 0 {
			pageSize := m.contentHeight()
			tab.Cursor -= pageSize
			if tab.Cursor < 0 {
				tab.Cursor = 0
			}
			return m, m.previewCmd(tab)
		}

	case key.Matches(msg, m.keys.PageDown):
		if len(tab.Files) > 0 {
			pageSize := m.contentHeight()
			tab.Cursor += pageSize
			if tab.Cursor >= len(tab.Files) {
				tab.Cursor = len(tab.Files) - 1
			}
			return m, m.previewCmd(tab)
		}

	case key.Matches(msg, m.keys.Home):
		if len(tab.Files) > 0 && tab.Cursor != 0 {
			tab.Cursor = 0
			return m, m.previewCmd(tab)
		}

	case key.Matches(msg, m.keys.End):
		if len(tab.Files) > 0 && tab.Cursor != len(tab.Files)-1 {
			tab.Cursor = len(tab.Files) - 1
			return m, m.previewCmd(tab)
		}

	case key.Matches(msg, m.keys.Right), key.Matches(msg, m.keys.Enter):
		if len(tab.Files) > 0 && tab.Files[tab.Cursor].IsDir {
			return m, m.loadDir(tab, tab.Files[tab.Cursor].Path)
		}

	case key.Matches(msg, m.keys.Left), key.Matches(msg, m.keys.Back):
		parentPath := filepath.Dir(tab.CurrentPath)
		if parentPath != tab.CurrentPath {
			return m, m.loadDir(tab, parentPath)
		}
		cmd := m.setStatus("Already at root directory")
		return m, cmd

	case key.Matches(msg, m.keys.Delete):
		if len(tab.Files) > 0 {
			if !m.config.ConfirmDelete {
				tab.Loading = true
				cmd := m.executeDelete()
				return m, cmd
			}
			m.confirmAction = "delete"
			m.mode = ModeConfirm
			return m, nil
		}

	case key.Matches(msg, m.keys.Copy):
		if len(tab.Files) > 0 {
			m.clipboard = tab.Files[tab.Cursor].Path
			m.clipboardMode = "copy"
			cmd := m.setStatus(fmt.Sprintf("Copied: %s", tab.Files[tab.Cursor].Name))
			return m, cmd
		}

	case key.Matches(msg, m.keys.Cut):
		if len(tab.Files) > 0 {
			m.clipboard = tab.Files[tab.Cursor].Path
			m.clipboardMode = "cut"
			cmd := m.setStatus(fmt.Sprintf("Cut: %s", tab.Files[tab.Cursor].Name))
			return m, cmd
		}

	case key.Matches(msg, m.keys.Paste):
		if m.clipboard != "" {
			destPath := filepath.Join(tab.CurrentPath, filepath.Base(m.clipboard))
			// Refuse up front rather than offering to overwrite the source with itself
			if err := fs.CheckTransfer(m.clipboard, destPath); err != nil {
				cmd := m.setStatus(fmt.Sprintf("Can't paste: %v", err))
				return m, cmd
			}
			// Check if destination exists
			if fs.Exists(destPath) {
				m.confirmAction = "paste"
				m.mode = ModeConfirm
				return m, nil
			}
			// No confirmation needed, paste directly
			tab.Loading = true
			cmd := m.executePaste()
			return m, cmd
		}
		cmd := m.setStatus("Nothing in clipboard")
		return m, cmd

	case key.Matches(msg, m.keys.Search):
		m.mode = ModeSearch
		tab.SearchQuery = ""
		m.updateSearchResults()
		return m, nil

	case key.Matches(msg, m.keys.Bookmark):
		m.mode = ModeBookmarks
		m.bookmarkCursor = 0
		return m, nil

	case key.Matches(msg, m.keys.AddBookmark):
		// Add current directory to bookmarks
		name := filepath.Base(tab.CurrentPath)
		if name == "" || name == "/" {
			name = "Root"
		}
		if err := m.bookmarks.Add(name, tab.CurrentPath); err != nil {
			cmd := m.setStatus(fmt.Sprintf("Error: %v", err))
			return m, cmd
		}
		cmd := m.setStatus(fmt.Sprintf("Bookmarked: %s", tab.CurrentPath))
		return m, cmd

	// Tab management
	case key.Matches(msg, m.keys.NewTab):
		return m.createTab(tab.CurrentPath)

	case key.Matches(msg, m.keys.NewTabHome):
		home, err := os.UserHomeDir()
		if err != nil {
			home = "/"
		}
		return m.createTab(home)

	case key.Matches(msg, m.keys.NextTab):
		if len(m.tabs) > 1 {
			m.activeTabIdx = (m.activeTabIdx + 1) % len(m.tabs)
			cmd := m.setStatus(fmt.Sprintf("Tab %d/%d", m.activeTabIdx+1, len(m.tabs)))
			return m, cmd
		}
		return m, nil

	case key.Matches(msg, m.keys.PrevTab):
		if len(m.tabs) > 1 {
			m.activeTabIdx = (m.activeTabIdx - 1 + len(m.tabs)) % len(m.tabs)
			cmd := m.setStatus(fmt.Sprintf("Tab %d/%d", m.activeTabIdx+1, len(m.tabs)))
			return m, cmd
		}
		return m, nil

	case key.Matches(msg, m.keys.CloseTab):
		return m.closeTab()
	}

	// Handle number keys 1-9 for quick bookmark access
	if len(msg.Runes) == 1 {
		r := msg.Runes[0]
		if r >= '1' && r <= '9' {
			idx := int(r - '1')
			if bm := m.bookmarks.Get(idx); bm != nil {
				return m, m.loadDir(tab, bm.Path)
			}
		}
	}

	return m, nil
}

// createTab creates a new tab at the specified path
func (m Model) createTab(path string) (tea.Model, tea.Cmd) {
	m.tabs = append(m.tabs, m.newTab(path))
	m.activeTabIdx = len(m.tabs) - 1
	loadCmd := m.loadDir(m.tab(), path)
	cmd := tea.Batch(loadCmd, m.setStatus(fmt.Sprintf("New tab %d", len(m.tabs))))
	return m, cmd
}

// closeTab closes the current tab
func (m Model) closeTab() (tea.Model, tea.Cmd) {
	if len(m.tabs) == 1 {
		// Last tab, quit the application
		return m, tea.Quit
	}

	// Remove current tab
	m.tabs = append(m.tabs[:m.activeTabIdx], m.tabs[m.activeTabIdx+1:]...)

	// Adjust active tab index
	if m.activeTabIdx >= len(m.tabs) {
		m.activeTabIdx = len(m.tabs) - 1
	}

	cmd := m.setStatus(fmt.Sprintf("Tab closed. %d remaining", len(m.tabs)))
	return m, cmd
}

// handleBookmarkMode handles key presses in bookmark mode
func (m Model) handleBookmarkMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Same movement keys as the file list, so j/k work as well as arrows
	if key.Matches(msg, m.keys.Up) {
		m.bookmarkCursor = max(m.bookmarkCursor-1, 0)
		return m, nil
	}
	if key.Matches(msg, m.keys.Down) {
		m.bookmarkCursor = min(m.bookmarkCursor+1, max(m.bookmarks.Len()-1, 0))
		return m, nil
	}

	switch msg.Type {
	case tea.KeyEsc:
		m.mode = ModeNormal
		return m, nil

	case tea.KeyEnter:
		// Go to selected bookmark
		if bm := m.bookmarks.Get(m.bookmarkCursor); bm != nil {
			m.mode = ModeNormal
			return m, m.loadDir(m.tab(), bm.Path)
		}
		return m, nil

	case tea.KeyRunes:
		if len(msg.Runes) == 1 && msg.Runes[0] == 'd' {
			// Delete selected bookmark
			var cmd tea.Cmd
			if err := m.bookmarks.Remove(m.bookmarkCursor); err != nil {
				cmd = m.setStatus(fmt.Sprintf("Error: %v", err))
			} else {
				cmd = m.setStatus("Bookmark removed")
				// Adjust cursor if needed
				if m.bookmarkCursor >= m.bookmarks.Len() && m.bookmarkCursor > 0 {
					m.bookmarkCursor--
				}
			}
			// Exit if no bookmarks left
			if m.bookmarks.Len() == 0 {
				m.mode = ModeNormal
			}
			return m, cmd
		}
	}

	return m, nil
}

// handleSearchMode handles key presses in search mode
func (m Model) handleSearchMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	tab := &m.tabs[m.activeTabIdx]

	switch msg.Type {
	case tea.KeyEsc:
		// Exit search mode, clear filter
		m.mode = ModeNormal
		tab.SearchQuery = ""
		tab.SearchResults = nil
		return m, nil

	case tea.KeyEnter:
		// Select current result and exit search
		m.mode = ModeNormal
		if len(tab.SearchResults) > 0 {
			// Keep cursor on selected file, clear search
			tab.SearchQuery = ""
			tab.SearchResults = nil
		}
		return m, nil

	case tea.KeyBackspace:
		// Remove last character (not byte, which would split accented letters)
		if query := []rune(tab.SearchQuery); len(query) > 0 {
			tab.SearchQuery = string(query[:len(query)-1])
			return m, m.jumpToFirstMatch()
		}
		return m, nil

	case tea.KeyUp:
		// Navigate to previous match
		m.navigateSearchResults(-1)
		return m, m.previewCmd(tab)

	case tea.KeyDown:
		// Navigate to next match
		m.navigateSearchResults(1)
		return m, m.previewCmd(tab)

	case tea.KeyRunes, tea.KeySpace:
		// Add typed character to query (space arrives as its own key type)
		tab.SearchQuery += string(msg.Runes)
		return m, m.jumpToFirstMatch()
	}

	return m, nil
}

// jumpToFirstMatch refreshes the search results and moves to the first match
func (m *Model) jumpToFirstMatch() tea.Cmd {
	m.updateSearchResults()
	tab := m.tab()
	if len(tab.SearchResults) == 0 {
		return nil
	}
	tab.Cursor = tab.SearchResults[0]
	return m.previewCmd(tab)
}

// updateSearchResults updates the search results based on current query
func (m *Model) updateSearchResults() {
	tab := &m.tabs[m.activeTabIdx]
	tab.SearchResults = nil
	tab.SearchMatchSet = make(map[int]struct{})
	tab.SearchResultIdx = 0 // Reset tracked index

	if tab.SearchQuery == "" {
		// No query, show all files - populate both slice and set
		tab.SearchResults = make([]int, len(tab.Files))
		for i := range tab.Files {
			tab.SearchResults[i] = i
			tab.SearchMatchSet[i] = struct{}{}
		}
		return
	}

	query := strings.ToLower(tab.SearchQuery)
	for i, file := range tab.Files {
		if fuzzyMatch(query, strings.ToLower(file.Name)) {
			tab.SearchResults = append(tab.SearchResults, i)
			tab.SearchMatchSet[i] = struct{}{}
		}
	}
}

// navigateSearchResults moves cursor through search results (O(1) using tracked index)
func (m *Model) navigateSearchResults(direction int) {
	tab := &m.tabs[m.activeTabIdx]

	if len(tab.SearchResults) == 0 {
		return
	}

	// Use tracked index for O(1) navigation
	newIdx := tab.SearchResultIdx + direction
	if newIdx < 0 {
		newIdx = len(tab.SearchResults) - 1
	} else if newIdx >= len(tab.SearchResults) {
		newIdx = 0
	}

	tab.SearchResultIdx = newIdx
	tab.Cursor = tab.SearchResults[newIdx]
}

// fuzzyMatch checks if query characters appear in target in order.
// It compares runes, not bytes, so accented letters only match themselves.
func fuzzyMatch(query, target string) bool {
	q := []rune(query)
	if len(q) == 0 {
		return true
	}

	i := 0
	for _, r := range target {
		if r == q[i] {
			i++
			if i == len(q) {
				return true
			}
		}
	}
	return false
}

// handleConfirmMode handles key presses in confirmation mode
func (m Model) handleConfirmMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		m.mode = ModeNormal
		m.tabs[m.activeTabIdx].Loading = true

		switch m.confirmAction {
		case "delete":
			cmd := m.executeDelete()
			return m, cmd
		case "paste":
			cmd := m.executePaste()
			return m, cmd
		}

	case "n", "N", "esc", "q":
		m.mode = ModeNormal
		cmd := m.setStatus("Cancelled")
		return m, cmd
	}

	return m, nil
}

// executeDelete performs the delete operation
func (m Model) executeDelete() tea.Cmd {
	tab := m.tabs[m.activeTabIdx]
	if tab.Cursor >= len(tab.Files) {
		return nil
	}
	file := tab.Files[tab.Cursor]
	return func() tea.Msg {
		err := fs.DeletePath(file.Path)
		if err != nil {
			return fileOperationMsg{
				operation: "delete",
				err:       err,
			}
		}
		return fileOperationMsg{
			operation: "delete",
			message:   fmt.Sprintf("Deleted: %s", file.Name),
		}
	}
}

// executePaste performs the copy or move operation
func (m Model) executePaste() tea.Cmd {
	tab := m.tabs[m.activeTabIdx]
	src := m.clipboard
	dst := filepath.Join(tab.CurrentPath, filepath.Base(src))
	mode := m.clipboardMode

	return func() tea.Msg {
		var err error
		var msg string

		if mode == "cut" {
			err = fs.MovePath(src, dst)
			msg = fmt.Sprintf("Moved: %s", filepath.Base(src))
		} else {
			err = fs.CopyPath(src, dst)
			msg = fmt.Sprintf("Copied: %s", filepath.Base(src))
		}

		if err != nil {
			return fileOperationMsg{
				operation: mode,
				err:       err,
			}
		}
		return fileOperationMsg{
			operation: mode,
			message:   msg,
		}
	}
}

// dirLoadedMsg is sent when a directory has been loaded
type dirLoadedMsg struct {
	tabID int
	seq   int
	path  string
	files []fs.FileInfo
	err   error
}

// previewLoadedMsg is sent when preview content has been loaded
type previewLoadedMsg struct {
	tabID   int
	preview components.PreviewContent
}

// fileOperationMsg is sent when a file operation completes
type fileOperationMsg struct {
	operation string
	message   string
	err       error
}

// clearStatusMsg is sent to clear the status message after a timeout
type clearStatusMsg struct {
	id int
}

// loadDirectory loads files from a directory asynchronously
func loadDirectory(tabID, seq int, path string, opts fs.ScanOptions) tea.Cmd {
	return func() tea.Msg {
		files, err := fs.ScanDirectory(path, opts)
		return dirLoadedMsg{
			tabID: tabID,
			seq:   seq,
			path:  path,
			files: files,
			err:   err,
		}
	}
}

// loadPreview loads preview content asynchronously
func loadPreview(tabID int, file fs.FileInfo, syntax string) tea.Cmd {
	return func() tea.Msg {
		return previewLoadedMsg{
			tabID:   tabID,
			preview: components.LoadPreviewWithConfig(file, previewConfig(syntax)),
		}
	}
}
