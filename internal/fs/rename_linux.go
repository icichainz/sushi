package fs

import "golang.org/x/sys/unix"

// renameExclusive renames from to to unless something is at to, in one step
func renameExclusive(from, to string) error {
	err := unix.Renameat2(unix.AT_FDCWD, from, unix.AT_FDCWD, to, unix.RENAME_NOREPLACE)
	// Kernels before 3.15 lack renameat2, and some filesystems the flag
	if err == unix.ENOSYS || err == unix.EINVAL {
		return errNoExclusiveRename
	}
	return err
}
