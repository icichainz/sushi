package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

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

// LoadConfig loads the configuration from the config file
// Returns default config if file doesn't exist or on error
func LoadConfig() *Config {
	cfg := DefaultConfig()

	configPath, err := getConfigPath()
	if err != nil {
		return cfg
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		// File doesn't exist, return defaults
		return cfg
	}

	// Parse YAML, keep defaults for any missing fields
	if err := yaml.Unmarshal(data, cfg); err != nil {
		name := filepath.Base(configPath)
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

	// Validate and clamp values
	cfg.validate()

	return cfg
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

// validate ensures config values are within acceptable ranges
func (c *Config) validate() {
	// Validate icon_mode
	switch c.IconMode {
	case "nerd", "bootstrap", "ascii":
		// Valid
	default:
		c.IconMode = "nerd"
	}

	// Validate preview_width (1-80%)
	if c.PreviewWidth < 1 {
		c.PreviewWidth = 1
	} else if c.PreviewWidth > 80 {
		c.PreviewWidth = 80
	}

	// Validate opener
	switch c.Opener {
	case "auto", "editor", "system":
		// Valid
	default:
		c.Opener = "auto"
	}

	// Validate sort_by
	switch c.SortBy {
	case "name", "size", "modified", "type":
		// Valid
	default:
		c.SortBy = "name"
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
