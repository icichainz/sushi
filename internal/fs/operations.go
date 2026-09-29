package fs

import (
	"errors"
	"fmt"
	"io"
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

// CopyFile copies a single file from src to dst
func CopyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("cannot open source: %w", err)
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("cannot stat source: %w", err)
	}

	// Opening dst with O_TRUNC would empty src before it is read
	if dstInfo, err := os.Stat(dst); err == nil && os.SameFile(srcInfo, dstInfo) {
		return ErrSamePath
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return fmt.Errorf("cannot create destination: %w", err)
	}

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		dstFile.Close()
		return fmt.Errorf("copy failed: %w", err)
	}

	if err := dstFile.Close(); err != nil {
		return fmt.Errorf("copy failed: %w", err)
	}

	return nil
}

// CopyPath copies a file or directory from src to dst.
// Symlinks are copied as symlinks rather than followed.
func CopyPath(src, dst string) error {
	if err := CheckTransfer(src, dst); err != nil {
		return err
	}
	return copyPath(src, dst)
}

// copyPath does the recursive copy once CheckTransfer has passed
func copyPath(src, dst string) error {
	srcInfo, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("cannot access source: %w", err)
	}

	if srcInfo.Mode()&os.ModeSymlink != 0 {
		return copySymlink(src, dst)
	}

	if !srcInfo.IsDir() {
		return CopyFile(src, dst)
	}

	// Create destination directory
	if err := os.MkdirAll(dst, srcInfo.Mode().Perm()); err != nil {
		return fmt.Errorf("cannot create destination directory: %w", err)
	}

	// Copy directory contents recursively
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("cannot read source directory: %w", err)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if err := copyPath(srcPath, dstPath); err != nil {
			return err
		}
	}

	return nil
}

// copySymlink recreates the symlink at src as dst, replacing any existing file
func copySymlink(src, dst string) error {
	target, err := os.Readlink(src)
	if err != nil {
		return fmt.Errorf("cannot read symlink: %w", err)
	}

	if info, err := os.Lstat(dst); err == nil && !info.IsDir() {
		if err := os.Remove(dst); err != nil {
			return fmt.Errorf("cannot replace destination: %w", err)
		}
	}

	if err := os.Symlink(target, dst); err != nil {
		return fmt.Errorf("cannot create symlink: %w", err)
	}
	return nil
}

// MovePath moves a file or directory from src to dst
func MovePath(src, dst string) error {
	if err := CheckTransfer(src, dst); err != nil {
		return err
	}

	// Try rename first (fast path for same filesystem)
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}

	// If rename fails (cross-device), fall back to copy+delete
	if err := copyPath(src, dst); err != nil {
		return fmt.Errorf("move failed during copy: %w", err)
	}

	if err := DeletePath(src); err != nil {
		return fmt.Errorf("move failed during cleanup: %w", err)
	}

	return nil
}

// Exists checks if a path exists
func Exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
