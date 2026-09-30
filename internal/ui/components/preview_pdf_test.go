package components

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeTools points the PDF preview at stand-ins for pdftotext and pdfinfo
// running the given shell scripts; an empty script means not installed
func fakeTools(t *testing.T, toText, info string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-ins are shell scripts")
	}
	bin := t.TempDir()
	oldToText, oldInfo := pdfToText, pdfInfo
	t.Cleanup(func() { pdfToText, pdfInfo = oldToText, oldInfo })

	for name, script := range map[string]string{"pdftotext": toText, "pdfinfo": info} {
		path := filepath.Join(bin, name)
		if script != "" {
			if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0755); err != nil {
				t.Fatal(err)
			}
		}
		if name == "pdftotext" {
			pdfToText = path
		} else {
			pdfInfo = path
		}
	}
}

// writePDF writes a file the preview takes for a PDF; the stand-in tools
// don't read it
func writePDF(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "report.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.4\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPDFWithoutPopplerSaysHowToGetIt(t *testing.T) {
	fakeTools(t, "", "")
	p := LoadPreview(fileInfo(t, writePDF(t)), 100)
	view := strings.Join(render(t, p, 80, 6, 0), "\n")
	for _, want := range []string{"report.pdf", "PDF Document", "Modified", "Install poppler"} {
		if !strings.Contains(view, want) {
			t.Errorf("missing %q:\n%s", want, view)
		}
	}
}

func TestPDFTextIsShown(t *testing.T) {
	fakeTools(t,
		`printf 'First page text\n\n\fSecond\tpage \033[2J\n\f'`,
		`printf 'Title:          Report\nPages:          7\n'`)
	p := LoadPreview(fileInfo(t, writePDF(t)), 100)
	lines := render(t, p, 60, 8, 0)
	if !strings.Contains(lines[0], "PDF Document") || !strings.Contains(lines[0], "7 pages") {
		t.Fatalf("heading = %q", lines[0])
	}
	want := []string{"First page text", "", "--- page 2 ---", "", "Second    page ?[2J"}
	for i, w := range want {
		if got := strings.TrimSpace(lines[1+i]); got != w {
			t.Fatalf("row %d = %q, want %q:\n%s", 1+i, got, w, strings.Join(lines, "\n"))
		}
	}
}

func TestPDFProblemsAreReported(t *testing.T) {
	for _, c := range []struct{ script, want string }{
		{`echo "Syntax Error: broken xref" >&2; exit 1`, "pdftotext failed: exit status 1: Syntax Error: broken xref"},
		{`printf '\f\f'`, "No text in the first 5 pages"},
	} {
		fakeTools(t, c.script, "")
		p := LoadPreview(fileInfo(t, writePDF(t)), 100)
		if !strings.Contains(p.Content, c.want) || !strings.Contains(p.Content, "Modified") {
			t.Errorf("script %q: content %q, want %q", c.script, p.Content, c.want)
		}
	}
}

func TestSlowPDFTimesOut(t *testing.T) {
	fakeTools(t, "exec sleep 5", "")
	old := pdfTimeout
	pdfTimeout = 200 * time.Millisecond
	defer func() { pdfTimeout = old }()

	start := time.Now()
	p := LoadPreview(fileInfo(t, writePDF(t)), 100)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("took %s despite the timeout", elapsed)
	}
	if !strings.Contains(p.Content, "Reading the text took over 200ms") {
		t.Fatalf("content = %q", p.Content)
	}
}

func TestQuickLoadLeavesPDFsForLater(t *testing.T) {
	fakeTools(t, `echo text`, "")
	cfg := DefaultPreviewConfig()
	cfg.Quick = true
	if p := LoadPreviewWithConfig(fileInfo(t, writePDF(t)), cfg); !p.Pending || strings.Contains(p.Content, "text\n") {
		t.Fatalf("Pending=%v Content=%q", p.Pending, p.Content)
	}
}
