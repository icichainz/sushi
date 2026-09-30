package components

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Poppler's tools, run to preview PDFs. Variables so tests can use
// stand-ins.
var (
	pdfToText = "pdftotext"
	pdfInfo   = "pdfinfo"
	// pdfTimeout stops a slow or stuck conversion holding up the preview
	pdfTimeout = 3 * time.Second
)

const (
	// pdfPages is how many pages of a PDF have their text shown
	pdfPages = 5
	// maxToolOutput caps what is kept of a tool's output
	maxToolOutput = 1 << 20
)

// toolSlots limits how many external tools run at once: moving quickly
// through a folder of PDFs starts a conversion for each
var toolSlots = make(chan struct{}, 2)

// loadPDFPreview shows the text of a PDF's first pages, using pdftotext
// when it is installed
func loadPDFPreview(p PreviewContent, config PreviewConfig) PreviewContent {
	p.Kind = "PDF Document"
	tool, err := exec.LookPath(pdfToText)
	if err != nil {
		return detailsView(p, "Install poppler (pdftotext) to preview the text of PDFs")
	}
	if config.Quick {
		p.Pending = true
		return detailsView(p, "Loading...")
	}

	ctx, cancel := context.WithTimeout(context.Background(), pdfTimeout)
	defer cancel()
	if pages := pdfPageCount(ctx, p.Path); pages == 1 {
		p.Details = []string{"1 page"}
	} else if pages > 1 {
		p.Details = []string{fmt.Sprintf("%d pages", pages)}
	}

	// The path is absolute, so it can't be taken for an option
	out, err := runTool(ctx, tool, "-l", strconv.Itoa(pdfPages), "-enc", "UTF-8", p.Path, "-")
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return detailsView(p, fmt.Sprintf("Reading the text took over %s", pdfTimeout))
	case err != nil:
		return detailsView(p, fmt.Sprintf("pdftotext failed: %v", err))
	}

	lines := pdfLines(out, config.MaxLines)
	if len(lines) == 0 {
		return detailsView(p, fmt.Sprintf("No text in the first %d pages", pdfPages))
	}
	return withLines(p, lines...)
}

// pagesLine is the page count in pdfinfo's output
var pagesLine = regexp.MustCompile(`(?m)^Pages:\s+(\d+)`)

// pdfPageCount asks pdfinfo how many pages a PDF has, or returns 0 when
// it can't tell
func pdfPageCount(ctx context.Context, path string) int {
	tool, err := exec.LookPath(pdfInfo)
	if err != nil {
		return 0
	}
	out, err := runTool(ctx, tool, path)
	if err != nil {
		return 0
	}
	m := pagesLine.FindSubmatch(out)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(string(m[1]))
	return n
}

// runTool runs an external tool and returns what it printed, giving up
// when ctx ends
func runTool(ctx context.Context, name string, args ...string) ([]byte, error) {
	select {
	case toolSlots <- struct{}{}:
		defer func() { <-toolSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	cmd := exec.CommandContext(ctx, name, args...)
	// Once killed, don't wait long for anything it started to let go of
	// the output
	cmd.WaitDelay = 100 * time.Millisecond
	var stdout, stderr limitedBuffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		if msg := lastLine(stderr.String()); msg != "" {
			return nil, fmt.Errorf("%w: %s", err, msg)
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

// pdfLines turns pdftotext's output into preview lines, with a marker
// where each page after the first begins
func pdfLines(out []byte, maxLines int) []string {
	var lines []string
	for i, page := range strings.Split(string(out), "\f") {
		text := strings.TrimRight(strings.ReplaceAll(page, "\r", ""), "\n ")
		if text == "" {
			continue
		}
		if i > 0 && len(lines) > 0 {
			lines = append(lines, "", fmt.Sprintf("--- page %d ---", i+1), "")
		}
		for _, line := range strings.Split(text, "\n") {
			lines = append(lines, cleanText(strings.ReplaceAll(line, "\t", "    ")))
		}
		if len(lines) >= maxLines {
			return lines[:maxLines]
		}
	}
	return lines
}

// lastLine returns the last line of s that isn't blank
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// limitedBuffer keeps the first maxToolOutput bytes written to it and
// drops the rest, so a runaway tool can't use up memory
type limitedBuffer struct {
	bytes.Buffer
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := maxToolOutput - b.Len(); room > 0 {
		b.Buffer.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}
