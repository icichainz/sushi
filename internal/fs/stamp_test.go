package fs

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStampNoticesAFileReplacedByALookalike(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "d")
	os.Mkdir(dir, 0755)
	f := filepath.Join(dir, "notes.txt")
	writeFile(t, f, "abc")
	old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	os.Chtimes(f, old, old)
	os.Chtimes(dir, old, old)
	fileBefore, _ := TakeStamp(f)
	dirBefore, _ := TakeStamp(dir)

	// Replaced by another file of the same size and time, as cp -p or a
	// sync tool would leave it: count, size and times alone can't tell
	other := filepath.Join(t.TempDir(), "other")
	writeFile(t, other, "xyz")
	os.Chtimes(other, old, old)
	if err := os.Rename(other, f); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(dir, old, old)

	fileAfter, _ := TakeStamp(f)
	dirAfter, _ := TakeStamp(dir)
	if fileAfter.Entries != fileBefore.Entries || fileAfter.Size != fileBefore.Size || fileAfter.Latest != fileBefore.Latest {
		t.Fatalf("the lookalike differs in count, size or time: %+v, %+v", fileBefore, fileAfter)
	}
	if fileAfter == fileBefore {
		t.Fatal("a replaced file went unnoticed")
	}
	if dirAfter == dirBefore {
		t.Fatal("a file replaced inside a folder went unnoticed")
	}
	if again, _ := TakeStamp(dir); again != dirAfter {
		t.Fatal("stamp changed without a change")
	}
}
