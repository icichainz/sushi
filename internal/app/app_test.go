package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/config"
)

// newTestModel builds a sized model rooted at dir, with HOME redirected so
// bookmarks and config never touch the real user directory
func newTestModel(t *testing.T, dir string, cfg *config.Config) Model {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	m := NewModelWithConfig(dir, cfg)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	return updated.(Model)
}

// press sends a single key to the model
func press(t *testing.T, m Model, k string) (Model, tea.Cmd) {
	t.Helper()
	var msg tea.KeyMsg
	switch k {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "backspace":
		msg = tea.KeyMsg{Type: tea.KeyBackspace}
	case " ":
		msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	updated, cmd := m.Update(msg)
	return updated.(Model), cmd
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestPasteIntoSameDirDoesNotOfferOverwrite(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	writeTestFile(t, f, "important data")

	m := newTestModel(t, dir, nil)
	m, _ = press(t, m, "c")
	m, _ = press(t, m, "v")

	if m.mode == ModeConfirm {
		t.Fatal("paste onto itself opened the overwrite prompt")
	}
	if !strings.Contains(m.statusMsg, "Can't paste") {
		t.Fatalf("statusMsg = %q, want a refusal", m.statusMsg)
	}
	b, _ := os.ReadFile(f)
	if string(b) != "important data" {
		t.Fatalf("content = %q, file was damaged", b)
	}
}

func TestDirLoadGoesToRequestingTab(t *testing.T) {
	d1, d2 := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(d2, "only-in-d2.txt"), "x")

	m := newTestModel(t, d1, nil)
	updated, loadCmd := m.createTab(d2)
	m = updated.(Model)
	// Switch back to tab 1 before tab 2's directory finishes loading
	m, _ = press(t, m, "tab")
	updated, _ = m.Update(loadCmd().(tea.BatchMsg)[0]())
	m = updated.(Model)

	if n := len(m.tabs[0].Files); n != 0 {
		t.Fatalf("tab 1 received %d files from tab 2's load", n)
	}
	if m.tabs[1].Loading || len(m.tabs[1].Files) != 1 {
		t.Fatalf("tab 2: loading=%v files=%d, want loaded with 1 file", m.tabs[1].Loading, len(m.tabs[1].Files))
	}
}

func TestSupersededDirLoadIsDropped(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "a"), 0755)
	os.Mkdir(filepath.Join(root, "b"), 0755)

	m := newTestModel(t, root, nil)
	tab := m.tab()
	first := m.loadDir(tab, filepath.Join(root, "a"))
	second := m.loadDir(tab, filepath.Join(root, "b"))

	// The newer load finishes first; the older one must not overwrite it
	updated, _ := m.Update(second())
	updated, _ = updated.Update(first())
	m = updated.(Model)

	if got := filepath.Base(m.tab().CurrentPath); got != "b" {
		t.Fatalf("CurrentPath = %s, want b", got)
	}
}

func TestStalePreviewIsDropped(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "aaa")
	writeTestFile(t, filepath.Join(dir, "b.txt"), "bbb")

	m := newTestModel(t, dir, nil)
	staleCmd := loadPreview(m.tab().ID, m.tab().Files[0])
	m, cmd := press(t, m, "j")

	// b.txt's preview lands first, then a late one for a.txt
	updated, _ := m.Update(cmd())
	updated, _ = updated.Update(staleCmd())
	m = updated.(Model)
	if got := filepath.Base(m.tab().Preview.Path); got != "b.txt" {
		t.Fatalf("preview shows %s while the cursor is on b.txt", got)
	}
}

func TestRelativeStartPathCanGoUp(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	os.Mkdir(child, 0755)
	t.Chdir(child)

	m := newTestModel(t, "..", nil)
	if !filepath.IsAbs(m.tab().CurrentPath) {
		t.Fatalf("CurrentPath = %q, want absolute", m.tab().CurrentPath)
	}
	m, cmd := press(t, m, "h")
	updated, _ := m.Update(cmd())
	m = updated.(Model)

	want, _ := filepath.EvalSymlinks(filepath.Dir(root))
	got, _ := filepath.EvalSymlinks(m.tab().CurrentPath)
	if got != want {
		t.Fatalf("after h: %s, want %s", got, want)
	}
}

func TestGoingUpFocusesPreviousDir(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		os.Mkdir(filepath.Join(root, name), 0755)
	}

	m := newTestModel(t, filepath.Join(root, "c"), nil)
	m, cmd := press(t, m, "h")
	updated, _ := m.Update(cmd())
	m = updated.(Model)

	if got := m.tab().Files[m.tab().Cursor].Name; got != "c" {
		t.Fatalf("cursor on %q, want c", got)
	}
}

func TestFailedLoadKeepsCurrentDir(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "x")

	m := newTestModel(t, dir, nil)
	cmd := m.loadDir(m.tab(), filepath.Join(dir, "missing"))
	updated, _ := m.Update(cmd())
	m = updated.(Model)

	if m.tab().CurrentPath != dir || len(m.tab().Files) != 1 || m.tab().Loading {
		t.Fatalf("tab = %s with %d files (loading=%v), want %s unchanged", m.tab().CurrentPath, len(m.tab().Files), m.tab().Loading, dir)
	}
	if !strings.HasPrefix(m.statusMsg, "Error:") {
		t.Fatalf("statusMsg = %q, want an error", m.statusMsg)
	}
}

func TestStatusClearsOnlyItsOwnMessage(t *testing.T) {
	m := newTestModel(t, t.TempDir(), nil)
	m.setStatus("first")
	firstID := m.statusID
	m.setStatus("second")

	updated, _ := m.Update(clearStatusMsg{id: firstID})
	m = updated.(Model)
	if m.statusMsg != "second" {
		t.Fatalf("statusMsg = %q, older timer cleared the newer message", m.statusMsg)
	}

	updated, _ = m.Update(clearStatusMsg{id: m.statusID})
	if msg := updated.(Model).statusMsg; msg != "" {
		t.Fatalf("statusMsg = %q, want cleared", msg)
	}
}

func TestNewTabUsesConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.PreviewEnabled = false
	cfg.PreviewWidth = 30

	m := newTestModel(t, t.TempDir(), cfg)
	updated, _ := m.createTab(t.TempDir())
	tab := updated.(Model).tabs[1]
	if tab.PreviewEnabled || tab.PreviewWidth != 30 {
		t.Fatalf("new tab preview=%v width=%d, want false/30", tab.PreviewEnabled, tab.PreviewWidth)
	}
}
