package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTagsSetting(t *testing.T) {
	if !DefaultConfig().Tags {
		t.Fatal("Finder tags should be on by default")
	}

	dir := useTempHome(t)
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("sort_by: size\n"), 0644)
	if cfg := LoadConfig(); !cfg.Tags || len(cfg.Problems) > 0 {
		t.Fatalf("a config without tags: should keep the default (problems %q)", cfg.Problems)
	}

	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("tags: false\n"), 0644)
	if cfg := LoadConfig(); cfg.Tags || len(cfg.Problems) > 0 {
		t.Fatalf("tags: false was ignored (problems %q)", cfg.Problems)
	}
}
