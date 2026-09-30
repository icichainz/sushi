package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/fs"
)

// undoLimit is how many operations are remembered for undo
const undoLimit = 20

// stepKind is what an undo step does
type stepKind int

const (
	stepRestore stepKind = iota // Move from back to to: undoes trash, rename and move
	stepRemove                  // Remove path if unchanged: undoes copy and create
	stepChmod                   // Put path's mode back
	stepRenames                 // Undo a bulk rename, all at once
)

// undoStep reverses one part of an operation
type undoStep struct {
	kind     stepKind
	from, to string          // stepRestore
	info     string          // stepRestore from the trash: the item's .trashinfo file
	trashed  bool            // stepRestore: from is in the trash
	path     string          // stepRemove and stepChmod
	stamp    fs.Stamp        // stepRemove: what it was like when created
	source   string          // stepRemove of a copy: what it was copied from
	original fs.Stamp        // stepRemove of a copy: what source was like then
	mode     os.FileMode     // stepChmod
	renames  []fs.RenamePair // stepRenames
}

// undoEntry is an operation that can be undone
type undoEntry struct {
	label  string     // What was done, as in "copy 3 items"
	steps  []undoStep // In the order they were done; undone in reverse
	lost   int        // Items the operation replaced, which can't come back
	reason string     // Why it can't be undone at all, if it can't
}

// addCreated records that the operation created path, so undo can remove
// it as long as it hasn't changed
func (e *undoEntry) addCreated(path string) {
	if stamp, err := fs.TakeStamp(path); err == nil {
		e.steps = append(e.steps, undoStep{kind: stepRemove, path: path, stamp: stamp})
	}
}

// addCopied records that the operation copied source to path. Undo removes
// the copy only while source is still there as it was: otherwise the copy
// may be all that is left of it.
func (e *undoEntry) addCopied(path, source string) {
	stamp, err := fs.TakeStamp(path)
	if err != nil {
		return
	}
	// A source that can't be stamped now won't match later, which keeps
	// the copy: the safe way to be wrong
	original, _ := fs.TakeStamp(source)
	e.steps = append(e.steps, undoStep{kind: stepRemove, path: path, stamp: stamp, source: source, original: original})
}

// renameUndo is how to undo renaming old to new
func renameUndo(old, new string) *undoEntry {
	return &undoEntry{
		label: fmt.Sprintf("rename %s → %s", filepath.Base(old), filepath.Base(new)),
		steps: []undoStep{{kind: stepRestore, from: new, to: old}},
	}
}

// createUndo is how to undo creating path
func createUndo(label, path string) *undoEntry {
	e := &undoEntry{label: label}
	e.addCreated(path)
	return e
}

// pushUndo remembers an operation for ctrl+z, forgetting the oldest beyond
// undoLimit. Operations that changed nothing aren't remembered.
func (m *Model) pushUndo(e *undoEntry) {
	if e == nil {
		return
	}
	if len(e.steps) == 0 && e.reason == "" {
		if e.lost == 0 {
			return
		}
		e.reason = "it replaced what was there"
	}
	// Clipped, so copies of the model never share where the entry goes
	m.undo = append(slices.Clip(m.undo), *e)
	if len(m.undo) > undoLimit {
		m.undo = m.undo[len(m.undo)-undoLimit:]
	}
}

// startUndo undoes the most recent operation in the background
func (m Model) startUndo() (tea.Model, tea.Cmd) {
	if len(m.undo) == 0 {
		cmd := m.setStatus("Nothing to undo")
		return m, cmd
	}
	e := m.undo[len(m.undo)-1]
	if e.reason != "" {
		// It stays, and undo stops at it: what came before may depend on
		// what it did, as a copy does on its original, which a permanent
		// delete may have removed
		cmd := m.setStatus(fmt.Sprintf("Can't undo %s: %s, so nothing before it can be undone", e.label, e.reason))
		return m, cmd
	}
	m.undo = m.undo[:len(m.undo)-1]

	useTrash := m.config.DeleteToTrash
	cmd := m.startJob("Undoing", func(t *fs.Task) jobDoneMsg {
		return undoWork(t, e, useTrash)
	})
	return m, cmd
}

