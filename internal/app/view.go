package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/ui"
	"github.com/icichainz/sushi/internal/ui/components"
	"github.com/icichainz/sushi/internal/utils"
)

// View renders the application
func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	var view string
	switch m.mode {
	case ModeHelp:
		view = m.renderHelpView()
	case ModeConfirm:
		view = m.renderConfirmDialog()
	case ModeBookmarks:
		view = m.renderBookmarksView()
	default:
		view = m.renderMainView()
	}

	// Never exceed the terminal: Bubble Tea drops lines from the top of an
	// oversized frame, which would hide the header
	return lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height).Render(view)
}

// contentHeight returns the rows between the header/tab bar and the status bar
func (m Model) contentHeight() int {
	h := m.height - 2 // Header and status bar
	if len(m.tabs) > 1 {
		h-- // Tab bar
	}
	return max(h, 1)
}

// renderMainView renders the header, file list, preview and status bar
func (m Model) renderMainView() string {
	tab := m.tabs[m.activeTabIdx]
	var sections []string

	// Header with current path
	sections = append(sections, m.renderHeader())

	// Tab bar (only if multiple tabs)
	if len(m.tabs) > 1 {
		sections = append(sections, m.renderTabBar())
	}

	// Main content: file list + preview (if enabled)
	if tab.PreviewEnabled {
		sections = append(sections, m.renderSplitView())
	} else {
		sections = append(sections, m.renderFileList(m.width))
	}

	// Search bar (if in search mode)
	if m.mode == ModeSearch {
		sections = append(sections, m.renderSearchBar())
	} else {
		// Status bar
		sections = append(sections, m.renderStatusBar())
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderHeader renders the header with current path
func (m Model) renderHeader() string {
	tab := m.tabs[m.activeTabIdx]
	prefix := " 📁 "
	// Header padding takes two columns; keep the end of long paths visible
	path := utils.TruncateLeft(tab.CurrentPath, m.width-2-lipgloss.Width(prefix))
	return m.styles.Header.Width(m.width).Render(prefix + path)
}

// renderTabBar renders the tab bar
func (m Model) renderTabBar() string {
	var tabs []string

	for i, tab := range m.tabs {
		name := filepath.Base(tab.CurrentPath)
		if name == "" || name == "/" {
			name = "/"
		}

		// Truncate long names
		name = utils.Truncate(name, 15)

		tabLabel := fmt.Sprintf("%d:%s", i+1, name)

		if i == m.activeTabIdx {
			tabs = append(tabs, m.styles.TabActive.Render(tabLabel))
		} else {
			tabs = append(tabs, m.styles.TabInactive.Render(tabLabel))
		}
	}

	// Cut off tabs that don't fit rather than wrapping onto a second line
	tabContent := utils.Truncate(lipgloss.JoinHorizontal(lipgloss.Top, tabs...), m.width-2)
	return m.styles.TabBar.Width(m.width).Render(tabContent)
}

// renderSplitView renders the split pane layout (file list + preview)
func (m Model) renderSplitView() string {
	tab := m.tabs[m.activeTabIdx]
	// Calculate widths for split view (PreviewWidth is the preview's share)
	previewWidth := m.width * tab.PreviewWidth / 100
	listWidth := m.width - previewWidth

	// Render both panes
	fileListPane := m.renderFileList(listWidth)
	previewPane := m.renderPreview(previewWidth)

	// Join horizontally
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		fileListPane,
		previewPane,
	)
}

// renderFileList renders the list of files
func (m Model) renderFileList(width int) string {
	tab := m.tabs[m.activeTabIdx]
	height := m.contentHeight()

	if tab.Loading {
		return m.styles.EmptyDir.
			Width(width).
			Height(height).
			Render("⏳ Loading...")
	}

	if len(tab.Files) == 0 {
		return m.styles.EmptyDir.
			Width(width).
			Height(height).
			Render("Empty directory")
	}

	// Calculate visible range
	start := max(0, tab.Cursor-height/2)
	end := min(len(tab.Files), start+height)

	// Adjust start if we're near the end
	if end-start < height && start > 0 {
		start = max(0, end-height)
	}

	// Pre-allocate slice with exact capacity needed
	visibleCount := end - start
	lines := make([]string, 0, visibleCount)

	for i := start; i < end; i++ {
		file := tab.Files[i]
		isMatch := m.isSearchMatch(i)
		line := m.renderFileLine(file, i == tab.Cursor, isMatch, width)
		lines = append(lines, line)
	}

	listContent := strings.Join(lines, "\n")
	return m.styles.FileList.
		Width(width).
		Height(height).
		Render(listContent)
}

// renderFileLine renders a single file line
func (m Model) renderFileLine(file fs.FileInfo, isCursor bool, isMatch bool, width int) string {
	icon := ui.GetFileIcon(file)

	// Check if this file is in clipboard (cut mode shows strikethrough effect)
	isCutFile := m.clipboardMode == "cut" && m.clipboard == file.Path

	// Measure in terminal cells, not bytes, so accented and wide names
	// are neither split mid-character nor misaligned
	const metaWidth = 24 // "%10s" size + "  Jan 02 15:04"
	inner := width - 2   // File list padding
	nameWidth := max(inner-lipgloss.Width(icon)-2-metaWidth, 10)
	name := utils.Truncate(file.Name, nameWidth)
	name += strings.Repeat(" ", max(nameWidth-lipgloss.Width(name), 0))

	size := utils.HumanizeSize(file.Size)
	modTime := file.ModTime.Format("Jan 02 15:04")

	// Narrow panes lose the size and date columns first
	line := utils.Clip(fmt.Sprintf("%s  %s%10s  %s", icon, name, size, modTime), inner)

	// Apply styling
	style := m.styles.File
	if isCursor {
		style = m.styles.SelectedFile
	}
	if file.IsDir {
		style = style.Foreground(lipgloss.Color("12"))
	}

	// Dim non-matching files in search mode
	if m.mode == ModeSearch && !isMatch {
		style = style.Foreground(lipgloss.Color("240"))
	}

	// Visual indicator for cut files (dimmed with strikethrough effect)
	if isCutFile {
		style = style.Foreground(lipgloss.Color("243")).Italic(true)
	}

	return style.Render(line)
}

// isSearchMatch checks if file index is in search results (O(1) lookup using map)
func (m Model) isSearchMatch(index int) bool {
	tab := m.tabs[m.activeTabIdx]
	if m.mode != ModeSearch || tab.SearchMatchSet == nil {
		return true // Not in search mode, everything matches
	}
	_, exists := tab.SearchMatchSet[index]
	return exists
}

// renderPreview renders the preview pane
func (m Model) renderPreview(width int) string {
	tab := m.tabs[m.activeTabIdx]
	height := m.contentHeight()
	innerWidth := max(width-1, 1) // The left border takes one column

	// Create preview border style
	previewStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(lipgloss.Color("238"))

	// If no file selected
	if len(tab.Files) == 0 {
		emptyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Align(lipgloss.Center, lipgloss.Center).
			Width(innerWidth).
			Height(height)
		return previewStyle.Render(emptyStyle.Render("No file selected"))
	}

	return previewStyle.Render(components.RenderPreview(tab.Preview, innerWidth, height, m.styles.File))
}

// renderStatusBar renders the status bar
func (m Model) renderStatusBar() string {
	tab := m.tabs[m.activeTabIdx]

	// Left side: file count and size (using cached TotalSize)
	leftInfo := fmt.Sprintf(" %d files | %s", len(tab.Files), utils.HumanizeSize(tab.TotalSize))

	// Center: status message
	centerInfo := ""
	if m.statusMsg != "" {
		centerInfo = " " + m.statusMsg + " "
	}

	// Right side: cursor position, tab count, and preview status
	rightInfo := ""
	if len(tab.Files) > 0 {
		previewStatus := ""
		if tab.PreviewEnabled {
			previewStatus = "👁️ "
		}
		tabInfo := ""
		if len(m.tabs) > 1 {
			tabInfo = fmt.Sprintf("Tab %d/%d | ", m.activeTabIdx+1, len(m.tabs))
		}
		rightInfo = fmt.Sprintf("%s%s%d/%d ", tabInfo, previewStatus, tab.Cursor+1, len(tab.Files))
	}

	// Status bar padding takes two columns; the message gives way first
	avail := m.width - 2
	room := avail - lipgloss.Width(leftInfo) - lipgloss.Width(rightInfo)
	centerInfo = utils.Truncate(centerInfo, room)
	gap := max(room-lipgloss.Width(centerInfo), 0)
	line := leftInfo + centerInfo + strings.Repeat(" ", gap) + rightInfo

	return m.styles.StatusBar.
		Width(m.width).
		Render(utils.Clip(line, avail))
}

// renderHelpView renders the help screen
func (m Model) renderHelpView() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("212")).
		MarginBottom(1)

	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("229")).
		Bold(true).
		Width(15)

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	helpItems := []struct {
		key  string
		desc string
	}{
		{"↑/k", "Move cursor up"},
		{"↓/j", "Move cursor down"},
		{"PgUp/^u", "Page up"},
		{"PgDn/^d", "Page down"},
		{"g/Home", "Go to first file"},
		{"G/End", "Go to last file"},
		{"←/h", "Go to parent directory"},
		{"→/l", "Enter directory"},
		{"Enter", "Open/Enter directory"},
		{"Backspace", "Go back"},
		{"/", "Fuzzy search"},
		{"b", "Show bookmarks"},
		{"B", "Add bookmark"},
		{"1-9", "Quick jump to bookmark"},
		{"p", "Toggle preview pane"},
		{"t", "New tab (current dir)"},
		{"T", "New tab (home dir)"},
		{"Tab", "Next tab"},
		{"Shift+Tab", "Previous tab"},
		{"Ctrl+w", "Close tab"},
		{"d", "Delete file/directory"},
		{"c", "Copy to clipboard"},
		{"x", "Cut to clipboard"},
		{"v", "Paste from clipboard"},
		{"q", "Quit"},
		{"?", "Show this help"},
	}

	items := make([]string, len(helpItems))
	for i, item := range helpItems {
		items[i] = keyStyle.Render(item.key) + descStyle.Render(item.desc)
	}
	body := strings.Join(items, "\n")

	// Split into two columns when one would not fit the terminal height
	const chrome = 9 // Title, hint, blank lines, padding and border
	if len(items)+chrome > m.height {
		half := (len(items) + 1) / 2
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			strings.Join(items[:half], "\n"), "    ", strings.Join(items[half:], "\n"))
	}

	var lines []string
	lines = append(lines, titleStyle.Render("Sushi - Keyboard Shortcuts"))
	lines = append(lines, "")
	lines = append(lines, body)
	lines = append(lines, "")
	lines = append(lines, lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Italic(true).
		Render("Press any key to close"))

	content := strings.Join(lines, "\n")

	// Center the help box
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 3).
		Align(lipgloss.Left)

	box := boxStyle.Render(content)

	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		box,
	)
}

