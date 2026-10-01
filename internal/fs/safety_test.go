package fs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// Regression tests for data-safety problems found in review

// crossDevice is the error a rename between filesystems fails with
func crossDevice(src, dst string) error {
	return &os.LinkError{Op: "rename", Old: src, New: dst, Err: syscall.EXDEV}
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

// readDirHook makes readDir call hook after reading a directory, which can
// change what it returns
func readDirHook(t *testing.T, hook func(dir string, entries []os.DirEntry, err error) ([]os.DirEntry, error)) {
	t.Helper()
	real := readDir
	readDir = func(dir string) ([]os.DirEntry, error) {
		entries, err := real(dir)
		return hook(dir, entries, err)
	}
	t.Cleanup(func() { readDir = real })
}

// album makes a folder src with a.txt, b.txt and sub/c.txt
func album(t *testing.T) (src, dst string) {
	t.Helper()
	root := t.TempDir()
	src = filepath.Join(root, "album")
	os.MkdirAll(filepath.Join(src, "sub"), 0755)
	for _, name := range []string{"a.txt", "b.txt", filepath.Join("sub", "c.txt")} {
		writeFile(t, filepath.Join(src, name), name)
	}
	os.Mkdir(filepath.Join(root, "dst"), 0755)
	return src, filepath.Join(root, "dst", "album")
}

func TestPartlyReadFolderFailsTheCopyAndKeepsTheSource(t *testing.T) {
	// A folder that fails part way through being listed, as on a bad disk
	// or network share: ReadDir returns what it read and an error, which
	// the copy used to drop, so a move then deleted the rest unread
	readDirHook(t, func(dir string, entries []os.DirEntry, err error) ([]os.DirEntry, error) {
		if filepath.Base(dir) == "album" && err == nil && len(entries) > 1 {
			return entries[:1], errors.New("input/output error")
		}
		return entries, err
	})

	src, dst := album(t)
	if err := CopyPath(src, dst); err == nil || !strings.Contains(err.Error(), "input/output error") {
		t.Fatalf("copy: err = %v, want the read error", err)
	}

	acrossFilesystems(t)
	src, dst = album(t)
	if err := MovePath(src, dst); err == nil {
		t.Fatal("move: want the read error")
	}
	for _, name := range []string{"a.txt", "b.txt", filepath.Join("sub", "c.txt")} {
		if readFile(t, filepath.Join(src, name)) != name {
			t.Fatalf("move: %s was deleted", name)
		}
	}
	if Exists(dst) || len(leftovers(t, filepath.Dir(dst))) != 0 {
		t.Fatal("move: the partial copy was left behind")
	}
}

func TestUnreadableFolderFailsTheMoveAndKeepsTheSource(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("needs Unix permissions that apply to the user")
	}
	acrossFilesystems(t)
	src, dst := album(t)
	locked := filepath.Join(src, "sub")
	os.Chmod(locked, 0)
	t.Cleanup(func() { os.Chmod(locked, 0755) })

	if err := MovePath(src, dst); err == nil {
		t.Fatal("want the move to fail")
	}
	os.Chmod(locked, 0755)
	for _, name := range []string{"a.txt", "b.txt", filepath.Join("sub", "c.txt")} {
		if readFile(t, filepath.Join(src, name)) != name {
			t.Fatalf("%s was deleted", name)
		}
	}
}

func TestMoveAcrossFilesystemsKeepsWhatChangesMeanwhile(t *testing.T) {
	acrossFilesystems(t)
	src, dst := album(t)
	// Once the copy reaches sub, a file is added to the source and one it
	// has already copied is changed
	readDirHook(t, func(dir string, entries []os.DirEntry, err error) ([]os.DirEntry, error) {
		if filepath.Base(dir) == "sub" {
			writeFile(t, filepath.Join(src, "added.txt"), "new")
			writeFile(t, filepath.Join(src, "a.txt"), "a.txt, edited")
		}
		return entries, err
	})

	err := MovePath(src, dst)
	if err == nil || !strings.Contains(err.Error(), "changed during the move") {
		t.Fatalf("err = %v, want what changed reported", err)
	}
	if readFile(t, filepath.Join(src, "added.txt")) != "new" || readFile(t, filepath.Join(src, "a.txt")) != "a.txt, edited" {
		t.Fatal("what changed during the move was deleted")
	}
	// What was copied and didn't change is moved
	if Exists(filepath.Join(src, "b.txt")) || Exists(filepath.Join(src, "sub")) {
		t.Fatalf("unchanged files are still in the source: %v", dirEntries(t, src))
	}
	if readFile(t, filepath.Join(dst, "b.txt")) != "b.txt" || readFile(t, filepath.Join(dst, "sub", "c.txt")) != filepath.Join("sub", "c.txt") {
		t.Fatal("the copy is incomplete")
	}
}

