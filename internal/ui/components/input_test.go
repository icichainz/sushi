package components

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func keys(t *TextInput, msgs ...tea.KeyMsg) {
	for _, m := range msgs {
		t.Update(m)
	}
}

func typed(s string) tea.KeyMsg    { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
func key(k tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: k} }

func TestTextInputEditing(t *testing.T) {
	in := NewTextInput("report.txt")
	in.SetCursor(6)
	keys(&in, typed("-final"), key(tea.KeySpace))
	if got := in.Value(); got != "report-final .txt" {
		t.Fatalf("insert at cursor: %q", got)
	}
	keys(&in, key(tea.KeyBackspace), key(tea.KeyEnd), key(tea.KeyLeft), key(tea.KeyDelete))
	if got := in.Value(); got != "report-final.tx" {
		t.Fatalf("backspace/delete: %q", got)
	}
	words := NewTextInput("my big  file")
	words.Update(key(tea.KeyCtrlW))
	if got := words.Value(); got != "my big  " {
		t.Fatalf("ctrl+w should delete the word before the cursor: %q", got)
	}
	words.Update(key(tea.KeyCtrlW))
	if got := words.Value(); got != "my " {
		t.Fatalf("ctrl+w should skip spaces, then delete a word: %q", got)
	}
	keys(&in, key(tea.KeyHome), typed("é"), key(tea.KeyCtrlK))
	if got := in.Value(); got != "é" {
		t.Fatalf("ctrl+k: %q", got)
	}
	keys(&in, key(tea.KeyCtrlU))
	if got := in.Value(); got != "" {
		t.Fatalf("ctrl+u: %q", got)
	}
}

func TestTextInputDropsControlCharacters(t *testing.T) {
	in := NewTextInput("")
	in.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a\nb\tc"), Paste: true})
	if got := in.Value(); got != "abc" {
		t.Fatalf("pasted value = %q", got)
	}
}

func TestTextInputIgnoresOtherKeys(t *testing.T) {
	in := NewTextInput("x")
	if in.Update(key(tea.KeyEnter)) || in.Update(key(tea.KeyEsc)) {
		t.Fatal("Enter and Esc should be left to the caller")
	}
}

func TestTextInputViewKeepsCursorVisible(t *testing.T) {
	in := NewTextInput(strings.Repeat("a", 50) + "END")
	view := in.View(20, lipgloss.NewStyle(), lipgloss.NewStyle())
	if w := lipgloss.Width(view); w > 20 {
		t.Fatalf("view is %d wide, want at most 20", w)
	}
	if !strings.Contains(view, "END") {
		t.Fatalf("view %q scrolled away from the cursor", view)
	}
}
