package app

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/ui/components"
)

// statusDuration is how long transient status messages stay visible
const statusDuration = 3 * time.Second

// statusTimer schedules clearing a status message; tests replace it so they
// don't wait on real timers
var statusTimer = tea.Tick

// update handles the messages of the browser itself; Update, in
// dispatch.go, routes the rest
func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyPress(msg)

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// A taller pane scrolls less far; every tab's pane is as tall
		for i := range m.tabs {
			tab := &m.tabs[i]
			tab.PreviewScroll = min(tab.PreviewScroll, tab.Preview.MaxScroll(m.previewRows()))
		}
		return m, nil

	case dirLoadedMsg:
		tab := m.tabByID(msg.tabID)
		// Drop results for closed tabs and for loads a newer one has superseded
		if tab == nil || msg.seq != tab.loadSeq {
			return m, nil
		}
		tab.Loading = false
		// The load read the directory before the order changed
		resort := tab.resortWanted
		tab.resortWanted = false
		if msg.err != nil {
			// Keep showing the previous directory rather than an empty
			// one. The file to focus was in the directory that failed, and
			// mustn't move the cursor at the next load of this one.
			tab.focusPath = ""
			cmd := tea.Batch(m.setStatus(fmt.Sprintf("Error: %v", msg.err)), m.reloadIfWanted(tab))
			return m, cmd
		}

		// Keep the cursor on the same file when reloading, and on the
		// directory we came from when going up
		samePath := msg.path == tab.CurrentPath
		focus := ""
		if tab.focusPath != "" {
			focus, tab.focusPath = tab.focusPath, ""
		} else if samePath && len(tab.Files) > 0 {
			focus = tab.Files[tab.Cursor].Path
		} else if filepath.Dir(tab.CurrentPath) == msg.path {
			focus = tab.CurrentPath
		}

		oldCursor := tab.Cursor
		tab.setFiles(msg.files)
		tab.ParentFiles = msg.parent
		tab.CurrentPath = msg.path
		if resort {
			fs.SortFiles(tab.Files, m.sortBy, m.sortReverse)
			fs.SortFiles(tab.ParentFiles, m.sortBy, m.sortReverse)
		}
		tab.pruneSelection()
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

		var prompt tea.Cmd
		if tab.ID == m.tab().ID {
			prompt = m.checkPrompt()
		}
		if m.mode == ModeSearch && tab.ID == m.tab().ID {
			m.updateSearchResults()
			m.cursorToMatch()
		}
		cmd := tea.Batch(m.refreshPreview(tab), m.reloadIfWanted(tab), prompt, m.gitAfterLoad(tab))
		return m, cmd

	case previewLoadedMsg:
		tab := m.tabByID(msg.tabID)
		// Drop previews that arrive after the cursor has moved on
		if tab != nil && len(tab.Files) > 0 && tab.Files[tab.Cursor].Path == msg.preview.Path {
			// A reload of the same file keeps its scroll position
			if tab.Preview.Path != msg.preview.Path {
				tab.PreviewScroll = 0
			}
			tab.Preview = msg.preview
			tab.PreviewScroll = min(tab.PreviewScroll, tab.Preview.MaxScroll(m.previewRows()))
		}
		return m, nil

	case fileOperationMsg:
		status := msg.message
		if msg.err != nil {
			status = fmt.Sprintf("Error: %v", msg.err)
		}
		if msg.operation == "cut" {
			// Whatever was moved has left the clipboard, even after a partial failure
			m.pruneClipboard()
		}

		// Any tab may be showing the source or destination, so reload them all
		cmd := tea.Batch(m.setStatus(status), m.reloadAll())
		return m, cmd

	case externalDoneMsg:
		return m.handleExternalDone(msg)

	case pluginDoneMsg:
		return m.handlePluginDone(msg)

	case selfApplying:
		// Background operations and file tools; see jobs.go and tools.go
		return msg.apply(m)

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
	return statusTimer(d, func(time.Time) tea.Msg {
		return clearStatusMsg{id: id}
	})
}

// loadDir starts loading path into tab, superseding any load already in
// flight. A load starting now sees every change so far, and lists files in
// the order chosen so far, so no reload or re-sort is wanted after it.
func (m *Model) loadDir(tab *Tab, path string) tea.Cmd {
	tab.Loading = true
	tab.reloadWanted = false
	tab.resortWanted = false
	tab.loadSeq++
	return loadDirectory(tab.ID, tab.loadSeq, path, m.scanOptions())
}

// reloadIfWanted reloads a tab whose load has just come in, once, if its
// directory changed or a refresh was asked for while it loaded
func (m *Model) reloadIfWanted(tab *Tab) tea.Cmd {
	if !tab.reloadWanted {
		return nil
	}
	return m.reloadTab(tab)
}

