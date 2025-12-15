package fs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// DeletePath deletes a file or directory (recursively if directory)
func DeletePath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("cannot access %s: %w", path, err)
	}

	if info.IsDir() {
		return os.RemoveAll(path)
	}
	return os.Remove(path)
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

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return fmt.Errorf("cannot create destination: %w", err)
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return fmt.Errorf("copy failed: %w", err)
	}

	return nil
}

// CopyPath copies a file or directory from src to dst
func CopyPath(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("cannot access source: %w", err)
	}

	if !srcInfo.IsDir() {
		return CopyFile(src, dst)
	}

	// Create destination directory
	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
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

		if err := CopyPath(srcPath, dstPath); err != nil {
			return err
		}
	}

	return nil
}

// MovePath moves a file or directory from src to dst
func MovePath(src, dst string) error {
	// Try rename first (fast path for same filesystem)
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}

	// If rename fails (cross-device), fall back to copy+delete
	if err := CopyPath(src, dst); err != nil {
		return fmt.Errorf("move failed during copy: %w", err)
	}

	if err := DeletePath(src); err != nil {
		return fmt.Errorf("move failed during cleanup: %w", err)
	}

	return nil
}

// Exists checks if a path exists
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
