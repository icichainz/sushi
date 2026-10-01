package app

import (
	"fmt"
	"path/filepath"
	"slices"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/config"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/plugins"
	"github.com/icichainz/sushi/internal/ui"
	"github.com/icichainz/sushi/internal/ui/components"
)

// Tab represents a single browsing session
type Tab struct {
	ID              int    // Stable identity so async results reach the right tab
	loadSeq         int    // Incremented per directory load; stale results are dropped
	focusPath       string // File to put the cursor on after the next load
	CurrentPath     string
	Files           []fs.FileInfo
	ParentFiles     []fs.FileInfo // Contents of the parent directory, for the parent pane
	Cursor          int
	Selected        map[string]bool // Paths marked for multi-file operations
	Preview         components.PreviewContent
	PreviewScroll   int // First visible line of the preview
	PreviewEnabled  bool
	PreviewWidth    int
	SearchQuery     string
	SearchResults   []int            // Ordered list of matching indices
	SearchMatchSet  map[int]struct{} // O(1) lookup set for isSearchMatch
	SearchResultIdx int              // Current position in SearchResults (avoids O(n) lookup)
	TotalSize       int64            // Cached total size of all files
	Loading         bool
	reloadWanted    bool // Changed while loading: reload once the load is in
	resortWanted    bool // Sort order changed while loading: sort the load once in

	git gitState // What Git says about CurrentPath; see git.go
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
	initCmd tea.Cmd // Returned from Init, e.g. to clear startup warnings

	// Key bindings
	keys KeyMap

	// Mode
	mode       Mode
	prompt     prompt // Text input for ModeInput
	helpScroll int    // First visible row of the key panel

	// Status message
	statusMsg string
	statusID  int // Identifies the current message so older timers don't clear it

	// File operations
	clipboard     []string // Paths in the clipboard
	clipboardMode string   // "copy" or "cut"
	confirmAction string   // "delete" or "paste"
	pending       []string // Paths to delete, or names a paste would overwrite
	pasteDir      string   // Where the paste the dialog asks about goes

	// Background operations and undo; see jobs.go and undo.go
	job    *job        // The operation running in the background, if any
	jobSeq int         // Gives each job its ID
	undo   []undoEntry // Operations that can be undone, most recent last

	// Bookmarks
	bookmarks      *config.BookmarkStore
	bookmarkCursor int

	// Mouse
	lastClick click // The last left click, to recognise a double-click

	// Plugins
	plugins      []plugins.Plugin
	pluginKeys   map[string]int // Shortcut to index in plugins
	pluginCursor int
	runInput     components.TextInput // Shell command typed in the Run palette
	runTyping    bool                 // Whether keys go to runInput or the plugin list

	// Configuration
	config     *config.Config
	showHidden bool // Starts from config, toggled at runtime

	// Sort order; starts from config, changed at runtime with s and S
	sortBy      string
	sortReverse bool
	sortCursor  int // Row of the sort menu

	find         finder      // Recursive search palette (f, F)
	jump         previewJump // Preview line to show once a search result's file loads
	watch        *dirWatcher // Reloads tabs when their directories change; nil when off
	keepShellDir bool        // Quit with Q: don't tell the shell to change directory

	pb           pbState          // What sushi knows of the macOS pasteboard; see pasteboard.go
	openWith     openWithState    // The Open with list; see openwith.go
	quickLookWin *quickLookWindow // The Quick Look window open, if any; see quicklook.go
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
		SortBy:      m.sortBy,
		SortReverse: m.sortReverse,
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
	ModeInput
	ModeHelp
	ModeConfirm
	ModeBookmarks
	ModePlugins
	ModeSort
	ModeFind
	ModeOpenWith
)

