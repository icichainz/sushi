package fs

import (
	"os"
	"time"

	"github.com/icichainz/sushi/internal/tags"
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
	Tags      []tags.Tag // Finder tags, when the scan read them
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
