package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Bookmark represents a saved directory bookmark
type Bookmark struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// BookmarkStore manages bookmarks persistence
type BookmarkStore struct {
	Bookmarks []Bookmark `json:"bookmarks"`
	configDir string
}

// getConfigDir returns the sushi config directory path
func getConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "sushi"), nil
}

// getBookmarksPath returns the bookmarks file path
func getBookmarksPath() (string, error) {
	configDir, err := getConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "bookmarks.json"), nil
}

// LoadBookmarks loads bookmarks from the config file
func LoadBookmarks() *BookmarkStore {
	store := &BookmarkStore{
		Bookmarks: []Bookmark{},
	}

	configDir, err := getConfigDir()
	if err != nil {
		return store
	}
	store.configDir = configDir

	bookmarksPath, err := getBookmarksPath()
	if err != nil {
		return store
	}

	data, err := os.ReadFile(bookmarksPath)
	if err != nil {
		// File doesn't exist yet, return empty store
		return store
	}

	// Handle corrupted JSON gracefully - return empty store if unmarshal fails
	if err := json.Unmarshal(data, store); err != nil {
		// Reset to empty bookmarks if JSON is corrupted
		store.Bookmarks = []Bookmark{}
	}
	return store
}

// Save persists bookmarks to the config file
func (b *BookmarkStore) Save() error {
	configDir, err := getConfigDir()
	if err != nil {
		return err
	}

	// Create config directory if it doesn't exist
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	bookmarksPath, err := getBookmarksPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(bookmarksPath, data, 0644)
}

// Add adds a new bookmark
func (b *BookmarkStore) Add(name, path string) error {
	// Check if bookmark already exists
	for _, bm := range b.Bookmarks {
		if bm.Path == path {
			return nil // Already exists
		}
	}

	b.Bookmarks = append(b.Bookmarks, Bookmark{
		Name: name,
		Path: path,
	})

	return b.Save()
}

// Remove removes a bookmark by index
func (b *BookmarkStore) Remove(index int) error {
	if index < 0 || index >= len(b.Bookmarks) {
		return nil
	}

	b.Bookmarks = append(b.Bookmarks[:index], b.Bookmarks[index+1:]...)
	return b.Save()
}

// Get returns a bookmark by index, or nil if out of range
func (b *BookmarkStore) Get(index int) *Bookmark {
	if index < 0 || index >= len(b.Bookmarks) {
		return nil
	}
	return &b.Bookmarks[index]
}

// Len returns the number of bookmarks
func (b *BookmarkStore) Len() int {
	return len(b.Bookmarks)
}
