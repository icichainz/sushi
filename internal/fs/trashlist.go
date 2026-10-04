package fs

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"
	"time"
)

// What the trash browser needs from the trash: what is in it, where each
// item came from, and deleting items for good.
//
// A freedesktop trash says where each item came from and when in its
// .trashinfo file. ~/.Trash keeps no such record that sushi can read
// (Finder's is in its .DS_Store), so sushi notes what it trashes there
// itself, in Trash.Record, and can put back only those items.

// TrashEntry is an item in the trash, as the trash browser lists it
type TrashEntry struct {
	Name     string    // Its name in the trash
	Path     string    // Where it is in the trash
	Original string    // Where it was trashed from, or "" if nothing recorded it
	Deleted  time.Time // When it was trashed, as recorded, or else when it last changed
	Info     string    // Its .trashinfo file, in a freedesktop trash that has one for it
	IsDir    bool
	IsLink   bool
	Size     int64 // Of the entry itself; a directory's contents aren't added up
}

// Item returns the entry as a trashed item that Restore moves to to
func (e TrashEntry) Item(to string) TrashedItem {
	return TrashedItem{Original: to, Path: e.Path, Info: e.Info}
}

// finderMetadata is Finder's file in ~/.Trash, which nobody trashed: it is
// neither listed nor emptied
const finderMetadata = ".DS_Store"

// List returns what is in the trash, the most recently trashed first. A
// trash that doesn't exist yet is empty. Entries sushi is still copying in
// from another drive aren't listed.
func (tr *Trash) List() ([]TrashEntry, error) {
	dirents, err := os.ReadDir(tr.Files)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read the trash: %w", err)
	}
	var noted map[string]notedItem
	if tr.Info == "" {
		noted = tr.notes()
	}

	entries := make([]TrashEntry, 0, len(dirents))
	for _, d := range dirents {
		name := d.Name()
		if isPartial(name) || tr.Info == "" && name == finderMetadata {
			continue
		}
		info, err := d.Info()
		if err != nil {
			continue // Gone since it was listed
		}
		e := TrashEntry{Name: name, Path: filepath.Join(tr.Files, name), IsDir: info.IsDir(),
			IsLink: info.Mode()&os.ModeSymlink != 0, Size: info.Size(), Deleted: info.ModTime()}
		// Moving an item into the trash changes its status time, which is
		// the best guess at when it was trashed where nothing says
		_, ino, changed := fileIDs(info)
		if changed > 0 {
			e.Deleted = time.Unix(0, changed)
		}
		if tr.Info != "" {
			infoPath := filepath.Join(tr.Info, name+".trashinfo")
			if original, when, err := readTrashInfo(infoPath); err == nil {
				e.Info, e.Original = infoPath, original
				if !when.IsZero() {
					e.Deleted = when
				}
			}
		} else if n, ok := noted[e.Path]; ok && n.matches(ino) {
			e.Original, e.Deleted = n.Original, n.Time
		}
		entries = append(entries, e)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if a, b := entries[i].Deleted, entries[j].Deleted; !a.Equal(b) {
			return a.After(b)
		}
		return entries[i].Name < entries[j].Name
	})
	return entries, nil
}

// readTrashInfo reads where an item came from and when it was trashed
// from its .trashinfo file. A path that isn't absolute is relative to the
// top of a drive's own trash, which this isn't, so it is left unknown.
func readTrashInfo(path string) (original string, deleted time.Time, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", time.Time{}, err
	}
	section := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "["):
			section = line == "[Trash Info]"
		case !section:
		case strings.HasPrefix(line, "Path="):
			if p, err := url.PathUnescape(strings.TrimPrefix(line, "Path=")); err == nil && filepath.IsAbs(p) {
				original = filepath.Clean(filepath.FromSlash(p))
			}
		case strings.HasPrefix(line, "DeletionDate="):
			// In local time, as the specification has it
			if t, err := time.ParseInLocation("2006-01-02T15:04:05", strings.TrimPrefix(line, "DeletionDate="), time.Local); err == nil {
				deleted = t
			}
		}
	}
	return original, deleted, nil
}

// Delete deletes an item in the trash for good, with its .trashinfo file.
// Only an entry directly in the trash folder, with symlinks resolved on
// both sides, is deleted; anything else is refused, so a stale or altered
// path can never delete something outside the trash. A symlink in the
// trash is removed, never followed.
func (tr *Trash) Delete(t *Task, e TrashEntry) error {
	path, err := tr.member(e.Path)
	if err != nil {
		return err
	}
	if err := t.Delete(path); err != nil {
		return err
	}
	if tr.Info != "" {
		// Named after the item rather than taken from the entry, so this
		// too stays within the trash
		os.Remove(filepath.Join(tr.Info, filepath.Base(path)+".trashinfo"))
	}
	return nil
}

