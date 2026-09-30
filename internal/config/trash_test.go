package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteToTrashSetting(t *testing.T) {
	dir := useTempHome(t)
	if !LoadConfig().DeleteToTrash {
		t.Fatal("delete_to_trash should default to true")
	}

	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("confirm_delete: false\n"), 0644)
	if !LoadConfig().DeleteToTrash {
		t.Fatal("a config without delete_to_trash should keep the default")
	}

	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("delete_to_trash: false\n"), 0644)
	if cfg := LoadConfig(); cfg.DeleteToTrash || !cfg.ConfirmDelete {
		t.Fatalf("delete_to_trash: false not read: %+v", cfg)
	}
}
