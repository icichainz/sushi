package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config represents the application configuration
type Config struct {
	// Display settings
	IconMode       string `yaml:"icon_mode"`       // "nerd", "bootstrap", "ascii"
	ShowHidden     bool   `yaml:"show_hidden"`     // Show hidden files by default
	PreviewEnabled bool   `yaml:"preview_enabled"` // Enable preview pane by default
	PreviewWidth   int    `yaml:"preview_width"`   // Preview pane width percentage (1-80)

	// Behavior settings
	ConfirmDelete bool   `yaml:"confirm_delete"` // Require confirmation for delete
	SortBy        string `yaml:"sort_by"`        // "name", "size", "modified", "type"
	SortReverse   bool   `yaml:"sort_reverse"`   // Reverse sort order

	// Theme settings
	Theme string `yaml:"theme"` // "default", "dark", "light" (for future use)
}

// DefaultConfig returns a config with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		IconMode:       "nerd",
		ShowHidden:     false,
		PreviewEnabled: true,
		PreviewWidth:   50,
		ConfirmDelete:  true,
		SortBy:         "name",
		SortReverse:    false,
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
		// Return defaults on parse error
		return DefaultConfig()
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

	// Validate sort_by
	switch c.SortBy {
	case "name", "size", "modified", "type":
		// Valid
	default:
		c.SortBy = "name"
	}

	// Validate theme
	switch c.Theme {
	case "default", "dark", "light":
		// Valid
	default:
		c.Theme = "default"
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

// GetConfigPath returns the config file path for display purposes
func GetConfigPath() string {
	path, err := getConfigPath()
	if err != nil {
		return "~/.config/sushi/config.yaml"
	}
	return path
}
