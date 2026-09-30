package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWatchSetting(t *testing.T) {
	if !DefaultConfig().Watch {
		t.Fatal("watching should be on by default")
	}

	dir := useTempHome(t)
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("sort_by: size\n"), 0644)
	if !LoadConfig().Watch {
		t.Fatal("a config without watch: should keep the default")
	}

	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("watch: false\n"), 0644)
	if LoadConfig().Watch {
		t.Fatal("watch: false was ignored")
	}
}
