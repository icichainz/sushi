package app

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/fs"
)

// isToolKey reports whether msg is handled by handleToolKey
func (k KeyMap) isToolKey(msg tea.KeyMsg) bool {
	return key.Matches(msg, k.HardDelete, k.Undo, k.Cancel)
}

// handleToolKey handles the keys for undo and permanent delete in normal
// mode
func (m Model) handleToolKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.HardDelete):
		return m.startHardDelete()
	case key.Matches(msg, m.keys.Undo):
		return m.startUndo()
	case key.Matches(msg, m.keys.Cancel):
		// A running operation is cancelled by whileBusy
		cmd := m.setStatus("Nothing to cancel")
		return m, cmd
	}
	return m, nil
}

// startTrash moves the targets to the trash. It doesn't ask first, since
// ctrl+z brings them back.
func (m Model) startTrash() (tea.Model, tea.Cmd) {
	paths := m.targets()
	if len(paths) == 0 {
		return m, nil
	}
	clear(m.tab().Selected)

	cmd := m.startJob("Moving to trash", func(t *fs.Task) jobDoneMsg {
		tr, err := fs.DefaultTrash()
		if err != nil {
			return jobDoneMsg{op: fileOperationMsg{operation: "trash", err: fmt.Errorf("can't find the trash: %w", err)}}
		}
		undo := &undoEntry{label: "trash " + describe(paths)}
		op, _ := runBatch(t, "trash", "Moved to trash", paths, func(path string) error {
			item, err := tr.Put(t, path)
			if err != nil {
				return err
			}
			undo.steps = append(undo.steps, undoStep{kind: stepRestore, trashed: true, from: item.Path, to: item.Original, info: item.Info})
			return nil
		})
		return jobDoneMsg{op: op, undo: undo}
	})
	return m, cmd
}

// startHardDelete deletes the targets permanently. It always asks first,
// whatever confirm_delete says: this can't be undone.
func (m Model) startHardDelete() (tea.Model, tea.Cmd) {
	paths := m.targets()
	if len(paths) == 0 {
		return m, nil
	}
	m.pending = paths
	m.confirmAction = "delete"
	m.mode = ModeConfirm
	return m, nil
}
