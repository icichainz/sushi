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
// true color. Sushi draws on the terminal's own background, so themes only
// color text, bars and highlights.
type Theme struct {
	Name          string
	HeaderFg      lipgloss.Color // Current directory in the breadcrumb
	Text          lipgloss.Color
	Muted         lipgloss.Color // Secondary text: sizes, dates, hint labels
	Faint         lipgloss.Color // Pane headings, the parent pane, dimmed screens
	Directory     lipgloss.Color
	CursorFg      lipgloss.Color // Also the text on mode badges and dialog titles
	CursorBg      lipgloss.Color
	BarFg         lipgloss.Color // Status bar
	BarBg         lipgloss.Color
	Raised        lipgloss.Color // Background of the row being renamed and the current directory in the parent pane
	TabBarBg      lipgloss.Color
	TabActiveFg   lipgloss.Color
	TabInactiveFg lipgloss.Color
	TabInactiveBg lipgloss.Color
	Accent        lipgloss.Color // Active tab, NORMAL badge and dialog borders
	Border        lipgloss.Color // Dividers between panes
	Title         lipgloss.Color // Prompts
	Highlight     lipgloss.Color // Key names and typed text
	Danger        lipgloss.Color // Errors and delete confirmation
	Selected      lipgloss.Color // Files marked for a multi-file operation
	Syntax        string         // Chroma style used for syntax highlighting
}

// themes holds the built-in palettes
var themes = map[string]Theme{
	// Nori: ink-dark bars, rice-white text, salmon accent, wasabi selection
	"default": {
		HeaderFg: "#ebe7dc",
		Text:     "#ebe7dc", Muted: "#a8a59c", Faint: "#8f8c84", Directory: "#ff9478",
		CursorFg: "#14151a", CursorBg: "#ff9478",
		BarFg: "#ebe7dc", BarBg: "#1b1c23", Raised: "#262832",
		TabBarBg: "#1b1c23", TabActiveFg: "#14151a", TabInactiveFg: "#a8a59c", TabInactiveBg: "#1b1c23",
		Accent: "#ff9478", Border: "#33353f", Title: "#ff9478", Highlight: "#ff9478",
		Danger: "#ff6b6b", Selected: "#b5d46b",
		Syntax: "sushi",
	},
	// The colors sushi used before the redesign
	"classic": {
		HeaderFg: "15",
		Text:     "252", Muted: "245", Faint: "243", Directory: "12",
		CursorFg: "0", CursorBg: "13",
		BarFg: "15", BarBg: "236", Raised: "237",
		TabBarBg: "235", TabActiveFg: "229", TabInactiveFg: "252", TabInactiveBg: "238",
		Accent: "62", Border: "238", Title: "212", Highlight: "229",
		Danger: "196", Selected: "214",
		Syntax: "monokai",
	},
	// A Dracula-inspired palette
	"dark": {
		HeaderFg: "#f8f8f2",
		Text:     "#f8f8f2", Muted: "#a9b1d0", Faint: "#8791b8", Directory: "#8be9fd",
		CursorFg: "#282a36", CursorBg: "#ff79c6",
		BarFg: "#f8f8f2", BarBg: "#44475a", Raised: "#343746",
		TabBarBg: "#21222c", TabActiveFg: "#282a36", TabInactiveFg: "#f8f8f2", TabInactiveBg: "#21222c",
		Accent: "#bd93f9", Border: "#44475a", Title: "#ff79c6", Highlight: "#f1fa8c",
		Danger: "#ff5555", Selected: "#ffb86c",
		Syntax: "dracula",
	},
	// For terminals with a light background
	"light": {
		HeaderFg: "#1f2024",
		Text:     "#1f2024", Muted: "#55534d", Faint: "#6b6862", Directory: "#b8432a",
		CursorFg: "#ffffff", CursorBg: "#b8432a",
		BarFg: "#1f2024", BarBg: "#e4dfd0", Raised: "#d9d3c1",
		TabBarBg: "#e4dfd0", TabActiveFg: "#ffffff", TabInactiveFg: "#55534d", TabInactiveBg: "#e4dfd0",
		Accent: "#b8432a", Border: "#cfc9b8", Title: "#b8432a", Highlight: "#b8432a",
		Danger: "#b3261e", Selected: "#4d6b12",
		Syntax: "sushi-light",
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
		"text":            &t.Text,
		"muted":           &t.Muted,
		"faint":           &t.Faint,
		"raised":          &t.Raised,
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
