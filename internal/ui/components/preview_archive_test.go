package components

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// writeZip writes a zip of the given files; names ending in "/" are
// directories
func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := zip.NewWriter(f)
	for _, name := range sortedNames(files) {
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(entry, files[name])
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// writeTar writes a tar of the given files, gzipped if asked
func writeTar(t *testing.T, path string, gzipped bool, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out io.Writer = f
	if gzipped {
		gz := gzip.NewWriter(f)
		defer gz.Close()
		out = gz
	}
	w := tar.NewWriter(out)
	for _, name := range sortedNames(files) {
		h := &tar.Header{Name: name, Mode: 0644, Size: int64(len(files[name])), Typeflag: tar.TypeReg}
		if strings.HasSuffix(name, "/") {
			h.Typeflag, h.Mode = tar.TypeDir, 0755
		}
		if err := w.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		io.WriteString(w, files[name])
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func sortedNames(files map[string]string) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// tarBz2 is "docs/" and "docs/readme.txt" (6 bytes), made with
// tar -cjf, as the standard library can't write bzip2
const tarBz2 = "QlpoOTFBWSZTWQJTJvMAAGr7kNGQAEBAAf+AIAhuRp5ABAAACCAAkglU9Q9Q0ANDIBpoJKNCT00j1NNBgnqDWROtLyf9FwIK9IgWIArhFnhgHajwaJXJtvhSQkTHCImQ4XrOAxIeE3YDmKpl1H3yUTCqo94jkZSi1hg4acbRg/2aqEEQUGAw73MCB8UBHs+iN0A/F3JFOFCQAlMm8w=="

func TestArchivePreviewListsEntries(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"docs/":          "",
		"docs/readme.md": strings.Repeat("r", 100),
		"main.go":        strings.Repeat("m", 2048),
	}
	writeZip(t, filepath.Join(dir, "project.zip"), files)
	writeTar(t, filepath.Join(dir, "project.tar"), false, files)
	writeTar(t, filepath.Join(dir, "project.tar.gz"), true, files)
	writeTar(t, filepath.Join(dir, "project.tgz"), true, files)

	for name, kind := range map[string]string{
		"project.zip":    "ZIP Archive",
		"project.tar":    "TAR Archive",
		"project.tar.gz": "TAR.GZ Archive",
		"project.tgz":    "TAR.GZ Archive",
	} {
		p := LoadPreview(fileInfo(t, filepath.Join(dir, name)), 100)
		if !p.Archive || p.Count != 3 || p.More || p.Kind != kind {
			t.Fatalf("%s: Archive=%v Count=%d More=%v Kind=%q: %v", name, p.Archive, p.Count, p.More, p.Kind, p.Lines)
		}
		lines := render(t, p, 110, 6, 0)
		for _, want := range []string{name, kind, "3 entries", "2.1 KB unpacked"} {
			if !strings.Contains(lines[0], want) {
				t.Errorf("%s: heading %q is missing %q", name, lines[0], want)
			}
		}
		body := strings.Join(lines[1:], "\n")
		for _, want := range []string{"docs/", "docs/readme.md", "100 B", "main.go", "2.0 KB"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: listing is missing %q:\n%s", name, want, body)
			}
		}
		// Narrow panes drop the sizes rather than the names
		if narrow := render(t, p, 20, 4, 0); !strings.Contains(narrow[3], "main.go") {
			t.Errorf("%s: narrow listing = %q", name, narrow[3])
		}
	}

	bz2, _ := base64.StdEncoding.DecodeString(tarBz2)
	path := filepath.Join(dir, "docs.tar.bz2")
	os.WriteFile(path, bz2, 0644)
	p := LoadPreview(fileInfo(t, path), 100)
	if p.Kind != "TAR.BZ2 Archive" || p.Count != 2 || p.Entries[1].Name != "docs/readme.txt" || p.Entries[1].Size != 6 || !p.Entries[0].IsDir {
		t.Fatalf("tar.bz2: Kind=%q Count=%d Entries=%v: %v", p.Kind, p.Count, p.Entries, p.Lines)
	}
}

func TestArchivesThatCantBeExtractedArePreviewOnly(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{"a.txt": "a"}
	writeZip(t, filepath.Join(dir, "project.zip"), files)
	writeZip(t, filepath.Join(dir, "app.jar"), files)
	writeTar(t, filepath.Join(dir, "project.tgz"), true, files)
	bz2, _ := base64.StdEncoding.DecodeString(tarBz2)
	os.WriteFile(filepath.Join(dir, "docs.tar.bz2"), bz2, 0644)

	// X extracts what fs.ArchiveKind names; the preview lists more
	for name, previewOnly := range map[string]bool{"project.zip": false, "project.tgz": false, "app.jar": true, "docs.tar.bz2": true} {
		p := LoadPreview(fileInfo(t, filepath.Join(dir, name)), 100)
		heading := render(t, p, 110, 3, 0)[0]
		if !p.Archive || strings.Contains(heading, "preview only") != previewOnly || !strings.Contains(heading, "unpacked") {
			t.Errorf("%s: heading %q, want preview only: %v", name, heading, previewOnly)
		}
	}
}

func TestLongArchivesAreCut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "many.zip")
	files := make(map[string]string)
	for i := 0; i < maxArchiveEntries+100; i++ {
		files[fmt.Sprintf("file-%04d.txt", i)] = ""
	}
	writeZip(t, path, files)

	p := LoadPreview(fileInfo(t, path), 100)
	if len(p.Entries) != maxArchiveEntries || p.Count != maxArchiveEntries+100 || !p.More {
		t.Fatalf("listed %d of %d (More=%v)", len(p.Entries), p.Count, p.More)
	}
	lines := render(t, p, 60, 11, p.MaxScroll(11))
	if !strings.Contains(lines[0], "600 entries") || !strings.Contains(lines[10], "and 100 more") {
		t.Fatalf("heading %q, last row %q", lines[0], lines[10])
	}
}