// Empty deletes everything the trash lists for good, item by item, with
// their .trashinfo files. The trash folder itself stays, and so does
// Finder's .DS_Store. Items that can't be deleted are skipped, and failed
// says which and why. Once the task is cancelled it stops, and err is the
// task's error.
func (tr *Trash) Empty(t *Task) (deleted []TrashEntry, failed []error, err error) {
	entries, err := tr.List()
	if err != nil {
		return nil, nil, err
	}
	paths := make([]string, len(entries))
	for i, e := range entries {
		paths[i] = e.Path
	}
	if err := t.CountFiles(paths...); err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		if err := t.Err(); err != nil {
			return deleted, failed, err
		}
		if err := tr.Delete(t, e); err != nil {
			if cerr := t.Err(); cerr != nil {
				return deleted, failed, cerr
			}
			failed = append(failed, fmt.Errorf("%s: %w", e.Name, err))
			continue
		}
		deleted = append(deleted, e)
	}
	return deleted, failed, nil
}

// member returns path, resolved, if it is an entry of the trash folder
// itself: not the folder, nor something deeper in it or elsewhere
func (tr *Trash) member(path string) (string, error) {
	files, err := tr.resolvedFiles()
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(path)
	name := filepath.Base(clean)
	parent, err := filepath.EvalSymlinks(filepath.Dir(clean))
	if err != nil {
		return "", fmt.Errorf("cannot find %s in the trash: %w", name, err)
	}
	if parent != files || name == "." || name == ".." || name == string(filepath.Separator) {
		return "", fmt.Errorf("%s is not in the trash, so it was left alone", name)
	}
	return filepath.Join(files, name), nil
}

// resolvedFiles returns the trash folder with symlinks resolved. One that
// resolves to the root, or to the home folder or above it, as a broken
// HOME could make it, is refused: emptying it would delete everything.
func (tr *Trash) resolvedFiles() (string, error) {
	files, err := filepath.EvalSymlinks(tr.Files)
	if err != nil {
		return "", fmt.Errorf("cannot find the trash: %w", err)
	}
	home, _ := os.UserHomeDir()
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	if files == filepath.Dir(files) || home != "" && within(files, home) {
		return "", fmt.Errorf("the trash resolves to %s, which sushi won't delete from", files)
	}
	return files, nil
}

// notedItem is what sushi notes of an item it puts in a trash that keeps
// no record of its own, so the trash browser can put it back. The record
// file holds {"items": [...]} of these, most recent last.
type notedItem struct {
	Original string    `json:"original"` // Where it was
	Trashed  string    `json:"trashed"`  // Where it went in the trash
	Time     time.Time `json:"time"`     // When
	// Its inode in the trash, so an item given the same name later, once
	// this one has gone, isn't taken for it
	Inode uint64 `json:"inode,omitempty"`
}

// trashNotes is the record file's contents
type trashNotes struct {
	Items []notedItem `json:"items"`
}

// matches reports whether the noted item is the one with inode ino
func (n notedItem) matches(ino uint64) bool {
	return n.Inode == 0 || ino == 0 || n.Inode == ino
}

// stillThere reports whether the noted item is still in the trash
func (n notedItem) stillThere() bool {
	info, err := os.Lstat(n.Trashed)
	if err != nil {
		return false
	}
	_, ino, _ := fileIDs(info)
	return n.matches(ino)
}

// notes reads the record, by where each item is in the trash. A missing
// or unreadable record notes nothing.
func (tr *Trash) notes() map[string]notedItem {
	out := make(map[string]notedItem)
	for _, n := range tr.readNotes() {
		out[filepath.Clean(n.Trashed)] = n
	}
	return out
}

func (tr *Trash) readNotes() []notedItem {
	data, err := os.ReadFile(tr.Record)
	if err != nil {
		return nil
	}
	var notes trashNotes
	if json.Unmarshal(data, &notes) != nil {
		return nil
	}
	return notes.Items
}

// note adds an item just put in the trash to the record, if the trash
// keeps one, dropping the items that have left the trash since
func (tr *Trash) note(it TrashedItem) error {
	if tr.Record == "" || tr.Info != "" {
		return nil
	}
	info, err := os.Lstat(it.Path)
	if err != nil {
		return err
	}
	_, ino, _ := fileIDs(info)
	return tr.editNotes(func(items []notedItem) []notedItem {
		return append(items, notedItem{Original: it.Original, Trashed: it.Path, Time: time.Now(), Inode: ino})
	})
}

// editNotes changes the record, holding a lock on it, as other sushi
// windows may write it too. The record is replaced whole, by a rename, so
// it is never read half written.
func (tr *Trash) editNotes(change func([]notedItem) []notedItem) error {
	if err := os.MkdirAll(filepath.Dir(tr.Record), 0755); err != nil {
		return fmt.Errorf("cannot write the trash record: %w", err)
	}
	lock, err := os.OpenFile(tr.Record+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("cannot lock the trash record: %w", err)
	}
	// Closing the file releases the lock
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("cannot lock the trash record: %w", err)
	}

	items := slices.DeleteFunc(tr.readNotes(), func(n notedItem) bool { return !n.stillThere() })
	data, err := json.MarshalIndent(trashNotes{Items: change(items)}, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(tr.Record), ".trash-record-*")
	if err != nil {
		return fmt.Errorf("cannot write the trash record: %w", err)
	}
	_, err = tmp.Write(append(data, '\n'))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), tr.Record)
	}
	if err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("cannot write the trash record: %w", err)
	}
	return nil
}
