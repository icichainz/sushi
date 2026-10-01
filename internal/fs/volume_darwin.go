package fs

import "golang.org/x/sys/unix"

// isLocal reports whether path is on a local volume rather than a network
// one (SMB, AFP, NFS, WebDAV), as macOS marks volumes with MNT_LOCAL. One
// that can't be told is taken for local.
func isLocal(path string) bool {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return true
	}
	return st.Flags&unix.MNT_LOCAL != 0
}
