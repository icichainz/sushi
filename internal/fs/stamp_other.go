//go:build !(darwin || freebsd || netbsd || linux || openbsd || dragonfly || solaris)

package fs

import "os"

// fileIDs returns zeros: this system's file information has no device,
// inode or status change time that sushi reads
func fileIDs(info os.FileInfo) (dev, ino uint64, changed int64) {
	return 0, 0, 0
}
