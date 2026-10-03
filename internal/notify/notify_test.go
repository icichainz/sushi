package notify

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

// env returns a getenv that reads vars and nothing else
func env(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func TestKindFrom(t *testing.T) {
	for _, tc := range []struct {
		vars map[string]string
		want Host
	}{
		{map[string]string{"TERM_PROGRAM": "Sushi"}, SushiApp},
		{map[string]string{"TERM_PROGRAM": "iTerm.app"}, ITerm},
		{map[string]string{"TERM_PROGRAM": "WezTerm"}, ITerm},
		{map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, Bell},
		{map[string]string{}, Bell},
		// Over ssh nothing says which terminal it is
		{map[string]string{"SSH_TTY": "/dev/ttys001", "TERM": "xterm-256color"}, Bell},
		// kitty and WezTerm without TERM_PROGRAM
		{map[string]string{"KITTY_WINDOW_ID": "1", "TERM": "xterm-kitty"}, Kitty},
		{map[string]string{"WEZTERM_PANE": "0"}, ITerm},
		// TERM_PROGRAM, when set, is the terminal the shell is in
		{map[string]string{"TERM_PROGRAM": "Apple_Terminal", "KITTY_WINDOW_ID": "1"}, Bell},
		// tmux and screen pass on a bell, and nothing else for certain,
		// whatever the terminal outside them
		{map[string]string{"TERM_PROGRAM": "iTerm.app", "TMUX": "/tmp/tmux-501/default,1,0"}, Bell},
		{map[string]string{"TERM_PROGRAM": "Sushi", "TMUX": "/tmp/tmux-501/default,1,0"}, Bell},
		{map[string]string{"TERM_PROGRAM": "tmux", "TERM": "tmux-256color"}, Bell},
		{map[string]string{"TERM_PROGRAM": "Sushi", "TERM": "screen-256color"}, Bell},
		{map[string]string{"KITTY_WINDOW_ID": "1", "STY": "1234.pts-0.host"}, Bell},
	} {
		if got := KindFrom(env(tc.vars)); got != tc.want {
			t.Errorf("%v: %v, want %v", tc.vars, got, tc.want)
		}
	}
}

func TestKindReadsTheEnvironment(t *testing.T) {
	for _, name := range []string{"TERM", "TMUX", "STY", "KITTY_WINDOW_ID", "WEZTERM_PANE"} {
		t.Setenv(name, "")
	}
	t.Setenv("TERM_PROGRAM", "Sushi")
	if Kind() != SushiApp || !InApp() {
		t.Fatalf("TERM_PROGRAM=Sushi: %v", Kind())
	}
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	if Kind() != Bell || InApp() {
		t.Fatalf("TERM_PROGRAM=Apple_Terminal: %v", Kind())
	}
}

func TestSanitize(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Copied: 3 items", "Copied: 3 items"},
		{"", ""},
		{"   ", ""},
		// What would end the sequence, or start another
		{"a\x1b]0;evil\x07b", "a ]0,evil b"},
		{"a\u009cb\u009bc", "a b c"},
		// The same as single bytes, as 8-bit terminals read them, aren't UTF-8
		{"a\x9cb", "a�b"},
		{"del\x7fete", "del ete"},
		// What the Sushi app and OSC fields split on
		{"a|b;c", "a b,c"},
		// Lines and runs of white space close up
		{"  line one\n\tline two\r\n", "line one line two"},
		{"a\u2028b\u00a0\u00a0c", "a b c"},
		// Not UTF-8
		{"caf\xe9", "caf\ufffd"},
		// Other scripts and emoji are kept
		{"Copié 🍣 ファイル", "Copié 🍣 ファイル"},
	} {
		if got := Sanitize(tc.in); got != tc.want {
			t.Errorf("Sanitize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	// Long text is cut short, at a character
	long := Sanitize(strings.Repeat("é", 500))
	if n := utf8.RuneCountInString(long); n != maxText+1 || !strings.HasSuffix(long, "é…") || !utf8.ValidString(long) {
		t.Fatalf("long text: %d characters, %q", n, long)
	}
	// A space due at the cut isn't left before the ellipsis
	if got := Sanitize(strings.Repeat("x", maxText-1) + " yz"); got != strings.Repeat("x", maxText-1)+"…" {
		t.Fatalf("cut at a space: %q", got)
	}
	if got := Sanitize(strings.Repeat("x", maxText) + "   \n"); got != strings.Repeat("x", maxText) {
		t.Fatalf("white space after the limit: %q", got)
	}
}

func TestSequence(t *testing.T) {
	for _, tc := range []struct {
		host        Host
		title, body string
		want        string
	}{
		{SushiApp, "Sushi", "Copied: 3 items", "\x1b]1337;SushiNotify=Sushi|Copied: 3 items\a"},
		{SushiApp, "Sushi", "", "\x1b]1337;SushiNotify=Sushi|\a"},
		{ITerm, "Sushi", "Copied: 3 items", "\x1b]9;Sushi: Copied: 3 items\a"},
		{ITerm, "Sushi", "", "\x1b]9;Sushi\a"},
		{Kitty, "Sushi", "Copied: 3 items", "\x1b]99;i=sushi:d=0;Sushi\x1b\\\x1b]99;i=sushi:p=body;Copied: 3 items\x1b\\"},
		{Kitty, "Sushi", "", "\x1b]99;i=sushi;Sushi\x1b\\"},
		{Bell, "Sushi", "Copied: 3 items", "\a"},
		// Text without a title becomes the title
		{ITerm, "", "Copied: 3 items", "\x1b]9;Copied: 3 items\a"},
		{SushiApp, " \n", "Done", "\x1b]1337;SushiNotify=Done|\a"},
		// Nothing to say still rings
		{SushiApp, "", "", "\a"},
		{ITerm, "\x1b", "\n", "\a"},
		{Kitty, "", "", "\a"},
		// Both are sanitized
		{SushiApp, "Su|shi", "a\x07b;c|d", "\x1b]1337;SushiNotify=Su shi|a b,c d\a"},
		{ITerm, "Sushi", "bad\x1b\\name", "\x1b]9;Sushi: bad \\name\a"},
		{Kitty, "Sushi", "x\x1b\\y", "\x1b]99;i=sushi:d=0;Sushi\x1b\\\x1b]99;i=sushi:p=body;x \\y\x1b\\"},
	} {
		if got := Sequence(tc.host, tc.title, tc.body); got != tc.want {
			t.Errorf("Sequence(%v, %q, %q) = %q, want %q", tc.host, tc.title, tc.body, got, tc.want)
		}
	}
}

// failingWriter fails every write
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestSendWritesTheSequenceForTheHost(t *testing.T) {
	var buf bytes.Buffer
	old := out
	out = &buf
	t.Cleanup(func() { out = old })
	for _, name := range []string{"TERM", "TMUX", "STY", "KITTY_WINDOW_ID", "WEZTERM_PANE"} {
		t.Setenv(name, "")
	}

	t.Setenv("TERM_PROGRAM", "Sushi")
	if err := Send("Sushi", "Moved: notes.txt"); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "\x1b]1337;SushiNotify=Sushi|Moved: notes.txt\a" {
		t.Fatalf("in the app: %q", got)
	}

	buf.Reset()
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	if err := Send("Sushi", "Moved: notes.txt"); err != nil || buf.String() != "\a" {
		t.Fatalf("in Terminal.app: %q, %v", buf.String(), err)
	}

	out = failingWriter{}
	if err := Send("Sushi", "x"); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("a failed write: %v", err)
	}
}
