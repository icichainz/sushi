//go:build unix

package fs

import (
	"os"
	"syscall"
)

// umask is the process's file mode creation mask, which modes taken from
// an archive are masked with, as the kernel masks those of new files. The
// only way to read it is to set it, and it is shared by the whole process,
// so it is read once, while the package is initialised and before sushi
// starts any goroutine that could be creating files, and in between it is
// set to a mask that can only make a new file more private, never less.
var umask = func() os.FileMode {
	old := syscall.Umask(0o077)
	syscall.Umask(old)
	return os.FileMode(old) & os.ModePerm
}()
