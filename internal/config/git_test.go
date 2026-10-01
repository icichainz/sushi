package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGitSetting(t *testing.T) {
	dir := useTempHome(t)
	if !DefaultConfig().Git {
		t.Fatal("Git badges should be on by default")
	}
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("show_hidden: true\n"), 0644)
	if !LoadConfig().Git {
		t.Fatal("a config file without git: should keep the badges on")
	}
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("git: false\n"), 0644)
	if cfg := LoadConfig(); cfg.Git || len(cfg.Problems) > 0 {
		t.Fatalf("git: false gave git=%v, problems %q", cfg.Git, cfg.Problems)
	}
}
