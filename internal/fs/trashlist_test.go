package fs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// macTrash returns a ~/.Trash-style trash in a temp dir, with sushi's
// record beside it
func macTrash(t *testing.T) *Trash {
	t.Helper()
	base := t.TempDir()
	return &Trash{Files: filepath.Join(base, ".Trash"), Record: filepath.Join(base, "config", "trash.json")}
}

// entryNames returns the names of entries, in order
func entryNames(entries []TrashEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name
	}
	return out
}

func TestListFreedesktopTrash(t *testing.T) {
	tr := testTrash(t)
	if entries, err := tr.List(); err != nil || len(entries) != 0 {
		t.Fatalf("a trash that doesn't exist yet: %v %v", entries, err)
	}

	dir := t.TempDir()
	f := filepath.Join(dir, "100% sure.txt")
	writeFile(t, f, "data")
	item, err := tr.Put(background(), f)
	if err != nil {
		t.Fatal(err)
	}
	// An item another program trashed earlier, and one with no info file
	writeFile(t, filepath.Join(tr.Files, "older.txt"), "x")
	writeFile(t, filepath.Join(tr.Info, "older.txt.trashinfo"), "[Trash Info]\nPath=/somewhere/else/older%20one.txt\nDeletionDate=2020-05-06T07:08:09\n")
	writeFile(t, filepath.Join(tr.Files, "orphan"), "x")
	os.Chtimes(filepath.Join(tr.Files, "orphan"), time.Time{}, time.Now().Add(-48*time.Hour))
	// What another copy into the trash is still writing
	writeFile(t, filepath.Join(tr.Files, partialPrefix+"123"), "")

	entries, err := tr.List()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(entryNames(entries), ","); got != "100% sure.txt,orphan,older.txt" && got != "orphan,100% sure.txt,older.txt" {
		t.Fatalf("entries = %s", got)
	}
	byName := map[string]TrashEntry{}
	for _, e := range entries {
		byName[e.Name] = e
	}
	abs, _ := filepath.Abs(f)
	if e := byName["100% sure.txt"]; e.Original != abs || e.Info != item.Info || e.Path != item.Path || time.Since(e.Deleted) > time.Minute {
		t.Errorf("trashed by sushi: %+v", e)
	}
	want := time.Date(2020, 5, 6, 7, 8, 9, 0, time.Local)
	if e := byName["older.txt"]; e.Original != "/somewhere/else/older one.txt" || !e.Deleted.Equal(want) {
		t.Errorf("trashed by another program: %+v", e)
	}
	if e := byName["orphan"]; e.Original != "" || e.Info != "" {
		t.Errorf("without an info file: %+v", e)
	}

	// Put back from the listing, as the browser does
	if err := byName["100% sure.txt"].Item(abs).Restore(background()); err != nil || readFile(t, f) != "data" || Exists(item.Info) {
		t.Fatalf("restore: %v", err)
	}
}

func TestListMacTrashWithRecord(t *testing.T) {
	tr := macTrash(t)
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	writeFile(t, a, "a")
	writeFile(t, b, "b")
	ia, err := tr.Put(background(), a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Put(background(), b); err != nil {
		t.Fatal(err)
	}
	// Finder's own items and metadata
	writeFile(t, filepath.Join(tr.Files, "from finder.pdf"), "pdf")
	writeFile(t, filepath.Join(tr.Files, finderMetadata), "")

	entries, err := tr.List()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]TrashEntry{}
	for _, e := range entries {
		byName[e.Name] = e
	}
	if len(entries) != 3 || byName["a.txt"].Original != a || byName["b.txt"].Original != b || byName["from finder.pdf"].Original != "" {
		t.Fatalf("entries = %+v", entries)
	}
	if byName["a.txt"].Info != "" || time.Since(byName["a.txt"].Deleted) > time.Minute {
		t.Fatalf("a.txt = %+v", byName["a.txt"])
	}

	// The record: what was trashed, from where, when, and its inode there
	var notes trashNotes
	if err := json.Unmarshal([]byte(readFile(t, tr.Record)), &notes); err != nil || len(notes.Items) != 2 {
		t.Fatalf("record %v: %s", err, readFile(t, tr.Record))
	}
	if n := notes.Items[0]; n.Original != a || n.Trashed != ia.Path || n.Inode == 0 || n.Time.IsZero() {
		t.Fatalf("noted %+v", n)
	}

	// Once a.txt has left the trash, another item given its name there
	// isn't taken for it, and the next note drops it from the record
	if err := ia.Restore(background()); err != nil {
		t.Fatal(err)
	}
	writeFile(t, ia.Path, "someone else's a.txt")
	entries, _ = tr.List()
	for _, e := range entries {
		if e.Name == "a.txt" && e.Original != "" {
			t.Fatalf("a newcomer was taken for the noted item: %+v", e)
		}
	}
	c := filepath.Join(dir, "c.txt")
	writeFile(t, c, "c")
	if _, err := tr.Put(background(), c); err != nil {
		t.Fatal(err)
	}
	notes = trashNotes{}
	json.Unmarshal([]byte(readFile(t, tr.Record)), &notes)
	if len(notes.Items) != 2 || notes.Items[0].Original != b || notes.Items[1].Original != c {
		t.Fatalf("record after a.txt left: %+v", notes.Items)
	}
}

