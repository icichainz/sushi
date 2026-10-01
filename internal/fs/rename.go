package fs

import (
	"errors"
	"os"
)

// errNoExclusiveRename says the system or filesystem can't rename without
// replacing in one step, so renameNoReplace falls back
var errNoExclusiveRename = errors.New("renaming without replacing is not supported")

// renameNoReplace renames from to to like os.Rename, but never replaces
// anything: if something is at to, it fails with an error matching
// os.ErrExist. The check and the rename are one step where the system
// allows it (renamex_np on macOS, renameat2 on Linux); on filesystems
// that can't, see renameAfterCheck.
func renameNoReplace(from, to string) error {
	err := renameExclusive(from, to)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errNoExclusiveRename):
		return renameAfterCheck(from, to)
	}
	return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
}

// renameAfterCheck is renameNoReplace where the system can't check and
// rename in one step. A regular file is hard-linked into place, which
// fails if the name is taken, and then unlinked from its old name.
// Anything else is renamed after checking that to is free, which leaves a
// moment in which something created at to would be replaced.
func renameAfterCheck(from, to string) error {
	taken := &os.LinkError{Op: "rename", Old: from, New: to, Err: os.ErrExist}
	info, err := os.Lstat(from)
	if err != nil {
		return err
	}
	if info.Mode().IsRegular() {
		err := os.Link(from, to)
		if err == nil {
			if err := os.Remove(from); err != nil {
				os.Remove(to)
				return err
			}
			return nil
		}
		if errors.Is(err, os.ErrExist) {
			return taken
		}
		// No hard links here, such as on FAT: check, then rename
	}
	if _, err := os.Lstat(to); err == nil {
		return taken
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(from, to)
}
