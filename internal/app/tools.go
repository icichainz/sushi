package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/opener"
	"github.com/icichainz/sushi/internal/ui/components"
)

// execProcess hands the terminal to a program until it exits; tests
// replace it, as a real one needs a terminal
var execProcess = tea.ExecProcess

// isToolKey reports whether msg is handled by handleToolKey
func (k KeyMap) isToolKey(msg tea.KeyMsg) bool {
	return key.Matches(msg, k.HardDelete, k.Undo, k.Cancel, k.Duplicate, k.PasteLink, k.Chmod, k.BulkRename, k.Archive, k.Extract)
}

// handleToolKey handles the keys for undo, permanent delete and the file
// tools in normal mode
func (m Model) handleToolKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.HardDelete):
		return m.startHardDelete()
	case key.Matches(msg, m.keys.Undo):
		return m.startUndo()
	case key.Matches(msg, m.keys.Duplicate):
		return m.startDuplicate()
	case key.Matches(msg, m.keys.PasteLink):
		return m.paste(true)
	case key.Matches(msg, m.keys.Chmod):
		return m.startChmod()
	case key.Matches(msg, m.keys.BulkRename):
		return m.startBulkRename()
	case key.Matches(msg, m.keys.Archive):
		return m.startArchive()
	case key.Matches(msg, m.keys.Extract):
		return m.startExtract()
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

// startDuplicate copies the targets beside themselves, as "name copy.ext"
func (m Model) startDuplicate() (tea.Model, tea.Cmd) {
	paths := m.targets()
	if len(paths) == 0 {
		return m, nil
	}
	cmd := m.startJob("Duplicating", func(t *fs.Task) jobDoneMsg {
		t.Count(paths...)
		undo := &undoEntry{label: "duplicate " + describe(paths)}
		focus := ""
		op, _ := runBatch(t, "duplicate", "Duplicated", paths, func(path string) error {
			// The name was free a moment ago: a copy never goes into, or
			// over, something that has taken it since
			dst := filepath.Join(filepath.Dir(path), fs.CopyName(path))
			err := t.CopyNew(path, dst)
			// Even a partial copy is recorded, so undo can clear it away,
			// but not what was there before it
			if !errors.Is(err, fs.ErrNotCreated) {
				undo.addCopied(dst, path)
			}
			if err == nil && focus == "" {
				focus = dst
			}
			return err
		})
		return jobDoneMsg{op: op, undo: undo, focus: focus}
	})
	return m, cmd
}

// pasteLinks creates symlinks in the current directory to the clipboard
// items, which V, as v, may first take from the pasteboard (see paste). It
// never replaces anything, and leaves the clipboard as it is.
func (m Model) pasteLinks() (tea.Model, tea.Cmd) {
	if len(m.clipboard) == 0 {
		cmd := m.setStatus("Nothing in clipboard")
		return m, cmd
	}
	dir := m.tab().CurrentPath
	undo := &undoEntry{label: "link " + describe(m.clipboard)}
	msg := batchResult("link", "Linked", m.clipboard, func(src string) error {
		dst := filepath.Join(dir, filepath.Base(src))
		if fs.Exists(dst) {
			return errors.New("already exists here")
		}
		if err := os.Symlink(src, dst); err != nil {
			return err
		}
		if len(undo.steps) == 0 {
			m.tab().focusPath = dst
		}
		undo.addCreated(dst)
		return nil
	})
	m.pushUndo(undo)
	return m.Update(msg)
}