func TestRecordIsOnlyKeptWhereNeeded(t *testing.T) {
	// A freedesktop trash says where items came from itself
	tr := testTrash(t)
	tr.Record = filepath.Join(t.TempDir(), "trash.json")
	f := filepath.Join(t.TempDir(), "f")
	writeFile(t, f, "")
	if _, err := tr.Put(background(), f); err != nil || Exists(tr.Record) {
		t.Fatalf("record kept beside .trashinfo files: %v", err)
	}

	// An unwritable record doesn't stop the trashing
	mac := macTrash(t)
	writeFile(t, filepath.Dir(mac.Record), "a file where the folder should be")
	g := filepath.Join(t.TempDir(), "g")
	writeFile(t, g, "")
	if item, err := mac.Put(background(), g); err != nil || Exists(g) || !Exists(item.Path) {
		t.Fatalf("trashing with an unwritable record: %v", err)
	}
}

func TestDeleteFromTrash(t *testing.T) {
	tr := testTrash(t)
	outside := t.TempDir()
	keep := filepath.Join(outside, "keep.txt")
	writeFile(t, keep, "precious")

	dir := filepath.Join(t.TempDir(), "dir")
	os.MkdirAll(filepath.Join(dir, "sub"), 0755)
	writeFile(t, filepath.Join(dir, "sub", "x"), "x")
	item, err := tr.Put(background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	// A link in the trash to something outside it
	link := filepath.Join(t.TempDir(), "link")
	os.Symlink(outside, link)
	linkItem, err := tr.Put(background(), link)
	if err != nil {
		t.Fatal(err)
	}

	entries, _ := tr.List()
	for _, e := range entries {
		if err := tr.Delete(background(), e); err != nil {
			t.Fatalf("deleting %s: %v", e.Name, err)
		}
	}
	if Exists(item.Path) || Exists(item.Info) || Exists(linkItem.Path) || Exists(linkItem.Info) {
		t.Fatal("deleted items or their info files are still there")
	}
	if readFile(t, keep) != "precious" {
		t.Fatal("deleting a link in the trash touched its target")
	}

	// Anything not directly in the trash is refused
	os.MkdirAll(filepath.Join(tr.Files, "folder"), 0755)
	writeFile(t, filepath.Join(tr.Files, "folder", "inner"), "")
	for _, path := range []string{keep, filepath.Join(tr.Files, "folder", "inner"), tr.Files, filepath.Join(tr.Files, ".."), filepath.Join(tr.Files, "folder", "..", "..", "info")} {
		err := tr.Delete(background(), TrashEntry{Name: filepath.Base(path), Path: path})
		if err == nil {
			t.Fatalf("%s was deleted", path)
		}
	}
	// Nor through a link that leads out of the trash
	sneaky := filepath.Join(tr.Files, "sneaky")
	os.Symlink(outside, sneaky)
	if err := tr.Delete(background(), TrashEntry{Name: "keep.txt", Path: filepath.Join(sneaky, "keep.txt")}); err == nil || !strings.Contains(err.Error(), "not in the trash") {
		t.Fatalf("deleting through a link: %v", err)
	}
	if readFile(t, keep) != "precious" || !Exists(filepath.Join(tr.Files, "folder", "inner")) || !Exists(tr.Info) {
		t.Fatal("a refused delete deleted something")
	}
}

func TestDeleteFollowsALinkedTrash(t *testing.T) {
	// ~/.Trash itself may be a link to the trash's real place
	real := filepath.Join(t.TempDir(), "real-trash")
	os.Mkdir(real, 0700)
	linked := filepath.Join(t.TempDir(), ".Trash")
	os.Symlink(real, linked)
	tr := &Trash{Files: linked}
	writeFile(t, filepath.Join(real, "item"), "")

	entries, err := tr.List()
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries %v %v", entries, err)
	}
	if err := tr.Delete(background(), entries[0]); err != nil || Exists(filepath.Join(real, "item")) {
		t.Fatalf("delete through the linked trash: %v", err)
	}
}

func TestTrashRefusesToDeleteFromHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeFile(t, filepath.Join(home, "notes.txt"), "mine")
	for _, files := range []string{home, filepath.Dir(home), "/"} {
		tr := &Trash{Files: files}
		err := tr.Delete(background(), TrashEntry{Name: "notes.txt", Path: filepath.Join(home, "notes.txt")})
		if err == nil {
			t.Fatalf("a trash at %s deleted from it", files)
		}
	}
	if readFile(t, filepath.Join(home, "notes.txt")) != "mine" {
		t.Fatal("a file in the home folder was deleted")
	}
}

func TestEmptyTrash(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can delete anything")
	}
	tr := macTrash(t)
	for _, name := range []string{"a", "b"} {
		f := filepath.Join(t.TempDir(), name)
		writeFile(t, f, name)
		if _, err := tr.Put(background(), f); err != nil {
			t.Fatal(err)
		}
	}
	// A folder whose contents can't be deleted
	stuck := filepath.Join(tr.Files, "stuck")
	os.Mkdir(stuck, 0755)
	writeFile(t, filepath.Join(stuck, "inside"), "")
	os.Chmod(stuck, 0555)
	defer os.Chmod(stuck, 0755)
	writeFile(t, filepath.Join(tr.Files, finderMetadata), "finder's")

	deleted, failed, err := tr.Empty(background())
	if err != nil || len(deleted) != 2 || len(failed) != 1 || !strings.Contains(failed[0].Error(), "stuck") {
		t.Fatalf("deleted %v failed %v err %v", entryNames(deleted), failed, err)
	}
	left, _ := os.ReadDir(tr.Files)
	if len(left) != 2 || readFile(t, filepath.Join(tr.Files, finderMetadata)) != "finder's" || !Exists(stuck) {
		t.Fatalf("left in the trash: %v", left)
	}

	// Cancelled, it stops
	os.Chmod(stuck, 0755)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := tr.Empty(NewTask(ctx, 0, nil)); err == nil || !Exists(stuck) {
		t.Fatalf("a cancelled empty: %v", err)
	}
}
