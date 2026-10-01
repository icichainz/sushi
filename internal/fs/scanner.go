package fs

import (
	"cmp"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/icichainz/sushi/internal/tags"
)

// ScanOptions controls which entries are listed and in what order
type ScanOptions struct {
	ShowHidden  bool   // Include dotfiles
	SortBy      string // "name", "size", "modified" or "type"
	SortReverse bool   // Reverse the order within directories and files
	Tags        bool   // Read Finder tags
}

// ScanDirectory scans a directory and returns a list of files. What an
// unfinished copy left behind, a day old or more, is removed on the way.
func ScanDirectory(path string, opts ScanOptions) ([]FileInfo, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	files := make([]FileInfo, 0, len(entries))

	for _, entry := range entries {
		fullPath := filepath.Join(path, entry.Name())
		if isPartial(entry.Name()) && removeIfStale(fullPath) {
			continue
		}
		if !opts.ShowHidden && strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue // Skip files we can't read
		}

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
		if opts.Tags {
			// Cheap for untagged files; tags that can't be read are left out
			fileInfo.Tags, _ = tags.Read(fullPath)
		}
		files = append(files, fileInfo)
	}

	SortFiles(files, opts.SortBy, opts.SortReverse)
	return files, nil
}

// staleAge is how long the partial file or folder of an unfinished copy
// must have gone unchanged before a listing removes it. A copy still
// running keeps writing to it, so only what was left by a sushi that was
// killed, or lost power, gets this old.
const staleAge = 24 * time.Hour

// removeIfStale removes path, a partial copy or archive by its name, if
// nothing in it has changed for staleAge, and reports whether it did
func removeIfStale(path string) bool {
	cutoff := time.Now().Add(-staleAge)
	fresh := false
	err := filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().After(cutoff) {
			fresh = true
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil || fresh {
		return false
	}
	return os.RemoveAll(path) == nil
}

// SortFiles orders directories first, then by the given key. Size and
// modified put the largest and newest first, like ls -S and ls -t; name and
// type are alphabetical. Reverse flips the order but keeps directories first.
func SortFiles(files []FileInfo, by string, reverse bool) {
	sort.SliceStable(files, func(i, j int) bool {
		a, b := files[i], files[j]
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		if c := compareBy(a, b, by); c != 0 {
			if reverse {
				return c > 0
			}
			return c < 0
		}
		return compareNames(a, b) < 0
	})
}

// compareBy compares two files by the given sort key
func compareBy(a, b FileInfo, by string) int {
	switch by {
	case "size":
		return cmp.Compare(b.Size, a.Size)
	case "modified":
		return b.ModTime.Compare(a.ModTime)
	case "type":
		extA := strings.ToLower(filepath.Ext(a.Name))
		extB := strings.ToLower(filepath.Ext(b.Name))
		if c := strings.Compare(extA, extB); c != 0 {
			return c
		}
		return compareNames(a, b)
	default:
		return compareNames(a, b)
	}
}

// compareNames compares names ignoring case, so "apple" sorts next to
// "Apple" instead of after "Zebra"; exact case breaks ties
func compareNames(a, b FileInfo) int {
	if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
		return c
	}
	return strings.Compare(a.Name, b.Name)
}
