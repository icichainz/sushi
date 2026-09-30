package fs

import "golang.org/x/sys/windows"

// renameExclusive renames from to to unless something is at to, in one
// step: MoveFileEx replaces only when asked to and, without
// MOVEFILE_COPY_ALLOWED, fails across drives as rename does elsewhere
func renameExclusive(from, to string) error {
	f, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	t, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(f, t, 0)
}
