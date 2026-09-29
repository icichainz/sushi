package utils

import "github.com/charmbracelet/x/ansi"

// Truncate shortens s to at most width terminal cells, ending with "..." when
// cut. It measures display width, so multi-byte and wide characters are never
// split and ANSI escape codes are preserved.
func Truncate(s string, width int) string {
	return ansi.Truncate(s, max(width, 0), "...")
}

// TruncateLeft shortens s to at most width cells by cutting from the start,
// which keeps the most specific end of a path visible.
func TruncateLeft(s string, width int) string {
	w := ansi.StringWidth(s)
	if w <= width {
		return s
	}
	if width <= 3 {
		return Clip("...", width)
	}
	return ansi.TruncateLeft(s, w-width+3, "...")
}

// Clip cuts s to at most width cells without adding an ellipsis
func Clip(s string, width int) string {
	return ansi.Truncate(s, max(width, 0), "")
}
