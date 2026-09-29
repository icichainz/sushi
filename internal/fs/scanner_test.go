package fs

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
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

	files, err := ScanDirectory(root, ScanOptions{})
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

func names(files []FileInfo) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Name
	}
	return out
}

func TestScanHidesDotfilesUnlessAsked(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, ".secret"), nil, 0644)
	os.WriteFile(filepath.Join(root, "visible"), nil, 0644)

	files, _ := ScanDirectory(root, ScanOptions{})
	if got := names(files); !slices.Equal(got, []string{"visible"}) {
		t.Fatalf("default listing = %v, want [visible]", got)
	}
	files, _ = ScanDirectory(root, ScanOptions{ShowHidden: true})
	if got := names(files); !slices.Equal(got, []string{".secret", "visible"}) {
		t.Fatalf("ShowHidden listing = %v", got)
	}
}

func TestSortFiles(t *testing.T) {
	now := time.Now()
	files := []FileInfo{
		{Name: "zebra.txt", Size: 10, ModTime: now.Add(-3 * time.Hour)},
		{Name: "Apple.md", Size: 300, ModTime: now.Add(-1 * time.Hour)},
		{Name: "banana.go", Size: 20, ModTime: now},
		{Name: "docs", IsDir: true, ModTime: now.Add(-5 * time.Hour)},
		{Name: "Build", IsDir: true, ModTime: now},
	}

	cases := []struct {
		by      string
		reverse bool
		want    []string
	}{
		{"name", false, []string{"Build", "docs", "Apple.md", "banana.go", "zebra.txt"}},
		{"name", true, []string{"docs", "Build", "zebra.txt", "banana.go", "Apple.md"}},
		{"size", false, []string{"Build", "docs", "Apple.md", "banana.go", "zebra.txt"}},
		{"size", true, []string{"Build", "docs", "zebra.txt", "banana.go", "Apple.md"}},
		{"modified", false, []string{"Build", "docs", "banana.go", "Apple.md", "zebra.txt"}},
		{"type", false, []string{"Build", "docs", "banana.go", "Apple.md", "zebra.txt"}},
	}
	for _, c := range cases {
		sorted := slices.Clone(files)
		SortFiles(sorted, c.by, c.reverse)
		if got := names(sorted); !slices.Equal(got, c.want) {
			t.Errorf("sort by %s (reverse=%v) = %v, want %v", c.by, c.reverse, got, c.want)
		}
	}
}
