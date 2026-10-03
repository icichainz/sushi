package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/config"
	"github.com/icichainz/sushi/internal/fs"
)

// recordNotifications replaces sendNotification for the test, returning
// the notifications sent, as "title|body"
func recordNotifications(t *testing.T) *[]string {
	t.Helper()
	var sent []string
	old := sendNotification
	sendNotification = func(title, body string) error {
		sent = append(sent, title+"|"+body)
		return nil
	}
	t.Cleanup(func() { sendNotification = old })
	return &sent
}

// startPaste copies a.txt from a new folder and starts pasting it into
// another, without running the job; it returns the model, the job's
// command, and the two folders
func startPaste(t *testing.T, cfg *config.Config) (Model, tea.Cmd, string, string) {
	t.Helper()
	src, dst := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(src, "a.txt"), "a")
	m := newTestModel(t, src, cfg)
	m, _ = press(t, m, "c")
	m.tab().CurrentPath = dst
	m, cmd := press(t, m, "v")
	if m.job == nil {
		t.Fatalf("the paste didn't start: %q", m.statusMsg)
	}
	return m, cmd, src, dst
}

// backdate makes the running job look as if it began ago
func backdate(m Model, ago time.Duration) Model {
	m.changeJob(func(j *job) { j.began = time.Now().Add(-ago) })
	return m
}

func TestLongOperationNotifiesWhenItFinishes(t *testing.T) {
	sent := recordNotifications(t)

	// Quicker than notify_after: the status bar is enough
	m, cmd, _, _ := startPaste(t, nil)
	m = drain(t, m, cmd)
	if m.statusMsg != "Copied: a.txt" || len(*sent) != 0 {
		t.Fatalf("a quick copy: status %q, notified %q", m.statusMsg, *sent)
	}

	// Longer: the notification says what the status bar says
	m, cmd, _, dst := startPaste(t, nil)
	m = drain(t, backdate(m, 6*time.Second), cmd)
	if !fs.Exists(filepath.Join(dst, "a.txt")) || len(*sent) != 1 || (*sent)[0] != "Sushi|Copied: a.txt" {
		t.Fatalf("a long copy notified %q", *sent)
	}

	// The threshold is the config's
	cfg := config.DefaultConfig()
	cfg.NotifyAfter = config.Duration(time.Minute)
	*sent = nil
	m, cmd, _, _ = startPaste(t, cfg)
	drain(t, backdate(m, 59*time.Second), cmd)
	m, cmd, _, _ = startPaste(t, cfg)
	drain(t, backdate(m, 61*time.Second), cmd)
	if len(*sent) != 1 {
		t.Fatalf("with notify_after: 1m, notified %q", *sent)
	}

	// And 0 turns it off
	cfg.NotifyAfter = 0
	*sent = nil
	m, cmd, _, _ = startPaste(t, cfg)
	drain(t, backdate(m, time.Hour), cmd)
	if len(*sent) != 0 {
		t.Fatalf("with notify_after: 0, notified %q", *sent)
	}
}

func TestFailedAndCancelledOperationsNotify(t *testing.T) {
	sent := recordNotifications(t)

	// Failed: the source went before the copy could start
	m, cmd, src, _ := startPaste(t, nil)
	if err := os.Remove(filepath.Join(src, "a.txt")); err != nil {
		t.Fatal(err)
	}
	m = drain(t, backdate(m, time.Minute), cmd)
	if !strings.HasPrefix(m.statusMsg, "Error: a.txt: ") {
		t.Fatalf("status %q, want the copy's error", m.statusMsg)
	}
	if len(*sent) != 1 || !strings.HasPrefix((*sent)[0], "Sushi|Copying failed: a.txt: ") {
		t.Fatalf("a failed copy notified %q", *sent)
	}

	// Cancelled with ctrl+x
	*sent = nil
	m, cmd, _, _ = startPaste(t, nil)
	m, _ = ctrl(t, backdate(m, time.Minute), tea.KeyCtrlX)
	m = drain(t, m, cmd)
	if len(*sent) != 1 || (*sent)[0] != "Sushi|"+m.statusMsg || !strings.HasPrefix(m.statusMsg, "Cancelled: ") {
		t.Fatalf("a cancelled copy: status %q, notified %q", m.statusMsg, *sent)
	}

	// Stopped to quit: sushi is going, and there is nobody to tell
	*sent = nil
	m, cmd, _, _ = startPaste(t, nil)
	m, _ = press(t, backdate(m, time.Minute), "q")
	run(t, m, cmd)
	if len(*sent) != 0 {
		t.Fatalf("quitting notified %q", *sent)
	}
}
