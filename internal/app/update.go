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
		tab := &m.tabs[m.activeTabIdx]
		tab.Files = msg.files
		tab.CurrentPath = msg.path
		tab.Cursor = 0
		m.err = msg.err
		tab.Loading = false

		// Cache total size (calculated once, not on every render)
		tab.TotalSize = 0
		for _, f := range tab.Files {
			tab.TotalSize += f.Size
		}

		// Load preview for first file
		if len(tab.Files) > 0 && tab.PreviewEnabled {
			return m, loadPreview(tab.Files[0])
		}
		return m, nil

	case previewLoadedMsg:
		m.tabs[m.activeTabIdx].Preview = msg.preview
		return m, nil

	case fileOperationMsg:
		m.tabs[m.activeTabIdx].Loading = false
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Error: %v", msg.err)
		} else {
			m.statusMsg = msg.message
			// Clear clipboard after successful cut operation
			if msg.operation == "cut" {
				m.clipboard = ""
				m.clipboardMode = ""
			}
		}
		// Reload directory after file operation, and clear status after 3 seconds
		return m, tea.Batch(
			loadDirectory(m.tabs[m.activeTabIdx].CurrentPath),
			clearStatusAfter(3*time.Second),
		)

	case clearStatusMsg:
		m.statusMsg = ""
		return m, nil
	}

	return m, nil
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
		m.statusMsg = "Preview toggled"
		return m, nil

	case key.Matches(msg, m.keys.Up):
		if tab.Cursor > 0 {
			tab.Cursor--
			if len(tab.Files) > 0 && tab.PreviewEnabled {
				return m, loadPreview(tab.Files[tab.Cursor])
			}
		}

	case key.Matches(msg, m.keys.Down):
		if tab.Cursor < len(tab.Files)-1 {
			tab.Cursor++
			if len(tab.Files) > 0 && tab.PreviewEnabled {
				return m, loadPreview(tab.Files[tab.Cursor])
			}
		}

	case key.Matches(msg, m.keys.PageUp):
		if len(tab.Files) > 0 {
			pageSize := m.height - 6 // Approximate visible items
			if pageSize < 1 {
				pageSize = 1
			}
			tab.Cursor -= pageSize
			if tab.Cursor < 0 {
				tab.Cursor = 0
			}
			if tab.PreviewEnabled {
				return m, loadPreview(tab.Files[tab.Cursor])
			}
		}

	case key.Matches(msg, m.keys.PageDown):
		if len(tab.Files) > 0 {
			pageSize := m.height - 6 // Approximate visible items
			if pageSize < 1 {
				pageSize = 1
			}
			tab.Cursor += pageSize
			if tab.Cursor >= len(tab.Files) {
				tab.Cursor = len(tab.Files) - 1
			}
			if tab.PreviewEnabled {
				return m, loadPreview(tab.Files[tab.Cursor])
			}
		}

	case key.Matches(msg, m.keys.Home):
		if len(tab.Files) > 0 && tab.Cursor != 0 {
			tab.Cursor = 0
			if tab.PreviewEnabled {
				return m, loadPreview(tab.Files[tab.Cursor])
			}
		}

	case key.Matches(msg, m.keys.End):
		if len(tab.Files) > 0 && tab.Cursor != len(tab.Files)-1 {
			tab.Cursor = len(tab.Files) - 1
			if tab.PreviewEnabled {
				return m, loadPreview(tab.Files[tab.Cursor])
			}
		}

	case key.Matches(msg, m.keys.Right), key.Matches(msg, m.keys.Enter):
		if len(tab.Files) > 0 && tab.Files[tab.Cursor].IsDir {
			newPath := tab.Files[tab.Cursor].Path
			tab.Loading = true
			return m, loadDirectory(newPath)
		}

	case key.Matches(msg, m.keys.Left), key.Matches(msg, m.keys.Back):
		parentPath := filepath.Dir(tab.CurrentPath)
		if parentPath != tab.CurrentPath {
			tab.Loading = true
			return m, loadDirectory(parentPath)
		} else {
			m.statusMsg = "Already at root directory"
			return m, clearStatusAfter(2 * time.Second)
		}

	case key.Matches(msg, m.keys.Delete):
		if len(tab.Files) > 0 {
			m.confirmAction = "delete"
			m.mode = ModeConfirm
			return m, nil
		}

	case key.Matches(msg, m.keys.Copy):
		if len(tab.Files) > 0 {
			m.clipboard = tab.Files[tab.Cursor].Path
			m.clipboardMode = "copy"
			m.statusMsg = fmt.Sprintf("Copied: %s", tab.Files[tab.Cursor].Name)
			return m, clearStatusAfter(3 * time.Second)
		}

	case key.Matches(msg, m.keys.Cut):
		if len(tab.Files) > 0 {
			m.clipboard = tab.Files[tab.Cursor].Path
			m.clipboardMode = "cut"
			m.statusMsg = fmt.Sprintf("Cut: %s", tab.Files[tab.Cursor].Name)
			return m, clearStatusAfter(3 * time.Second)
		}

	case key.Matches(msg, m.keys.Paste):
		if m.clipboard != "" {
			destPath := filepath.Join(tab.CurrentPath, filepath.Base(m.clipboard))
			// Check if destination exists
			if fs.Exists(destPath) {
				m.confirmAction = "paste"
				m.mode = ModeConfirm
				return m, nil
			}
			// No confirmation needed, paste directly
			tab.Loading = true
			return m, m.executePaste()
		} else {
			m.statusMsg = "Nothing in clipboard"
			return m, clearStatusAfter(2 * time.Second)
		}

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
			m.statusMsg = fmt.Sprintf("Error: %v", err)
		} else {
			m.statusMsg = fmt.Sprintf("Bookmarked: %s", tab.CurrentPath)
		}
		return m, clearStatusAfter(3 * time.Second)

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
			m.statusMsg = fmt.Sprintf("Tab %d/%d", m.activeTabIdx+1, len(m.tabs))
		}
		return m, nil

	case key.Matches(msg, m.keys.PrevTab):
		if len(m.tabs) > 1 {
			m.activeTabIdx = (m.activeTabIdx - 1 + len(m.tabs)) % len(m.tabs)
			m.statusMsg = fmt.Sprintf("Tab %d/%d", m.activeTabIdx+1, len(m.tabs))
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
				tab.Loading = true
				return m, loadDirectory(bm.Path)
			}
		}
	}

	return m, nil
}