func TestSlowTarScanStopsEarly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.tar.gz")
	writeTar(t, path, true, map[string]string{"a": "1", "b": "2", "c": "3"})
	old := archiveScanTime
	archiveScanTime = -1
	defer func() { archiveScanTime = old }()

	p := LoadPreview(fileInfo(t, path), 100)
	if !p.Partial || !p.More || p.Count != 1 {
		t.Fatalf("Partial=%v More=%v Count=%d, want the scan cut after one entry", p.Partial, p.More, p.Count)
	}
	lines := render(t, p, 60, 4, 0)
	if !strings.Contains(lines[0], "1+ entries") || strings.Contains(lines[0], "unpacked") || !strings.Contains(lines[2], "and more") {
		t.Fatalf("heading %q, rows %q", lines[0], lines[1:])
	}
}

func TestBrokenArchivesSayWhy(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"bad.zip", "bad.tar.gz", "bad.tar.bz2"} {
		path := filepath.Join(dir, name)
		os.WriteFile(path, []byte("this is not an archive at all, just some text"), 0644)
		p := LoadPreview(fileInfo(t, path), 100)
		if p.Error == nil || !strings.Contains(p.Content, "Cannot read archive") {
			t.Errorf("%s: Error=%v Content=%q", name, p.Error, p.Content)
		}
		render(t, p, 40, 5, 0)
	}

	empty := filepath.Join(dir, "empty.zip")
	writeZip(t, empty, nil)
	if lines := render(t, LoadPreview(fileInfo(t, empty), 100), 40, 3, 0); !strings.Contains(lines[1], "Empty archive") {
		t.Fatalf("empty archive: %q", lines[1])
	}
}

func TestArchiveNamesCantControlTheTerminal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evil.zip")
	writeZip(t, path, map[string]string{"evil\x1b[2Jname.txt": "x"})

	p := LoadPreview(fileInfo(t, path), 100)
	if p.Entries[0].Name != "evil?[2Jname.txt" {
		t.Fatalf("name = %q", p.Entries[0].Name)
	}
	for _, line := range RenderPreview(p, 60, 3, 0, plainStyles()) {
		if strings.Contains(line, "\x1b[2J") {
			t.Fatalf("escape code passed through: %q", line)
		}
	}
}

func TestQuickLoadLeavesArchivesForLater(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.zip")
	writeZip(t, path, map[string]string{"a.txt": "a"})
	cfg := DefaultPreviewConfig()
	cfg.Quick = true
	if p := LoadPreviewWithConfig(fileInfo(t, path), cfg); !p.Pending || p.Archive {
		t.Fatalf("Pending=%v Archive=%v", p.Pending, p.Archive)
	}
}
