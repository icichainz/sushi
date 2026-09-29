package fs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// isSeparator reports whether r separates path elements; "/" is accepted on
// every platform. Only ASCII is checked, as uint8(r) would wrap other runes
// (U+012F would become '/').
func isSeparator(r rune) bool {
	return r < utf8.RuneSelf && (r == '/' || os.IsPathSeparator(uint8(r)))
}

// ValidateName checks that name can be used as a single file name
func ValidateName(name string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return errors.New("name is empty")
	case name == "." || name == "..":
		return fmt.Errorf("%q is not a valid name", name)
	case strings.IndexFunc(name, isSeparator) >= 0:
		return errors.New("name can't contain a path separator")
	case strings.ContainsRune(name, 0):
		return errors.New("name can't contain a NUL character")
	}
	return nil
}

// Rename gives path a new name in the same directory and returns the new
// path. It never replaces an existing file, but allows changing only the
// case of a name on case-insensitive filesystems.
func Rename(path, newName string) (string, error) {
	if err := ValidateName(newName); err != nil {
		return "", err
	}
	dst := filepath.Join(filepath.Dir(path), newName)
	if dst == path {
		return path, nil
	}

	if dstInfo, err := os.Lstat(dst); err == nil {
		srcInfo, srcErr := os.Lstat(path)
		if srcErr != nil || !os.SameFile(srcInfo, dstInfo) {
			return "", fmt.Errorf("%s already exists", newName)
		}
	}

	if err := os.Rename(path, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// newEntryPath validates a name for a new file or directory, which may
// include subdirectories ("src/main.go"), and returns its path inside dir
func newEntryPath(dir, name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", errors.New("name is empty")
	}
	if filepath.IsAbs(name) || strings.IndexFunc(name, isSeparator) == 0 {
		return "", errors.New("name must be relative to the current directory")
	}
	for _, part := range strings.FieldsFunc(name, isSeparator) {
		if part == "." || part == ".." {
			return "", fmt.Errorf("%q is not allowed in a name", part)
		}
	}
	return filepath.Join(dir, name), nil
}

// CreateFile creates an empty file in dir, and any missing parent
// directories, and returns its path. It fails if the file already exists.
func CreateFile(dir, name string) (string, error) {
	path, err := newEntryPath(dir, name)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if errors.Is(err, os.ErrExist) {
		return "", fmt.Errorf("%s already exists", name)
	}
	if err != nil {
		return "", err
	}
	return path, f.Close()
}

// CreateDir creates a directory in dir, and any missing parents, and returns
// its path. It fails if something already exists there.
func CreateDir(dir, name string) (string, error) {
	path, err := newEntryPath(dir, name)
	if err != nil {
		return "", err
	}
	if Exists(path) {
		return "", fmt.Errorf("%s already exists", strings.TrimRightFunc(name, isSeparator))
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return "", err
	}
	return path, nil
}
