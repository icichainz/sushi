package config

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"
)

// The folders visited, which the z palette offers to jump back to, are
// kept in history.json beside the bookmarks, ranked by "frecency" as zoxide
// ranks them: how often, and how lately, each was visited.

const (
	// HistoryLimit is the most folders history.json keeps; the least
	// frecent are dropped first
	HistoryLimit = 500
	// historyMaxCount is the most visits the folders may count between
	// them. Past it every count shrinks to fit in 90% of it, and folders
	// left with less than one visit are forgotten, as in zoxide: old habits
	// fade, so new ones can overtake them.
	historyMaxCount = 10000
)

// DirVisit is how often and how lately a folder was visited
type DirVisit struct {
	Path  string  `json:"path"`
	Count float64 `json:"count"` // Visits, scaled down as they age; see historyMaxCount
	Last  int64   `json:"last"`  // Unix time of the latest visit
}

// Score ranks a folder by frecency, as zoxide does: its visits, weighted by
// how long ago the latest of them was.
//
//	score = count × 4     visited within the last hour
//	        count × 2     within the last day
//	        count × 0.5   within the last week
//	        count × 0.25  longer ago
func (v DirVisit) Score(now time.Time) float64 {
	age := now.Sub(time.Unix(v.Last, 0))
	switch {
	case age < time.Hour:
		return v.Count * 4
	case age < 24*time.Hour:
		return v.Count * 2
	case age < 7*24*time.Hour:
		return v.Count / 2
	}
	return v.Count / 4
}

// historyMu keeps the saves of one process from interleaving
var historyMu sync.Mutex

// historyFile is what history.json holds
type historyFile struct {
	Dirs []DirVisit `json:"dirs"`
}

// historyPath returns where the history is kept
func historyPath() (string, error) {
	dir, err := getConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "history.json"), nil
}

// LoadDirHistory returns the folders in history.json, most frecent first.
// A missing or unreadable file is an empty history, as for bookmarks.
func LoadDirHistory() []DirVisit {
	path, err := historyPath()
	if err != nil {
		return nil
	}
	return readHistory(path)
}

// readHistory reads the history at path, leaving out entries that can't be
// right, such as relative paths, and merging any listed twice
func readHistory(path string) []DirVisit {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var f historyFile
	if json.Unmarshal(data, &f) != nil {
		return nil
	}
	byPath := make(map[string]DirVisit, len(f.Dirs))
	for _, v := range f.Dirs {
		if !filepath.IsAbs(v.Path) || math.IsNaN(v.Count) || math.IsInf(v.Count, 0) || v.Count <= 0 {
			continue
		}
		v.Path = filepath.Clean(v.Path)
		byPath[v.Path] = merge(byPath[v.Path], v)
	}
	return pruneHistory(slices.Collect(maps.Values(byPath)), time.Now())
}

// merge adds the visits of v to e
func merge(e, v DirVisit) DirVisit {
	e.Path = v.Path
	e.Count += v.Count
	e.Last = max(e.Last, v.Last)
	return e
}

// SaveDirHistory adds visits to history.json, and drops the folders in
// gone. The counts of visits are added to those in the file, rather than
// replacing them, so sushis running at the same time don't undo each
// other's visits. The file is replaced whole, through a temporary file, so
// it is never left half written.
func SaveDirHistory(visits []DirVisit, gone []string, now time.Time) error {
	historyMu.Lock()
	defer historyMu.Unlock()
	path, err := historyPath()
	if err != nil {
		return fmt.Errorf("finding the history file: %w", err)
	}

	byPath := make(map[string]DirVisit)
	for _, v := range readHistory(path) {
		byPath[v.Path] = v
	}
	for _, p := range gone {
		delete(byPath, p)
	}
	for _, v := range visits {
		byPath[v.Path] = merge(byPath[v.Path], v)
	}
	dirs := pruneHistory(slices.Collect(maps.Values(byPath)), now)

	data, err := json.MarshalIndent(historyFile{Dirs: dirs}, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding the history: %w", err)
	}
	return replaceFile(path, append(data, '\n'))
}

// pruneHistory ages the counts once they add up to more than
// historyMaxCount, and keeps the HistoryLimit most frecent folders, most
// frecent first
func pruneHistory(dirs []DirVisit, now time.Time) []DirVisit {
	total := 0.0
	for _, d := range dirs {
		total += d.Count
	}
	if total > historyMaxCount {
		factor := 0.9 * historyMaxCount / total
		kept := dirs[:0]
		for _, d := range dirs {
			if d.Count *= factor; d.Count >= 1 {
				kept = append(kept, d)
			}
		}
		dirs = kept
	}
	sort.Slice(dirs, func(i, j int) bool {
		a, b := dirs[i].Score(now), dirs[j].Score(now)
		if a != b {
			return a > b
		}
		return dirs[i].Path < dirs[j].Path
	})
	if len(dirs) > HistoryLimit {
		dirs = dirs[:HistoryLimit]
	}
	return dirs
}

// replaceFile writes data to path through a temporary file beside it,
// which is renamed over it once complete. Folder names say where someone
// works, so only its owner can read it.
func replaceFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".history-*.json")
	if err != nil {
		return fmt.Errorf("saving the history: %w", err)
	}
	defer os.Remove(tmp.Name()) // Nothing left to remove once renamed
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("saving the history: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("saving the history: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("saving the history: %w", err)
	}
	return nil
}
