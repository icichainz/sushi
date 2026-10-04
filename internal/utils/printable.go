package utils

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Placeholder is drawn in place of a character that can't be shown
const Placeholder = '?'

// Printable makes text from outside sushi safe to draw: file names and
// paths, file contents, error messages and plugin output. A name is only
// bytes to the file system, and drawn as it is, a newline breaks the
// layout and an escape code can clear the screen or change colors. So
// control characters (C0 including ESC, DEL, C1), line and paragraph
// separators, the bidi embeddings, overrides and isolates that reorder
// what follows them, and bytes that aren't UTF-8 each become Placeholder. It
// replaces rune for rune, as []rune counts runes, so positions found in
// the original, like search matches, still hold. Other characters,
// whatever the script, are kept.
func Printable(s string) string {
	// Most text is fine as it is, and needn't be copied
	i := 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !keep(r, size) {
			break
		}
		i += size
	}
	if i == len(s) {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))
	b.WriteString(s[:i])
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if keep(r, size) {
			b.WriteString(s[i : i+size])
		} else {
			b.WriteRune(Placeholder)
		}
		i += size
	}
	return b.String()
}

// keep reports whether a rune decoded with the given size can be drawn
// as it is. An invalid byte decodes as RuneError of size 1; a real U+FFFD
// is three bytes and is kept.
func keep(r rune, size int) bool {
	switch {
	case r == utf8.RuneError && size == 1:
		return false
	case unicode.IsControl(r), r == '\u2028', r == '\u2029':
		return false
	case r >= '\u202a' && r <= '\u202e', r >= '\u2066' && r <= '\u2069':
		// Bidi embeddings, overrides and isolates reorder what is drawn
		// after them, as "invoice\u202efdp.exe" shows as invoiceexe.pdf,
		// and can carry on past the name into the rest of the row
		return false
	}
	return true
}
