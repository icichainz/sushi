package fs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// The root of a mounted volume (a disk, a disk image, a share) looks like
// any folder, but it can't be renamed into the trash or another folder:
// that fails as a rename across filesystems does, and the copy-and-delete
// that follows would copy everything on the volume and then delete it,
// leaving it empty. Deleting it would empty it too. So trashing, moving and
// deleting refuse a volume's root, and deleting a folder stops at a volume
// mounted inside it: a volume is ejected, not deleted.

// ErrMounted matches the errors for items that are mounted volumes
var ErrMounted = errors.New("mounted volume")

// mountedError says an item is the root of a mounted volume, or that one
// is mounted inside it
type mountedError struct {
	name   string
	inside bool // Found inside what was being deleted, which stopped there
}

func (e mountedError) Error() string {
	if e.inside {
		return e.name + " inside it is a mounted volume, so the delete stopped there; eject it first"
	}
	return e.name + " is a mounted volume; eject it instead"
}

func (e mountedError) Is(target error) bool { return target == ErrMounted }

// deviceOf returns the device of the entry at path, which info describes.
// Tests replace it to make a folder look like the root of another volume.
var deviceOf = func(path string, info os.FileInfo) uint64 {
	dev, _, _ := fileIDs(info)
	return dev
}

// checkNotMount refuses path, which info describes as Lstat does, if it is
// the root of a mounted volume: the root folder, a folder on another device
// than the folder holding it (as /Volumes/USB is), or where the system says
// a volume is mounted. A symlink is only a link, wherever it leads.
func checkNotMount(path string, info os.FileInfo) error {
	if !info.IsDir() {
		return nil
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	path = filepath.Clean(path)
	parent := filepath.Dir(path)
	if parent == path || mountedOn(path) {
		return mountedError{name: filepath.Base(path)}
	}
	// Followed, as path is reached through what its parent leads to
	up, err := os.Stat(parent)
	if err != nil {
		return fmt.Errorf("cannot tell whether %s is a mounted volume: %w", filepath.Base(path), err)
	}
	if deviceOf(parent, up) != deviceOf(path, info) {
		return mountedError{name: filepath.Base(path)}
	}
	return nil
}

// checkRemovable refuses path if the folder holding it can't be written,
// as on a read-only volume or in a folder of another user's: removing it
// from there fails, and found out only after copying it to another volume,
// as trashing and moving do, the whole copy would be left behind
func checkRemovable(path string) error {
	dir := filepath.Dir(filepath.Clean(path))
	if err := unix.Access(dir, unix.W_OK); err != nil {
		return fmt.Errorf("cannot remove %s from %s: %w", filepath.Base(path), dir, err)
	}
	return nil
}
