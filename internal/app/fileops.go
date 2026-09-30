package app

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/utils"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// targets returns the paths an operation applies to: the selection if there
// is one, otherwise the file under the cursor. Paths are cleaned, as one
// with a trailing slash, as in "link/", names what a symlink points to.
func (m *Model) targets() []string {
	tab := m.tab()
	if len(tab.Selected) > 0 {
		paths := make([]string, 0, len(tab.Selected))
		seen := make(map[string]bool, len(tab.Selected))
		for path := range tab.Selected {
			if path = filepath.Clean(path); !seen[path] {
				seen[path] = true
				paths = append(paths, path)
			}
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

// startDelete moves the targets to the trash, or with delete_to_trash off,
// deletes them, asking first unless confirm_delete is off
func (m Model) startDelete() (tea.Model, tea.Cmd) {
	if m.config.DeleteToTrash {
		return m.startTrash()
	}
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

	dir := m.tab().CurrentPath
	conflicts, err := m.checkPaste(dir)
	if err != nil {
		// Refuse up front rather than offering to overwrite a source with itself
		cmd := m.setStatus(fmt.Sprintf("Can't paste: %v", err))
		return m, cmd
	}
	if len(conflicts) > 0 {
		m.pending = conflicts
		m.pasteDir = dir
		m.confirmAction = "paste"
		m.mode = ModeConfirm
		return m, nil
	}
	cmd := m.executePaste(dir)
	return m, cmd
}

// confirmPaste carries out the paste the overwrite dialog asked about, if
// it is still that paste. The dialog stays open while the tab moves on: a
// load lands, or the watcher finds the folder deleted and goes up, and
// other names can be taken meanwhile. Pasting into whatever is shown now,
// or over names nobody was asked about, could overwrite anything.
func (m *Model) confirmPaste() tea.Cmd {
	dir, asked := m.pasteDir, m.pending
	m.pasteDir, m.pending = "", nil
	why := ""
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		why = filepath.Base(dir) + " is gone"
	} else if m.tab().CurrentPath != dir {
		why = "The folder shown changed"
	} else if conflicts, err := m.checkPaste(dir); err != nil {
		return m.setStatus(fmt.Sprintf("Can't paste: %v", err))
	} else if !slices.Equal(conflicts, asked) {
		why = "What the paste would overwrite changed"
	}
	if why != "" {
		return m.setStatus(why + ", so nothing was pasted")
	}
	return m.executePaste(dir)
}

// nameKey folds a file name the way filesystems that ignore case and
// Unicode normalisation compare names (APFS and HFS+ by default, NTFS,
// FAT): "Report.txt" and "report.txt" name one file, and so do two
// spellings of "résumé" with each é one character or two
func nameKey(name string) string {
	return norm.NFC.String(cases.Fold().String(norm.NFC.String(name)))
}

// checkPaste validates pasting the clipboard into dir and returns the names
// that already exist there. Two items whose names differ only in case or
// normalisation are refused, whatever the filesystem: where they are the
// same name, the second would replace the first.
func (m Model) checkPaste(dir string) ([]string, error) {
	var conflicts []string
	seen := make(map[string]string, len(m.clipboard))
	for _, src := range m.clipboard {
		name := filepath.Base(src)
		if other, ok := seen[nameKey(name)]; ok {
			if filepath.Base(other) != name {
				return nil, fmt.Errorf("%s and %s have names that differ only in case or accents, the same name on many filesystems", other, src)
			}
			return nil, fmt.Errorf("%s and %s have the same name", other, src)
		}
		seen[nameKey(name)] = src

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

// executeDelete deletes the pending paths permanently, in the background
func (m *Model) executeDelete() tea.Cmd {
	paths := m.pending
	m.pending = nil
	clear(m.tab().Selected)

	return m.startJob("Deleting", func(t *fs.Task) jobDoneMsg {
		t.CountFiles(paths...)
		op, done := runBatch(t, "delete", "Deleted", paths, t.Delete)
		if done == 0 && t.Progress().Files == 0 {
			return jobDoneMsg{op: op}
		}
		// Nothing to undo, but ctrl+z says so rather than undoing something older
		return jobDoneMsg{op: op, undo: &undoEntry{label: "delete " + describe(paths), reason: "it was permanent"}}
	})
}

// executePaste copies or moves the clipboard into dir, in the background
func (m *Model) executePaste(dir string) tea.Cmd {
	srcs := append([]string(nil), m.clipboard...)
	mode := m.clipboardMode
	m.pending = nil

	doing, verb, label := "Copying", "Copied", "copy "
	if mode == "cut" {
		doing, verb, label = "Moving", "Moved", "move "
	}
	return m.startJob(doing, func(t *fs.Task) jobDoneMsg {
		// Moves are mostly renames, so they count only what they copy
		if mode != "cut" {
			t.Count(srcs...)
		}
		undo := &undoEntry{label: label + describe(srcs)}
		var made []os.FileInfo // What this paste has put in dir so far
		op, _ := runBatch(t, mode, verb, srcs, func(src string) error {
			dst := filepath.Join(dir, filepath.Base(src))
			// Something this paste has just put here under another spelling
			// of the name, which the filesystem takes for the same: pasting
			// would replace it, and undo would then put the wrong file back
			if info, err := os.Lstat(dst); err == nil && slices.ContainsFunc(made, func(p os.FileInfo) bool { return os.SameFile(p, info) }) {
				return fmt.Errorf("would replace %s, which this paste has just put there", filepath.Base(dst))
			}
			defer func() {
				if info, err := os.Lstat(dst); err == nil {
					made = append(made, info)
				}
			}()
			// Only what the paste created can be undone: what it replaced is gone
			replaced := fs.Exists(dst)
			var err error
			if mode == "cut" {
				err = t.Move(src, dst)
			} else {
				err = t.Copy(src, dst)
			}
			switch {
			case replaced:
				if err == nil {
					undo.lost++
				}
			case mode == "cut":
				if err == nil {
					undo.steps = append(undo.steps, undoStep{kind: stepRestore, from: dst, to: src})
				}
			default:
				// Even a partial copy is recorded, so undo can clear it away
				undo.addCopied(dst, src)
			}
			return err
		})
		return jobDoneMsg{op: op, undo: undo}
	})
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

// deleteMessage describes what the pending delete will remove. Items
// outside the current folder, which a plugin can select, are shown by
// their full path and counted, so nothing is deleted unseen. Names are
// made printable here, as the dialog splits the message into lines: a name
// with a newline in it would otherwise add lines of its own.
func (m Model) deleteMessage() string {
	dir := m.tab().CurrentPath
	if len(m.pending) == 1 {
		path := m.pending[0]
		name := utils.Printable(filepath.Base(path))
		where := ""
		if filepath.Dir(path) != dir {
			where = "\n\nIt is in another folder:\n" + utils.TruncateLeft(utils.Printable(path), pathWidth)
		}
		info, err := os.Lstat(path)
		switch {
		case err == nil && info.Mode()&os.ModeSymlink != 0:
			return fmt.Sprintf("Delete symlink '%s'? Its target is not touched.", name) + where
		case err == nil && info.IsDir():
			return fmt.Sprintf("Delete directory '%s' and all its contents?", name) + where
		default:
			return fmt.Sprintf("Delete file '%s'?", name) + where
		}
	}

	msg := fmt.Sprintf("Delete %d items?\n\n%s", len(m.pending), listPaths(m.pending, dir, 5))
	elsewhere := 0
	for _, p := range m.pending {
		if filepath.Dir(p) != dir {
			elsewhere++
		}
	}
	switch {
	case elsewhere == len(m.pending):
		msg += "\n\nNone of them is in this folder."
	case elsewhere == 1:
		msg += "\n\n1 of them is in another folder."
	case elsewhere > 1:
		msg += fmt.Sprintf("\n\n%d of them are in other folders.", elsewhere)
	}
	return msg
}

// pathWidth is how much of a full path fits on a line of the confirmation
const pathWidth = 56

// listPaths lists up to limit paths, one per line: those in dir by name,
// others in full, shortened from the start so their names show
func listPaths(paths []string, dir string, limit int) string {
	lines := make([]string, 0, limit+1)
	for i, p := range paths {
		if i == limit {
			lines = append(lines, fmt.Sprintf("…and %d more", len(paths)-limit))
			break
		}
		if filepath.Dir(p) == dir {
			lines = append(lines, utils.Printable(filepath.Base(p)))
		} else {
			lines = append(lines, utils.TruncateLeft(utils.Printable(p), pathWidth))
		}
	}
	return strings.Join(lines, "\n")
}

// pasteMessage describes what the pending paste will overwrite, with the
// names made printable, as deleteMessage does
func (m Model) pasteMessage() string {
	if len(m.pending) == 1 {
		return fmt.Sprintf("'%s' already exists. Overwrite?", utils.Printable(m.pending[0]))
	}
	return fmt.Sprintf("%d items already exist here. Overwrite them?\n\n%s", len(m.pending), listNames(m.pending, 5))
}

// listNames lists up to limit base names, one per line, made printable
func listNames(paths []string, limit int) string {
	names := make([]string, 0, limit+1)
	for i, p := range paths {
		if i == limit {
			names = append(names, fmt.Sprintf("…and %d more", len(paths)-limit))
			break
		}
		names = append(names, utils.Printable(filepath.Base(p)))
	}
	return strings.Join(names, "\n")
}
