package ui

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"

	"github.com/charmbracelet/lipgloss"
)

// Theme is a named color palette. Colors are ANSI 256-color numbers ("62")
// or hex values ("#bd93f9"); lipgloss degrades hex on terminals without
// true color.
type Theme struct {
	Name          string
	HeaderFg      lipgloss.Color
	HeaderBg      lipgloss.Color
	Text          lipgloss.Color
	Muted         lipgloss.Color // Hints, empty panes, non-matching search results
	Directory     lipgloss.Color
	CursorFg      lipgloss.Color
	CursorBg      lipgloss.Color
	BarFg         lipgloss.Color // Status and search bars
	BarBg         lipgloss.Color
	TabBarBg      lipgloss.Color
	TabActiveFg   lipgloss.Color
	TabInactiveFg lipgloss.Color
	TabInactiveBg lipgloss.Color
	Accent        lipgloss.Color // Active tab and dialog borders
	Border        lipgloss.Color // Divider between the file list and preview
	Title         lipgloss.Color // Dialog titles and the search prompt
	Highlight     lipgloss.Color // Key names, typed text, the chosen list item
	Danger        lipgloss.Color // Errors and delete confirmation
	Selected      lipgloss.Color // Files marked for a multi-file operation
	Syntax        string         // Chroma style used for syntax highlighting
}

// themes holds the built-in palettes
var themes = map[string]Theme{
	// The original sushi colors, tuned for dark terminals
	"default": {
		HeaderFg: "15", HeaderBg: "62",
		Text: "252", Muted: "240", Directory: "12",
		CursorFg: "0", CursorBg: "13",
		BarFg: "15", BarBg: "236",
		TabBarBg: "235", TabActiveFg: "229", TabInactiveFg: "252", TabInactiveBg: "238",
		Accent: "62", Border: "238", Title: "212", Highlight: "229",
		Danger: "196", Selected: "214",
		Syntax: "monokai",
	},
	// A Dracula-inspired palette
	"dark": {
		HeaderFg: "#282a36", HeaderBg: "#bd93f9",
		Text: "#f8f8f2", Muted: "#6272a4", Directory: "#8be9fd",
		CursorFg: "#282a36", CursorBg: "#ff79c6",
		BarFg: "#f8f8f2", BarBg: "#44475a",
		TabBarBg: "#21222c", TabActiveFg: "#282a36", TabInactiveFg: "#f8f8f2", TabInactiveBg: "#44475a",
		Accent: "#bd93f9", Border: "#44475a", Title: "#ff79c6", Highlight: "#f1fa8c",
		Danger: "#ff5555", Selected: "#ffb86c",
		Syntax: "dracula",
	},
	// For terminals with a light background
	"light": {
		HeaderFg: "255", HeaderBg: "25",
		Text: "235", Muted: "245", Directory: "26",
		CursorFg: "255", CursorBg: "31",
		BarFg: "235", BarBg: "252",
		TabBarBg: "254", TabActiveFg: "255", TabInactiveFg: "238", TabInactiveBg: "250",
		Accent: "25", Border: "250", Title: "125", Highlight: "130",
		Danger: "160", Selected: "166",
		Syntax: "github",
	},
}

// ThemeNames returns the built-in theme names in alphabetical order
func ThemeNames() []string {
	names := make([]string, 0, len(themes))
	for name := range themes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// colorPattern matches hex colors; ANSI numbers are checked separately
var colorPattern = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// validColor reports whether c is a hex color or an ANSI color number
func validColor(c string) bool {
	if colorPattern.MatchString(c) {
		return true
	}
	n, err := strconv.Atoi(c)
	return err == nil && n >= 0 && n <= 255
}

// colorFields maps the config names of a theme's colors to its fields
func (t *Theme) colorFields() map[string]*lipgloss.Color {
	return map[string]*lipgloss.Color{
		"header_fg":       &t.HeaderFg,
		"header_bg":       &t.HeaderBg,
		"text":            &t.Text,
		"muted":           &t.Muted,
		"directory":       &t.Directory,
		"cursor_fg":       &t.CursorFg,
		"cursor_bg":       &t.CursorBg,
		"bar_fg":          &t.BarFg,
		"bar_bg":          &t.BarBg,
		"tab_bar_bg":      &t.TabBarBg,
		"tab_active_fg":   &t.TabActiveFg,
		"tab_inactive_fg": &t.TabInactiveFg,
		"tab_inactive_bg": &t.TabInactiveBg,
		"accent":          &t.Accent,
		"border":          &t.Border,
		"title":           &t.Title,
		"highlight":       &t.Highlight,
		"danger":          &t.Danger,
		"selected":        &t.Selected,
	}
}

// LoadTheme returns the named theme with color overrides applied. Unknown
// theme names fall back to "default"; each problem is reported as a warning
// rather than stopping sushi from starting.
func LoadTheme(name string, overrides map[string]string) (Theme, []string) {
	var warnings []string
	if name == "" {
		name = "default"
	}
	theme, ok := themes[name]
	if !ok {
		warnings = append(warnings, fmt.Sprintf("unknown theme %q, using default", name))
		name = "default"
		theme = themes[name]
	}
	theme.Name = name

	fields := theme.colorFields()
	// Apply in a fixed order so warnings are stable
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := overrides[key]
		field, ok := fields[key]
		switch {
		case !ok:
			warnings = append(warnings, fmt.Sprintf("unknown theme color %q", key))
		case !validColor(value):
			warnings = append(warnings, fmt.Sprintf("invalid color %q for %s", value, key))
		default:
			*field = lipgloss.Color(value)
		}
	}

	return theme, warnings
}
