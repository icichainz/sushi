package fs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateName(t *testing.T) {
	for _, bad := range []string{"", "  ", ".", "..", "a/b", "nul\x00"} {
		if ValidateName(bad) == nil {
			t.Errorf("ValidateName(%q) accepted", bad)
		}
	}
	for _, good := range []string{"notes.txt", ".env", "réservé", "į-not-a-slash"} {
		if err := ValidateName(good); err != nil {
			t.Errorf("ValidateName(%q) = %v", good, err)
		}
	}
}

func TestRename(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(dir, "taken.txt"), []byte("t"), 0644)

	if _, err := Rename(filepath.Join(dir, "a.txt"), "taken.txt"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("rename onto existing file: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "taken.txt")); string(b) != "t" {
		t.Fatal("existing file was replaced")
	}

	got, err := Rename(filepath.Join(dir, "a.txt"), "b.txt")
	if err != nil || got != filepath.Join(dir, "b.txt") || !Exists(got) {
		t.Fatalf("Rename = %q, %v", got, err)
	}

	// Changing only the case must work even where the filesystem ignores case
	if _, err := Rename(got, "B.txt"); err != nil {
		t.Fatalf("case-only rename: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !strings.Contains(strings.Join(names, ","), "B.txt") {
		t.Fatalf("after case rename: %v", names)
	}
}

func TestCreateFileAndDir(t *testing.T) {
	dir := t.TempDir()

	path, err := CreateFile(dir, "src/app/main.go")
	if err != nil || !Exists(path) {
		t.Fatalf("CreateFile nested = %q, %v", path, err)
	}
	if _, err := CreateFile(dir, "src/app/main.go"); err == nil {
		t.Fatal("CreateFile replaced an existing file")
	}
	if _, err := CreateDir(dir, "build/out/"); err != nil || !Exists(filepath.Join(dir, "build", "out")) {
		t.Fatalf("CreateDir: %v", err)
	}
	if _, err := CreateDir(dir, "build"); err == nil {
		t.Fatal("CreateDir on an existing directory should fail")
	}

	for _, bad := range []string{"", "../escape", "a/../../b", "/abs/path"} {
		if _, err := CreateFile(dir, bad); err == nil {
			t.Errorf("CreateFile(%q) accepted", bad)
		}
	}
	if Exists(filepath.Join(filepath.Dir(dir), "escape")) {
		t.Fatal("file created outside the directory")
	}
}
