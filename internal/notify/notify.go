// Package notify tells the terminal sushi runs in that something has
// finished, so a user who looked away while a long copy ran hears of it:
// the Sushi app and terminals that post desktop notifications get one, and
// the rest a bell, which Terminal.app can turn into a Dock badge or bounce.
//
// The notification is an escape sequence written straight to the terminal,
// as Bubble Tea v1 has no way to send one through its renderer. It is
// written in a single Write, as the renderer writes each frame, so the two
// don't interleave; call Send from the goroutine that runs Update.
package notify

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

// out is where sequences go: the terminal Bubble Tea draws on. Tests
// replace it, so they never ring the bell of the terminal running them.
var out io.Writer = os.Stdout

// Host is the kind of terminal sushi runs in, as far as notifications go
type Host int

const (
	// Bell is Terminal.app, tmux, screen, ssh and every terminal not known
	// to post notifications: they get a bell
	Bell Host = iota
	// SushiApp is the Sushi macOS app, which sets TERM_PROGRAM=Sushi and
	// posts the notification itself when its window isn't active
	SushiApp
	// ITerm is iTerm2 and WezTerm, which take iTerm2's OSC 9
	ITerm
	// Kitty is kitty, which has a protocol of its own, OSC 99
	Kitty
)

func (h Host) String() string {
	switch h {
	case SushiApp:
		return "Sushi app"
	case ITerm:
		return "OSC 9 terminal"
	case Kitty:
		return "kitty"
	}
	return "bell"
}

// Kind says what sushi runs in, from the environment. Anything that needs
// to know whether sushi is inside the Sushi app asks this.
func Kind() Host {
	return KindFrom(os.Getenv)
}

// KindFrom is Kind with the environment read through getenv
func KindFrom(getenv func(string) string) Host {
	// tmux and screen swallow sequences they don't know unless passthrough
	// is set up, which can't be assumed, and pass a bell on to the terminal
	// tab or window they run in. Their variables may sit beside a
	// TERM_PROGRAM inherited from the terminal outside, so they come first.
	term := getenv("TERM")
	if getenv("TMUX") != "" || getenv("STY") != "" || strings.HasPrefix(term, "screen") || strings.HasPrefix(term, "tmux") {
		return Bell
	}
	switch getenv("TERM_PROGRAM") {
	case "Sushi":
		return SushiApp
	case "iTerm.app", "WezTerm":
		return ITerm
	case "":
		// kitty doesn't set TERM_PROGRAM, and WezTerm may not have where
		// its shell was started some other way
		switch {
		case getenv("KITTY_WINDOW_ID") != "":
			return Kitty
		case getenv("WEZTERM_PANE") != "":
			return ITerm
		}
	}
	return Bell
}

// maxText is the most characters of a title or body sent: a notification
// shows a line or two, and terminals may cap how long an OSC can be
const maxText = 200

// Sanitize makes text safe to put in an escape sequence. Control
// characters (C0 including ESC and BEL, DEL, C1), which would end the
// sequence early or start another, become spaces, as do newlines and
// other white space, and '|', which separates the title from the body for
// the Sushi app; ';', which separates the fields of OSC sequences, becomes
// ','. Bytes that aren't UTF-8 become U+FFFD. Runs of spaces are closed
// up, the ends trimmed, and text longer than maxText characters is cut
// short with "…".
func Sanitize(s string) string {
	var b strings.Builder
	b.Grow(min(len(s), maxText*utf8.UTFMax))
	space := false // A space is due before the next character
	n := 0         // Characters written
	for _, r := range s {
		// range decodes a byte that isn't UTF-8 as U+FFFD, kept as that
		switch {
		case r == ';':
			r = ','
		case r == '|', unicode.IsControl(r), unicode.IsSpace(r):
			space = n > 0
			continue
		}
		width := 1
		if space {
			width = 2
		}
		if n+width > maxText {
			b.WriteString("…")
			break
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
		n += width
	}
	return b.String()
}

// Sequence returns what to write to the terminal for a notification with
// the given title and body, for host h; both are sanitized first. Text
// without a title moves up into it. With no text at all, every host gets
// the bell, so it still hears that something finished.
func Sequence(h Host, title, body string) string {
	title, body = Sanitize(title), Sanitize(body)
	if title == "" {
		title, body = body, ""
	}
	if title == "" {
		return "\a"
	}
	switch h {
	case SushiApp:
		// The app reads OSC 1337 ; SushiNotify=<title>|<body>, the
		// separator there even when the body is empty. Both are
		// percent-encoded, so only ASCII is sent: SwiftTerm takes a UTF-8
		// byte in 0x80-0x9F that starts a read for a C1 control, which
		// ends the sequence and loses the notification.
		return "\x1b]1337;SushiNotify=" + url.PathEscape(title) + "|" + url.PathEscape(body) + "\a"
	case ITerm:
		// iTerm2's OSC 9 has the one string, which WezTerm shows as it is
		text := title
		if body != "" {
			text += ": " + body
		}
		return "\x1b]9;" + text + "\a"
	case Kitty:
		// OSC 99 sends the title and body as chunks of one notification,
		// tied together by their id; d=0 says more is to come.
		// o=unfocused shows it only if the user isn't looking at sushi's
		// window, as the Sushi app does.
		if body == "" {
			return "\x1b]99;i=sushi:o=unfocused;" + title + "\x1b\\"
		}
		return "\x1b]99;i=sushi:d=0:o=unfocused;" + title + "\x1b\\" + "\x1b]99;i=sushi:o=unfocused:p=body;" + body + "\x1b\\"
	}
	return "\a"
}

// Send notifies the terminal sushi runs in, or the Sushi app, with one
// Write of the sequence for the host
func Send(title, body string) error {
	if _, err := io.WriteString(out, Sequence(Kind(), title, body)); err != nil {
		return fmt.Errorf("notify: %w", err)
	}
	return nil
}

// DirectorySequence is the OSC 7 sequence that tells the Sushi app which
// directory is being shown, to title its window after it. Other terminals
// are left alone: they take OSC 7 for the shell's directory, and would
// open new tabs in the last folder sushi showed.
func DirectorySequence(h Host, hostname, dir string) string {
	if h != SushiApp || dir == "" {
		return ""
	}
	u := url.URL{Scheme: "file", Host: hostname, Path: dir}
	return "\x1b]7;" + u.String() + "\x07"
}

// SetDirectory tells the Sushi app which directory sushi is showing
func SetDirectory(dir string) error {
	hostname, _ := os.Hostname()
	seq := DirectorySequence(Kind(), hostname, dir)
	if seq == "" {
		return nil
	}
	_, err := io.WriteString(out, seq)
	return err
}
