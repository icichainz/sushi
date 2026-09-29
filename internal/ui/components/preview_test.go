package components

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
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

func TestRenderPreviewIsExactlyPaneSized(t *testing.T) {
	long := strings.Repeat(strings.Repeat("word ", 60)+"\n", 300)
	for _, content := range []string{long, "short", ""} {
		out := RenderPreview(PreviewContent{Content: content}, 40, 12, lipgloss.NewStyle())
		lines := strings.Split(out, "\n")
		if len(lines) != 12 {
			t.Errorf("got %d lines, want 12", len(lines))
		}
		for i, l := range lines {
			if w := lipgloss.Width(l); w != 40 {
				t.Errorf("line %d is %d wide, want 40", i, w)
			}
		}
	}
}

func TestBinaryPreviewShowsDetails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image.png")
	os.WriteFile(path, []byte{0x89, 'P', 'N', 'G', 0, 0, 0, 0}, 0644)

	p := LoadPreview(fileInfo(t, path), 100)
	if p.IsText {
		t.Fatal("binary file detected as text")
	}
	for _, want := range []string{"PNG Image", "Size:", "Modified:", "Permissions:"} {
		if !strings.Contains(p.Content, want) {
			t.Errorf("binary preview missing %q:\n%s", want, p.Content)
		}
	}
}

func TestTextPreviewLimitsLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt")
	os.WriteFile(path, []byte(strings.Repeat("line\n", 150)), 0644)

	p := LoadPreview(fileInfo(t, path), 100)
	if !p.IsText || !strings.Contains(p.Content, "more lines") {
		t.Fatalf("expected a truncated text preview, got:\n%s", p.Content)
	}
}

func TestDirectoryPreviewListsEntries(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "inside.txt"), nil, 0644)

	p := LoadPreview(fileInfo(t, dir), 100)
	if !strings.Contains(p.Content, "inside.txt") || !strings.Contains(p.Content, "1 items") {
		t.Fatalf("directory preview = %q", p.Content)
	}
}
