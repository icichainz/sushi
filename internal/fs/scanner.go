package fs

import (
	"os"
	"path/filepath"
	"sort"
)

// ScanDirectory scans a directory and returns a list of files
func ScanDirectory(path string) ([]FileInfo, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	files := make([]FileInfo, 0, len(entries))

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue // Skip files we can't read
		}

		fullPath := filepath.Join(path, entry.Name())
		fileInfo := NewFileInfo(fullPath, info)

		// Report what a symlink points to, so links to directories can be
		// entered and previews see the real file size. Dangling links stay
		// as plain entries.
		if fileInfo.IsSymlink {
			if target, err := os.Stat(fullPath); err == nil {
				fileInfo.IsDir = target.IsDir()
				fileInfo.Size = target.Size()
			}
		}
		files = append(files, fileInfo)
	}

	// Sort: directories first, then by name
	sort.Slice(files, func(i, j int) bool {
		if files[i].IsDir != files[j].IsDir {
			return files[i].IsDir
		}
		return files[i].Name < files[j].Name
	})

	return files, nil
}
