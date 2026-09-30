package components

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/icichainz/sushi/internal/utils"
)

// maxLineBytes caps each previewed line: the pane never shows that much,
// and a minified file can be one enormous line
const maxLineBytes = 4096

// readLines reads the first maxLines lines of a text file and counts the
// rest without keeping them, so a large file costs no more memory than a
// small one. Tabs become spaces and carriage returns go, as neither has a
// fixed width in a terminal.
func readLines(path string, maxLines int) ([]string, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	r := bufio.NewReaderSize(f, 64*1024)
	var lines []string
	var line []byte
	cut := false
	for len(lines) < maxLines {
		chunk, err := r.ReadSlice('\n')
		if room := maxLineBytes - len(line); room < len(chunk) {
			chunk, cut = chunk[:max(room, 0)], true
		}
		line = append(line, chunk...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue // The line goes on past the buffer
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, 0, err
		}
		if err != nil && len(line) == 0 {
			return lines, len(lines), nil // Ended with a newline
		}
		lines = append(lines, textLine(line, cut))
		line, cut = line[:0], false
		if err != nil {
			return lines, len(lines), nil // Ended without a newline
		}
	}

	rest, err := countLines(r)
	if err != nil {
		return nil, 0, err
	}
	return lines, len(lines) + rest, nil
}

// textLine turns a line as read into one ready to show
func textLine(b []byte, cut bool) string {
	b = bytes.TrimSuffix(b, []byte("\n"))
	// A cut line may end partway through a character
	if cut {
		for i := 1; i <= utf8.UTFMax && i <= len(b); i++ {
			if utf8.RuneStart(b[len(b)-i]) {
				if !utf8.FullRune(b[len(b)-i:]) {
					b = b[:len(b)-i]
				}
				break
			}
		}
	}
	s := strings.ReplaceAll(string(b), "\r", "")
	// Other control characters could move the cursor or change colors, as
	// highlighting leaves them in place
	return utils.Printable(strings.ReplaceAll(s, "\t", "    "))
}

// countLines counts the lines left in r, including a last one without a
// newline
func countLines(r io.Reader) (int, error) {
	buf := make([]byte, 64*1024)
	n := 0
	last := byte('\n')
	for {
		k, err := r.Read(buf)
		if k > 0 {
			n += bytes.Count(buf[:k], []byte("\n"))
			last = buf[k-1]
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, err
		}
	}
	if last != '\n' {
		n++
	}
	return n, nil
}

// withLines sets the lines of a preview that isn't a file's text
func withLines(p PreviewContent, lines ...string) PreviewContent {
	p.Lines = lines
	p.Content = strings.Join(lines, "\n")
	return p
}

// detailsView shows what is known about a file whose content isn't shown:
// when it was modified, and why
func detailsView(p PreviewContent, note string) PreviewContent {
	return withLines(p, "Modified  "+p.FileInfo.ModTime.Format("2006-01-02 15:04:05"), "", note)
}

// spaces returns n spaces, or none for n <= 0
func spaces(n int) string {
	return strings.Repeat(" ", max(n, 0))
}
