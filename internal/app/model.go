package app

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/config"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/ui"
	"github.com/icichainz/sushi/internal/ui/components"
)

// Tab represents a single browsing session
type Tab struct {
	CurrentPath     string
	Files           []fs.FileInfo
	Cursor          int
	Selected        map[string]bool
	Preview         components.PreviewContent
	PreviewEnabled  bool
	PreviewWidth    int
	SearchQuery     string
	SearchResults   []int            // Ordered list of matching indices
	SearchMatchSet  map[int]struct{} // O(1) lookup set for isSearchMatch
	SearchResultIdx int              // Current position in SearchResults (avoids O(n) lookup)
	TotalSize       int64            // Cached total size of all files
	Loading         bool
}

// Model represents the application state
type Model struct {
	// Tab management
	tabs         []Tab
	activeTabIdx int

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

	// File operations
	clipboard     string // Path of file in clipboard
	clipboardMode string // "copy" or "cut"
	confirmAction string // "delete" or "paste"

	// Bookmarks
	bookmarks      *config.BookmarkStore
	bookmarkCursor int

	// Configuration
	config *config.Config
}

// tab returns a pointer to the active tab
func (m *Model) tab() *Tab {
	return &m.tabs[m.activeTabIdx]
}

// Mode represents the current application mode
type Mode int

const (
	ModeNormal Mode = iota
	ModeSearch
	ModeCommand
	ModeHelp
	ModeConfirm
	ModeBookmarks
)

// KeyMap defines all key bindings
type KeyMap struct {
	Up          key.Binding
	Down        key.Binding
	Left        key.Binding
	Right       key.Binding
	Enter       key.Binding
	Back        key.Binding
	PageUp      key.Binding
	PageDown    key.Binding
	Home        key.Binding
	End         key.Binding
	Delete      key.Binding
	Copy        key.Binding
	Cut         key.Binding
	Paste       key.Binding
	Search      key.Binding
	Bookmark    key.Binding
	AddBookmark key.Binding
	Quit        key.Binding
	Help        key.Binding
	Preview     key.Binding
	NewTab      key.Binding
	NewTabHome  key.Binding
	NextTab     key.Binding
	PrevTab     key.Binding
	CloseTab    key.Binding
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
		PageUp: key.NewBinding(
			key.WithKeys("pgup", "ctrl+u"),
			key.WithHelp("PgUp/^u", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown", "ctrl+d"),
			key.WithHelp("PgDn/^d", "page down"),
		),
		Home: key.NewBinding(
			key.WithKeys("home", "g"),
			key.WithHelp("Home/g", "go to first"),
		),
		End: key.NewBinding(
			key.WithKeys("end", "G"),
			key.WithHelp("End/G", "go to last"),
		),
		Delete: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "delete"),
		),
		Copy: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "copy"),
		),
		Cut: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("x", "cut"),
		),
		Paste: key.NewBinding(
			key.WithKeys("v"),
			key.WithHelp("v", "paste"),
		),
		Search: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "search"),
		),
		Bookmark: key.NewBinding(
			key.WithKeys("b"),
			key.WithHelp("b", "bookmarks"),
		),
		AddBookmark: key.NewBinding(
			key.WithKeys("B"),
			key.WithHelp("B", "add bookmark"),
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
		NewTab: key.NewBinding(
			key.WithKeys("t"),
			key.WithHelp("t", "new tab"),
		),
		NewTabHome: key.NewBinding(
			key.WithKeys("T"),
			key.WithHelp("T", "new tab (home)"),
		),
		NextTab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "next tab"),
		),
		PrevTab: key.NewBinding(
			key.WithKeys("shift+tab"),
			key.WithHelp("shift+tab", "prev tab"),
		),
		CloseTab: key.NewBinding(
			key.WithKeys("ctrl+w"),
			key.WithHelp("ctrl+w", "close tab"),
		),
	}
}

// NewModel creates a new model with the given starting path
func NewModel(path string) Model {
	return NewModelWithConfig(path, nil)
}

// NewModelWithConfig creates a new model with the given starting path and config
func NewModelWithConfig(path string, cfg *config.Config) Model {
	// Use provided config or load from file
	if cfg == nil {
		cfg = config.LoadConfig()
	}

	files, err := fs.ScanDirectory(path)
	if err != nil {
		files = []fs.FileInfo{}
	}

	// Calculate initial total size
	var totalSize int64
	for _, f := range files {
		totalSize += f.Size
	}

	// Create initial tab with config settings
	initialTab := Tab{
		CurrentPath:    path,
		Files:          files,
		Cursor:         0,
		Selected:       make(map[string]bool),
		PreviewEnabled: cfg.PreviewEnabled,
		PreviewWidth:   cfg.PreviewWidth,
		TotalSize:      totalSize,
		Loading:        false,
	}

	// Load initial preview
	if len(files) > 0 && cfg.PreviewEnabled {
		initialTab.Preview = components.LoadPreview(files[0], 100)
	}

	m := Model{
		tabs:         []Tab{initialTab},
		activeTabIdx: 0,
		styles:       ui.DefaultStyles(),
		keys:         DefaultKeyMap(),
		mode:         ModeNormal,
		bookmarks:    config.LoadBookmarks(),
		config:       cfg,
	}

	return m
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return nil
}
