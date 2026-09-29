package fs

import (
	"os"
	"path/filepath"
	"testing"
)

func findFile(t *testing.T, files []FileInfo, name string) FileInfo {
	t.Helper()
	for _, f := range files {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("%s not in listing", name)
	return FileInfo{}
}

func TestScanFollowsSymlinks(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "realdir"), 0755)
	os.WriteFile(filepath.Join(root, "big.txt"), make([]byte, 5000), 0644)
	os.Symlink(filepath.Join(root, "realdir"), filepath.Join(root, "dirlink"))
	os.Symlink(filepath.Join(root, "big.txt"), filepath.Join(root, "filelink"))
	os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "dangling"))

	files, err := ScanDirectory(root)
	if err != nil {
		t.Fatal(err)
	}

	if f := findFile(t, files, "dirlink"); !f.IsDir || !f.IsSymlink {
		t.Errorf("dirlink: IsDir=%v IsSymlink=%v, want both true", f.IsDir, f.IsSymlink)
	}
	if f := findFile(t, files, "filelink"); f.IsDir || !f.IsSymlink || f.Size != 5000 {
		t.Errorf("filelink: IsDir=%v IsSymlink=%v Size=%d, want file of target size 5000", f.IsDir, f.IsSymlink, f.Size)
	}
	if f := findFile(t, files, "dangling"); f.IsDir || !f.IsSymlink {
		t.Errorf("dangling: IsDir=%v IsSymlink=%v, want a plain symlink entry", f.IsDir, f.IsSymlink)
	}
}