func TestConcurrentTrashingKeepsEveryItem(t *testing.T) {
	const n = 40
	for _, c := range []struct {
		name              string
		freedesktop, dirs bool
		acrossFilesystems bool
	}{
		{name: "macOS files"},
		{name: "macOS folders", dirs: true},
		{name: "macOS folders across filesystems", dirs: true, acrossFilesystems: true},
		{name: "freedesktop folders", freedesktop: true, dirs: true},
		{name: "freedesktop files across filesystems", freedesktop: true, acrossFilesystems: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.acrossFilesystems {
				acrossFilesystems(t)
			}
			tr := &Trash{Files: filepath.Join(t.TempDir(), ".Trash")}
			if c.freedesktop {
				tr = testTrash(t)
			}
			// Items of the same name, as when two sushis, or sushi and the
			// Finder, trash "notes.txt" at once
			paths := make([]string, n)
			for i := range paths {
				dir := t.TempDir()
				if c.dirs {
					paths[i] = filepath.Join(dir, "photos")
					os.Mkdir(paths[i], 0755)
					writeFile(t, filepath.Join(paths[i], fmt.Sprintf("%d.jpg", i)), fmt.Sprint(i))
				} else {
					paths[i] = filepath.Join(dir, "notes.txt")
					writeFile(t, paths[i], fmt.Sprint(i))
				}
			}

			start := make(chan struct{})
			errs := make(chan error, n)
			var wg sync.WaitGroup
			for _, p := range paths {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					_, err := tr.Put(background(), p)
					errs <- err
				}()
			}
			close(start)
			wg.Wait()
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatal(err)
				}
			}

			// Every item is in the trash, on its own
			found := map[string]bool{}
			for _, name := range dirEntries(t, tr.Files) {
				item := filepath.Join(tr.Files, name)
				if c.dirs {
					inside := dirEntries(t, item)
					if len(inside) != 1 {
						t.Fatalf("%s holds %v: folders were merged", name, inside)
					}
					item = filepath.Join(item, inside[0])
				}
				found[readFile(t, item)] = true
			}
			if len(found) != n {
				t.Fatalf("the trash holds %d of the %d items: %v", len(found), n, dirEntries(t, tr.Files))
			}
		})
	}
}

// ignoresCase reports whether the filesystem holding dir takes names that
// differ only in case for the same, as macOS does by default
func ignoresCase(t *testing.T, dir string) bool {
	t.Helper()
	probe := filepath.Join(dir, "CaseProbe")
	writeFile(t, probe, "")
	defer os.Remove(probe)
	return Exists(filepath.Join(dir, "caseprobe"))
}

func TestCopyIntoItselfUnderAnotherCaseIsRefused(t *testing.T) {
	root := t.TempDir()
	if !ignoresCase(t, root) {
		t.Skip("the filesystem tells Proj from proj")
	}
	// "proj/sub/Proj" is inside "Proj" here: copying used to go on copying
	// the copy into itself until the path was too long
	src := filepath.Join(root, "Proj")
	os.MkdirAll(filepath.Join(src, "sub"), 0755)
	writeFile(t, filepath.Join(src, "main.go"), "package main")
	dst := filepath.Join(root, "proj", "sub", "Proj")

	if err := CopyPath(src, dst); !errors.Is(err, ErrDestInsideSource) {
		t.Fatalf("copy: err = %v, want ErrDestInsideSource", err)
	}
	if err := MovePath(src, dst); !errors.Is(err, ErrDestInsideSource) {
		t.Fatalf("move: err = %v, want ErrDestInsideSource", err)
	}
	if got := dirEntries(t, filepath.Join(src, "sub")); len(got) != 0 {
		t.Fatalf("something was copied: %v", got)
	}
}

