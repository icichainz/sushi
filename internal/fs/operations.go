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
)

// DeletePath deletes a file or directory (recursively if directory).
// Symlinks are removed without touching their target.
func DeletePath(path string) error {
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
	if srcEntry.IsDir() {
		if strings.HasPrefix(resolvePath(dst), resolvePath(src)+string(filepath.Separator)) {
			return ErrDestInsideSource
		}
	}

	return nil
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
	return background().copyFile(src, dst, info)
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
