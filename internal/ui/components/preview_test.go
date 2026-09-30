package components

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/icichainz/sushi/internal/fs"
)

func fileInfo(t *testing.T, path string) fs.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fs.NewFileInfo(path, info)
}

func plainStyles() PreviewStyles {
	s := lipgloss.NewStyle()
	return PreviewStyles{Title: s, Text: s, Faint: s, Dir: s, Error: s}
}

// render returns the preview as plain text lines, checking its size
func render(t *testing.T, p PreviewContent, width, height, scroll int) []string {
	t.Helper()
	lines := RenderPreview(p, width, height, scroll, plainStyles())
	if len(lines) != height {
		t.Fatalf("got %d lines, want %d", len(lines), height)
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != width {
			t.Fatalf("line %d is %d wide, want %d: %q", i, w, width, l)
		}
		lines[i] = ansi.Strip(l)
	}
	return lines
}

func TestRenderPreviewIsExactlyPaneSized(t *testing.T) {
	dir := t.TempDir()
	long := filepath.Join(dir, "réservé-"+strings.Repeat("long-name-", 8)+".go")
	os.WriteFile(long, []byte(strings.Repeat("package main // "+strings.Repeat("word ", 60)+"\n", 300)), 0644)
	empty := filepath.Join(dir, "empty.txt")
	os.WriteFile(empty, nil, 0644)
	binary := filepath.Join(dir, "a.png")
	os.WriteFile(binary, []byte{0, 1, 2}, 0644)

	var previews []PreviewContent
	for _, path := range []string{long, empty, binary, dir} {
		previews = append(previews, LoadPreview(fileInfo(t, path), 2000))
	}
	link := filepath.Join(dir, "link.go")
	if os.Symlink(long, link) == nil {
		info, _ := os.Lstat(link)
		previews = append(previews, LoadPreview(fs.NewFileInfo(link, info), 2000))
	}

	// Every new kind too: images in both color modes, archives, a PDF
	wide := filepath.Join(dir, "wide.png")
	writeImage(t, wide, 300, 40, stripes)
	tall := filepath.Join(dir, "tall.png")
	writeImage(t, tall, 7, 90, stripes)
	for _, colors := range []ImageColors{ImagesTrueColor, Images256, ImagesOff} {
		cfg := DefaultPreviewConfig()
		cfg.Images = colors
		for _, path := range []string{wide, tall} {
			p := LoadPreviewWithConfig(fileInfo(t, path), cfg)
			if colors != ImagesOff && p.Image == nil {
				t.Fatalf("%s: not drawn with colors %d: %v", path, colors, p.Lines)
			}
			previews = append(previews, p)
		}
	}
	zipPath := filepath.Join(dir, "files.zip")
	writeZip(t, zipPath, map[string]string{"a/very/deeply/nested/" + strings.Repeat("long-", 20) + ".txt": "x", "b.go": "package b"})
	tgz := filepath.Join(dir, "files.tar.gz")
	writeTar(t, tgz, true, map[string]string{"c.txt": "c"})
	pdf := filepath.Join(dir, "doc.pdf")
	os.WriteFile(pdf, []byte("%PDF-1.4"), 0644)
	for _, path := range []string{zipPath, tgz, pdf} {
		previews = append(previews, LoadPreview(fileInfo(t, path), 2000))
	}

	for _, p := range previews {
		for _, size := range [][2]int{{40, 12}, {12, 3}, {3, 1}, {120, 40}, {1, 1}, {2, 5}, {7, 30}} {
			render(t, p, size[0], size[1], 0)
			render(t, p, size[0], size[1], 9999)
		}
	}
}

func TestTextPreviewHasHeadingAndLineNumbers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.go")
	os.WriteFile(path, []byte("package main\n\n/* a comment\nover two lines */\nfunc main() {\n\tprintln(1)\n}\n"), 0644)

	p := LoadPreview(fileInfo(t, path), 2000)
	if !p.IsText || p.Kind != "Go" || p.Total != 7 {
		t.Fatalf("IsText=%v Kind=%q Total=%d, want a 7-line Go file", p.IsText, p.Kind, p.Total)
	}
	lines := render(t, p, 60, 6, 0)
	for _, want := range []string{"main.go", "Go", "rw-r--r--", "1-5 of 7"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("heading %q is missing %q", lines[0], want)
		}
	}
	if !strings.Contains(lines[1], "1  package main") || !strings.Contains(lines[4], "4  over two lines */") {
		t.Fatalf("body:\n%s", strings.Join(lines, "\n"))
	}

	// Each line carries its own color, so the comment's second line is
	// still colored after the first has scrolled away
	if !strings.Contains(p.Lines[3], "\x1b[") {
		t.Fatalf("line 4 lost its highlighting: %q", p.Lines[3])
	}
	// Tabs become spaces, as their width in a terminal varies
	if strings.Contains(p.Content, "\t") {
		t.Fatal("tab left in the preview")
	}
}

func TestPreviewScrolls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt")
	var b strings.Builder
	for i := 1; i <= 50; i++ {
		b.WriteString("line " + strings.Repeat("x", i%7) + "\n")
	}
	os.WriteFile(path, []byte(b.String()), 0644)

	p := LoadPreview(fileInfo(t, path), 2000)
	if got := p.MaxScroll(11); got != 40 {
		t.Fatalf("MaxScroll = %d, want 40 (50 lines, 10 rows)", got)
	}
	lines := render(t, p, 40, 11, 20)
	if !strings.Contains(lines[0], "21-30 of 50") || !strings.Contains(lines[1], "21  line") {
		t.Fatalf("scrolled to 20:\n%s", strings.Join(lines, "\n"))
	}
	// Scrolling past the end stops at the last page
	lines = render(t, p, 40, 11, 500)
	if !strings.Contains(lines[10], "50  line") {
		t.Fatalf("last row = %q, want line 50", lines[10])
	}
}

