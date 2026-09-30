//go:build !linux && !darwin && !windows

package fs

// renameExclusive can't be done in one step here, so renameNoReplace falls
// back to its slower ways
func renameExclusive(from, to string) error {
	return errNoExclusiveRename
}
