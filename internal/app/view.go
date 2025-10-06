package app

import (
	"fmt"
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

	var sections []string

	// Header with current path
	sections = append(sections, m.renderHeader())

	// Main content: file list + preview (if enabled)
	if m.previewEnabled {
		sections = append(sections, m.renderSplitView())
	} else {
		sections = append(sections, m.renderFileList(m.width))
	}

	// Status bar
	sections = append(sections, m.renderStatusBar())

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderHeader renders the header with current path
func (m Model) renderHeader() string {
	pathStyle := m.styles.Header.Width(m.width)
	return pathStyle.Render(fmt.Sprintf(" 📁 %s", m.currentPath))
}

// renderSplitView renders the split pane layout (file list + preview)
func (m Model) renderSplitView() string {
	// Calculate widths for split view
	listWidth := m.width * m.previewWidth / 100
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
	if len(m.files) == 0 {
		return m.styles.EmptyDir.
			Width(width).
			Height(m.height - 4).
			Render("Empty directory")
	}

	var lines []string

	// Calculate visible range
	height := m.height - 4 // Account for header and status bar
	start := max(0, m.cursor-height/2)
	end := min(len(m.files), start+height)

	// Adjust start if we're near the end
	if end-start < height && start > 0 {
		start = max(0, end-height)
	}

	for i := start; i < end; i++ {
		file := m.files[i]
		line := m.renderFileLine(file, i == m.cursor, width)
		lines = append(lines, line)
	}

	listContent := strings.Join(lines, "\n")
	return m.styles.FileList.
		Width(width).
		Height(height).
		Render(listContent)
}

// renderFileLine renders a single file line
func (m Model) renderFileLine(file fs.FileInfo, isCursor bool, width int) string {
	icon := ui.GetFileIcon(file)
	name := file.Name
	
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

	return style.Render(line)
}

// renderPreview renders the preview pane
func (m Model) renderPreview(width int) string {
	height := m.height - 4

	// Create preview border style
	previewStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(lipgloss.Color("238")).
		Width(width).
		Height(height)

	// If no file selected
	if len(m.files) == 0 {
		emptyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Align(lipgloss.Center, lipgloss.Center).
			Width(width - 2).
			Height(height - 2)
		return previewStyle.Render(emptyStyle.Render("No file selected"))
	}

	// Render preview content
	previewContent := components.RenderPreview(
		m.preview,
		width-4, // Account for border and padding
		height-2,
		m.styles.File,
	)

	return previewStyle.Render(previewContent)
}

// renderStatusBar renders the status bar
func (m Model) renderStatusBar() string {
	// Left side: file count and size
	totalSize := int64(0)
	for _, f := range m.files {
		totalSize += f.Size
	}

	leftInfo := fmt.Sprintf(" %d files | %s", len(m.files), utils.HumanizeSize(totalSize))

	// Center: status message
	centerInfo := ""
	if m.statusMsg != "" {
		centerInfo = " " + m.statusMsg + " "
	} else if m.err != nil {
		centerInfo = fmt.Sprintf(" Error: %v ", m.err)
	}

	// Right side: cursor position and preview status
	rightInfo := ""
	if len(m.files) > 0 {
		previewStatus := ""
		if m.previewEnabled {
			previewStatus = "👁️ "
		}
		rightInfo = fmt.Sprintf("%s%d/%d ", previewStatus, m.cursor+1, len(m.files))
	}

	// Build status bar
	gap := m.width - lipgloss.Width(leftInfo) - lipgloss.Width(centerInfo) - lipgloss.Width(rightInfo)
	if gap < 0 {
		gap = 0
	}

	statusLine := leftInfo + centerInfo + strings.Repeat(" ", gap) + rightInfo

	return m.styles.StatusBar.
		Width(m.width).
		Render(statusLine)
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