func TestLongFilesAreCutWithANote(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.txt")
	os.WriteFile(path, []byte(strings.Repeat("line\n", 150)), 0644)

	p := LoadPreview(fileInfo(t, path), 100)
	if len(p.Lines) != 100 || p.Total != 150 {
		t.Fatalf("loaded %d of %d lines, want 100 of 150", len(p.Lines), p.Total)
	}
	lines := render(t, p, 40, 11, p.MaxScroll(11))
	if !strings.Contains(lines[10], "50 more lines not shown") {
		t.Fatalf("last row = %q", lines[10])
	}
}

func TestBinaryPreviewShowsDetails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image.png")
	os.WriteFile(path, []byte{0x89, 'P', 'N', 'G', 0, 0, 0, 0}, 0644)

	p := LoadPreview(fileInfo(t, path), 100)
	if p.IsText {
		t.Fatal("binary file detected as text")
	}
	view := strings.Join(render(t, p, 60, 8, 0), "\n")
	for _, want := range []string{"image.png", "PNG Image", "8 B", "rw-r--r--", "Modified", "Cannot preview"} {
		if !strings.Contains(view, want) {
			t.Errorf("binary preview missing %q:\n%s", want, view)
		}
	}
}

func TestDirectoryPreviewListsEntries(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "inside.txt"), nil, 0644)
	os.Mkdir(filepath.Join(dir, "zz-folder"), 0755)

	p := LoadPreview(fileInfo(t, dir), 100)
	lines := render(t, p, 50, 6, 0)
	if !strings.Contains(lines[0], "Directory") || !strings.Contains(lines[0], "2 items") {
		t.Fatalf("heading = %q", lines[0])
	}
	// Directories come first
	if !strings.Contains(lines[1], "zz-folder") || !strings.Contains(lines[2], "inside.txt") {
		t.Fatalf("body:\n%s", strings.Join(lines, "\n"))
	}

	empty := filepath.Join(dir, "zz-folder")
	if lines := render(t, LoadPreview(fileInfo(t, empty), 100), 50, 4, 0); !strings.Contains(lines[1], "Empty directory") {
		t.Fatalf("empty directory: %q", lines[1])
	}
}

func TestDirectoryPreviewIsSortedLikeTheList(t *testing.T) {
	dir := t.TempDir()
	// Made out of order, as a directory may list them in any
	for _, name := range []string{"b.txt", "Zeta", "a.txt", "C.txt", "beta", "Alpha", "c.txt"} {
		if strings.HasSuffix(name, ".txt") {
			os.WriteFile(filepath.Join(dir, name), nil, 0644)
		} else {
			os.Mkdir(filepath.Join(dir, name), 0755)
		}
	}
	p := LoadPreview(fileInfo(t, dir), 100)
	var got []string
	for _, e := range p.Entries {
		got = append(got, e.Name)
	}
	// Folders first, then by name ignoring case, with capitals first on a tie
	want := "Alpha beta Zeta a.txt b.txt C.txt c.txt"
	if ignoresCase(dir) {
		want = strings.TrimSuffix(want, " c.txt") // The same file as C.txt
	}
	if strings.Join(got, " ") != want {
		t.Fatalf("entries = %v, want %s", got, want)
	}
}

// ignoresCase reports whether dir's filesystem takes C.txt and c.txt for
// the same name, so only one of them was made
func ignoresCase(dir string) bool {
	entries, _ := os.ReadDir(dir)
	n := 0
	for _, e := range entries {
		if strings.EqualFold(e.Name(), "c.txt") {
			n++
		}
	}
	return n < 2
}

func TestLinksWithoutAnExtensionPreviewAsWhatTheyLeadTo(t *testing.T) {
	dir := t.TempDir()
	writeImage(t, filepath.Join(dir, "pic.png"), 4, 4, stripes)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644)
	os.WriteFile(filepath.Join(dir, "blob.exe"), []byte{0, 1, 2, 0}, 0644)
	for link, target := range map[string]string{"noext-link": "pic.png", "tool": "main.go", "run": "blob.exe", "gone": "missing.png"} {
		if err := os.Symlink(target, filepath.Join(dir, link)); err != nil {
			t.Skipf("can't make symlinks here: %v", err)
		}
	}

	// As the file list sees them
	files, err := fs.ScanDirectory(dir, fs.ScanOptions{SortBy: "name"})
	if err != nil {
		t.Fatal(err)
	}
	kinds := make(map[string]string)
	for _, f := range files {
		kinds[f.Name] = LoadPreview(f, 100).Kind
	}
	for name, want := range map[string]string{"noext-link": "PNG Image", "tool": "Go", "run": "Executable", "gone": "File"} {
		if kinds[name] != want {
			t.Errorf("%s previews as %q, want %q", name, kinds[name], want)
		}
	}
}

func TestEmptyFilePreviewsAsText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.txt")
	os.WriteFile(path, nil, 0644)

	p := LoadPreview(fileInfo(t, path), 100)
	if p.Error != nil || !p.IsText {
		t.Fatalf("empty file preview: error=%v text=%v", p.Error, p.IsText)
	}
	if lines := render(t, p, 40, 4, 0); !strings.Contains(lines[1], "Empty file") {
		t.Fatalf("body = %q", lines[1])
	}
}
