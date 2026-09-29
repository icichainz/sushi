package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/fs"
)

// targets returns the paths an operation applies to: the selection if there
// is one, otherwise the file under the cursor
func (m *Model) targets() []string {
	tab := m.tab()
	if len(tab.Selected) > 0 {
		paths := make([]string, 0, len(tab.Selected))
		for path := range tab.Selected {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		return paths
	}
	if len(tab.Files) == 0 {
		return nil
	}
	return []string{tab.Files[tab.Cursor].Path}
}

// describe names paths for status messages: "notes.txt" or "3 items"
func describe(paths []string) string {
	if len(paths) == 1 {
		return filepath.Base(paths[0])
	}
	return fmt.Sprintf("%d items", len(paths))
}

// inClipboard reports whether path is in the clipboard
func (m Model) inClipboard(path string) bool {
	for _, p := range m.clipboard {
		if p == path {
			return true
		}
	}
	return false
}

// toggleSelection selects or deselects the file under the cursor and moves down
func (m Model) toggleSelection() (tea.Model, tea.Cmd) {
	tab := m.tab()
	if len(tab.Files) == 0 {
		return m, nil
	}
	path := tab.Files[tab.Cursor].Path
	if tab.Selected[path] {
		delete(tab.Selected, path)
	} else {
		tab.Selected[path] = true
	}
	if tab.Cursor < len(tab.Files)-1 {
		tab.Cursor++
	}
	return m, m.previewCmd(tab)
}

// invertSelection flips the selection of every file in the current directory
func (m Model) invertSelection() (tea.Model, tea.Cmd) {
	tab := m.tab()
	for _, f := range tab.Files {
		if tab.Selected[f.Path] {
			delete(tab.Selected, f.Path)
		} else {
			tab.Selected[f.Path] = true
		}
	}
	cmd := m.setStatus(fmt.Sprintf("%d selected", len(tab.Selected)))
	return m, cmd
}

// clearSelection deselects everything in the current tab
func (m Model) clearSelection() (tea.Model, tea.Cmd) {
	clear(m.tab().Selected)
	cmd := m.setStatus("Selection cleared")
	return m, cmd
}

// yank puts the targets in the clipboard for a later paste
func (m Model) yank(mode string) (tea.Model, tea.Cmd) {
	paths := m.targets()
	if len(paths) == 0 {
		return m, nil
	}
	m.clipboard = paths
	m.clipboardMode = mode
	clear(m.tab().Selected)

	verb := "Copied"
	if mode == "cut" {
		verb = "Cut"
	}
	cmd := m.setStatus(fmt.Sprintf("%s: %s", verb, describe(paths)))
	return m, cmd
}

// startDelete deletes the targets, asking first unless confirm_delete is off
func (m Model) startDelete() (tea.Model, tea.Cmd) {
	paths := m.targets()
	if len(paths) == 0 {
		return m, nil
	}
	m.pending = paths
	if !m.config.ConfirmDelete {
		cmd := m.executeDelete()
		return m, cmd
	}
	m.confirmAction = "delete"
	m.mode = ModeConfirm
	return m, nil
}

// startPaste pastes the clipboard into the current directory, asking first
// if anything would be overwritten
func (m Model) startPaste() (tea.Model, tea.Cmd) {
	if len(m.clipboard) == 0 {
		cmd := m.setStatus("Nothing in clipboard")
		return m, cmd
	}

	conflicts, err := m.checkPaste(m.tab().CurrentPath)
	if err != nil {
		// Refuse up front rather than offering to overwrite a source with itself
		cmd := m.setStatus(fmt.Sprintf("Can't paste: %v", err))
		return m, cmd
	}
	if len(conflicts) > 0 {
		m.pending = conflicts
		m.confirmAction = "paste"
		m.mode = ModeConfirm
		return m, nil
	}
	cmd := m.executePaste()
	return m, cmd
}

// checkPaste validates pasting the clipboard into dir and returns the names
// that already exist there
func (m Model) checkPaste(dir string) ([]string, error) {
	var conflicts []string
	seen := make(map[string]string, len(m.clipboard))
	for _, src := range m.clipboard {
		name := filepath.Base(src)
		if other, ok := seen[name]; ok {
			return nil, fmt.Errorf("%s and %s have the same name", other, src)
		}
		seen[name] = src

		dst := filepath.Join(dir, name)
		if err := fs.CheckTransfer(src, dst); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if fs.Exists(dst) {
			conflicts = append(conflicts, name)
		}
	}
	return conflicts, nil
}

// executeDelete deletes the pending paths
func (m *Model) executeDelete() tea.Cmd {
	paths := m.pending
	m.pending = nil
	m.tab().Loading = true
	clear(m.tab().Selected)

	return func() tea.Msg {
		return batchResult("delete", "Deleted", paths, fs.DeletePath)
	}
}

// executePaste copies or moves the clipboard into the current directory
func (m *Model) executePaste() tea.Cmd {
	srcs := append([]string(nil), m.clipboard...)
	dir := m.tab().CurrentPath
	mode := m.clipboardMode
	m.pending = nil
	m.tab().Loading = true

	return func() tea.Msg {
		transfer, verb := fs.CopyPath, "Copied"
		if mode == "cut" {
			transfer, verb = fs.MovePath, "Moved"
		}
		return batchResult(mode, verb, srcs, func(src string) error {
			return transfer(src, filepath.Join(dir, filepath.Base(src)))
		})
	}
}

// batchResult runs op on each path and summarises the outcome. It carries on
// after a failure so one bad file doesn't block the rest.
func batchResult(operation, verb string, paths []string, op func(string) error) fileOperationMsg {
	done := 0
	var firstErr error
	for _, path := range paths {
		if err := op(path); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", filepath.Base(path), err)
			}
			continue
		}
		done++
	}

	msg := fileOperationMsg{operation: operation}
	switch {
	case firstErr == nil:
		msg.message = fmt.Sprintf("%s: %s", verb, describe(paths))
	case len(paths) == 1:
		msg.err = firstErr
	default:
		msg.err = fmt.Errorf("%s %d of %d items; %w", verb, done, len(paths), firstErr)
	}
	return msg
}