// undoWork carries out an undo. Steps blocked by something in the way stay
// on the stack, so once it is moved, ctrl+z tries them again; steps that
// can never work, say because the file is gone, are dropped so they don't
// keep older operations from being undone.
func undoWork(t *fs.Task, e undoEntry, useTrash bool) jobDoneMsg {
	var done jobDoneMsg
	var kept []undoStep
	var firstErr error
	failures := 0
	for i := len(e.steps) - 1; i >= 0; i-- {
		s := e.steps[i]
		if t.Err() != nil {
			kept = append(kept, s)
			continue
		}
		moved, err := s.undo(t, useTrash)
		if err != nil && t.Err() != nil {
			// Cancelled part way: kept, so undoing again carries on. A
			// removal cut short has deleted some of what it removes, so it
			// is stamped again as it now is, or it would look changed
			if s.kind == stepRemove {
				if stamp, err := fs.TakeStamp(s.path); err == nil {
					s.stamp = stamp
				}
			}
			kept = append(kept, s)
			continue
		}
		if err != nil {
			failures++
			if errors.Is(err, os.ErrExist) {
				kept = append(kept, s)
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		done.moved = append(done.moved, moved...)
	}

	if len(kept) > 0 {
		slices.Reverse(kept)
		rest := e
		rest.steps = kept
		done.undo = &rest
	}

	op := &done.op
	op.operation = "undo"
	switch {
	case t.Err() != nil:
		op.message = fmt.Sprintf("Cancelled undoing %s; undoing again carries on", e.label)
	case firstErr != nil && failures == len(e.steps):
		op.err = fmt.Errorf("can't undo %s: %w", e.label, firstErr)
	case firstErr != nil:
		op.err = fmt.Errorf("undid only part of %s: %w", e.label, firstErr)
	default:
		op.message = "Undone: " + e.label
		if e.lost > 0 {
			op.message += fmt.Sprintf(" (%s it replaced can't be brought back)", plural(e.lost, "item"))
		}
	}
	return done
}

// undo carries out the step and returns any paths that moved
func (s undoStep) undo(t *fs.Task, useTrash bool) ([]fs.RenamePair, error) {
	switch s.kind {
	case stepRestore:
		var err error
		if s.trashed {
			err = fs.TrashedItem{Original: s.to, Path: s.from, Info: s.info}.Restore(t)
		} else {
			err = t.Restore(s.from, s.to)
		}
		if err != nil {
			return nil, err
		}
		return []fs.RenamePair{{From: s.from, To: s.to}}, nil

	case stepRemove:
		return nil, removeCreated(t, s, useTrash)

	case stepChmod:
		return nil, os.Chmod(s.path, s.mode)

	case stepRenames:
		if err := fs.RenameAll(s.renames); err != nil {
			return nil, err
		}
		return s.renames, nil
	}
	return nil, nil
}

// removeCreated removes what step s says an operation created, if it is as
// the operation left it. Empty files and folders and symlinks are deleted;
// anything else goes to the trash, when it is used, in case it's wanted
// after all. Without the trash, a copy is deleted only while its original
// is still there as it was: otherwise the copy may be all that is left.
func removeCreated(t *fs.Task, s undoStep, useTrash bool) error {
	path := s.path
	now, err := fs.TakeStamp(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil // Already gone
	}
	if err != nil {
		return err
	}
	if now != s.stamp {
		return fmt.Errorf("%s has changed since, so it was kept", filepath.Base(path))
	}
	if now.Trivial() {
		return t.Delete(path)
	}
	if !useTrash {
		if s.source != "" {
			if original, err := fs.TakeStamp(s.source); err != nil || original != s.original {
				return fmt.Errorf("%s was kept: its original %s is gone or has changed since it was copied", filepath.Base(path), s.source)
			}
		}
		return t.Delete(path)
	}
	tr, err := fs.DefaultTrash()
	if err != nil {
		return err
	}
	_, err = tr.Put(t, path)
	return err
}

// retargetAll updates references to paths that moved together, like
// retarget does for one. They go through placeholders, as RenameAll goes
// through temporary names, so in a swap a's references become b's without
// then becoming a's again.
func (m *Model) retargetAll(pairs []fs.RenamePair) {
	if len(pairs) == 1 {
		m.retarget(pairs[0].From, pairs[0].To)
		return
	}
	holding := make([]string, len(pairs))
	for i, p := range pairs {
		// Can't be a real path: no file name contains a NUL
		holding[i] = filepath.Join(string(filepath.Separator), "\x00sushi", strconv.Itoa(i))
		m.retarget(p.From, holding[i])
	}
	for i, p := range pairs {
		m.retarget(holding[i], p.To)
	}
}
