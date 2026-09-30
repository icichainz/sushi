package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestKeysSection(t *testing.T) {
	dir := useTempHome(t)
	yaml := `show_hidden: true
keys:
  up: [k, up]
  delete: d
  quit: [q, "ctrl+c"]
  help: "?"
  refresh: []
  hidden:
`
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0644)

	cfg := LoadConfig()
	want := map[string]KeyList{
		"up":      {"k", "up"},
		"delete":  {"d"}, // A single key needs no list
		"quit":    {"q", "ctrl+c"},
		"help":    {"?"},
		"refresh": {},  // Unbound
		"hidden":  nil, // No value at all, which the app reports
	}
	if !reflect.DeepEqual(cfg.Keys, want) {
		t.Fatalf("keys = %#v, want %#v", cfg.Keys, want)
	}
	if cfg.Keys["refresh"] == nil {
		t.Fatal("[] should be an empty list, not nil, so it can unbind an action")
	}
	if !cfg.ShowHidden || len(cfg.Problems) != 0 {
		t.Fatalf("show_hidden=%v problems=%q", cfg.ShowHidden, cfg.Problems)
	}
}

func TestMalformedKeysAreReported(t *testing.T) {
	dir := useTempHome(t)
	yaml := "keys:\n  up: {key: k}\n  down: [j, [x]]\n  left: [h]\nshow_hidden: true\n"
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0644)

	// Entries that are neither a key nor a list are reported by line, and
	// the rest of the file still applies
	cfg := LoadConfig()
	if len(cfg.Problems) != 2 || !strings.Contains(cfg.Problems[0], "line 2") || !strings.Contains(cfg.Problems[1], "line 3") {
		t.Fatalf("problems = %q", cfg.Problems)
	}
	if !cfg.ShowHidden || !reflect.DeepEqual(cfg.Keys, map[string]KeyList{"left": {"h"}}) {
		t.Fatalf("show_hidden=%v keys=%#v", cfg.ShowHidden, cfg.Keys)
	}
}