// maxPreviewLines is the most lines of a text file a preview reads, to
// show a search result far into it; a variable so tests can lower it
var maxPreviewLines = 20000

// previewCmd loads the preview for the file under the tab's cursor, if
// shown. A text preview reads its usual lines, or enough to show the line
// of a search result being opened, and on a reload as many as it had.
func (m *Model) previewCmd(tab *Tab) tea.Cmd {
	if !tab.PreviewEnabled || len(tab.Files) == 0 {
		return nil
	}
	file := tab.Files[tab.Cursor]
	cfg := previewConfig(m.theme.Syntax)
	if m.jumpingTo(tab, file.Path) {
		cfg.MaxLines = max(cfg.MaxLines, min(m.jump.line+m.previewRows(), maxPreviewLines))
	}
	if p := tab.Preview; p.Path == file.Path && p.IsText {
		cfg.MaxLines = max(cfg.MaxLines, len(p.Lines))
	}
	return loadPreviewWith(tab.ID, file, cfg)
}

// refreshPreview loads the preview again once a directory load is in,
// unless it already shows the file under the cursor as it is: the watcher
// reloads every couple of seconds while files change nearby, and each
// preview could run pdftotext, read an archive or highlight a file again
func (m *Model) refreshPreview(tab *Tab) tea.Cmd {
	if len(tab.Files) > 0 {
		file := tab.Files[tab.Cursor]
		if previewShows(tab.Preview, file) && !m.jumpingTo(tab, file.Path) {
			return nil
		}
	}
	return m.previewCmd(tab)
}

// jumpingTo reports whether a search result's line is waiting to be shown
// in the tab's preview of path
func (m *Model) jumpingTo(tab *Tab, path string) bool {
	return m.jump.line > 0 && m.jump.tabID == tab.ID && m.jump.path == path
}

// loadPreviewNow loads a preview synchronously using the theme's syntax
// style. Slow previews (images, archives, PDFs) are left Pending for Init
// to load, so they don't hold up startup.
func (m *Model) loadPreviewNow(file fs.FileInfo) components.PreviewContent {
	cfg := previewConfig(m.theme.Syntax)
	cfg.Quick = true
	return components.LoadPreviewWithConfig(file, cfg)
}

// previewConfig returns the preview settings for the given syntax style
func previewConfig(syntax string) components.PreviewConfig {
	cfg := components.DefaultPreviewConfig()
	cfg.SyntaxTheme = syntax
	return cfg
}

