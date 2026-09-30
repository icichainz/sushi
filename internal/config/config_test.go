package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestReplacedValuesAreReported(t *testing.T) {
	dir := useTempHome(t)
	yaml := "icon_mode: emoji\nsort_by: date\nopener: vim\npreview_width: 0\nshow_hidden: true\ntheme: neon\n"
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0644)

	cfg := LoadConfig()
	want := []string{
		`icon_mode: unknown value "emoji", using nerd`,
		`opener: unknown value "vim", using auto`,
		`sort_by: unknown value "date", using name`,
		`preview_width: invalid value 0, using 1 (it is 1 to 80)`,
	}
	if !reflect.DeepEqual(cfg.Problems, want) {
		t.Fatalf("problems = %q, want %q", cfg.Problems, want)
	}
	if cfg.IconMode != "nerd" || cfg.SortBy != "name" || cfg.Opener != "auto" || cfg.PreviewWidth != 1 || !cfg.ShowHidden {
		t.Fatalf("got %+v", cfg)
	}
	// The theme is left for the app to check against the themes it has
	if cfg.Theme != "neon" {
		t.Fatalf("theme = %q", cfg.Theme)
	}

	// Values that are fine, or left empty, aren't problems
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("icon_mode: ascii\nsort_by: \"\"\npreview_width: 80\n"), 0644)
	if cfg := LoadConfig(); len(cfg.Problems) != 0 || cfg.IconMode != "ascii" || cfg.SortBy != "name" {
		t.Fatalf("problems = %q, icon_mode=%s sort_by=%s", cfg.Problems, cfg.IconMode, cfg.SortBy)
	}
}

func TestUnknownSettingsAreReported(t *testing.T) {
	dir := useTempHome(t)
	yaml := "shw_hidden: true\nsort_by: size\ncolour:\n  accent: red\nwhatever: 1\n"
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0644)

	// Reported by line, with the setting meant if it is a typo, and the
	// settings after them still apply
	cfg := LoadConfig()
	want := []string{
		"config.yaml: line 1: unknown setting shw_hidden (did you mean show_hidden?)",
		"config.yaml: line 3: unknown setting colour (did you mean colors?)",
		"config.yaml: line 5: unknown setting whatever",
	}
	if !reflect.DeepEqual(cfg.Problems, want) {
		t.Fatalf("problems = %q, want %q", cfg.Problems, want)
	}
	if cfg.ShowHidden || cfg.SortBy != "size" {
		t.Fatalf("show_hidden=%v sort_by=%s", cfg.ShowHidden, cfg.SortBy)
	}

	// Every setting the file can have is known, as sushi --init-config
	// writes them
	if err := DefaultConfig().Save(); err != nil {
		t.Fatal(err)
	}
	if cfg := LoadConfig(); len(cfg.Problems) != 0 {
		t.Fatalf("problems with the config sushi writes: %q", cfg.Problems)
	}
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("keys:\n  up: k\nplugins: []\ncolors: {}\nwatch: false\n"), 0644)
	if cfg := LoadConfig(); len(cfg.Problems) != 0 {
		t.Fatalf("problems = %q", cfg.Problems)
	}
}

func TestUnreadableConfigIsReported(t *testing.T) {
	dir := useTempHome(t)
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte("show_hidden: true\n"), 0644)
	os.Chmod(path, 0)
	t.Cleanup(func() { os.Chmod(path, 0644) })
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("the file can be read without permission here, as by root")
	}

	cfg := LoadConfig()
	if len(cfg.Problems) != 1 || !strings.Contains(cfg.Problems[0], "config.yaml: failed to read it") || cfg.ShowHidden {
		t.Fatalf("problems = %q, show_hidden = %v; want the defaults, and why", cfg.Problems, cfg.ShowHidden)
	}

	// A folder in its place can't be read either
	os.Remove(path)
	os.Mkdir(path, 0755)
	if cfg := LoadConfig(); len(cfg.Problems) != 1 || !strings.Contains(cfg.Problems[0], "failed to read it") {
		t.Fatalf("problems = %q", cfg.Problems)
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

func TestConfigProblemsAreKept(t *testing.T) {
	dir := useTempHome(t)

	// A value of the wrong kind keeps its default, and the rest is read
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("show_hidden: true\npreview_width: wide\n"), 0644)
	cfg := LoadConfig()
	if !cfg.ShowHidden || cfg.PreviewWidth != DefaultConfig().PreviewWidth {
		t.Fatalf("show_hidden=%v preview_width=%d", cfg.ShowHidden, cfg.PreviewWidth)
	}
	if len(cfg.Problems) != 1 || !strings.Contains(cfg.Problems[0], "config.yaml: line 2") {
		t.Fatalf("problems = %q", cfg.Problems)
	}

	// YAML that can't be read at all leaves the defaults, and says so
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("show_hidden: true\n  sort_by: [\n"), 0644)
	cfg = LoadConfig()
	if cfg.ShowHidden || len(cfg.Problems) != 1 || !strings.Contains(cfg.Problems[0], "config.yaml") {
		t.Fatalf("show_hidden=%v problems=%q", cfg.ShowHidden, cfg.Problems)
	}
}
