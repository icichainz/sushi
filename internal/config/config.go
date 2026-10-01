package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/icichainz/sushi/internal/plugins"
	"gopkg.in/yaml.v3"
)

// Config represents the application configuration
type Config struct {
	// Display settings
	IconMode       string `yaml:"icon_mode"`       // "nerd", "bootstrap", "ascii"
	ShowHidden     bool   `yaml:"show_hidden"`     // Show hidden files by default
	PreviewEnabled bool   `yaml:"preview_enabled"` // Enable preview pane by default
	PreviewWidth   int    `yaml:"preview_width"`   // Preview pane width percentage (1-80)
	Mouse          bool   `yaml:"mouse"`           // Clicks and the wheel; off leaves the mouse to the terminal

	// Behavior settings
	ConfirmDelete bool   `yaml:"confirm_delete"` // Require confirmation for delete
	Opener        string `yaml:"opener"`         // How Enter opens files: "auto", "editor", "system"
	SortBy        string `yaml:"sort_by"`        // "name", "size", "modified", "type"
	SortReverse   bool   `yaml:"sort_reverse"`   // Reverse sort order
	Watch         bool   `yaml:"watch"`          // Reload when files change on disk
	Pasteboard    bool   `yaml:"pasteboard"`     // Share copies with Finder through the macOS pasteboard

	// d moves files to the trash, which ctrl+z can undo; D always deletes
	DeleteToTrash bool `yaml:"delete_to_trash"`

	// Theme settings
	Theme       string            `yaml:"theme"`        // "default", "dark" or "light"
	Colors      map[string]string `yaml:"colors"`       // Per-color overrides of the theme
	SyntaxTheme string            `yaml:"syntax_theme"` // Chroma style; empty follows the theme

	// External commands; scripts in PluginDir are added to these
	Plugins []plugins.Plugin `yaml:"plugins"`

	// Keys for actions by name, replacing their defaults; actions left
	// out keep theirs. sushi --list-keys prints the names.
	Keys map[string]KeyList `yaml:"keys"`

	// Problems found reading the file, for the app to show at startup
	Problems []string `yaml:"-"`
}

// DefaultConfig returns a config with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		IconMode:       "nerd",
		ShowHidden:     false,
		PreviewEnabled: true,
		PreviewWidth:   50,
		Mouse:          true,
		ConfirmDelete:  true,
		DeleteToTrash:  true,
		Opener:         "auto",
		SortBy:         "name",
		SortReverse:    false,
		Watch:          true,
		Pasteboard:     true,
		Theme:          "default",
	}
}

// getConfigPath returns the path to the config file
func getConfigPath() (string, error) {
	configDir, err := getConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "config.yaml"), nil
}

// LoadConfig loads the configuration from the config file. Without one it
// returns the defaults. Problems with it, such as a file that can't be
// read, a setting of the wrong kind or an unknown one, are noted in
// Problems, and the rest of the file still applies.
func LoadConfig() *Config {
	cfg := DefaultConfig()

	configPath, err := getConfigPath()
	if err != nil {
		return cfg
	}
	name := filepath.Base(configPath)

	data, err := os.ReadFile(configPath)
	if errors.Is(err, os.ErrNotExist) {
		return cfg
	}
	if err != nil {
		// There, but not readable: say so rather than quietly ignore it
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			err = pathErr.Err
		}
		cfg.Problems = []string{fmt.Sprintf("%s: failed to read it (%v), so the defaults are used", name, err)}
		return cfg
	}

	// Parse YAML, keep defaults for any missing fields
	if err := yaml.Unmarshal(data, cfg); err != nil {
		var typeErr *yaml.TypeError
		if !errors.As(err, &typeErr) {
			// Unreadable: use the defaults, and say why
			cfg = DefaultConfig()
			cfg.Problems = []string{fmt.Sprintf("%s: %v", name, err)}
			return cfg
		}
		// A value of the wrong kind keeps its default; the rest still counts
		for _, e := range typeErr.Errors {
			cfg.Problems = append(cfg.Problems, name+": "+e)
		}
	}
	// Misspelt settings are otherwise ignored without a word
	cfg.Problems = append(cfg.Problems, unknownSettings(name, data)...)

	// Validate and clamp values
	cfg.validate()

	return cfg
}

