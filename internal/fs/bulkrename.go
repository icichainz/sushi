package fs

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
)

// RenamePair is one rename in a batch
type RenamePair struct {
	From, To string
}

// PlanRenames works out the renames asked for by an edited list of names,
// one line for each of paths in the same order, and checks that all of them
// can be done before any is. Lines left as they were are skipped. Each file
// stays in its own directory.
//
// It refuses a different number of lines, invalid names, two files given
// the same name, and names already taken, except by files that are being
// renamed away in the same batch: swaps and rotations are fine.
// occupied reports whether something other than src is at path.
func PlanRenames(paths []string, edited string, occupied func(path, src string) bool) ([]RenamePair, error) {
	lines := strings.Split(edited, "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) != len(paths) {
		return nil, fmt.Errorf("expected %d names, one per line, but got %d", len(paths), len(lines))
	}

	for i, a := range paths {
		for _, b := range paths[i+1:] {
			if within(a, b) || within(b, a) {
				return nil, fmt.Errorf("can't rename %s and something inside it together", filepath.Base(min(a, b)))
			}
		}
	}

	var pairs []RenamePair
	var lineOf []int
	moving := make(Leaving)
	for i, path := range paths {
		if lines[i] == filepath.Base(path) {
			continue
		}
		if err := ValidateName(lines[i]); err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		pairs = append(pairs, RenamePair{From: path, To: filepath.Join(filepath.Dir(path), lines[i])})
		lineOf = append(lineOf, i+1)
		moving.Add(path)
	}

	claimed := make(map[string]int)
	for i, p := range pairs {
		if other, ok := claimed[p.To]; ok {
			return nil, fmt.Errorf("lines %d and %d both say %s", other, lineOf[i], filepath.Base(p.To))
		}
		claimed[p.To] = lineOf[i]
		if !moving.Holds(p.To) && occupied(p.To, p.From) {
			return nil, fmt.Errorf("line %d: %s already exists", lineOf[i], filepath.Base(p.To))
		}
	}
	return pairs, nil
}

// Leaving is the files a batch of renames moves away from their names, by
// folder and name as file systems compare them (see NameKey), so a name
// being freed is known however it is spelled
type Leaving map[string][]string

// leaveKey keys path by its folder and folded name
func leaveKey(path string) string {
	return filepath.Dir(path) + "\x00" + NameKey(filepath.Base(path))
}

// Add notes that the file at path is renamed away
func (l Leaving) Add(path string) {
	k := leaveKey(path)
	l[k] = append(l[k], path)
}

// Holds reports whether what is at path is a file being renamed away: path
// itself, or the same file under another spelling of its name, as a file
// system that ignores case finds Notes.txt at notes.txt. Another file of
// that name, as a case-sensitive one can have, isn't.
func (l Leaving) Holds(path string) bool {
	paths := l[leaveKey(path)]
	if len(paths) == 0 {
		return false
	}
	for _, p := range paths {
		if p == path {
			return true
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	for _, p := range paths {
		if other, err := os.Lstat(p); err == nil && os.SameFile(info, other) {
			return true
		}
	}
	return false
}

// RenameAll does a batch of renames as if all at once, so names can be
// swapped or rotated: every file first moves to a temporary name beside it,
// then to its new name. It never replaces an existing file, even one that
// turns up while it runs, and if a step fails, the renames already done
// are undone, again without replacing anything.
func RenameAll(pairs []RenamePair) error {
	temps := make([]string, 0, len(pairs))
	// rollback puts everything back: the first done renames from their new
	// names to their temporary ones, then all from those to the originals
	rollback := func(done int) {
		for j := done - 1; j >= 0; j-- {
			renameNoReplace(pairs[j].To, temps[j])
		}
		for j := len(temps) - 1; j >= 0; j-- {
			renameNoReplace(temps[j], pairs[j].From)
		}
	}

	for _, p := range pairs {
		tmp := tempName(filepath.Dir(p.From), ".sushi-rename-")
		if err := renameNoReplace(p.From, tmp); err != nil {
			rollback(0)
			return fmt.Errorf("cannot rename %s: %w", filepath.Base(p.From), err)
		}
		temps = append(temps, tmp)
	}
	for i, p := range pairs {
		err := renameNoReplace(temps[i], p.To)
		if errors.Is(err, os.ErrExist) {
			rollback(i)
			return inTheWay(p.To)
		}
		if err != nil {
			rollback(i)
			return fmt.Errorf("cannot rename %s: %w", filepath.Base(p.From), err)
		}
	}
	return nil
}

// tempName returns an unused hidden name in dir, starting with prefix
func tempName(dir, prefix string) string {
	for {
		path := filepath.Join(dir, fmt.Sprintf("%s%016x", prefix, rand.Uint64()))
		if !Exists(path) {
			return path
		}
	}
}
