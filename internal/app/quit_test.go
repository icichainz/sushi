package app

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// quits reports whether cmd quits the program
func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// cwdFile makes an empty file, as the shell wrapper's mktemp does
func cwdFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cwd")
	writeTestFile(t, path, "")
	return path
}

func TestQuitWritesTheDirectoryForTheShell(t *testing.T) {
	root := t.TempDir()
	name := "my dir\nwith a newline\n"
	if runtime.GOOS == "windows" {
		name = "my dir"
	}
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Join(dir, "inner"), 0755); err != nil {
		t.Fatal(err)
	}

	m := newTestModel(t, root, nil)
	m, cmd := press(t, m, "l") // Into the awkwardly named directory
	m = drain(t, m, cmd)
	m, cmd = press(t, m, "q")
	if !quits(cmd) {
		t.Fatal("q did not quit")
	}

	file := cwdFile(t)
	if err := m.WriteExitDir(file); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(file); string(b) != dir {
		t.Fatalf("wrote %q, want exactly %q", b, dir)
	}
}

func TestQuitWithoutChangingDirectory(t *testing.T) {
	m := newTestModel(t, t.TempDir(), nil)
	m, cmd := press(t, m, "Q")
	if !quits(cmd) {
		t.Fatal("Q did not quit")
	}
	if m.ExitDir() != "" {
		t.Fatalf("ExitDir = %q after Q, want none", m.ExitDir())
	}
	file := cwdFile(t)
	if err := m.WriteExitDir(file); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(file); len(b) != 0 {
		t.Fatalf("wrote %q after Q, want nothing", b)
	}

	// Also from the key panel, which passes the key on
	m = newTestModel(t, t.TempDir(), nil)
	m, _ = press(t, m, "?")
	if m, cmd = press(t, m, "Q"); !quits(cmd) || m.ExitDir() != "" {
		t.Fatal("Q from the key panel should quit without a directory")
	}
}

func TestExitDirIsTheActiveTabs(t *testing.T) {
	d1, d2 := t.TempDir(), t.TempDir()
	m := newTestModel(t, d1, nil)
	updated, cmd := m.createTab(d2)
	m = drain(t, updated.(Model), cmd)
	if m.ExitDir() != d2 {
		t.Fatalf("ExitDir = %s, want the active tab's %s", m.ExitDir(), d2)
	}

	// Closing the last tab quits like q does
	m, _ = pressKey(t, m, tea.KeyCtrlW)
	m, cmd = pressKey(t, m, tea.KeyCtrlW)
	if !quits(cmd) || m.ExitDir() != d1 {
		t.Fatalf("closing the last tab: ExitDir = %q, want %s", m.ExitDir(), d1)
	}
}

func TestWriteExitDirReportsFailure(t *testing.T) {
	m := newTestModel(t, t.TempDir(), nil)
	if err := m.WriteExitDir(filepath.Join(t.TempDir(), "missing", "cwd")); err == nil {
		t.Fatal("writing into a missing directory should fail")
	}
}