// createTab creates a new tab at the specified path
func (m Model) createTab(path string) (tea.Model, tea.Cmd) {
	newTab := Tab{
		CurrentPath:    path,
		Files:          []fs.FileInfo{},
		Cursor:         0,
		Selected:       make(map[string]bool),
		PreviewEnabled: true,
		PreviewWidth:   50,
		Loading:        true,
	}
	m.tabs = append(m.tabs, newTab)
	m.activeTabIdx = len(m.tabs) - 1
	m.statusMsg = fmt.Sprintf("New tab %d", len(m.tabs))
	return m, loadDirectory(path)
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

	m.statusMsg = fmt.Sprintf("Tab closed. %d remaining", len(m.tabs))
	return m, nil
}

// handleBookmarkMode handles key presses in bookmark mode
func (m Model) handleBookmarkMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.mode = ModeNormal
		return m, nil

	case tea.KeyEnter:
		// Go to selected bookmark
		if bm := m.bookmarks.Get(m.bookmarkCursor); bm != nil {
			m.mode = ModeNormal
			m.tabs[m.activeTabIdx].Loading = true
			return m, loadDirectory(bm.Path)
		}
		return m, nil

	case tea.KeyUp:
		if m.bookmarkCursor > 0 {
			m.bookmarkCursor--
		}
		return m, nil

	case tea.KeyDown:
		if m.bookmarkCursor < m.bookmarks.Len()-1 {
			m.bookmarkCursor++
		}
		return m, nil

	case tea.KeyRunes:
		if len(msg.Runes) == 1 && msg.Runes[0] == 'd' {
			// Delete selected bookmark
			if err := m.bookmarks.Remove(m.bookmarkCursor); err != nil {
				m.statusMsg = fmt.Sprintf("Error: %v", err)
			} else {
				m.statusMsg = "Bookmark removed"
				// Adjust cursor if needed
				if m.bookmarkCursor >= m.bookmarks.Len() && m.bookmarkCursor > 0 {
					m.bookmarkCursor--
				}
			}
			// Exit if no bookmarks left
			if m.bookmarks.Len() == 0 {
				m.mode = ModeNormal
			}
			return m, nil
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
		// Remove last character from query
		if len(tab.SearchQuery) > 0 {
			tab.SearchQuery = tab.SearchQuery[:len(tab.SearchQuery)-1]
			m.updateSearchResults()
			// Reset cursor to first match
			if len(tab.SearchResults) > 0 {
				tab.Cursor = tab.SearchResults[0]
			}
		}
		return m, nil

	case tea.KeyUp:
		// Navigate to previous match
		m.navigateSearchResults(-1)
		if tab.PreviewEnabled && len(tab.Files) > 0 {
			return m, loadPreview(tab.Files[tab.Cursor])
		}
		return m, nil

	case tea.KeyDown:
		// Navigate to next match
		m.navigateSearchResults(1)
		if tab.PreviewEnabled && len(tab.Files) > 0 {
			return m, loadPreview(tab.Files[tab.Cursor])
		}
		return m, nil

	case tea.KeyRunes:
		// Add typed character to query
		tab.SearchQuery += string(msg.Runes)
		m.updateSearchResults()
		// Move cursor to first match
		if len(tab.SearchResults) > 0 {
			tab.Cursor = tab.SearchResults[0]
			if tab.PreviewEnabled {
				return m, loadPreview(tab.Files[tab.Cursor])
			}
		}
		return m, nil
	}

	return m, nil
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