func TestListingRemovesStaleLeftovers(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-48 * time.Hour)
	aged := func(path string) {
		t.Helper()
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	// What a killed sushi left: a partial file and a partial folder
	writeFile(t, filepath.Join(dir, ".sushi-partial-111"), "half")
	aged(filepath.Join(dir, ".sushi-partial-111"))
	os.Mkdir(filepath.Join(dir, ".sushi-partial-222"), 0755)
	writeFile(t, filepath.Join(dir, ".sushi-partial-222", "a"), "a")
	aged(filepath.Join(dir, ".sushi-partial-222", "a"))
	aged(filepath.Join(dir, ".sushi-partial-222"))
	// A copy still running, with a file in an old folder being written
	os.Mkdir(filepath.Join(dir, ".sushi-partial-333"), 0755)
	writeFile(t, filepath.Join(dir, ".sushi-partial-333", "growing"), "g")
	aged(filepath.Join(dir, ".sushi-partial-333"))
	// A bulk rename's temporary name holds a real file: never touched
	writeFile(t, filepath.Join(dir, ".sushi-rename-444"), "real")
	aged(filepath.Join(dir, ".sushi-rename-444"))

	files, err := ScanDirectory(dir, ScanOptions{ShowHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(names(files), ","); got != ".sushi-partial-333,.sushi-rename-444" {
		t.Fatalf("listed %s", got)
	}
	if got := strings.Join(dirEntries(t, dir), ","); got != ".sushi-partial-333,.sushi-rename-444" {
		t.Fatalf("left %s", got)
	}
}

func TestCopyLeavesOutUnfinishedFiles(t *testing.T) {
	src, dst := album(t)
	writeFile(t, filepath.Join(src, ".sushi-partial-555"), "another copy's")
	if err := CopyPath(src, dst); err != nil {
		t.Fatal(err)
	}
	if Exists(filepath.Join(dst, ".sushi-partial-555")) || !Exists(filepath.Join(dst, "a.txt")) {
		t.Fatalf("copied %v", dirEntries(t, dst))
	}
}

func TestCopyNewNeverTouchesWhatTookTheName(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "notes.txt")
	writeFile(t, file, "mine")
	folder := filepath.Join(root, "album")
	os.Mkdir(folder, 0755)
	writeFile(t, filepath.Join(folder, "a.jpg"), "a")

	// Duplicate picks "notes copy.txt" as free; someone takes it first
	takenFile := filepath.Join(root, "notes copy.txt")
	writeFile(t, takenFile, "theirs")
	takenFolder := filepath.Join(root, "album copy")
	os.Mkdir(takenFolder, 0755)
	writeFile(t, filepath.Join(takenFolder, "b.jpg"), "b")

	for src, dst := range map[string]string{file: takenFile, folder: takenFolder} {
		err := background().CopyNew(src, dst)
		if !errors.Is(err, ErrNotCreated) || !errors.Is(err, os.ErrExist) {
			t.Errorf("%s: err = %v, want one matching ErrNotCreated and os.ErrExist", filepath.Base(src), err)
		}
	}
	if readFile(t, takenFile) != "theirs" {
		t.Fatal("the file that took the name was replaced")
	}
	if got := dirEntries(t, takenFolder); len(got) != 1 || got[0] != "b.jpg" {
		t.Fatalf("the folder that took the name was merged into: %v", got)
	}
	if len(leftovers(t, root)) != 0 {
		t.Fatalf("left %v", leftovers(t, root))
	}

	// A free name is copied to as by Copy
	if err := background().CopyNew(folder, filepath.Join(root, "album copy 2")); err != nil {
		t.Fatal(err)
	}
	if readFile(t, filepath.Join(root, "album copy 2", "a.jpg")) != "a" {
		t.Fatal("the copy is incomplete")
	}
}

func TestExtractSaysWhenItCreatedNothing(t *testing.T) {
	root := t.TempDir()
	good, bad := filepath.Join(root, "good.zip"), filepath.Join(root, "bad.zip")
	writeZip(t, good, []entry{{name: "a", body: "a"}, {name: "a", body: "again"}})
	writeZip(t, bad, []entry{{name: "../escape", body: "x"}})
	taken := filepath.Join(root, "taken")
	os.Mkdir(taken, 0755)
	// Cut off in the second file's data, after the first was written
	cut := filepath.Join(root, "cut.tar")
	writeTar(t, cut, false, []entry{{name: "first", body: "1"}, {name: "second", body: strings.Repeat("2", 2000)}})
	os.Truncate(cut, 3*512+100)

	for _, c := range []struct {
		archive, dir string
		created      bool
	}{
		{good, taken, false},                    // The folder was there first
		{bad, filepath.Join(root, "b"), false},  // Refused before anything was made
		{good, filepath.Join(root, "g"), false}, // Refused once it had made the folder, which went
		{cut, filepath.Join(root, "c"), true},   // Failed once it had made the folder
		{filepath.Join(root, "x.rar"), filepath.Join(root, "r"), false},
	} {
		err := background().Extract(c.archive, c.dir)
		if err == nil || errors.Is(err, ErrNotCreated) == c.created {
			t.Errorf("%s into %s: err = %v, created = %v", filepath.Base(c.archive), filepath.Base(c.dir), err, c.created)
		}
		if c.dir != taken && Exists(c.dir) != c.created {
			t.Errorf("%s into %s: the folder is there: %v", filepath.Base(c.archive), filepath.Base(c.dir), Exists(c.dir))
		}
	}
}

func TestRefusedExtractionLeavesNothingBehind(t *testing.T) {
	// A tar archive is read in order, so entries before the bad one have
	// been written by the time it is refused
	for _, bad := range []entry{
		{name: "../outside.txt", body: "pwned"},
		{name: "link", body: "../outside.txt", mode: os.ModeSymlink},
		{name: "sub/ok.txt", body: "twice"},
		{name: "hard", body: "missing", hardlink: true},
	} {
		root, dest := sandbox(t)
		archive := filepath.Join(root, "a.tar")
		writeTar(t, archive, false, []entry{
			{name: "sub/", mode: os.ModeDir},
			{name: "sub/ok.txt", body: "fine"},
			bad,
		})
		err := background().Extract(archive, dest)
		if err == nil || !errors.Is(err, ErrNotCreated) {
			t.Errorf("%s: err = %v, want a refusal that created nothing", bad.name, err)
		}
		if Exists(dest) {
			t.Errorf("%s: the folder was left behind", bad.name)
		}
		assertOutsideUntouched(t, root)
	}

	// Something else in the folder's place by then is left alone
	root, dest := sandbox(t)
	archive := filepath.Join(root, "a.tar")
	writeTar(t, archive, false, []entry{{name: "ok.txt", body: "fine"}, {name: "../x", body: "pwned"}})
	x := newExtractor(background())
	if err := x.open(dest); err != nil {
		t.Fatal(err)
	}
	x.close()
	os.Rename(dest, dest+"-moved")
	os.Mkdir(dest, 0755)
	writeFile(t, filepath.Join(dest, "theirs"), "")
	if err := x.removeRefused(dest); err == nil || !Exists(filepath.Join(dest, "theirs")) {
		t.Fatalf("err = %v: another folder in its place was removed", err)
	}
}

func TestCopiedLinkReplacesOnlyWhenItIsPlaced(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(root, "link")
	symlink(t, "target", link)
	over := filepath.Join(root, "over")
	writeFile(t, over, "old")
	if err := CopyPath(link, over); err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(over); err != nil || target != "target" {
		t.Fatalf("over = %q, %v", target, err)
	}

	// Where it can't go, what is there stays and nothing is left over
	folder := filepath.Join(root, "folder")
	os.Mkdir(folder, 0755)
	writeFile(t, filepath.Join(folder, "keep"), "k")
	if err := CopyPath(link, folder); err == nil {
		t.Fatal("a link can't replace a folder")
	}
	if readFile(t, filepath.Join(folder, "keep")) != "k" || len(leftovers(t, root)) != 0 {
		t.Fatalf("left %v", leftovers(t, root))
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
