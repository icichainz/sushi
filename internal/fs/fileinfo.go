package fs

import (
	"os"
	"time"
)

// FileInfo represents metadata about a file or directory
type FileInfo struct {
	Name      string
	Path      string
	Size      int64
	ModTime   time.Time
	IsDir     bool
	IsSymlink bool
	Perms     os.FileMode
}

// NewFileInfo creates a FileInfo from os.FileInfo
func NewFileInfo(path string, info os.FileInfo) FileInfo {
	return FileInfo{
		Name:      info.Name(),
		Path:      path,
		Size:      info.Size(),
		ModTime:   info.ModTime(),
		IsDir:     info.IsDir(),
		IsSymlink: info.Mode()&os.ModeSymlink != 0,
		Perms:     info.Mode(),
	}
}
