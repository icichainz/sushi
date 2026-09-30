//go:build unix

package fs

import (
	"os"
	"syscall"
	"testing"
)

func TestUmaskIsTheProcesss(t *testing.T) {
	old := syscall.Umask(0o077)
	syscall.Umask(old)
	if umask != os.FileMode(old) {
		t.Fatalf("umask = %o, want the process's %o", umask, old)
	}
}