// renderConfirmDialog renders a confirmation dialog
func (m Model) renderConfirmDialog() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("196")).
		MarginBottom(1)

	messageStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Italic(true)

	var title, message string

	tab := m.tabs[m.activeTabIdx]
	switch m.confirmAction {
	case "delete":
		title = "Confirm Delete"
		if len(tab.Files) > 0 {
			file := tab.Files[tab.Cursor]
			switch {
			case file.IsSymlink:
				message = fmt.Sprintf("Delete symlink '%s'? Its target is not touched.", file.Name)
			case file.IsDir:
				message = fmt.Sprintf("Delete directory '%s' and all its contents?", file.Name)
			default:
				message = fmt.Sprintf("Delete file '%s'?", file.Name)
			}
		}
	case "paste":
		title = "Confirm Overwrite"
		message = fmt.Sprintf("'%s' already exists. Overwrite?", filepath.Base(m.clipboard))
	}

	var lines []string
	lines = append(lines, titleStyle.Render(title))
	lines = append(lines, "")
	lines = append(lines, messageStyle.Render(message))
	lines = append(lines, "")
	lines = append(lines, hintStyle.Render("(y) Yes  (n) No"))

	content := strings.Join(lines, "\n")

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("196")).
		Padding(1, 3).
		Align(lipgloss.Center)

	box := boxStyle.Render(content)

	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		box,
	)
}

