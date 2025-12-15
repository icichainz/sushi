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

	// Show help view if in help mode
	if m.mode == ModeHelp {
		return m.renderHelpView()
	}

	// Show confirmation dialog if in confirm mode
	if m.mode == ModeConfirm {
		return m.renderConfirmDialog()
	}

	// Show bookmarks view if in bookmarks mode
	if m.mode == ModeBookmarks {
		return m.renderBookmarksView()
	}

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
	pathStyle := m.styles.Header.Width(m.width)
	return pathStyle.Render(fmt.Sprintf(" 📁 %s", tab.CurrentPath))
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
		if len(name) > 15 {
			name = name[:12] + "..."
		}

		tabLabel := fmt.Sprintf("%d:%s", i+1, name)

		if i == m.activeTabIdx {
			tabs = append(tabs, m.styles.TabActive.Render(tabLabel))
		} else {
			tabs = append(tabs, m.styles.TabInactive.Render(tabLabel))
		}
	}

	tabContent := lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
	return m.styles.TabBar.Width(m.width).Render(tabContent)
}

// renderSplitView renders the split pane layout (file list + preview)
func (m Model) renderSplitView() string {
	tab := m.tabs[m.activeTabIdx]
	// Calculate widths for split view
	listWidth := m.width * tab.PreviewWidth / 100
	previewWidth := m.width - listWidth

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

	// Account for tab bar in height calculation
	heightOffset := 4
	if len(m.tabs) > 1 {
		heightOffset = 5
	}

	if tab.Loading {
		return m.styles.EmptyDir.
			Width(width).
			Height(m.height - heightOffset).
			Render("⏳ Loading...")
	}

	if len(tab.Files) == 0 {
		return m.styles.EmptyDir.
			Width(width).
			Height(m.height - heightOffset).
			Render("Empty directory")
	}

	// Calculate visible range
	height := m.height - heightOffset // Account for header, tab bar, and status bar
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
	name := file.Name

	// Check if this file is in clipboard (cut mode shows strikethrough effect)
	isCutFile := m.clipboardMode == "cut" && m.clipboard == file.Path

	// Truncate name if too long
	maxNameLen := width - 30 // Leave room for size and date
	if maxNameLen < 10 {
		maxNameLen = 10
	}
	if len(name) > maxNameLen {
		name = name[:maxNameLen-3] + "..."
	}

	size := utils.HumanizeSize(file.Size)
	modTime := file.ModTime.Format("Jan 02 15:04")

	// Build the line with proper spacing
	namePart := fmt.Sprintf("%s  %-*s", icon, maxNameLen, name)
	sizePart := fmt.Sprintf("%10s", size)
	timePart := fmt.Sprintf("  %s", modTime)

	line := namePart + sizePart + timePart

	// Ensure line doesn't exceed width
	if len(line) > width-2 {
		line = line[:width-2]
	}

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

	// Account for tab bar in height calculation
	heightOffset := 4
	if len(m.tabs) > 1 {
		heightOffset = 5
	}
	height := m.height - heightOffset

	// Create preview border style
	previewStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(lipgloss.Color("238")).
		Width(width).
		Height(height)

	// If no file selected
	if len(tab.Files) == 0 {
		emptyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Align(lipgloss.Center, lipgloss.Center).
			Width(width - 2).
			Height(height - 2)
		return previewStyle.Render(emptyStyle.Render("No file selected"))
	}

	// Render preview content
	previewContent := components.RenderPreview(
		tab.Preview,
		width-4, // Account for border and padding
		height-2,
		m.styles.File,
	)

	return previewStyle.Render(previewContent)
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
	} else if m.err != nil {
		centerInfo = fmt.Sprintf(" Error: %v ", m.err)
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

	// Build status bar using strings.Builder for efficient concatenation
	leftWidth := lipgloss.Width(leftInfo)
	centerWidth := lipgloss.Width(centerInfo)
	rightWidth := lipgloss.Width(rightInfo)
	totalContent := leftWidth + centerWidth + rightWidth

	var sb strings.Builder
	sb.Grow(m.width) // Pre-allocate capacity

	if totalContent > m.width {
		// Terminal too small, truncate center info or show minimal
		if m.width < 40 {
			// Very small terminal, just show position
			sb.WriteString(rightInfo)
		} else {
			// Truncate center info
			available := m.width - leftWidth - rightWidth - 2
			if available > 0 && len(centerInfo) > available {
				centerInfo = centerInfo[:available-3] + "..."
			} else if available <= 0 {
				centerInfo = ""
			}
			gap := m.width - leftWidth - lipgloss.Width(centerInfo) - rightWidth
			if gap < 0 {
				gap = 0
			}
			sb.WriteString(leftInfo)
			sb.WriteString(centerInfo)
			for i := 0; i < gap; i++ {
				sb.WriteByte(' ')
			}
			sb.WriteString(rightInfo)
		}
	} else {
		gap := m.width - totalContent
		sb.WriteString(leftInfo)
		sb.WriteString(centerInfo)
		for i := 0; i < gap; i++ {
			sb.WriteByte(' ')
		}
		sb.WriteString(rightInfo)
	}

	return m.styles.StatusBar.
		Width(m.width).
		Render(sb.String())
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

	var lines []string
	lines = append(lines, titleStyle.Render("Sushi - Keyboard Shortcuts"))
	lines = append(lines, "")

	for _, item := range helpItems {
		line := keyStyle.Render(item.key) + descStyle.Render(item.desc)
		lines = append(lines, line)
	}

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
			if file.IsDir {
				message = fmt.Sprintf("Delete directory '%s' and all its contents?", file.Name)
			} else {
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
			name := bm.Name
			path := bm.Path

			// Truncate path if too long
			maxLen := 40
			if len(path) > maxLen {
				path = "..." + path[len(path)-maxLen+3:]
			}

			line := fmt.Sprintf("%s → %s", name, path)

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

	prompt := promptStyle.Render("/")
	query := queryStyle.Render(tab.SearchQuery)
	cursor := "█"

	matchCount := fmt.Sprintf(" [%d/%d]", len(tab.SearchResults), len(tab.Files))
	matches := matchStyle.Render(matchCount)

	searchLine := prompt + query + cursor + matches

	return searchStyle.Render(searchLine)
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