// fuzzyMatch checks if query characters appear in target in order
func fuzzyMatch(query, target string) bool {
	if query == "" {
		return true
	}

	queryIdx := 0
	for i := 0; i < len(target) && queryIdx < len(query); i++ {
		if target[i] == query[queryIdx] {
			queryIdx++
		}
	}

	return queryIdx == len(query)
}

// handleConfirmMode handles key presses in confirmation mode
func (m Model) handleConfirmMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		m.mode = ModeNormal
		m.tabs[m.activeTabIdx].Loading = true

		switch m.confirmAction {
		case "delete":
			return m, m.executeDelete()
		case "paste":
			return m, m.executePaste()
		}

	case "n", "N", "esc", "q":
		m.mode = ModeNormal
		m.statusMsg = "Cancelled"
		return m, nil
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
	path  string
	files []fs.FileInfo
	err   error
}

// previewLoadedMsg is sent when preview content has been loaded
type previewLoadedMsg struct {
	preview components.PreviewContent
}

// fileOperationMsg is sent when a file operation completes
type fileOperationMsg struct {
	operation string
	message   string
	err       error
}

// clearStatusMsg is sent to clear the status message after a timeout
type clearStatusMsg struct{}

// clearStatusAfter returns a command that clears the status after a delay
func clearStatusAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return clearStatusMsg{}
	})
}

// loadDirectory loads files from a directory asynchronously
func loadDirectory(path string) tea.Cmd {
	return func() tea.Msg {
		files, err := fs.ScanDirectory(path)
		return dirLoadedMsg{
			path:  path,
			files: files,
			err:   err,
		}
	}
}

// loadPreview loads preview content asynchronously
func loadPreview(file fs.FileInfo) tea.Cmd {
	return func() tea.Msg {
		preview := components.LoadPreview(file, 100)
		return previewLoadedMsg{
			preview: preview,
		}
	}
}
