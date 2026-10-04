package utils

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

func TestTruncateKeepsValidUTF8(t *testing.T) {
	for _, s := range []string{"réservé-élève.txt", "日本語のファイル名.txt", "plain-ascii-name.txt"} {
		for w := 0; w <= 20; w++ {
			got := Truncate(s, w)
			if !utf8.ValidString(got) {
				t.Fatalf("Truncate(%q, %d) = %q, invalid UTF-8", s, w, got)
			}
			if ansi.StringWidth(got) > w {
				t.Fatalf("Truncate(%q, %d) = %q, wider than %d", s, w, got, w)
			}
		}
	}
}

func TestTruncateLeftKeepsEnd(t *testing.T) {
	got := TruncateLeft("/Users/élève/projects/sushi", 12)
	if ansi.StringWidth(got) != 12 {
		t.Fatalf("TruncateLeft = %q (width %d), want width 12", got, ansi.StringWidth(got))
	}
	if !strings.HasPrefix(got, "...") || !strings.HasSuffix(got, "/sushi") {
		t.Fatalf("TruncateLeft = %q, lost the end of the path", got)
	}
	if got := TruncateLeft("short", 12); got != "short" {
		t.Fatalf("TruncateLeft(short) = %q", got)
	}
}

func TestHumanizeSize(t *testing.T) {
	cases := map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KB", 1536: "1.5 KB", 1 << 20: "1.0 MB", 5 << 30: "5.0 GB"}
	for in, want := range cases {
		if got := HumanizeSize(in); got != want {
			t.Errorf("HumanizeSize(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestPrintableReplacesControlsRuneForRune(t *testing.T) {
	for in, want := range map[string]string{
		"plain-name.txt": "plain-name.txt",
		"réservé 日本語 🍣 a\u200db \ufffd": "réservé 日本語 🍣 a\u200db \ufffd", // A real U+FFFD is kept
		"two\nlines.txt":                "two?lines.txt",
		"esc\x1b[2Jclear.txt":           "esc?[2Jclear.txt",
		"tab\tcr\rbell\anul\x00":        "tab?cr?bell?nul?",
		"del\x7f c1\u009b31m nel\u0085": "del? c1?31m nel?",
		"line\u2028para\u2029end":       "line?para?end",
		"bad\xff\xfeutf8\xc3":           "bad??utf8?",
		// Bidi overrides and isolates reorder what follows, as an
		// "invoice\u202efdp.exe" shows as invoiceexe.pdf
		"invoice\u202efdp.exe":                              "invoice?fdp.exe",
		"\u202a\u202b\u202c\u202d \u2066\u2067\u2068\u2069": "???? ????",
		"\u05e2\u05d1\u05e8\u05d9\u05ea \u200f mark":        "\u05e2\u05d1\u05e8\u05d9\u05ea \u200f mark", // Marks and right-to-left text are kept
	} {
		got := Printable(in)
		if got != want {
			t.Errorf("Printable(%q) = %q, want %q", in, got, want)
		}
		// Rune for rune, as []rune counts them, so positions still hold
		if !utf8.ValidString(got) || len([]rune(got)) != len([]rune(in)) {
			t.Errorf("Printable(%q) = %q: %d runes for %d", in, got, len([]rune(got)), len([]rune(in)))
		}
	}
}

func TestFitAndCellsGiveExactWidths(t *testing.T) {
	colored := "\x1b[31mréservé 日本語\x1b[0m plain"
	for _, s := range []string{"", "short", colored, strings.Repeat("字", 30)} {
		for w := 0; w <= 24; w++ {
			for name, got := range map[string]string{"Fit": Fit(s, w), "FitRight": FitRight(s, w), "Cells": Cells(s, 0, w), "Cells mid": Cells(s, 3, 3+w)} {
				if Width(got) != w {
					t.Fatalf("%s(%q, %d) = %q, %d wide", name, s, w, got, Width(got))
				}
			}
		}
	}
	if got := FitRight("42", 5); got != "   42" {
		t.Fatalf("FitRight = %q", got)
	}
	if got := Cells("abcdefgh", 2, 5); got != "cde" {
		t.Fatalf("Cells = %q", got)
	}
}
