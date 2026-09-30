package fs

import (
	"os"
	"path/filepath"
)

// Stamp summarises a file, or a directory and everything in it, well
// enough to tell whether it has changed: undo only removes what an
// operation created if it is still as the operation left it. Where the
// system keeps them (Unix), it also holds the root's device and inode and
// the latest status change time, which move whenever a file is replaced,
// even by one of the same size and modification time.
type Stamp struct {
	Entries  int    // Files, directories and links, the root included
	Size     int64  // Bytes in regular files
	Latest   int64  // Latest modification time, in Unix nanoseconds
	Changed  int64  // Latest status change time (ctime), in Unix nanoseconds
	Dev, Ino uint64 // The root's device and inode
	Link     string // Target, when the root is a symlink
}

// TakeStamp summarises path without following symlinks
func TakeStamp(path string) (Stamp, error) {
	var s Stamp
	path = filepath.Clean(path)
	root, err := os.Lstat(path)
	if err != nil {
		return s, err
	}
	s.Dev, s.Ino, _ = fileIDs(root)
	if root.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return s, err
		}
		_, _, s.Changed = fileIDs(root)
		s.Entries, s.Latest, s.Link = 1, root.ModTime().UnixNano(), target
		return s, nil
	}

	err = filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		s.Entries++
		if info.Mode().IsRegular() {
			s.Size += info.Size()
		}
		s.Latest = max(s.Latest, info.ModTime().UnixNano())
		_, _, changed := fileIDs(info)
		s.Changed = max(s.Changed, changed)
		return nil
	})
	return s, err
}

// Trivial reports whether removing what the stamp describes would lose
// nothing: an empty file or directory, or a symlink
func (s Stamp) Trivial() bool {
	return s.Link != "" || (s.Entries == 1 && s.Size == 0)
}
