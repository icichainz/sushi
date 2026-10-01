package app

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/config"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/pasteboard"
)

// started applies what sushi reads of the pasteboard when it starts, as
// Init has it do
func started(t *testing.T, m Model) Model {
	t.Helper()
	return drain(t, m, m.startPasteboard())
}

func TestCopyAndCutPutFilesOnThePasteboard(t *testing.T) {
	f := useFakeMac(t)
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		writeTestFile(t, filepath.Join(dir, name), name)
	}
	m := newTestModel(t, dir, nil)
	m, _ = press(t, m, " ")
	m, _ = press(t, m, " ")
	m, cmd := press(t, m, "c")
	if m.statusMsg != "Copied to clipboard: 2 items" {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
	m = drain(t, m, cmd)
	count, files := f.pasteboard()
	if want := []string{filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")}; !slices.Equal(files, want) {
		t.Fatalf("pasteboard holds %q, want %q", files, want)
	}
	if !m.pb.ours || m.pb.count != count || m.statusMsg != "Copied to clipboard: 2 items" {
		t.Fatalf("pb = %+v at count %d, statusMsg %q", m.pb, count, m.statusMsg)
	}

	// Finder has no cut, so a cut goes there as it is
	m, cmd = press(t, m, "x")
	m = drain(t, m, cmd)
	if _, files := f.pasteboard(); !slices.Equal(files, []string{filepath.Join(dir, "c.txt")}) || m.clipboardMode != "cut" {
		t.Fatalf("after a cut the pasteboard holds %q, mode %q", files, m.clipboardMode)
	}
}

