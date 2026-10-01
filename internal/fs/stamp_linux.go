package fs

import (
	"os"
	"syscall"
)

// fileIDs returns the device and inode of info's file and when its status
// last changed, or zeros where the system doesn't say
func fileIDs(info os.FileInfo) (dev, ino uint64, changed int64) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, 0
	}
	return uint64(st.Dev), uint64(st.Ino), st.Ctim.Nano()
}
