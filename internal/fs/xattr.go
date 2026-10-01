package fs

import (
	"bytes"
	"errors"

	"golang.org/x/sys/unix"
)

// copyXattrs gives dst the extended attributes of src, neither followed if
// it is a symlink: on macOS, Finder tags and colour labels, where a
// download came from, and the rest, as Finder's copies keep them. It is
// best effort, attribute by attribute: one the destination doesn't take,
// as on a volume without them, or one the system keeps for itself, is left
// behind without failing the copy. dst must still be writable.
func copyXattrs(src, dst string) {
	names, err := listXattrs(src)
	if err != nil {
		return
	}
	for _, name := range names {
		if value, err := getXattr(src, name); err == nil {
			unix.Lsetxattr(dst, name, value, 0)
		}
	}
}

// listXattrs returns the names of path's extended attributes
func listXattrs(path string) ([]string, error) {
	buf := make([]byte, 256)
	for {
		n, err := unix.Llistxattr(path, buf)
		if errors.Is(err, unix.ERANGE) {
			// More names than fit: ask how much room they need
			if size, err := unix.Llistxattr(path, nil); err == nil {
				buf = make([]byte, size+64)
				continue
			}
		}
		if err != nil {
			return nil, err
		}
		var names []string
		for _, name := range bytes.Split(buf[:n], []byte{0}) {
			if len(name) > 0 {
				names = append(names, string(name))
			}
		}
		return names, nil
	}
}

// getXattr reads attribute name of path
func getXattr(path, name string) ([]byte, error) {
	buf := make([]byte, 256)
	for {
		n, err := unix.Lgetxattr(path, name, buf)
		if errors.Is(err, unix.ERANGE) {
			if size, err := unix.Lgetxattr(path, name, nil); err == nil {
				buf = make([]byte, size+64)
				continue
			}
		}
		if err != nil {
			return nil, err
		}
		return buf[:n], nil
	}
}
