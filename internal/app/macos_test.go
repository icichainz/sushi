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
	// No test reaches the real pasteboard: it is off, unless a test stands
	// in for macOS with useFakeMac
	onMac = false
	osascript = func(context.Context, string) ([]byte, error) {
		return nil, errors.New("osascript isn't run in tests")
	}
}

// fakeMac stands in for macOS: a pasteboard
type fakeMac struct {
	mu    sync.Mutex
	count int      // The pasteboard's change count
	files []string // The files on it
	reads int      // Reads of the pasteboard
	fail  error    // Every osascript call fails with it, if set
}

// useFakeMac makes sushi think it runs on macOS, with f for its parts
func useFakeMac(t *testing.T) *fakeMac {
	t.Helper()
	f := &fakeMac{count: 100}
	oldMac, oldRun := onMac, osascript
	onMac, osascript = true, f.osascript
	t.Cleanup(func() { onMac, osascript = oldMac, oldRun })
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
	}
	return nil, errors.New("unexpected script")
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
