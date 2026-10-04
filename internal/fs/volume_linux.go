package fs

// isLocal reports whether path is on a local volume. Only macOS has the
// Finder tags it is asked for, so here every volume is.
func isLocal(path string) bool {
	return true
}

// mountedOn reports whether the system says a volume is mounted at path.
// Here only the device numbers tell; see checkNotMount.
func mountedOn(path string) bool {
	return false
}
