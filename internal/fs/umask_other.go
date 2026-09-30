//go:build !unix

package fs

import "os"

// umask is zero where there is none: Windows only keeps whether a file is
// read-only, which modes from an archive can't widen
var umask os.FileMode
