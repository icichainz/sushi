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
}

// DefaultStyles returns the default style configuration
func DefaultStyles() Styles {
	return Styles{
		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("62")).
			Padding(0, 1),

		FileList: lipgloss.NewStyle().
			Padding(0, 1),

		File: lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")),

		SelectedFile: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("0")).
			Background(lipgloss.Color("13")),

		StatusBar: lipgloss.NewStyle().
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("236")).
			Padding(0, 1),

		EmptyDir: lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Align(lipgloss.Center, lipgloss.Center),
	}
}