// KeyMap defines all key bindings. Each field is an action that keys: in
// the config file can remap, by its name in snake_case; see keys.go.
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
	Edit        key.Binding
	Open        key.Binding
	OpenWith    key.Binding
	Reveal      key.Binding
	QuickLook   key.Binding
	Rename      key.Binding
	NewFile     key.Binding
	NewDir      key.Binding
	Select      key.Binding
	Invert      key.Binding
	Unselect    key.Binding
	Copy        key.Binding
	Cut         key.Binding
	Paste       key.Binding
	HardDelete  key.Binding
	Undo        key.Binding
	Cancel      key.Binding
	Duplicate   key.Binding
	PasteLink   key.Binding
	Chmod       key.Binding
	BulkRename  key.Binding
	Archive     key.Binding
	Extract     key.Binding
	Search      key.Binding
	Bookmark    key.Binding
	AddBookmark key.Binding
	Quit        key.Binding
	Help        key.Binding
	PreviewUp   key.Binding
	PreviewDown key.Binding
	Plugins     key.Binding
	Shell       key.Binding
	Preview     key.Binding
	Hidden      key.Binding
	NewTab      key.Binding
	NewTabHome  key.Binding
	NextTab     key.Binding
	PrevTab     key.Binding
	CloseTab    key.Binding
	Refresh     key.Binding
	Sort        key.Binding
	Reverse     key.Binding
	Find        key.Binding
	Grep        key.Binding
	QuitNoCd    key.Binding
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
			key.WithHelp("→/l", "open"),
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
			key.WithHelp("d", "move to trash"),
		),
		Edit: key.NewBinding(
			key.WithKeys("e"),
			key.WithHelp("e", "edit"),
		),
		Open: key.NewBinding(
			key.WithKeys("o"),
			key.WithHelp("o", "open with default app"),
		),
		// macOS; see openwith.go
		OpenWith: key.NewBinding(
			key.WithKeys("O"),
			key.WithHelp("O", "open with app"),
		),
		Reveal: key.NewBinding(
			key.WithKeys("ctrl+o"),
			key.WithHelp("ctrl+o", "reveal in Finder"),
		),
		QuickLook: key.NewBinding(
			key.WithKeys("i"),
			key.WithHelp("i", "quick look"),
		),
		Rename: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "rename"),
		),
		NewFile: key.NewBinding(
			key.WithKeys("n"),
			key.WithHelp("n", "new file"),
		),
		NewDir: key.NewBinding(
			key.WithKeys("N"),
			key.WithHelp("N", "new directory"),
		),
		Select: key.NewBinding(
			key.WithKeys(" "),
			key.WithHelp("space", "select"),
		),
		Invert: key.NewBinding(
			key.WithKeys("*"),
			key.WithHelp("*", "invert selection"),
		),
		Unselect: key.NewBinding(
			key.WithKeys("u"),
			key.WithHelp("u", "clear selection"),
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
		// Trash, undo and file tools; see tools.go
		HardDelete: key.NewBinding(
			key.WithKeys("D"),
			key.WithHelp("D", "delete permanently"),
		),
		Undo: key.NewBinding(
			key.WithKeys("ctrl+z"),
			key.WithHelp("ctrl+z", "undo"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("ctrl+x"),
			key.WithHelp("ctrl+x", "cancel operation"),
		),
		Duplicate: key.NewBinding(
			key.WithKeys("y"),
			key.WithHelp("y", "duplicate"),
		),
		PasteLink: key.NewBinding(
			key.WithKeys("V"),
			key.WithHelp("V", "paste as symlink"),
		),
		Chmod: key.NewBinding(
			key.WithKeys("m"),
			key.WithHelp("m", "permissions"),
		),
		BulkRename: key.NewBinding(
			key.WithKeys("R"),
			key.WithHelp("R", "bulk rename"),
		),
		Archive: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "compress to zip"),
		),
		Extract: key.NewBinding(
			key.WithKeys("X"),
			key.WithHelp("X", "extract archive"),
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
		PreviewUp: key.NewBinding(
			key.WithKeys("K"),
			key.WithHelp("K", "scroll preview up"),
		),
		PreviewDown: key.NewBinding(
			key.WithKeys("J"),
			key.WithHelp("J", "scroll preview down"),
		),
		Plugins: key.NewBinding(
			key.WithKeys("P"),
			key.WithHelp("P", "plugins"),
		),
		Shell: key.NewBinding(
			key.WithKeys("!"),
			key.WithHelp("!", "run shell command"),
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
		Refresh: key.NewBinding(
			key.WithKeys("ctrl+r"),
			key.WithHelp("ctrl+r", "refresh"),
		),
		Sort: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "sort by"),
		),
		Reverse: key.NewBinding(
			key.WithKeys("S"),
			key.WithHelp("S", "reverse sort"),
		),
		Find: key.NewBinding(
			key.WithKeys("f"),
			key.WithHelp("f", "find by name"),
		),
		Grep: key.NewBinding(
			key.WithKeys("F"),
			key.WithHelp("F", "find in files"),
		),
		QuitNoCd: key.NewBinding(
			key.WithKeys("Q"),
			key.WithHelp("Q", "quit without cd"),
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

	// Problems are shown in the status bar rather than stopping startup.
	// They are found in the same order as sushi --list-keys finds them.
	theme, problems := loadTheme(cfg)
	problems = append(slices.Clone(cfg.Problems), problems...)
	keys, keyProblems := loadKeyMap(cfg.Keys)
	problems = append(problems, keyProblems...)

	m := Model{
		theme:       theme,
		keys:        keys,
		mode:        ModeNormal,
		bookmarks:   config.LoadBookmarks(),
		config:      cfg,
		showHidden:  cfg.ShowHidden,
		sortBy:      cfg.SortBy,
		sortReverse: cfg.SortReverse,
	}
	if cfg.Watch {
		m.watch = newDirWatcher()
	}

	// Create initial tab with config settings
	initialTab := m.newTab(path)
	files, err := fs.ScanDirectory(path, m.scanOptions())
	if err != nil {
		// First, as sushi --list-keys can't say it
		problems = append([]string{fmt.Sprintf("Error: %v", err)}, problems...)
	} else {
		initialTab.setFiles(files)
	}
	initialTab.ParentFiles = scanParent(path, m.scanOptions())

	// Load initial preview
	if len(initialTab.Files) > 0 && initialTab.PreviewEnabled {
		initialTab.Preview = m.loadPreviewNow(initialTab.Files[0])
	}

	m.tabs = []Tab{initialTab}

	loaded, warnings := plugins.Load(cfg.Plugins, config.PluginDir())
	m.plugins = loaded
	problems = append(problems, warnings...)
	problems = append(problems, m.bindPluginKeys()...)

	if len(problems) > 0 {
		m.initCmd = m.setStatusFor(startupMessage(problems), 10*time.Second)
	}
	return m
}

// startupMessage shows the first of the problems found at startup, and
// how many more there are: all of them on one line are cut short anyway
func startupMessage(problems []string) string {
	if len(problems) == 1 {
		return problems[0]
	}
	return fmt.Sprintf("%s (+%d more, see sushi --list-keys)", problems[0], len(problems)-1)
}

// loadTheme returns the theme the config asks for, and its problems: an
// unknown theme, color or syntax style
func loadTheme(cfg *config.Config) (ui.Theme, []string) {
	theme, problems := ui.LoadTheme(cfg.Theme, cfg.Colors)
	if cfg.SyntaxTheme != "" {
		if components.HasSyntaxTheme(cfg.SyntaxTheme) {
			theme.Syntax = cfg.SyntaxTheme
		} else {
			problems = append(problems, fmt.Sprintf("unknown syntax_theme %q", cfg.SyntaxTheme))
		}
	}
	return theme, problems
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.initCmd, m.watch.listen(), m.startGit()}
	if m.tab().Preview.Pending {
		cmds = append(cmds, m.previewCmd(m.tab()))
	}
	return tea.Batch(cmds...)
}
