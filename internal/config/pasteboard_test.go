package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPasteboardSetting(t *testing.T) {
	if !DefaultConfig().Pasteboard {
		t.Fatal("the pasteboard should be shared by default")
	}

	dir := useTempHome(t)
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("sort_by: size\n"), 0644)
	if cfg := LoadConfig(); !cfg.Pasteboard || len(cfg.Problems) > 0 {
		t.Fatalf("a config without pasteboard: should keep the default (problems %q)", cfg.Problems)
	}

	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("pasteboard: false\n"), 0644)
	if cfg := LoadConfig(); cfg.Pasteboard || len(cfg.Problems) > 0 {
		t.Fatalf("pasteboard: false was ignored (problems %q)", cfg.Problems)
	}
}
