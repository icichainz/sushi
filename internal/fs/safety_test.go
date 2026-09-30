package fs

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

// Regression tests for data-safety problems found in review

// crossDevice is the error a rename between filesystems fails with
func crossDevice(src, dst string) error {
	errno := syscall.EXDEV
	if runtime.GOOS == "windows" {
		errno = syscall.Errno(17) // ERROR_NOT_SAME_DEVICE
	}
	return &os.LinkError{Op: "rename", Old: src, New: dst, Err: errno}
}

// acrossFilesystems makes Move's rename fail as it does between
// filesystems, so moves take the copy-and-delete path
func acrossFilesystems(t *testing.T) {
	t.Helper()
	real := renameForMove
	renameForMove = func(src, dst string, replace bool) error { return crossDevice(src, dst) }
	t.Cleanup(func() { renameForMove = real })
}

// linkedPhotos sets up src/photos and, at dst/photos, a symlink to a folder
// elsewhere, and returns the three
func linkedPhotos(t *testing.T) (src, dst, elsewhere string) {
	t.Helper()
	root := t.TempDir()
	src = filepath.Join(root, "src", "photos")
	os.MkdirAll(src, 0755)
	writeFile(t, filepath.Join(src, "beach.jpg"), "sand")
	elsewhere = filepath.Join(root, "elsewhere")
	os.Mkdir(elsewhere, 0755)
	writeFile(t, filepath.Join(elsewhere, "mine.txt"), "untouched")
	os.Mkdir(filepath.Join(root, "dst"), 0755)
	dst = filepath.Join(root, "dst", "photos")
	symlink(t, elsewhere, dst)
	return src, dst, elsewhere
}

// assertReplacedLink checks that dst became a real folder holding the copy,
// and that the folder the link led to wasn't written into
func assertReplacedLink(t *testing.T, what, dst, elsewhere string) {
	t.Helper()
	info, err := os.Lstat(dst)
	if err != nil || !info.IsDir() {
		t.Fatalf("%s: %s is not a folder now: %v", what, dst, err)
	}
	if readFile(t, filepath.Join(dst, "beach.jpg")) != "sand" {
		t.Fatalf("%s: the copy isn't in %s", what, dst)
	}
	if got := dirEntries(t, elsewhere); len(got) != 1 || got[0] != "mine.txt" {
		t.Fatalf("%s: wrote through the link into %v", what, got)
	}
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestCopyReplacesALinkedFolderRatherThanWritingThroughIt(t *testing.T) {
	src, dst, elsewhere := linkedPhotos(t)
	if err := CopyPath(src, dst); err != nil {
		t.Fatal(err)
	}
	assertReplacedLink(t, "copy", dst, elsewhere)
	if readFile(t, filepath.Join(src, "beach.jpg")) != "sand" {
		t.Fatal("the source changed")
	}
}

func TestMoveReplacesALinkedFolderRatherThanWritingThroughIt(t *testing.T) {
	for _, across := range []bool{false, true} {
		t.Run(map[bool]string{false: "rename", true: "across filesystems"}[across], func(t *testing.T) {
			if across {
				acrossFilesystems(t)
			}
			src, dst, elsewhere := linkedPhotos(t)
			if err := MovePath(src, dst); err != nil {
				t.Fatal(err)
			}
			assertReplacedLink(t, "move", dst, elsewhere)
			if Exists(src) {
				t.Fatal("the source is still there")
			}
		})
	}
}

func TestFailedMoveOntoALinkPutsTheLinkBack(t *testing.T) {
	acrossFilesystems(t)
	src, dst, elsewhere := linkedPhotos(t)
	// The copy fails: the source has something that can't be copied
	os.Mkdir(filepath.Join(src, "locked"), 0755)
	realReadDir := readDir
	readDir = func(name string) ([]os.DirEntry, error) {
		if filepath.Base(name) == "locked" {
			return nil, os.ErrPermission
		}
		return realReadDir(name)
	}
	t.Cleanup(func() { readDir = realReadDir })

	if err := MovePath(src, dst); err == nil {
		t.Fatal("the move should fail")
	}
	if target, err := os.Readlink(dst); err != nil || target != elsewhere {
		t.Fatalf("the link was not put back: %q, %v", target, err)
	}
	if readFile(t, filepath.Join(src, "beach.jpg")) != "sand" || len(leftovers(t, filepath.Dir(dst))) != 0 {
		t.Fatal("a failed move should leave the source as it was and nothing half-copied")
	}
}

func TestMoveKeepsWhatTurnsUpAtTheDestination(t *testing.T) {
	for _, across := range []bool{false, true} {
		root := t.TempDir()
		src := filepath.Join(root, "notes.txt")
		writeFile(t, src, "mine")
		dst := filepath.Join(root, "dst", "notes.txt")
		os.Mkdir(filepath.Dir(dst), 0755)

		// Someone else's file appears after the move has looked at dst
		real := renameForMove
		renameForMove = func(src, dst string, replace bool) error {
			writeFile(t, dst, "theirs")
			if across {
				return crossDevice(src, dst)
			}
			return real(src, dst, replace)
		}
		err := MovePath(src, dst)
		renameForMove = real
		if !errors.Is(err, os.ErrExist) {
			t.Errorf("across=%v: err = %v, want one matching os.ErrExist", across, err)
		}
		if readFile(t, dst) != "theirs" || readFile(t, src) != "mine" {
			t.Errorf("across=%v: a file that turned up was replaced", across)
		}
	}
}
