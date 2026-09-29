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
