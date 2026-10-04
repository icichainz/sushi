package app

import (
	"fmt"
	"os"
)

// Close stops the work sushi does in the background: watching directories,
// searching and git status, closes the Quick Look window, and saves the
// folders visited that aren't saved yet. Call it once the program has
// finished.
func (m Model) Close() {
	m.watch.stop()
	m.stopFind()
	m.stopGit()
	m.quickLookWin.close()
	// Nowhere left to say it failed; the history is a convenience
	_ = m.frecent.flush()
	m.arc.cache.removeAll() // Copies of archive entries; see archive.go
}

// ExitDir returns the directory the shell should change to now that sushi
// has quit: the active tab's, or the folder holding the archive it is
// inside of, or "" after Q, which leaves the shell where it was
func (m Model) ExitDir() string {
	if m.keepShellDir {
		return ""
	}
	return m.tab().realDir()
}

// WriteExitDir writes ExitDir to path for the shell function printed by
// --print-shell-wrapper to cd to. The path is written as it is, without a
// newline, so any name survives. After Q nothing is written.
func (m Model) WriteExitDir(path string) error {
	dir := m.ExitDir()
	if dir == "" {
		return nil
	}
	if err := os.WriteFile(path, []byte(dir), 0600); err != nil {
		return fmt.Errorf("writing the last directory for the shell: %w", err)
	}
	return nil
}
