package fs

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"
)

// Trash is where deleted files are moved so they can be restored.
//
// On macOS it is ~/.Trash. Elsewhere on Unix it follows the freedesktop.org
// trash specification: $XDG_DATA_HOME/Trash (~/.local/share/Trash), with the
// items in files/ and a .trashinfo file for each in info/, so desktop file
// managers can list and restore them.
//
// Windows' Recycle Bin can't be reached from Go's standard library, so there
// sushi keeps its own trash in the user config directory, laid out like
// freedesktop's. Explorer doesn't show it: items in it are restored with
// undo, or by hand.
//
// Items on another filesystem than the trash are copied into it and then
// deleted, rather than being put in a trash on their own volume (a
// freedesktop $topdir/.Trash-$uid, or macOS's .Trashes), and Finder's Put
// Back doesn't know where items sushi trashed came from.
type Trash struct {
	Files string // Where trashed items go
	Info  string // Where their .trashinfo files go; empty for ~/.Trash
}

// TrashedItem records where a trashed item came from, so it can be restored
type TrashedItem struct {
	Original string // Where it was
	Path     string // Where it is in the trash
	Info     string // Its .trashinfo file, if the trash keeps them
}

// DefaultTrash returns the user's trash
func DefaultTrash() (*Trash, error) {
	return trashFor(runtime.GOOS)
}

// trashFor returns the user's trash on the given operating system
func trashFor(goos string) (*Trash, error) {
	var base string
	switch goos {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		return &Trash{Files: filepath.Join(home, ".Trash")}, nil
	case "windows":
		dir, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		base = filepath.Join(dir, "sushi", "Trash")
	default:
		// The specification says relative values are to be ignored
		data := os.Getenv("XDG_DATA_HOME")
		if data == "" || !filepath.IsAbs(data) {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, err
			}
			data = filepath.Join(home, ".local", "share")
		}
		base = filepath.Join(data, "Trash")
	}
	return &Trash{Files: filepath.Join(base, "files"), Info: filepath.Join(base, "info")}, nil
}

// root is the directory holding everything that belongs to the trash
func (tr *Trash) root() string {
	if tr.Info != "" {
		return filepath.Dir(tr.Files)
	}
	return tr.Files
}

// Put moves path into the trash and returns where it went. Nothing in the
// trash is ever replaced: a name that is taken gets a number, as in
// "notes 2.txt". Across filesystems the move is a copy and a delete, which
// the task can cancel before the original is touched.
func (tr *Trash) Put(t *Task, path string) (TrashedItem, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return TrashedItem{}, err
	}
	if _, err := os.Lstat(abs); err != nil {
		return TrashedItem{}, fmt.Errorf("cannot access %s: %w", filepath.Base(abs), err)
	}

	// The item itself isn't resolved: trashing a symlink moves only the link
	item := filepath.Join(resolvePath(filepath.Dir(abs)), filepath.Base(abs))
	root := resolvePath(tr.root())
	switch {
	case within(root, item):
		return TrashedItem{}, fmt.Errorf("%s is in the trash already", filepath.Base(abs))
	case within(item, root):
		return TrashedItem{}, errors.New("can't move the trash into itself")
	}

	for _, dir := range []string{tr.Files, tr.Info} {
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			return TrashedItem{}, fmt.Errorf("cannot create the trash: %w", err)
		}
	}
	dest, info, err := tr.reserve(abs)
	if err != nil {
		return TrashedItem{}, err
	}

	trashed := TrashedItem{Original: abs, Path: dest, Info: info}
	if err := t.Move(abs, dest); err != nil {
		// A failed copy leaves nothing behind; a failed delete of the
		// original after a full copy leaves the item in the trash as well
		if !Exists(dest) {
			if info != "" {
				os.Remove(info)
			}
			return TrashedItem{}, err
		}
		return trashed, err
	}
	return trashed, nil
}

// reserve picks a name in the trash for the item at abs. With a freedesktop
// trash, creating the .trashinfo file claims the name, as the specification
// asks, so two programs can't pick the same one.
func (tr *Trash) reserve(abs string) (dest, info string, err error) {
	name := filepath.Base(abs)
	for n := 1; n < 10000; n++ {
		candidate := trashName(name, n)
		dest = filepath.Join(tr.Files, candidate)
		if tr.Info == "" {
			if _, err := os.Lstat(dest); errors.Is(err, os.ErrNotExist) {
				return dest, "", nil
			} else if err != nil {
				return "", "", fmt.Errorf("cannot check the trash: %w", err)
			}
			continue
		}

		info = filepath.Join(tr.Info, candidate+".trashinfo")
		f, err := os.OpenFile(info, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", "", fmt.Errorf("cannot write to the trash: %w", err)
		}
		// An item without an info file may still be in the way
		if Exists(dest) {
			f.Close()
			os.Remove(info)
			continue
		}
		_, err = fmt.Fprintf(f, "[Trash Info]\nPath=%s\nDeletionDate=%s\n",
			(&url.URL{Path: filepath.ToSlash(abs)}).EscapedPath(), time.Now().Format("2006-01-02T15:04:05"))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(info)
			return "", "", fmt.Errorf("cannot write to the trash: %w", err)
		}
		return dest, info, nil
	}
	return "", "", fmt.Errorf("cannot find a free name for %s in the trash", name)
}

// trashName returns the nth name to try for name in the trash: name, then
// "name 2.ext" and so on. Long names are shortened to leave room for the
// number and ".trashinfo" within the usual 255-byte limit.
func trashName(name string, n int) string {
	const limit = 200
	stem, ext := SplitExt(name)
	if len(ext) > limit/2 {
		stem, ext = name, ""
	}
	suffix := ""
	if n > 1 {
		suffix = fmt.Sprintf(" %d", n)
	}
	for len(stem)+len(suffix)+len(ext) > limit && len(stem) > 1 {
		_, size := utf8.DecodeLastRuneInString(stem)
		stem = stem[:len(stem)-size]
	}
	return stem + suffix + ext
}

// within reports whether path is dir or inside it
func within(dir, path string) bool {
	return path == dir || strings.HasPrefix(path, strings.TrimSuffix(dir, string(filepath.Separator))+string(filepath.Separator))
}

// Restore moves the item back where it was, unless something else is there
// now, and removes its .trashinfo file
func (it TrashedItem) Restore(t *Task) error {
	if _, err := os.Lstat(it.Path); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s is no longer in the trash", filepath.Base(it.Original))
	} else if err != nil {
		// Such as macOS refusing access to ~/.Trash
		return fmt.Errorf("cannot reach %s in the trash: %w", filepath.Base(it.Original), err)
	}
	if err := t.Restore(it.Path, it.Original); err != nil {
		return err
	}
	if it.Info != "" {
		os.Remove(it.Info)
	}
	return nil
}