func TestPasteTakesWhatFinderCopied(t *testing.T) {
	f := useFakeMac(t)
	src, dst := t.TempDir(), t.TempDir()
	report := filepath.Join(src, "report.pdf")
	writeTestFile(t, report, "pdf")
	os.Mkdir(filepath.Join(src, "photos"), 0755)
	writeTestFile(t, filepath.Join(src, "photos", "1.jpg"), "jpg")

	m := started(t, newTestModel(t, dst, nil))
	f.finderCopies(report, filepath.Join(src, "photos"))
	m, cmd := press(t, m, "v")
	if m.statusMsg != "Reading the pasteboard…" {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
	m = drain(t, m, cmd)
	if got := dirNames(t, dst); !slices.Equal(got, []string{"photos", "report.pdf"}) {
		t.Fatalf("pasted %v", got)
	}
	if !fs.Exists(report) || !fs.Exists(filepath.Join(dst, "photos", "1.jpg")) {
		t.Fatal("a paste from Finder should copy, keeping the originals")
	}
	if m.clipboardMode != "copy" || len(m.clipboard) != 2 || m.statusMsg != "Copied: 2 items" {
		t.Fatalf("clipboard %q (%s), statusMsg %q", m.clipboard, m.clipboardMode, m.statusMsg)
	}

	// Again, and the names are taken: it asks, as any paste does
	m, cmd = press(t, m, "v")
	m = drain(t, m, cmd)
	if m.mode != ModeConfirm || m.confirmAction != "paste" || len(m.pending) != 2 {
		t.Fatalf("mode %v, pending %q", m.mode, m.pending)
	}
	writeTestFile(t, report, "pdf, edited")
	m, cmd = press(t, m, "y")
	m = drain(t, m, cmd)
	if b, _ := os.ReadFile(filepath.Join(dst, "report.pdf")); string(b) != "pdf, edited" {
		t.Fatalf("overwritten with %q", b)
	}
}

func TestSushisClipboardWinsUntilThePasteboardChanges(t *testing.T) {
	f := useFakeMac(t)
	src, dst := t.TempDir(), t.TempDir()
	a, other := filepath.Join(src, "a.txt"), filepath.Join(src, "other.txt")
	writeTestFile(t, a, "a")
	writeTestFile(t, other, "other")

	// A cut in sushi is on the pasteboard too, but pasted in sushi it moves
	m := cursorTo(t, newTestModel(t, src, nil), "a.txt")
	m, cmd := press(t, m, "x")
	m = drain(t, at(t, m, dst), cmd)
	m, cmd = press(t, m, "v")
	m = drain(t, m, cmd)
	if fs.Exists(a) || !fs.Exists(filepath.Join(dst, "a.txt")) {
		t.Fatal("pasting sushi's cut should move it")
	}
	// What moved has left the clipboard, and the pasteboard still holds it
	// where it was, which isn't pasted
	m, cmd = press(t, m, "v")
	if m = drain(t, m, cmd); m.statusMsg != "Nothing in clipboard" {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}

	// Copied in Finder since: that is pasted
	f.finderCopies(other)
	m, cmd = press(t, m, "v")
	m = drain(t, m, cmd)
	if !fs.Exists(filepath.Join(dst, "other.txt")) || !fs.Exists(other) {
		t.Fatal("what Finder copied wasn't pasted")
	}

	// A copy in sushi whose write hasn't come in yet is the newer, and
	// pasted at once, without reading the pasteboard
	writeTestFile(t, filepath.Join(src, "new.txt"), "new")
	m = cursorTo(t, at(t, m, src), "new.txt")
	m, _ = press(t, m, "c") // Its write is never run
	f.finderCopies(other)
	reads := f.reads
	m = at(t, m, dst)
	m, cmd = press(t, m, "v")
	if m = drain(t, m, cmd); f.reads != reads || !fs.Exists(filepath.Join(dst, "new.txt")) {
		t.Fatalf("%d reads, pasted %v", f.reads-reads, dirNames(t, dst))
	}
	if files := m.fromPasteboard(pasteboard.Contents{Count: 1000, Files: []string{other}}); files != nil {
		t.Fatalf("took %q from the pasteboard over sushi's newer clipboard", files)
	}
}

func TestPasteReadsTheFilesOnlyWhenThePasteboardChanged(t *testing.T) {
	f := useFakeMac(t)
	src, dst := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(src, "a.txt"), "a")
	m := cursorTo(t, newTestModel(t, src, nil), "a.txt")
	m, cmd := press(t, m, "c")
	m = at(t, drain(t, m, cmd), dst)

	// The pasteboard holds what sushi put there: only its count is read
	f.files = []string{"/not/what/sushi/put/there"}
	m, cmd = press(t, m, "v")
	if m = drain(t, m, cmd); f.reads != 1 || !fs.Exists(filepath.Join(dst, "a.txt")) {
		t.Fatalf("%d reads, pasted %v, statusMsg %q", f.reads, dirNames(t, dst), m.statusMsg)
	}
}

