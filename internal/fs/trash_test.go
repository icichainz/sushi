package fs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testTrash returns a freedesktop-style trash inside a temp dir
func testTrash(t *testing.T) *Trash {
	t.Helper()
	base := filepath.Join(t.TempDir(), "Trash")
	return &Trash{Files: filepath.Join(base, "files"), Info: filepath.Join(base, "info")}
}

func TestTrashLocationPerSystem(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")

	mac, _ := trashFor("darwin")
	if mac.Files != filepath.Join(home, ".Trash") || mac.Info != "" {
		t.Errorf("macOS trash = %+v", mac)
	}
	linux, _ := trashFor("linux")
	if linux.Files != filepath.Join(home, ".local", "share", "Trash", "files") || linux.Info != filepath.Join(home, ".local", "share", "Trash", "info") {
		t.Errorf("Linux trash = %+v", linux)
	}

	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	if tr, _ := trashFor("linux"); tr.Files != filepath.Join(data, "Trash", "files") {
		t.Errorf("XDG_DATA_HOME ignored: %+v", tr)
	}
	// Relative values are to be ignored, says the specification
	t.Setenv("XDG_DATA_HOME", "relative/data")
	if tr, _ := trashFor("freebsd"); !strings.HasPrefix(tr.Files, home) {
		t.Errorf("relative XDG_DATA_HOME used: %+v", tr)
	}

	t.Setenv("AppData", data) // What os.UserConfigDir reads on Windows
	config, _ := os.UserConfigDir()
	if tr, _ := trashFor("windows"); tr.Files != filepath.Join(config, "sushi", "Trash", "files") {
		t.Errorf("Windows trash = %+v", tr)
	}
}

func TestTrashPutAndRestore(t *testing.T) {
	tr := testTrash(t)
	dir := t.TempDir()
	f := filepath.Join(dir, "100% done.txt")
	writeFile(t, f, "keep me")

	item, err := tr.Put(background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if Exists(f) || readFile(t, item.Path) != "keep me" || filepath.Dir(item.Path) != tr.Files {
		t.Fatalf("item = %+v", item)
	}
	info := readFile(t, item.Info)
	abs, _ := filepath.Abs(f)
	wantPath := "Path=" + strings.ReplaceAll(strings.ReplaceAll(filepath.ToSlash(abs), "%", "%25"), " ", "%20")
	if !strings.HasPrefix(info, "[Trash Info]\n") || !strings.Contains(info, wantPath+"\n") || !strings.Contains(info, "\nDeletionDate=") {
		t.Fatalf("trashinfo:\n%s\nwant %s", info, wantPath)
	}

	if err := item.Restore(background()); err != nil {
		t.Fatal(err)
	}
	if readFile(t, f) != "keep me" || Exists(item.Path) || Exists(item.Info) {
		t.Fatal("restore did not put the file back and clean up the trash")
	}
}

func TestTrashNeverReplaces(t *testing.T) {
	tr := testTrash(t)
	var items []TrashedItem
	for i, content := range []string{"first", "second", "third"} {
		dir := t.TempDir()
		f := filepath.Join(dir, "notes.txt")
		writeFile(t, f, content)
		if i == 2 {
			// A file in the trash without an info file still holds its name
			writeFile(t, filepath.Join(tr.Files, "notes 3.txt"), "orphan")
		}
		item, err := tr.Put(background(), f)
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, item)
	}

	want := []string{"notes.txt", "notes 2.txt", "notes 4.txt"}
	for i, item := range items {
		if filepath.Base(item.Path) != want[i] {
			t.Errorf("item %d went to %s, want %s", i, filepath.Base(item.Path), want[i])
		}
	}
	if readFile(t, filepath.Join(tr.Files, "notes 3.txt")) != "orphan" {
		t.Fatal("an item in the trash was replaced")
	}
	for i, content := range []string{"first", "second", "third"} {
		if err := items[i].Restore(background()); err != nil || readFile(t, items[i].Original) != content {
			t.Fatalf("restore %d: %v", i, err)
		}
	}
}

