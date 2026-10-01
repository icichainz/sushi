package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
)

func init() {
	// No test reaches the real pasteboard, Launch Services or Finder: the
	// pasteboard is off, and Open With and Finder say they need macOS,
	// unless a test stands in for macOS with useFakeMac
	onMac = false
	osascript = func(context.Context, string) ([]byte, error) {
		return nil, errors.New("osascript isn't run in tests")
	}
	runOpen = func(...string) ([]byte, error) {
		return nil, errors.New("open isn't run in tests")
	}
}

// fakeMac stands in for macOS: a pasteboard, the apps Launch Services
// knows, and open(1) and Finder, which only record what they are asked
type fakeMac struct {
	mu       sync.Mutex
	count    int      // The pasteboard's change count
	files    []string // The files on it
	apps     string   // What the app lookup answers, as JSON
	lookups  []string // Scripts that looked apps up
	reads    int      // Reads of the pasteboard
	opened   [][]string
	revealed [][]string
	fail     error // Every osascript call fails with it, if set
}

// useFakeMac makes sushi think it runs on macOS, with f for its parts
func useFakeMac(t *testing.T) *fakeMac {
	t.Helper()
	f := &fakeMac{count: 100}
	oldMac, oldRun, oldOpen := onMac, osascript, runOpen
	onMac, osascript, runOpen = true, f.osascript, f.open
	t.Cleanup(func() { onMac, osascript, runOpen = oldMac, oldRun, oldOpen })
	return f
}

// scriptPaths reads the paths a script was given
func scriptPaths(script string) []string {
	_, rest, _ := strings.Cut(script, "const paths = ")
	list, _, _ := strings.Cut(rest, ";\n")
	var paths []string
	json.Unmarshal([]byte(list), &paths)
	return paths
}

func (f *fakeMac) osascript(_ context.Context, script string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	switch {
	case strings.Contains(script, "writeObjects"):
		f.count++
		f.files = scriptPaths(script)
		return fmt.Appendf(nil, `{"count":%d}`, f.count), nil
	case strings.Contains(script, "readObjectsForClasses"):
		f.reads++
		// The files are left unread when the count is the one sushi knows
		var known int
		_, rest, _ := strings.Cut(script, "if (count === ")
		fmt.Sscan(rest, &known)
		files, _ := json.Marshal(f.files)
		if known == f.count {
			files = []byte("[]")
		}
		return fmt.Appendf(nil, `{"count":%d,"files":%s}`, f.count, files), nil
	case strings.Contains(script, "URLsForApplicationsToOpenURL"):
		f.lookups = append(f.lookups, script)
		return []byte(f.apps), nil
	case strings.Contains(script, "activateFileViewerSelectingURLs"):
		f.revealed = append(f.revealed, scriptPaths(script))
		return []byte("{}"), nil
	}
	return nil, errors.New("unexpected script")
}

func (f *fakeMac) open(args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return []byte("LSOpenURLsWithRole() failed\n"), f.fail
	}
	f.opened = append(f.opened, args)
	return nil, nil
}

// finderCopies puts files on the pasteboard, as Cmd+C in Finder does
func (f *fakeMac) finderCopies(paths ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.count++
	f.files = paths
}

// pasteboard returns what is on the pasteboard
func (f *fakeMac) pasteboard() (int, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.count, slices.Clone(f.files)
}

// at shows dir in m's tab, as going there would
func at(t *testing.T, m Model, dir string) Model {
	t.Helper()
	return drain(t, m, m.loadDir(m.tab(), dir))
}