func TestPasteboardOffOrFailing(t *testing.T) {
	f := useFakeMac(t)
	src, dst := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(src, "a.txt"), "a")
	writeTestFile(t, filepath.Join(dst, "a.txt"), "old")

	// Off: nothing goes there, and a paste asks at once, as it always has
	cfg := config.DefaultConfig()
	cfg.Pasteboard = false
	m := newTestModel(t, src, cfg)
	m, cmd := press(t, m, "c")
	if m = drain(t, m, cmd); m.pb.writeSeq != 0 {
		t.Fatal("the pasteboard was written with pasteboard: false")
	}
	m.tab().CurrentPath = dst
	if m, _ = press(t, m, "v"); m.mode != ModeConfirm {
		t.Fatalf("with the pasteboard off, mode = %v", m.mode)
	}
	if count, files := f.pasteboard(); count != 100 || files != nil {
		t.Fatalf("pasteboard touched: %d %q", count, files)
	}

	// Failing: copying says so, and pasting uses sushi's clipboard
	f.fail = errors.New("osascript: exit status 1: no pasteboard server")
	m = newTestModel(t, src, nil)
	m, cmd = press(t, m, "c")
	m = drain(t, m, cmd)
	if !strings.Contains(m.statusMsg, "Can't share the clipboard with Finder: osascript: exit status 1: no pasteboard server") {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
	m = at(t, m, dst)
	m, cmd = press(t, m, "v")
	if m = drain(t, m, cmd); m.mode != ModeConfirm || m.pasteDir != dst {
		t.Fatalf("with the pasteboard failing: mode %v, statusMsg %q", m.mode, m.statusMsg)
	}
	m, _ = press(t, m, "n")
	m.clipboard = nil
	m, cmd = press(t, m, "v")
	if m = drain(t, m, cmd); !strings.Contains(m.statusMsg, "Can't read the pasteboard") {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
	// A failed read says nothing of what was there when sushi started
	if m.pb.started {
		t.Fatalf("pb = %+v", m.pb)
	}
}

func TestPasteFromThePasteboardOnlyWhereItWasAsked(t *testing.T) {
	f := useFakeMac(t)
	src, dst, elsewhere := t.TempDir(), t.TempDir(), t.TempDir()
	report := filepath.Join(src, "report.pdf")
	writeTestFile(t, report, "pdf")

	// The folder changed while the pasteboard was read
	m := started(t, newTestModel(t, dst, nil))
	f.finderCopies(report, filepath.Join(src, "deleted since.txt"))
	m, cmd := press(t, m, "v")
	m = drain(t, at(t, m, elsewhere), cmd)
	if len(dirNames(t, dst))+len(dirNames(t, elsewhere)) != 0 || !strings.Contains(m.statusMsg, "folder shown changed") {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}

	// A dialog opened meanwhile: dropped, and said so
	m, cmd = press(t, m, "v")
	m, _ = press(t, m, "s")
	if m = drain(t, m, cmd); m.mode != ModeSort || len(dirNames(t, elsewhere)) != 0 || m.statusMsg != "Paste cancelled" {
		t.Fatalf("mode %v, pasted %v, statusMsg %q", m.mode, dirNames(t, elsewhere), m.statusMsg)
	}
	m, _ = press(t, m, "esc")

	// A second paste supersedes the first; files gone since are left out
	m, first := press(t, m, "v")
	m, second := press(t, m, "v")
	m = drain(t, drain(t, m, first), second)
	if got := dirNames(t, elsewhere); !slices.Equal(got, []string{"report.pdf"}) {
		t.Fatalf("pasted %v", got)
	}
}

func TestPasteboardOnlyOnMacOS(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "")
	m := newTestModel(t, dir, nil)
	m, cmd := press(t, m, "c")
	if m = drain(t, m, cmd); m.pb.writeSeq != 0 {
		t.Fatal("the pasteboard was written off macOS")
	}
	// A paste is from sushi's clipboard, at once
	if m, _ = press(t, m, "v"); m.pb.readSeq != 0 || !strings.Contains(m.statusMsg, "Can't paste") {
		t.Fatalf("readSeq %d, statusMsg %q", m.pb.readSeq, m.statusMsg)
	}
}

// readPasteboard presses key, and applies the pasteboard's contents it
// reads, returning the model then and what follows
func readPasteboard(t *testing.T, m Model, key string) (Model, tea.Cmd) {
	t.Helper()
	m, cmd := press(t, m, key)
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("%s didn't read the pasteboard", key)
	}
	for _, c := range batch {
		if c == nil {
			continue
		}
		if msg, ok := c().(pasteboardReadMsg); ok {
			updated, next := m.Update(msg)
			return updated.(Model), next
		}
	}
	t.Fatalf("%s didn't read the pasteboard", key)
	return m, nil
}