func TestMacTrashNeverReplaces(t *testing.T) {
	tr := &Trash{Files: filepath.Join(t.TempDir(), ".Trash")}
	a, b := filepath.Join(t.TempDir(), "x"), filepath.Join(t.TempDir(), "x")
	writeFile(t, a, "a")
	writeFile(t, b, "b")

	first, err1 := tr.Put(background(), a)
	second, err2 := tr.Put(background(), b)
	if err1 != nil || err2 != nil || first.Path == second.Path || first.Info != "" {
		t.Fatalf("first=%+v second=%+v errs=%v %v", first, second, err1, err2)
	}
	if readFile(t, first.Path) != "a" || readFile(t, second.Path) != "b" {
		t.Fatal("contents mixed up")
	}
}

func TestRestoreRefusesWhenOriginalIsTaken(t *testing.T) {
	tr := testTrash(t)
	f := filepath.Join(t.TempDir(), "a.txt")
	writeFile(t, f, "trashed")
	item, _ := tr.Put(background(), f)
	writeFile(t, f, "new file with the same name")

	err := item.Restore(background())
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err = %v", err)
	}
	if readFile(t, f) != "new file with the same name" || readFile(t, item.Path) != "trashed" || !Exists(item.Info) {
		t.Fatal("a refused restore changed something")
	}

	os.Remove(item.Path)
	if err := item.Restore(background()); err == nil || !strings.Contains(err.Error(), "no longer in the trash") {
		t.Fatalf("restoring an emptied item: %v", err)
	}
}

func TestRestoreReportsAnUnreachableTrash(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can read anything")
	}
	tr := testTrash(t)
	f := filepath.Join(t.TempDir(), "a.txt")
	writeFile(t, f, "")
	item, _ := tr.Put(background(), f)
	// Like macOS's privacy protection of ~/.Trash
	os.Chmod(tr.Files, 0)
	defer os.Chmod(tr.Files, 0700)

	err := item.Restore(background())
	if err == nil || !strings.Contains(err.Error(), "cannot reach a.txt in the trash") {
		t.Fatalf("err = %v", err)
	}
}

func TestTrashDirectoryAndSymlink(t *testing.T) {
	tr := testTrash(t)
	root := t.TempDir()
	dir := filepath.Join(root, "dir")
	os.Mkdir(dir, 0755)
	writeFile(t, filepath.Join(dir, "inside"), "x")
	link := filepath.Join(root, "link")
	os.Symlink(dir, link)

	// Only the link goes; its target stays
	item, err := tr.Put(background(), link)
	if err != nil || Exists(link) || readFile(t, filepath.Join(dir, "inside")) != "x" {
		t.Fatalf("trashing a symlink: %v", err)
	}
	if info, err := os.Lstat(item.Path); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the trashed link isn't a link")
	}
	if _, err := tr.Put(background(), dir); err != nil || Exists(dir) {
		t.Fatalf("trashing a directory: %v", err)
	}
}

func TestTrashRefusesItself(t *testing.T) {
	tr := testTrash(t)
	f := filepath.Join(t.TempDir(), "a")
	writeFile(t, f, "")
	item, _ := tr.Put(background(), f)

	if _, err := tr.Put(background(), item.Path); err == nil || !strings.Contains(err.Error(), "in the trash already") {
		t.Fatalf("trashing something in the trash: %v", err)
	}
	if _, err := tr.Put(background(), filepath.Dir(tr.Files)); err == nil {
		t.Fatal("the trash was moved into itself")
	}
	if _, err := tr.Put(background(), filepath.Dir(filepath.Dir(tr.Files))); err == nil || !strings.Contains(err.Error(), "into itself") {
		t.Fatalf("trashing the folder holding the trash: %v", err)
	}
}

func TestTrashShortensLongNames(t *testing.T) {
	name := strings.Repeat("é", 120) + ".txt" // 244 bytes
	if got := trashName(name, 1); got != name[:len(got)-4]+".txt" || len(got)+len(".trashinfo") > 255 {
		t.Fatalf("trashName = %d bytes", len(got))
	}
	if got := trashName(name, 12); !strings.HasSuffix(got, " 12.txt") || len(got) > 200 {
		t.Fatalf("numbered name %q", got)
	}
	if trashName("short.txt", 2) != "short 2.txt" {
		t.Fatal(trashName("short.txt", 2))
	}
}