// startChmod asks for a new mode for the targets. Symlinks are left out:
// chmod follows them, so it would change what they point to, which may be
// anywhere, and on most systems a link has no mode of its own. With one
// item, or several of the same mode, the prompt starts from that mode; with
// several of different modes it starts empty, as they all get the one
// typed.
func (m Model) startChmod() (tea.Model, tea.Cmd) {
	targets := m.targets()
	if len(targets) == 0 {
		return m, nil
	}
	var paths []string
	var modes []string
	for _, path := range targets {
		info, err := os.Lstat(path)
		if err != nil {
			cmd := m.setStatus(fmt.Sprintf("Error: %v", err))
			return m, cmd
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		paths = append(paths, path)
		if mode := formatMode(info.Mode()); !slices.Contains(modes, mode) {
			modes = append(modes, mode)
		}
	}
	links := len(targets) - len(paths)
	if len(paths) == 0 {
		cmd := m.setStatus("Symlinks have no permissions of their own: change those of what they point to")
		return m, cmd
	}

	// Several items all get the mode typed, which the label says
	label := "Permissions of " + describe(paths)
	value := modes[0]
	switch {
	case len(modes) > 3:
		label = fmt.Sprintf("Set all %d items, now of %d different modes, to", len(paths), len(modes))
		value = ""
	case len(modes) > 1:
		label = fmt.Sprintf("Set all %d items, now %s, to", len(paths), strings.Join(modes, ", "))
		value = ""
	case len(paths) > 1:
		label = fmt.Sprintf("Set all %d items, now %s, to", len(paths), value)
	}
	switch {
	case links == 1:
		label += " (1 symlink left as it is)"
	case links > 1:
		label += fmt.Sprintf(" (%d symlinks left as they are)", links)
	}
	label += ":"
	return m.openPrompt(prompt{
		action: promptTool,
		badge:  "CHMOD",
		label:  label,
		input:  components.NewTextInput(value),
		submit: func(m Model, value string) (tea.Model, tea.Cmd) { return m.chmod(paths, value) },
	})
}

// chmod sets the mode typed in the prompt on paths
func (m Model) chmod(paths []string, value string) (tea.Model, tea.Cmd) {
	mode, err := parseMode(strings.TrimSpace(value))
	if err != nil {
		m.prompt.err = err.Error()
		return m, nil
	}
	m.mode = ModeNormal

	undo := &undoEntry{label: "permissions of " + describe(paths)}
	msg := batchResult("chmod", "Changed permissions to "+formatMode(mode), paths, func(path string) error {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		// Made a link since the prompt opened: never followed
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("is a symlink now, so it was left as it is")
		}
		if err := os.Chmod(path, mode); err != nil {
			return err
		}
		undo.steps = append(undo.steps, undoStep{kind: stepChmod, path: path, mode: info.Mode() & modeBits})
		return nil
	})
	m.pushUndo(undo)
	return m.Update(msg)
}

// modeBits are the parts of a mode chmod sets
const modeBits = os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky

// formatMode writes a mode in octal as chmod takes it: "644", or "4755"
// with special bits
func formatMode(mode os.FileMode) string {
	v := uint32(mode.Perm())
	if mode&os.ModeSetuid != 0 {
		v |= 04000
	}
	if mode&os.ModeSetgid != 0 {
		v |= 02000
	}
	if mode&os.ModeSticky != 0 {
		v |= 01000
	}
	if v > 0777 {
		return fmt.Sprintf("%04o", v)
	}
	return fmt.Sprintf("%03o", v)
}

// parseMode reads an octal mode of three or four digits, as in 644 or 1777
func parseMode(s string) (os.FileMode, error) {
	v, err := strconv.ParseUint(s, 8, 32)
	if err != nil || len(s) < 3 || len(s) > 4 {
		return 0, errors.New("use 3 or 4 octal digits, as in 644 or 0755")
	}
	mode := os.FileMode(v & 0777)
	if v&04000 != 0 {
		mode |= os.ModeSetuid
	}
	if v&02000 != 0 {
		mode |= os.ModeSetgid
	}
	if v&01000 != 0 {
		mode |= os.ModeSticky
	}
	return mode, nil
}

// bulkRenameMsg is sent when the editor for a bulk rename exits
type bulkRenameMsg struct {
	file  string   // The list of names that was edited
	paths []string // The files it names, line by line
	err   error
}

// startBulkRename opens the editor on the selected files' names, one per
// line; the files take the names on their lines once it exits. Without a
// selection it renames the file under the cursor, like r.
func (m Model) startBulkRename() (tea.Model, tea.Cmd) {
	if len(m.tab().Selected) == 0 {
		return m.startRename()
	}
	paths := m.targets()
	var list strings.Builder
	for _, p := range paths {
		name := filepath.Base(p)
		if strings.ContainsAny(name, "\n\r") {
			cmd := m.setStatus(fmt.Sprintf("Can't bulk rename %q: its name has a line break", name))
			return m, cmd
		}
		list.WriteString(name + "\n")
	}

	f, err := os.CreateTemp("", "sushi-rename-*.txt")
	if err == nil {
		_, err = f.WriteString(list.String())
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(f.Name())
		}
	}
	if err != nil {
		cmd := m.setStatus(fmt.Sprintf("Can't bulk rename: %v", err))
		return m, cmd
	}

	file := f.Name()
	cmd := execProcess(opener.EditorCommand(file), func(err error) tea.Msg {
		return bulkRenameMsg{file: file, paths: paths, err: err}
	})
	return m, cmd
}

func (msg bulkRenameMsg) apply(m Model) (tea.Model, tea.Cmd) {
	edited, err := os.ReadFile(msg.file)
	os.Remove(msg.file)
	if msg.err != nil {
		err = fmt.Errorf("editor failed: %w", msg.err)
	}
	var pairs []fs.RenamePair
	if err == nil {
		pairs, err = fs.PlanRenames(msg.paths, string(edited), occupied)
	}
	if err == nil && len(pairs) == 0 {
		cmd := m.setStatus("No names changed")
		return m, cmd
	}
	if err == nil {
		err = fs.RenameAll(pairs)
	}
	if err != nil {
		cmd := m.setStatus("Can't rename: " + err.Error())
		return m, cmd
	}

	m.retargetAll(pairs)
	clear(m.tab().Selected)
	back := make([]fs.RenamePair, len(pairs))
	for i, p := range pairs {
		back[i] = fs.RenamePair{From: p.To, To: p.From}
	}
	m.pushUndo(&undoEntry{label: "rename " + plural(len(pairs), "item"), steps: []undoStep{{kind: stepRenames, renames: back}}})
	cmd := tea.Batch(m.setStatus("Renamed "+plural(len(pairs), "item")), m.reloadAll())
	return m, cmd
}

