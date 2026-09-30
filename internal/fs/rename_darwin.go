package fs

import "golang.org/x/sys/unix"

// renameExclusive renames from to to unless something is at to, in one step
func renameExclusive(from, to string) error {
	err := unix.RenamexNp(from, to, unix.RENAME_EXCL)
	// Some filesystems, such as network shares, lack the flag
	if err == unix.ENOTSUP || err == unix.EINVAL {
		return errNoExclusiveRename
	}
	return err
}