// unknownSettings describes the top-level settings in the YAML data that
// Config doesn't have, as in "config.yaml: line 2: unknown setting
// shw_hidden (did you mean show_hidden?)". The file is read a second time
// for this, as a decoder told to refuse unknown fields would stop at the
// first and lose the settings after it.
func unknownSettings(name string, data []byte) []string {
	var doc yaml.Node
	if yaml.Unmarshal(data, &doc) != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	known := settingNames()
	var problems []string
	pairs := doc.Content[0].Content
	for i := 0; i+1 < len(pairs); i += 2 {
		key := pairs[i]
		if slices.Contains(known, key.Value) {
			continue
		}
		problem := fmt.Sprintf("%s: line %d: unknown setting %s", name, key.Line, key.Value)
		if near := nearest(key.Value, known); near != "" {
			problem += fmt.Sprintf(" (did you mean %s?)", near)
		}
		problems = append(problems, problem)
	}
	return problems
}

// settingNames lists the settings of the config file, as their yaml tags
func settingNames() []string {
	var names []string
	t := reflect.TypeOf(Config{})
	for i := 0; i < t.NumField(); i++ {
		if tag, _, _ := strings.Cut(t.Field(i).Tag.Get("yaml"), ","); tag != "" && tag != "-" {
			names = append(names, tag)
		}
	}
	return names
}

// nearest returns the name among names that s is a typo of, one or two
// letters off, or ""
func nearest(s string, names []string) string {
	best, bestDist := "", 3
	for _, name := range names {
		if d := editDistance(strings.ToLower(s), name); d < bestDist {
			best, bestDist = name, d
		}
	}
	return best
}

// editDistance counts the letters to insert, delete or change to turn a
// into b
func editDistance(a, b string) int {
	x, y := []rune(a), []rune(b)
	row := make([]int, len(y)+1)
	for j := range row {
		row[j] = j
	}
	for i := 1; i <= len(x); i++ {
		diag := row[0]
		row[0] = i
		for j := 1; j <= len(y); j++ {
			cost := 1
			if x[i-1] == y[j-1] {
				cost = 0
			}
			diag, row[j] = row[j], min(row[j]+1, row[j-1]+1, diag+cost)
		}
	}
	return row[len(y)]
}

// Save writes the configuration to the config file
func (c *Config) Save() error {
	configDir, err := getConfigDir()
	if err != nil {
		return err
	}

	// Create config directory if it doesn't exist
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	configPath, err := getConfigPath()
	if err != nil {
		return err
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}

	// Add header comment
	header := []byte("# Sushi configuration file\n# Location: ~/.config/sushi/config.yaml\n\n")
	data = append(header, data...)

	return os.WriteFile(configPath, data, 0644)
}

// validate replaces values out of range with ones that work, noting each
// in Problems. An empty value is taken as unset. The theme is checked
// where themes are, by ui.LoadTheme.
func (c *Config) validate() {
	choose := func(setting string, value *string, choices ...string) {
		if *value == "" {
			*value = choices[0]
			return
		}
		if !slices.Contains(choices, *value) {
			c.Problems = append(c.Problems, fmt.Sprintf("%s: unknown value %q, using %s", setting, *value, choices[0]))
			*value = choices[0]
		}
	}
	choose("icon_mode", &c.IconMode, "nerd", "bootstrap", "ascii")
	choose("opener", &c.Opener, "auto", "editor", "system")
	choose("sort_by", &c.SortBy, "name", "size", "modified", "type")

	// A share of the screen, in percent
	if width := min(max(c.PreviewWidth, 1), 80); width != c.PreviewWidth {
		c.Problems = append(c.Problems, fmt.Sprintf("preview_width: invalid value %d, using %d (it is 1 to 80)", c.PreviewWidth, width))
		c.PreviewWidth = width
	}
}

// CreateDefaultConfigFile creates a default config file if it doesn't exist
// Returns true if a new file was created
func CreateDefaultConfigFile() (bool, error) {
	configPath, err := getConfigPath()
	if err != nil {
		return false, err
	}

	// Check if file already exists
	if _, err := os.Stat(configPath); err == nil {
		return false, nil // File exists
	}

	// Create default config
	cfg := DefaultConfig()
	if err := cfg.Save(); err != nil {
		return false, err
	}

	return true, nil
}

// PluginDir returns the directory scanned for plugin scripts
func PluginDir() string {
	dir, err := getConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "plugins")
}

// GetConfigPath returns the config file path for display purposes
func GetConfigPath() string {
	path, err := getConfigPath()
	if err != nil {
		return "~/.config/sushi/config.yaml"
	}
	return path
}