func TestPasteLeavesWhatWasCopiedBeforeSushiStarted(t *testing.T) {
	f := useFakeMac(t)
	src, dst := t.TempDir(), t.TempDir()
	old, report := filepath.Join(src, "old.txt"), filepath.Join(src, "report.pdf")
	writeTestFile(t, old, "copied last week")
	writeTestFile(t, report, "pdf")

	// On the pasteboard since before sushi started: not pasted, nor read
	f.finderCopies(old)
	m := started(t, newTestModel(t, dst, nil))
	m, cmd := press(t, m, "v")
	if m = drain(t, m, cmd); len(dirNames(t, dst)) != 0 || m.statusMsg != "Nothing in clipboard" {
		t.Fatalf("pasted %v, statusMsg %q", dirNames(t, dst), m.statusMsg)
	}
	if f.counts != 1 || f.reads != 1 {
		t.Fatalf("%d counts, %d reads", f.counts, f.reads)
	}

	// Copied in Finder since: pasted, saying where from
	f.finderCopies(report, old)
	m, cmd = readPasteboard(t, m, "v")
	if m.statusMsg != "Pasting 2 items copied in Finder" {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
	if m = drain(t, m, cmd); !slices.Equal(dirNames(t, dst), []string{"old.txt", "report.pdf"}) {
		t.Fatalf("pasted %v, statusMsg %q", dirNames(t, dst), m.statusMsg)
	}

	// A paste before the count at startup is known takes nothing either
	f.finderCopies(report)
	m = newTestModel(t, t.TempDir(), nil)
	startup := m.startPasteboard()
	m, cmd = press(t, m, "v")
	if m = drain(t, m, cmd); len(m.clipboard) != 0 || m.statusMsg != "Nothing in clipboard" {
		t.Fatalf("clipboard %q, statusMsg %q", m.clipboard, m.statusMsg)
	}
	f.finderCopies(report)
	if m = drain(t, m, startup); m.pb.startCount != f.count-1 {
		t.Fatalf("the late count replaced the one read first: %d", m.pb.startCount)
	}
}

func TestPasteLinksTakesWhatFinderCopied(t *testing.T) {
	f := useFakeMac(t)
	src, dst := t.TempDir(), t.TempDir()
	report := filepath.Join(src, "report.pdf")
	writeTestFile(t, report, "pdf")

	m := started(t, newTestModel(t, dst, nil))
	f.finderCopies(report)
	m, cmd := press(t, m, "V")
	m = drain(t, m, cmd)
	if target, err := os.Readlink(filepath.Join(dst, "report.pdf")); err != nil || target != report {
		t.Fatalf("link to %q (%v), statusMsg %q", target, err, m.statusMsg)
	}
	if !slices.Equal(m.clipboard, []string{report}) || m.clipboardMode != "copy" || !strings.HasPrefix(m.statusMsg, "Linked") {
		t.Fatalf("clipboard %q (%s), statusMsg %q", m.clipboard, m.clipboardMode, m.statusMsg)
	}
}

func TestReadingThePasteboardSaysSoUntilItIsIn(t *testing.T) {
	useFakeMac(t)
	src, dst := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(src, "a.txt"), "a")
	writeTestFile(t, filepath.Join(dst, "a.txt"), "old")
	var lasts []time.Duration
	defer func(old func(time.Duration, func(time.Time) tea.Msg) tea.Cmd) { statusTimer = old }(statusTimer)
	statusTimer = func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
		lasts = append(lasts, d)
		return func() tea.Msg { return nil }
	}

	m := started(t, newTestModel(t, src, nil))
	m, cmd := press(t, m, "c")
	m = at(t, drain(t, m, cmd), dst)

	// As long as the read may take, however slow osascript is
	lasts = nil
	m, cmd = press(t, m, "v")
	if m.statusMsg != "Reading the pasteboard…" || len(lasts) != 1 || lasts[0] < macTimeout {
		t.Fatalf("statusMsg %q for %v", m.statusMsg, lasts)
	}
	// Gone once it is in: here the paste asks before overwriting a.txt
	if m = drain(t, m, cmd); m.mode != ModeConfirm || strings.Contains(m.statusMsg, "Reading") {
		t.Fatalf("mode %v, statusMsg %q", m.mode, m.statusMsg)
	}
	m, _ = press(t, m, "n")

	// A message that came meanwhile stays
	m, cmd = press(t, m, "v")
	m.statusMsg, m.statusID = "Something else", m.statusID+1
	if m = drain(t, m, cmd); m.statusMsg != "Something else" {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
}
