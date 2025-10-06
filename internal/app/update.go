package app

import (
	"path/filepath"

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
		m.files = msg.files
		m.currentPath = msg.path
		m.cursor = 0
		m.err = msg.err
		
		// Load preview for first file
		if len(m.files) > 0 && m.previewEnabled {
			return m, loadPreview(m.files[0])
		}
		return m, nil

	case previewLoadedMsg:
		m.preview = msg.preview
		return m, nil
	}

	return m, nil
}

// handleKeyPress processes keyboard input
func (m Model) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keys.Preview):
		m.previewEnabled = !m.previewEnabled
		m.statusMsg = "Preview toggled"
		return m, nil

	case key.Matches(msg, m.keys.Up):
		if m.cursor > 0 {
			m.cursor--
			if len(m.files) > 0 && m.previewEnabled {
				return m, loadPreview(m.files[m.cursor])
			}
		}

	case key.Matches(msg, m.keys.Down):
		if m.cursor < len(m.files)-1 {
			m.cursor++
			if len(m.files) > 0 && m.previewEnabled {
				return m, loadPreview(m.files[m.cursor])
			}
		}

	case key.Matches(msg, m.keys.Right), key.Matches(msg, m.keys.Enter):
		if len(m.files) > 0 && m.files[m.cursor].IsDir {
			newPath := m.files[m.cursor].Path
			return m, loadDirectory(newPath)
		}

	case key.Matches(msg, m.keys.Left), key.Matches(msg, m.keys.Back):
		parentPath := filepath.Dir(m.currentPath)
		if parentPath != m.currentPath {
			return m, loadDirectory(parentPath)
		}
	}

	return m, nil
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