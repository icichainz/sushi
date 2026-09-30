package fonts

import (
	"archive/zip"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func writeZip(t *testing.T, path string, names ...string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, name := range names {
		w, _ := zw.Create(name)
		w.Write([]byte("font data"))
	}
	zw.Close()
	f.Close()
}

func TestExtractFontsReportsExistingAndNew(t *testing.T) {
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "fonts.zip")
	writeZip(t, zipPath, "A-Regular.ttf", "sub/B-Bold.otf", "README.md", "../../evil.ttf")
	dest := filepath.Join(tmp, "fonts")
	os.Mkdir(dest, 0755)
	os.WriteFile(filepath.Join(dest, "A-Regular.ttf"), []byte("old"), 0644)

	paths, added, err := extractFonts(zipPath, dest)
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, p := range paths {
		got = append(got, filepath.Base(p))
	}
	slices.Sort(got)
	// Existing fonts are still reported so they can be registered;
	// archive paths are flattened, so "../" can't escape the font dir
	if want := []string{"A-Regular.ttf", "B-Bold.otf", "evil.ttf"}; !slices.Equal(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
	if added != 2 {
		t.Fatalf("added = %d, want 2", added)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "A-Regular.ttf")); string(b) != "old" {
		t.Fatal("existing font was overwritten")
	}
	if _, err := os.Stat(filepath.Join(tmp, "evil.ttf")); err == nil {
		t.Fatal("zip entry escaped the destination directory")
	}
}

func TestFindFont(t *testing.T) {
	if f, ok := FindFont("firacode"); !ok || f.Name != "FiraCode" {
		t.Fatalf("FindFont(firacode) = %v, %v", f, ok)
	}
	if _, ok := FindFont("Comic Sans"); ok {
		t.Fatal("found a font that isn't available")
	}
}