// handleKeyPress processes keyboard input
func (m Model) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// ctrl+c quits from every mode, prompts and dialogs included, whatever
	// the config binds. It is always a quit key (see loadKeyMap), so a
	// running job is stopped first, as with the quit key.
	if msg.String() == alwaysQuit {
		if m.job != nil {
			if model, cmd, handled := m.whileBusy(msg); handled {
				return model, cmd
			}
		}
		return m, tea.Quit
	}

	// The key panel: the down and up keys scroll it when it doesn't fit,
	// and esc and the help key close it. So does the quit key it shows (q),
	// as people press it to leave the panel, not sushi; ctrl+c, handled
	// above, still quits. Any other key closes it and does what the panel
	// says it does.
	if m.mode == ModeHelp {
		scrolls := m.maxHelpScroll() > 0
		quit := shownKey(m.keys.Quit) // Never ctrl+c while quit has another key
		switch s := msg.String(); {
		case s == "esc", key.Matches(msg, m.keys.Help), quit != "" && keyName(s) == quit:
			m.mode = ModeNormal
			m.helpScroll = 0
			return m, nil
		case scrolls && key.Matches(msg, m.keys.Down):
			m.helpScroll = min(m.helpScroll+1, m.maxHelpScroll())
			return m, nil
		case scrolls && key.Matches(msg, m.keys.Up):
			m.helpScroll = max(m.helpScroll-1, 0)
			return m, nil
		}
		m.mode = ModeNormal
		m.helpScroll = 0
		return m.handleKeyPress(msg)
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

	// Handle text prompts (rename, new file)
	if m.mode == ModeInput {
		return m.handleInputMode(msg)
	}

	// Handle the plugin menu
	if m.mode == ModePlugins {
		return m.handlePluginMode(msg)
	}

	// Handle the sort menu and the recursive search palette
	if m.mode == ModeSort {
		return m.handleSortMode(msg)
	}
	if m.mode == ModeFind {
		return m.handleFindMode(msg)
	}
	if m.mode == ModeOpenWith {
		return m.handleOpenWithMode(msg)
	}

	// Plugin shortcuts; bindPluginKeys keeps them clear of built-in keys
	if i, ok := m.pluginKeys[msg.String()]; ok {
		return m.runPlugin(m.plugins[i])
	}

	// While an operation runs, ctrl+x cancels it and changing files waits
	if m.job != nil {
		if model, cmd, handled := m.whileBusy(msg); handled {
			return model, cmd
		}
	}

	tab := &m.tabs[m.activeTabIdx]

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keys.QuitNoCd):
		m.keepShellDir = true
		return m, tea.Quit

	case key.Matches(msg, m.keys.Refresh):
		return m.refresh()

	case key.Matches(msg, m.keys.Sort):
		return m.openSortMenu()

	case key.Matches(msg, m.keys.Reverse):
		return m.reverseSort()

	case key.Matches(msg, m.keys.Find):
		return m.openFind(false)

	case key.Matches(msg, m.keys.Grep):
		return m.openFind(true)

	case key.Matches(msg, m.keys.Help):
		m.mode = ModeHelp
		return m, nil

	case key.Matches(msg, m.keys.Plugins):
		return m.openRun(false)

	case key.Matches(msg, m.keys.Shell):
		return m.openRun(true)

	case key.Matches(msg, m.keys.PreviewDown):
		tab.PreviewScroll = min(tab.PreviewScroll+m.previewStep(), tab.Preview.MaxScroll(m.previewRows()))
		return m, nil

	case key.Matches(msg, m.keys.PreviewUp):
		// From where the pane is actually scrolled to, which can be less
		// than the offset if the preview got shorter
		tab.PreviewScroll = max(min(tab.PreviewScroll, tab.Preview.MaxScroll(m.previewRows()))-m.previewStep(), 0)
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
		cmd := tea.Batch(m.setStatus(status), m.reloadAll())
		return m, cmd

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
			pageSize := m.listRows()
			tab.Cursor -= pageSize
			if tab.Cursor < 0 {
				tab.Cursor = 0
			}
			return m, m.previewCmd(tab)
		}

	case key.Matches(msg, m.keys.PageDown):
		if len(tab.Files) > 0 {
			pageSize := m.listRows()
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
		return m.openCursor()

	case key.Matches(msg, m.keys.Edit):
		return m.edit(m.targets())

	case key.Matches(msg, m.keys.Open):
		return m.openWithSystem(m.targets())

	case key.Matches(msg, m.keys.OpenWith):
		return m.startOpenWith()

	case key.Matches(msg, m.keys.Reveal):
		return m.reveal()

	case key.Matches(msg, m.keys.QuickLook):
		return m.quickLook(m.targets())

	case key.Matches(msg, m.keys.Left), key.Matches(msg, m.keys.Back):
		return m.goParent()

	case key.Matches(msg, m.keys.Delete):
		return m.startDelete()

	case key.Matches(msg, m.keys.Rename):
		return m.startRename()

	case key.Matches(msg, m.keys.NewFile):
		return m.openPrompt(prompt{action: promptNewFile, label: "New file:"})

	case key.Matches(msg, m.keys.NewDir):
		return m.openPrompt(prompt{action: promptNewDir, label: "New directory:"})

	case key.Matches(msg, m.keys.Select):
		return m.toggleSelection()

	case key.Matches(msg, m.keys.Invert):
		return m.invertSelection()

	case key.Matches(msg, m.keys.Unselect):
		return m.clearSelection()

	case key.Matches(msg, m.keys.Copy):
		return m.yank("copy")

	case key.Matches(msg, m.keys.Cut):
		return m.yank("cut")

	case key.Matches(msg, m.keys.Paste):
		return m.paste()

	case m.keys.isToolKey(msg):
		return m.handleToolKey(msg)

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

	// Handle number keys 1-9 for quick bookmark access, unless the config
	// has given the digit to an action
	if len(msg.Runes) == 1 && m.keys.actionFor(msg.String()) == "" {
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

// openCursor opens the file under the cursor, or enters the directory
func (m Model) openCursor() (tea.Model, tea.Cmd) {
	tab := m.tab()
	if len(tab.Files) == 0 {
		return m, nil
	}
	if file := tab.Files[tab.Cursor]; !file.IsDir {
		// An editor or app could change the files a job is working on
		if m.job != nil {
			cmd := m.stillBusy()
			return m, cmd
		}
		return m.openFile(file)
	}
	return m, m.loadDir(tab, tab.Files[tab.Cursor].Path)
}

// goParent goes up to the parent directory
func (m Model) goParent() (tea.Model, tea.Cmd) {
	tab := m.tab()
	parentPath := filepath.Dir(tab.CurrentPath)
	if parentPath != tab.CurrentPath {
		return m, m.loadDir(tab, parentPath)
	}
	cmd := m.setStatus("Already at root directory")
	return m, cmd
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
	switch {
	case msg.Type == tea.KeyEsc:
		m.mode = ModeNormal
		return m, nil

	case msg.Type == tea.KeyEnter:
		// Go to selected bookmark
		if bm := m.bookmarks.Get(m.bookmarkCursor); bm != nil {
			m.mode = ModeNormal
			return m, m.loadDir(m.tab(), bm.Path)
		}
		return m, nil

	// The same keys as in the file list move, so j/k work as well as the
	// arrows, and the delete key removes a bookmark
	case key.Matches(msg, m.keys.Up):
		m.bookmarkCursor = max(m.bookmarkCursor-1, 0)
		return m, nil

	case key.Matches(msg, m.keys.Down):
		m.bookmarkCursor = min(m.bookmarkCursor+1, max(m.bookmarks.Len()-1, 0))
		return m, nil

	case key.Matches(msg, m.keys.Delete):
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
	tab.SearchResultIdx = 0
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
			// A reload keeps the cursor, so ↑ and ↓ go on from where it is
			if i == tab.Cursor {
				tab.SearchResultIdx = len(tab.SearchResults)
			}
			tab.SearchResults = append(tab.SearchResults, i)
			tab.SearchMatchSet[i] = struct{}{}
		}
	}
}

// cursorToMatch puts the cursor on the nearest match when a reload has
// left it on a file the search hides, such as the one after a match that
// was deleted
func (m *Model) cursorToMatch() {
	tab := m.tab()
	res := tab.SearchResults
	if tab.SearchQuery == "" || len(res) == 0 {
		return
	}
	if _, ok := tab.SearchMatchSet[tab.Cursor]; ok {
		return
	}
	// Results are in list order: the first below the cursor, or the one
	// above if that is as near
	i, _ := slices.BinarySearch(res, tab.Cursor)
	if i == len(res) || i > 0 && tab.Cursor-res[i-1] <= res[i]-tab.Cursor {
		i--
	}
	tab.Cursor, tab.SearchResultIdx = res[i], i
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

// fuzzyPositions returns the character positions in target that a fuzzy
// match of query uses, or nil if it doesn't match
func fuzzyPositions(query, target string) map[int]bool {
	q := []rune(query)
	if len(q) == 0 {
		return nil
	}
	positions := make(map[int]bool, len(q))
	i := 0
	for pos, r := range []rune(target) {
		if r == q[i] {
			positions[pos] = true
			i++
			if i == len(q) {
				return positions
			}
		}
	}
	return nil
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

		switch m.confirmAction {
		case "delete":
			cmd := m.executeDelete()
			return m, cmd
		case "paste":
			cmd := m.confirmPaste()
			return m, cmd
		}

	case "n", "N", "esc", "q":
		m.mode = ModeNormal
		m.pending, m.pasteDir = nil, ""
		cmd := m.setStatus("Cancelled")
		return m, cmd
	}

	return m, nil
}

// dirLoadedMsg is sent when a directory has been loaded
type dirLoadedMsg struct {
	tabID  int
	seq    int
	path   string
	files  []fs.FileInfo
	parent []fs.FileInfo
	err    error
}

// scanParent lists the directory above path for the parent pane. It is
// extra context, so problems reading it just leave the pane empty.
func scanParent(path string, opts fs.ScanOptions) []fs.FileInfo {
	parent := filepath.Dir(path)
	if parent == path {
		return nil
	}
	files, err := fs.ScanDirectory(parent, opts)
	if err != nil {
		return nil
	}
	return files
}

// previewStep is how many lines J and K scroll the preview: half a pane
func (m Model) previewStep() int {
	return max((m.previewRows()-1)/2, 1)
}

// previewShows reports whether p is a preview of file as it is now: the
// same size, modification time and permissions (shown in the heading), and
// complete
func previewShows(p components.PreviewContent, file fs.FileInfo) bool {
	f := p.FileInfo
	return p.Path == file.Path && !p.Pending && p.Error == nil && f.Size == file.Size &&
		f.ModTime.Equal(file.ModTime) && f.Perms == file.Perms && f.IsDir == file.IsDir && f.IsSymlink == file.IsSymlink
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
		msg := dirLoadedMsg{tabID: tabID, seq: seq, path: path, files: files, err: err}
		if err == nil {
			msg.parent = scanParent(path, opts)
		}
		return msg
	}
}

// loadPreview loads preview content asynchronously
func loadPreview(tabID int, file fs.FileInfo, syntax string) tea.Cmd {
	return loadPreviewWith(tabID, file, previewConfig(syntax))
}

// loadPreviewWith loads preview content asynchronously with cfg
func loadPreviewWith(tabID int, file fs.FileInfo, cfg components.PreviewConfig) tea.Cmd {
	return func() tea.Msg {
		return previewLoadedMsg{
			tabID:   tabID,
			preview: components.LoadPreviewWithConfig(file, cfg),
		}
	}
}
