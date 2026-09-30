package fs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	// ErrSamePath is returned when the source and destination are the same file
	ErrSamePath = errors.New("source and destination are the same")
	// ErrDestInsideSource is returned when copying or moving a directory into itself
	ErrDestInsideSource = errors.New("cannot copy or move a directory into itself")
	// ErrNotCreated matches the errors of operations that make something
	// new, such as CopyNew and Extract, when they failed before making it:
	// whatever is at the destination isn't theirs
	ErrNotCreated = errors.New("nothing was created")
)

// notCreated wraps an error from before an operation made its destination
type notCreated struct{ err error }

func (e notCreated) Error() string        { return e.err.Error() }
func (e notCreated) Unwrap() error        { return e.err }
func (e notCreated) Is(target error) bool { return target == ErrNotCreated }

// DeletePath deletes a file or directory (recursively if directory).
// Symlinks are removed without touching their target, even when named with
// a trailing slash, which Lstat would follow.
func DeletePath(path string) error {
	path = filepath.Clean(path)
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("cannot access %s: %w", path, err)
	}

	if info.IsDir() {
		return os.RemoveAll(path)
	}
	return os.Remove(path)
}

// CheckTransfer reports whether src can be copied or moved to dst.
// It rejects copying a file onto itself and copying a directory into itself.
func CheckTransfer(src, dst string) error {
	src, dst = filepath.Clean(src), filepath.Clean(dst)
	srcEntry, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("cannot access source: %w", err)
	}

	// Compare both the entries and what they point to, so a symlink is
	// never written over its own target
	if dstEntry, err := os.Lstat(dst); err == nil {
		if os.SameFile(srcEntry, dstEntry) {
			return ErrSamePath
		}
		srcTarget, srcErr := os.Stat(src)
		dstTarget, dstErr := os.Stat(dst)
		if srcErr == nil && dstErr == nil && os.SameFile(srcTarget, dstTarget) {
			return ErrSamePath
		}
	}

	// Symlinks are copied as links, so only real directories can recurse into themselves
	if srcEntry.IsDir() && inside(src, srcEntry, dst) {
		return ErrDestInsideSource
	}

	return nil
}

// inside reports whether dst is somewhere below the directory src, which
// info describes. Names can't always tell: a filesystem that ignores case
// has "proj/sub/Proj" inside "Proj", and firmlinks and bind mounts give a
// folder a second path. So each folder above dst that exists, followed
// from the real path of its deepest existing part, is compared with src
// by identity.
func inside(src string, info os.FileInfo, dst string) bool {
	if strings.HasPrefix(resolvePath(dst), resolvePath(src)+string(filepath.Separator)) {
		return true
	}
	for dir := resolvePath(filepath.Dir(dst)); ; {
		if here, err := os.Stat(dir); err == nil && os.SameFile(here, info) {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

// resolvePath returns an absolute, symlink-free form of path.
// Trailing elements may not exist yet, so the deepest existing ancestor is
// resolved and the rest is joined back on.
func resolvePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}

	existing, rest := abs, ""
	for {
		if resolved, err := filepath.EvalSymlinks(existing); err == nil {
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return abs
		}
		rest = filepath.Join(filepath.Base(existing), rest)
		existing = parent
	}
}

// CopyFile copies a single file from src to dst, keeping its mode and
// modification time
func CopyFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("cannot open source: %w", err)
	}
	return background().copyFile(src, dst, info, Exists(dst))
}

// CopyPath copies a file or directory from src to dst.
// Symlinks are copied as symlinks rather than followed.
func CopyPath(src, dst string) error {
	return background().Copy(src, dst)
}

// MovePath moves a file or directory from src to dst
func MovePath(src, dst string) error {
	return background().Move(src, dst)
}

// Exists checks if a path exists
func Exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
