package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// useTempHome points the config directory at a temp dir for the test
func useTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".config", "sushi")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadConfigReadsAndValidates(t *testing.T) {
	dir := useTempHome(t)
	yaml := "show_hidden: true\nsort_by: size\nsort_reverse: true\nconfirm_delete: false\npreview_width: 500\nicon_mode: bogus\n"
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0644)

	cfg := LoadConfig()
	if !cfg.ShowHidden || cfg.SortBy != "size" || !cfg.SortReverse || cfg.ConfirmDelete {
		t.Fatalf("settings not loaded: %+v", cfg)
	}
	if cfg.PreviewWidth != 80 || cfg.IconMode != "nerd" {
		t.Fatalf("invalid values not clamped: preview_width=%d icon_mode=%q", cfg.PreviewWidth, cfg.IconMode)
	}
	if !cfg.PreviewEnabled {
		t.Fatal("unset keys should keep their defaults")
	}
}

func TestMouseSetting(t *testing.T) {
	dir := useTempHome(t)
	if !DefaultConfig().Mouse {
		t.Fatal("the mouse should be on by default")
	}
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("show_hidden: true\n"), 0644)
	if !LoadConfig().Mouse {
		t.Fatal("a config file without mouse: should keep the mouse on")
	}
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("mouse: false\n"), 0644)
	if LoadConfig().Mouse {
		t.Fatal("mouse: false was ignored")
	}
}

func TestLoadConfigMissingFileUsesDefaults(t *testing.T) {
	useTempHome(t)
	if cfg := LoadConfig(); !reflect.DeepEqual(cfg, DefaultConfig()) {
		t.Fatalf("got %+v, want defaults", cfg)
	}
}

func TestBookmarksRoundTrip(t *testing.T) {
	useTempHome(t)
	store := LoadBookmarks()
	store.Add("proj", "/tmp/proj")
	store.Add("proj again", "/tmp/proj") // Duplicate path is ignored
	store.Add("docs", "/tmp/docs")

	reloaded := LoadBookmarks()
	if reloaded.Len() != 2 || reloaded.Get(1).Name != "docs" {
		t.Fatalf("reloaded %d bookmarks: %+v", reloaded.Len(), reloaded.Bookmarks)
	}
	reloaded.Remove(0)
	if again := LoadBookmarks(); again.Len() != 1 || again.Get(0).Path != "/tmp/docs" {
		t.Fatalf("after remove: %+v", again.Bookmarks)
	}
}