// renderBookmarksView renders the bookmarks modal
func (m Model) renderBookmarksView() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("212")).
		MarginBottom(1)

	itemStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("236")).
		Bold(true)

	numStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Italic(true)

	var lines []string
	lines = append(lines, titleStyle.Render("Bookmarks"))
	lines = append(lines, "")

	if m.bookmarks.Len() == 0 {
		lines = append(lines, itemStyle.Render("No bookmarks yet"))
		lines = append(lines, "")
		lines = append(lines, hintStyle.Render("Press B to add current directory"))
	} else {
		for i := 0; i < m.bookmarks.Len(); i++ {
			bm := m.bookmarks.Get(i)
			num := numStyle.Render(fmt.Sprintf("%d. ", i+1))
			// Keep the end of long paths visible
			line := fmt.Sprintf("%s → %s", bm.Name, utils.TruncateLeft(bm.Path, 40))

			if i == m.bookmarkCursor {
				lines = append(lines, num+selectedStyle.Render(line))
			} else {
				lines = append(lines, num+itemStyle.Render(line))
			}
		}
		lines = append(lines, "")
		lines = append(lines, hintStyle.Render("Enter=Go  d=Delete  Esc=Close"))
	}

	content := strings.Join(lines, "\n")

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 3).
		Align(lipgloss.Left)

	box := boxStyle.Render(content)

	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		box,
	)
}

// renderSearchBar renders the search input bar
func (m Model) renderSearchBar() string {
	tab := m.tabs[m.activeTabIdx]

	searchStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("252")).
		Padding(0, 1).
		Width(m.width)

	promptStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("212")).
		Bold(true)

	queryStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("229"))

	matchStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	matchCount := fmt.Sprintf(" [%d/%d]", len(tab.SearchResults), len(tab.Files))

	// Keep the end of a long query (where the user is typing) visible
	queryWidth := m.width - 2 - 1 - 1 - lipgloss.Width(matchCount) // Padding, prompt, cursor
	prompt := promptStyle.Render("/")
	query := queryStyle.Render(utils.TruncateLeft(tab.SearchQuery, queryWidth))
	cursor := "█"
	matches := matchStyle.Render(matchCount)

	searchLine := prompt + query + cursor + matches

	return searchStyle.Render(utils.Clip(searchLine, m.width-2))
}

// Helper functions
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
