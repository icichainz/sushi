package fs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRenameNoReplace(t *testing.T) {
	for name, rename := range map[string]func(from, to string) error{
		"in one step":   renameNoReplace,
		"after a check": renameAfterCheck,
	} {
		dir := t.TempDir()
		a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
		writeFile(t, a, "a")
		writeFile(t, b, "b")
		folder, other := filepath.Join(dir, "folder"), filepath.Join(dir, "other")
		os.Mkdir(folder, 0755)
		os.Mkdir(other, 0755)

		// Neither a file nor an empty folder is replaced
		if err := rename(a, b); !errors.Is(err, os.ErrExist) {
			t.Errorf("%s: file over file: err = %v, want one matching os.ErrExist", name, err)
		}
		if err := rename(folder, other); !errors.Is(err, os.ErrExist) {
			t.Errorf("%s: folder over empty folder: err = %v", name, err)
		}
		if readFile(t, a) != "a" || readFile(t, b) != "b" || !Exists(folder) || !Exists(other) {
			t.Fatalf("%s: a refused rename changed something", name)
		}

		// A free name is fine
		c, moved := filepath.Join(dir, "c"), filepath.Join(dir, "moved")
		if err := rename(a, c); err != nil || readFile(t, c) != "a" || Exists(a) {
			t.Errorf("%s: file to a free name: %v", name, err)
		}
		if err := rename(folder, moved); err != nil || !Exists(moved) || Exists(folder) {
			t.Errorf("%s: folder to a free name: %v", name, err)
		}
	}
}