// occupied reports whether something other than src is at path. Changing
// only the case of a name finds src itself on case-insensitive filesystems.
func occupied(path, src string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	srcInfo, err := os.Lstat(src)
	return err != nil || !os.SameFile(info, srcInfo)
}

// startArchive asks for the name of a zip archive to compress the targets
// into, suggesting one after the first
func (m Model) startArchive() (tea.Model, tea.Cmd) {
	paths := m.targets()
	if len(paths) == 0 {
		return m, nil
	}
	stem := filepath.Base(paths[0])
	if info, err := os.Lstat(paths[0]); err == nil && !info.IsDir() {
		stem, _ = fs.SplitExt(stem)
	}
	input := components.NewTextInput(stem + ".zip")
	input.SetCursor(len([]rune(stem)))

	dir := m.tab().CurrentPath
	return m.openPrompt(prompt{
		action: promptTool,
		badge:  "ARCHIVE",
		label:  "Compress " + describe(paths) + " to:",
		input:  input,
		submit: func(m Model, value string) (tea.Model, tea.Cmd) { return m.archive(paths, dir, value) },
	})
}

// archive compresses paths into the zip named in the prompt, in dir
func (m Model) archive(paths []string, dir, name string) (tea.Model, tea.Cmd) {
	if strings.TrimSpace(name) != "" && !strings.HasSuffix(strings.ToLower(name), ".zip") {
		name += ".zip"
	}
	dst := filepath.Join(dir, name)
	err := fs.ValidateName(name)
	if err == nil && fs.Exists(dst) {
		err = fmt.Errorf("%s already exists", name)
	}
	if err == nil {
		err = sameNames(paths)
	}
	if err != nil {
		m.prompt.err = err.Error()
		return m, nil
	}
	m.mode = ModeNormal

	cmd := m.startJob("Compressing", func(t *fs.Task) jobDoneMsg {
		t.Count(paths...)
		err := t.CreateZip(dst, paths)
		done := jobDoneMsg{op: fileOperationMsg{operation: "archive"}}
		switch {
		case t.Err() != nil:
			done.op.message = "Cancelled: " + howFar("compressed", t.Progress(), 0, len(paths))
		case err != nil:
			done.op.err = err
		default:
			done.op.message = fmt.Sprintf("Compressed %s into %s", describe(paths), name)
			done.undo = createUndo("archive "+name, dst)
			done.focus = dst
		}
		return done
	})
	return m, cmd
}

// sameNames reports two paths with the same name, which can't sit side by
// side in an archive
func sameNames(paths []string) error {
	seen := make(map[string]string, len(paths))
	for _, p := range paths {
		name := filepath.Base(p)
		if other, ok := seen[name]; ok {
			return fmt.Errorf("%s and %s have the same name", other, p)
		}
		seen[name] = p
	}
	return nil
}

// startExtract unpacks the archives among the targets, each into a new
// folder beside it named after it: "photos.zip" into "photos", or "photos
// 2" if that is taken
func (m Model) startExtract() (tea.Model, tea.Cmd) {
	var archives []string
	for _, p := range m.targets() {
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() && fs.ArchiveKind(p) != "" {
			archives = append(archives, p)
		}
	}
	if len(archives) == 0 {
		// Named by the key as bound, which keys: in the config may change
		what := "only .zip, .tar, .tar.gz and .tgz files can be extracted"
		if k := shownKey(m.keys.Extract); k != "" {
			what = k + " unpacks .zip, .tar, .tar.gz and .tgz files"
		}
		cmd := m.setStatus("Nothing to extract: " + what)
		return m, cmd
	}

	cmd := m.startJob("Extracting", func(t *fs.Task) jobDoneMsg {
		undo := &undoEntry{label: "extract " + describe(archives)}
		focus := ""
		op, _ := runBatch(t, "extract", "Extracted", archives, func(archive string) error {
			parent := filepath.Dir(archive)
			stem, _ := fs.SplitExt(filepath.Base(archive))
			dir := filepath.Join(parent, fs.FreeName(parent, stem, ""))
			err := t.Extract(archive, dir)
			// Even a partial extraction is recorded, so undo can clear it
			// away, but not a folder that something else made first
			if !errors.Is(err, fs.ErrNotCreated) {
				undo.addCreated(dir)
			}
			if err == nil && focus == "" {
				focus = dir
			}
			return err
		})
		return jobDoneMsg{op: op, undo: undo, focus: focus}
	})
	return m, cmd
}