// pruneClipboard drops clipboard entries that no longer exist, e.g. after
// some of a cut have been moved
func (m *Model) pruneClipboard() {
	kept := m.clipboard[:0]
	for _, p := range m.clipboard {
		if _, err := os.Lstat(p); err == nil {
			kept = append(kept, p)
		}
	}
	m.clipboard = kept
	if len(m.clipboard) == 0 {
		m.clipboardMode = ""
	}
}

// deleteMessage describes what the pending delete will remove
func (m Model) deleteMessage() string {
	if len(m.pending) == 1 {
		path := m.pending[0]
		name := filepath.Base(path)
		info, err := os.Lstat(path)
		switch {
		case err == nil && info.Mode()&os.ModeSymlink != 0:
			return fmt.Sprintf("Delete symlink '%s'? Its target is not touched.", name)
		case err == nil && info.IsDir():
			return fmt.Sprintf("Delete directory '%s' and all its contents?", name)
		default:
			return fmt.Sprintf("Delete file '%s'?", name)
		}
	}
	return fmt.Sprintf("Delete %d items?\n\n%s", len(m.pending), listNames(m.pending, 5))
}

// pasteMessage describes what the pending paste will overwrite
func (m Model) pasteMessage() string {
	if len(m.pending) == 1 {
		return fmt.Sprintf("'%s' already exists. Overwrite?", m.pending[0])
	}
	return fmt.Sprintf("%d items already exist here. Overwrite them?\n\n%s", len(m.pending), listNames(m.pending, 5))
}

// listNames lists up to limit base names, one per line
func listNames(paths []string, limit int) string {
	names := make([]string, 0, limit+1)
	for i, p := range paths {
		if i == limit {
			names = append(names, fmt.Sprintf("…and %d more", len(paths)-limit))
			break
		}
		names = append(names, filepath.Base(p))
	}
	return strings.Join(names, "\n")
}
