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
