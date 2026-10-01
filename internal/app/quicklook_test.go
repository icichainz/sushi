package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeQlmanage puts a stand-in for qlmanage alone on PATH, so the real one
// can't open a window. It writes the arguments it gets to a file named
// after its process, then waits as qlmanage does while its window is open.
func fakeQlmanage(t *testing.T) (bin string) {
	t.Helper()
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("uses a shell script")
	}
	bin = t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\n[ \"$1\" = warm ] && exit 0\nprintf '%%s\\n' \"$@\" > \"%s/$$.args\"\nexec %s 60\n", bin, sleep)
	warmUp(t, filepath.Join(bin, "qlmanage"), script)
	t.Setenv("PATH", bin)
	return bin
}

// wantQuickLook checks that w is a Quick Look window open on paths
func wantQuickLook(t *testing.T, bin string, w *quickLookWindow, paths ...string) {
	t.Helper()
	if !w.isOpen() {
		t.Fatalf("no Quick Look window open for %q", paths)
	}
	t.Cleanup(w.close)
	want := strings.Join(append([]string{"-p"}, paths...), "\n") + "\n"
	record := filepath.Join(bin, fmt.Sprintf("%d.args", w.proc.Pid))
	var got []byte
	eventually(t, "qlmanage to start", func() bool {
		got, _ = os.ReadFile(record)
		return string(got) == want
	})
}

// waitClosedWindow waits for a Quick Look window's qlmanage to exit
func waitClosedWindow(t *testing.T, w *quickLookWindow) {
	t.Helper()
	select {
	case <-w.exited:
	case <-time.After(10 * time.Second):
		t.Fatal("qlmanage is still running")
	}
}

func TestQuickLook(t *testing.T) {
	bin := fakeQlmanage(t)
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	writeTestFile(t, a, "a")
	writeTestFile(t, b, "b")

	m := newTestModel(t, dir, nil)
	m, _ = press(t, m, "i")
	first := m.quickLookWin
	wantQuickLook(t, bin, first, a)
	if m.statusMsg != "Quick Look: a.txt" {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}

	// On another file, the window shows that one instead
	m, _ = press(t, m, "j")
	m, _ = press(t, m, "i")
	waitClosedWindow(t, first)
	second := m.quickLookWin
	if second == first {
		t.Fatal("no new window")
	}
	wantQuickLook(t, bin, second, b)

	// Again on the same file, it closes
	m, _ = press(t, m, "i")
	waitClosedWindow(t, second)
	if m.quickLookWin != nil || m.statusMsg != "Quick Look closed" {
		t.Fatalf("window %v, statusMsg %q", m.quickLookWin, m.statusMsg)
	}

	// The selection, all of it at once
	m, _ = press(t, m, "k")
	m, _ = press(t, m, " ")
	m, _ = press(t, m, " ")
	m, _ = press(t, m, "i")
	third := m.quickLookWin
	wantQuickLook(t, bin, third, a, b)
	if m.statusMsg != "Quick Look: 2 items" {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}

	// Closed from its own window: i opens it again rather than close it
	third.proc.Kill()
	waitClosedWindow(t, third)
	m, _ = press(t, m, "i")
	fourth := m.quickLookWin
	wantQuickLook(t, bin, fourth, a, b)

	// Quitting closes it
	m.Close()
	waitClosedWindow(t, fourth)
}

func TestQuickLookWaitsForTheJob(t *testing.T) {
	fakeQlmanage(t)
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "")
	writeTestFile(t, filepath.Join(dir, "b.txt"), "")

	m := busy(t, dir, nil)
	m, _ = press(t, m, "i")
	if m.quickLookWin != nil || !strings.HasPrefix(m.statusMsg, "Still ") {
		t.Fatalf("window %v, statusMsg %q", m.quickLookWin, m.statusMsg)
	}
}

func TestQuickLookWithoutQlmanage(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "")

	m := newTestModel(t, dir, nil)
	m, _ = press(t, m, "i")
	if m.quickLookWin != nil || m.statusMsg != "Quick Look failed: qlmanage not found; Quick Look needs macOS" {
		t.Fatalf("window %v, statusMsg %q", m.quickLookWin, m.statusMsg)
	}
	m.Close() // Nothing to close, but safe

	// Nothing to look at
	m = newTestModel(t, t.TempDir(), nil)
	if m, _ = press(t, m, "i"); m.quickLookWin != nil || m.statusMsg != "" {
		t.Fatalf("in an empty directory: window %v, statusMsg %q", m.quickLookWin, m.statusMsg)
	}
}
