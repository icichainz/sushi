package components

import (
	"slices"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/icichainz/sushi/internal/utils"
)

// TextInput is a single-line text field with a movable cursor
type TextInput struct {
	value  []rune
	cursor int // Position in runes; len(value) means after the last character
}

// NewTextInput returns an input holding value with the cursor at the end
func NewTextInput(value string) TextInput {
	r := []rune(value)
	return TextInput{value: r, cursor: len(r)}
}

// Value returns the current text
func (t TextInput) Value() string {
	return string(t.value)
}

// SetCursor moves the cursor to pos, clamped to the text
func (t *TextInput) SetCursor(pos int) {
	t.cursor = max(0, min(pos, len(t.value)))
}

// Update edits the text for a key press. It returns false for keys it
// doesn't handle, so the caller can use them.
func (t *TextInput) Update(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeySpace:
		t.value = slices.Insert(t.value, t.cursor, ' ')
		t.cursor++
	case tea.KeyRunes:
		// Drop control characters, e.g. newlines in pasted text
		runes := slices.DeleteFunc(slices.Clone(msg.Runes), unicode.IsControl)
		t.value = slices.Insert(t.value, t.cursor, runes...)
		t.cursor += len(runes)
	case tea.KeyBackspace:
		if t.cursor > 0 {
			t.value = slices.Delete(t.value, t.cursor-1, t.cursor)
			t.cursor--
		}
	case tea.KeyDelete:
		if t.cursor < len(t.value) {
			t.value = slices.Delete(t.value, t.cursor, t.cursor+1)
		}
	case tea.KeyLeft, tea.KeyCtrlB:
		t.SetCursor(t.cursor - 1)
	case tea.KeyRight, tea.KeyCtrlF:
		t.SetCursor(t.cursor + 1)
	case tea.KeyHome, tea.KeyCtrlA:
		t.cursor = 0
	case tea.KeyEnd, tea.KeyCtrlE:
		t.cursor = len(t.value)
	case tea.KeyCtrlU:
		// Delete to the start of the line
		t.value = slices.Delete(t.value, 0, t.cursor)
		t.cursor = 0
	case tea.KeyCtrlK:
		// Delete to the end of the line
		t.value = t.value[:t.cursor]
	case tea.KeyCtrlW:
		// Delete the word before the cursor
		start := t.cursor
		for start > 0 && unicode.IsSpace(t.value[start-1]) {
			start--
		}
		for start > 0 && !unicode.IsSpace(t.value[start-1]) {
			start--
		}
		t.value = slices.Delete(t.value, start, t.cursor)
		t.cursor = start
	default:
		return false
	}
	return true
}

// View renders the text with a block cursor within width cells, scrolling
// so the cursor stays visible
func (t TextInput) View(width int, style, cursorStyle lipgloss.Style) string {
	at, after := " ", ""
	if t.cursor < len(t.value) {
		at = string(t.value[t.cursor])
		after = string(t.value[t.cursor+1:])
	}
	atWidth := max(ansi.StringWidth(at), 1)

	before := utils.TruncateLeft(string(t.value[:t.cursor]), max(width-atWidth, 0))
	after = utils.Clip(after, max(width-ansi.StringWidth(before)-atWidth, 0))
	return style.Render(before) + cursorStyle.Render(at) + style.Render(after)
}
