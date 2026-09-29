package ui

import "github.com/charmbracelet/lipgloss"

// Styles holds all the styling for the application
type Styles struct {
	Header       lipgloss.Style
	FileList     lipgloss.Style
	File         lipgloss.Style
	SelectedFile lipgloss.Style
	StatusBar    lipgloss.Style
	EmptyDir     lipgloss.Style
	TabBar       lipgloss.Style
	TabActive    lipgloss.Style
	TabInactive  lipgloss.Style
	PreviewError lipgloss.Style
}

// NewStyles builds the application styles from a theme
func NewStyles(t Theme) Styles {
	return Styles{
		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(t.HeaderFg).
			Background(t.HeaderBg).
			Padding(0, 1),

		FileList: lipgloss.NewStyle().
			Padding(0, 1),

		File: lipgloss.NewStyle().
			Foreground(t.Text),

		SelectedFile: lipgloss.NewStyle().
			Bold(true).
			Foreground(t.CursorFg).
			Background(t.CursorBg),

		StatusBar: lipgloss.NewStyle().
			Foreground(t.BarFg).
			Background(t.BarBg).
			Padding(0, 1),

		EmptyDir: lipgloss.NewStyle().
			Foreground(t.Muted).
			Align(lipgloss.Center, lipgloss.Center),

		TabBar: lipgloss.NewStyle().
			Background(t.TabBarBg).
			Padding(0, 1),

		TabActive: lipgloss.NewStyle().
			Bold(true).
			Foreground(t.TabActiveFg).
			Background(t.Accent).
			Padding(0, 1).
			MarginRight(1),

		TabInactive: lipgloss.NewStyle().
			Foreground(t.TabInactiveFg).
			Background(t.TabInactiveBg).
			Padding(0, 1).
			MarginRight(1),

		PreviewError: lipgloss.NewStyle().
			Foreground(t.Danger),
	}
}
