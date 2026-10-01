package fs

// isLocal reports whether path is on a local volume. Only macOS has the
// Finder tags it is asked for, so here every volume is.
func isLocal(path string) bool {
	return true
}
