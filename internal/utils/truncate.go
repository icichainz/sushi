package utils

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

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

// Fit makes s exactly width cells wide: longer text is cut with "...",
// shorter text is padded with spaces on the right
func Fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = Truncate(s, width)
	return s + spaces(width-ansi.StringWidth(s))
}

// FitRight is Fit with the padding on the left, for right-aligned columns
func FitRight(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = Truncate(s, width)
	return spaces(width-ansi.StringWidth(s)) + s
}

// Width returns the number of terminal cells s occupies, ignoring ANSI codes
func Width(s string) int {
	return ansi.StringWidth(s)
}

// Cells returns the part of s between the cell columns from and to, padded
// to exactly to-from cells. A wide character cut by either edge is dropped.
func Cells(s string, from, to int) string {
	if to <= from {
		return ""
	}
	part := ansi.Cut(s, from, to)
	if ansi.StringWidth(part) > to-from {
		// A wide character straddles the left edge: replace its visible
		// half with a space
		part = " " + ansi.Cut(s, from+1, to)
	}
	part = Clip(part, to-from)
	return part + spaces(to-from-ansi.StringWidth(part))
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(" ", n)
}
