package components

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/ui"
)

func TestLargeTextFilesAreReadOnlyAsFarAsShown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.log")
	var b strings.Builder
	for i := 1; i <= 5000; i++ {
		fmt.Fprintf(&b, "line %d\r\n", i)
	}
	b.WriteString("last line without a newline")
	os.WriteFile(path, []byte(b.String()), 0644)

	p := LoadPreview(fileInfo(t, path), 100)
	if len(p.Lines) != 100 || p.Total != 5001 {
		t.Fatalf("read %d lines of %d, want 100 of 5001", len(p.Lines), p.Total)
	}
	if plain := strings.Split(p.Content, "\n"); plain[0] != "line 1" || plain[99] != "line 100" {
		t.Fatalf("lines = %q ... %q", plain[0], plain[99])
	}
}

func TestLineCounts(t *testing.T) {
	for content, want := range map[string]int{
		"":        0,
		"a":       1,
		"a\n":     1,
		"a\nb":    2,
		"a\n\n":   2,
		"\n":      1,
		"\t\r\n":  1,
		"a\nb\nc": 3,
	} {
		path := filepath.Join(t.TempDir(), "f.txt")
		os.WriteFile(path, []byte(content), 0644)
		for _, maxLines := range []int{1, 100} {
			lines, total, err := readLines(path, maxLines)
			if err != nil || total != want || len(lines) != min(want, maxLines) {
				t.Errorf("%q with max %d: %d lines, total %d (%v), want total %d", content, maxLines, len(lines), total, err, want)
			}
		}
	}
}

func TestLongLinesAreCutWholeCharacters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "minified.js")
	// Two-byte characters, offset by one byte so the cut lands mid-character
	line := "x" + strings.Repeat("é", maxLineBytes)
	os.WriteFile(path, []byte(line+"\nsecond\n"), 0644)

	p := LoadPreview(fileInfo(t, path), 100)
	if p.Total != 2 || len(p.Lines) != 2 {
		t.Fatalf("Total=%d lines=%d, want 2", p.Total, len(p.Lines))
	}
	if first := p.Content[:strings.Index(p.Content, "\n")]; len(first) > maxLineBytes || !utf8.ValidString(first) {
		t.Fatalf("first line is %d bytes, valid UTF-8: %v", len(first), utf8.ValidString(first))
	}
	if !strings.HasSuffix(p.Content, "second") {
		t.Fatalf("the line after the long one is lost: %q", p.Content[len(p.Content)-20:])
	}
}

func TestSymlinkShowsItsTarget(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "target.txt"), []byte("hello"), 0644)
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink("target.txt", link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	info, _ := os.Lstat(link)

	p := LoadPreview(fs.NewFileInfo(link, info), 100)
	if lines := render(t, p, 80, 3, 0); !strings.Contains(lines[0], "link.txt  → target.txt") || !strings.Contains(lines[1], "hello") {
		t.Fatalf("preview:\n%s", strings.Join(lines, "\n"))
	}

	old := ui.GetIconMode()
	ui.SetIconMode(ui.IconModeASCII)
	defer ui.SetIconMode(old)
	if lines := render(t, p, 80, 3, 0); !strings.Contains(lines[0], "link.txt  -> target.txt") {
		t.Fatalf("ascii heading = %q", lines[0])
	}

	// Links to directories too; the scanner reports them as directories
	os.Mkdir(filepath.Join(dir, "real"), 0755)
	os.Symlink("real", filepath.Join(dir, "shortcut"))
	info, _ = os.Lstat(filepath.Join(dir, "shortcut"))
	dirLink := fs.NewFileInfo(filepath.Join(dir, "shortcut"), info)
	dirLink.IsDir = true
	if lines := render(t, LoadPreview(dirLink, 100), 80, 3, 0); !strings.Contains(lines[0], "shortcut  -> real  Directory") {
		t.Fatalf("directory link heading = %q", lines[0])
	}
}
