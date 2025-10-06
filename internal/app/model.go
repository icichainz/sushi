package app

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/ui"
	"github.com/icichainz/sushi/internal/ui/components"
)

// Model represents the application state
type Model struct {
	// Current state
	currentPath string
	files       []fs.FileInfo
	cursor      int
	selected    map[string]bool

	// Preview state
	preview            components.PreviewContent
	previewEnabled     bool
	previewWidth       int
	syntaxHighlight    bool
	syntaxTheme        string

	// UI state
	width  int
	height int
	styles ui.Styles

	// Key bindings
	keys KeyMap

	// Mode
	mode Mode

	// Status message
	statusMsg string
	err       error
}

// Mode represents the current application mode
type Mode int

const (
	ModeNormal Mode = iota
	ModeSearch
	ModeCommand
)

// KeyMap defines all key bindings
type KeyMap struct {
	Up              key.Binding
	Down            key.Binding
	Left            key.Binding
	Right           key.Binding
	Enter           key.Binding
	Back            key.Binding
	Delete          key.Binding
	Quit            key.Binding
	Help            key.Binding
	Preview         key.Binding
	ToggleSyntax    key.Binding
}

// DefaultKeyMap returns the default key bindings
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "move up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "move down"),
		),
		Left: key.NewBinding(
			key.WithKeys("left", "h"),
			key.WithHelp("←/h", "parent dir"),
		),
		Right: key.NewBinding(
			key.WithKeys("right", "l"),
			key.WithHelp("→/l", "enter dir"),
		),
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "open"),
		),
		Back: key.NewBinding(
			key.WithKeys("backspace"),
			key.WithHelp("backspace", "back"),
		),
		Delete: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "delete"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Preview: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "toggle preview"),
		),
		ToggleSyntax: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "toggle syntax"),
		),
	}
}

// NewModel creates a new model with the given starting path
func NewModel(path string) Model {
	files, err := fs.ScanDirectory(path)
	if err != nil {
		files = []fs.FileInfo{}
	}

	m := Model{
		currentPath:     path,
		files:           files,
		cursor:          0,
		selected:        make(map[string]bool),
		styles:          ui.DefaultStyles(),
		keys:            DefaultKeyMap(),
		mode:            ModeNormal,
		previewEnabled:  true,
		previewWidth:    50, // 50% of screen
		syntaxHighlight: true,
		syntaxTheme:     "monokai", // Can be: monokai, dracula, github, nord, etc.
	}

	// Load initial preview
	if len(files) > 0 {
		config := components.PreviewConfig{
			MaxLines:        100,
			SyntaxHighlight: m.syntaxHighlight,
			SyntaxTheme:     m.syntaxTheme,
			MaxPreviewSize:  10 * 1024 * 1024,
		}
		m.preview = components.LoadPreviewWithConfig(files[0], config)
	}

	return m
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return nil
}