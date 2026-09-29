package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/config"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/ui"
	"github.com/icichainz/sushi/internal/ui/components"
)

// Tab represents a single browsing session
type Tab struct {
	ID              int // Stable identity so async results reach the right tab
	loadSeq         int // Incremented per directory load; stale results are dropped
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
	nextTabID    int

	// UI state
	width   int
	height  int
	theme   ui.Theme
	styles  ui.Styles
	initCmd tea.Cmd // Returned from Init, e.g. to clear startup warnings

	// Key bindings
	keys KeyMap

	// Mode
	mode Mode

	// Status message
	statusMsg string
	statusID  int // Identifies the current message so older timers don't clear it

	// File operations
	clipboard     string // Path of file in clipboard
	clipboardMode string // "copy" or "cut"
	confirmAction string // "delete" or "paste"

	// Bookmarks
	bookmarks      *config.BookmarkStore
	bookmarkCursor int

	// Configuration
	config     *config.Config
	showHidden bool // Starts from config, toggled at runtime
}

// tab returns a pointer to the active tab
func (m *Model) tab() *Tab {
	return &m.tabs[m.activeTabIdx]
}

// tabByID returns the tab with the given ID, or nil if it has been closed
func (m *Model) tabByID(id int) *Tab {
	for i := range m.tabs {
		if m.tabs[i].ID == id {
			return &m.tabs[i]
		}
	}
	return nil
}

// scanOptions returns the listing options for directory scans
func (m *Model) scanOptions() fs.ScanOptions {
	return fs.ScanOptions{
		ShowHidden:  m.showHidden,
		SortBy:      m.config.SortBy,
		SortReverse: m.config.SortReverse,
	}
}

// newTab creates an empty tab at path using the configured defaults
func (m *Model) newTab(path string) Tab {
	m.nextTabID++
	return Tab{
		ID:             m.nextTabID,
		CurrentPath:    path,
		Files:          []fs.FileInfo{},
		Selected:       make(map[string]bool),
		PreviewEnabled: m.config.PreviewEnabled,
		PreviewWidth:   m.config.PreviewWidth,
	}
}

// setFiles replaces the tab's file list and refreshes the cached total size
func (t *Tab) setFiles(files []fs.FileInfo) {
	t.Files = files
	t.TotalSize = 0
	for _, f := range files {
		t.TotalSize += f.Size
	}
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
	Hidden      key.Binding
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
		Hidden: key.NewBinding(
			key.WithKeys("."),
			key.WithHelp(".", "toggle hidden files"),
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

	// Relative paths break parent navigation (the parent of ".." is ".")
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}

	// Problems are shown in the status bar rather than stopping startup
	theme, problems := ui.LoadTheme(cfg.Theme, cfg.Colors)
	if cfg.SyntaxTheme != "" {
		if components.HasSyntaxTheme(cfg.SyntaxTheme) {
			theme.Syntax = cfg.SyntaxTheme
		} else {
			problems = append(problems, fmt.Sprintf("unknown syntax_theme %q", cfg.SyntaxTheme))
		}
	}

	m := Model{
		theme:      theme,
		styles:     ui.NewStyles(theme),
		keys:       DefaultKeyMap(),
		mode:       ModeNormal,
		bookmarks:  config.LoadBookmarks(),
		config:     cfg,
		showHidden: cfg.ShowHidden,
	}

	// Create initial tab with config settings
	initialTab := m.newTab(path)
	files, err := fs.ScanDirectory(path, m.scanOptions())
	if err != nil {
		problems = append(problems, fmt.Sprintf("Error: %v", err))
	} else {
		initialTab.setFiles(files)
	}

	// Load initial preview
	if len(initialTab.Files) > 0 && initialTab.PreviewEnabled {
		initialTab.Preview = m.loadPreviewNow(initialTab.Files[0])
	}

	m.tabs = []Tab{initialTab}
	if len(problems) > 0 {
		m.initCmd = m.setStatusFor(strings.Join(problems, "; "), 10*time.Second)
	}
	return m
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return m.initCmd
}
