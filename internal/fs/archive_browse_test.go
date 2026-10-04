package fs

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// browseEntries are the entries of the test archives: nested folders with
// no entries of their own, unicode names, a dotfile, a symlink, and names
// leading outside the archive
var browseEntries = []entry{
	{name: "./"},
	{name: "README.md", body: "# hello"},
	{name: "src/main.go", body: "package main"},
	{name: "src/util/strings.go", body: "package util"},
	{name: "src/util/"},
	{name: "docs/日本語/résumé.txt", body: "café"},
	{name: ".hidden", body: "secret"},
	{name: "src/latest", body: "main.go", mode: os.ModeSymlink | 0777},
	{name: "../evil.txt", body: "pwned"},
	{name: "/abs.txt", body: "pwned"},
}

func browseArchives(t *testing.T) map[string]string {
	t.Helper()
	dir := t.TempDir()
	paths := map[string]string{
		"zip":    filepath.Join(dir, "bundle.zip"),
		"tar.gz": filepath.Join(dir, "bundle.tar.gz"),
	}
	var zipEntries []entry
	for _, e := range browseEntries {
		// A zip's folders are entries ending in a slash
		if e.name != "./" {
			zipEntries = append(zipEntries, e)
		}
	}
	writeZip(t, paths["zip"], zipEntries)
	var tarEntries []entry
	for _, e := range browseEntries {
		if strings.HasSuffix(e.name, "/") {
			e.mode = os.ModeDir
		}
		tarEntries = append(tarEntries, e)
	}
	writeTar(t, paths["tar.gz"], true, tarEntries)
	return paths
}

func TestBrowseKind(t *testing.T) {
	for name, want := range map[string]string{
		"a.zip": "zip", "A.JAR": "zip", "a.tar": "tar", "a.tar.gz": "tar.gz", "a.tgz": "tar.gz",
		"a.tar.bz2": "tar.bz2", "a.tbz2": "tar.bz2", "a.gz": "", "a.txt": "", "zip": "",
	} {
		if got := BrowseKind(name); got != want {
			t.Errorf("BrowseKind(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestScanArchive(t *testing.T) {
	for kind, archive := range browseArchives(t) {
		ix, err := ReadArchiveIndex(archive)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if ix.Kind != kind || ix.Partial {
			t.Fatalf("%s: kind %q, partial %v", kind, ix.Kind, ix.Partial)
		}

		// Folders first, made up where there is no entry for them; the
		// unsafe names at the top, even with hidden files hidden
		top, err := ScanArchive(ix, "", ScanOptions{SortBy: "name"})
		if err != nil {
			t.Fatal(err)
		}
		if got := names(top); !slices.Equal(got, []string{"docs", "src", "../evil.txt", "/abs.txt", "README.md"}) {
			t.Errorf("%s: top = %q", kind, got)
		}
		withHidden, _ := ScanArchive(ix, "", ScanOptions{SortBy: "name", ShowHidden: true})
		if !slices.Contains(names(withHidden), ".hidden") {
			t.Errorf("%s: .hidden missing with hidden files shown: %q", kind, names(withHidden))
		}

		for _, f := range top {
			inner, ok := ix.Inner(f.Path)
			e, found := ix.Entry(inner)
			if !ok || !found {
				t.Fatalf("%s: %s is listed at %q, which the index doesn't know", kind, f.Name, f.Path)
			}
			switch f.Name {
			case "src":
				if !f.IsDir || f.Path != filepath.Join(archive, "src") {
					t.Errorf("%s: src = %+v", kind, f)
				}
			case "../evil.txt", "/abs.txt":
				// Nowhere on disk, and nowhere outside the archive's path
				if !e.Unsafe || !strings.Contains(f.Path, "\x00") || !strings.HasPrefix(f.Path, archive+"/") || filepath.Dir(f.Path) != archive {
					t.Errorf("%s: unsafe entry %q listed at %q", kind, f.Name, f.Path)
				}
			case "README.md":
				if f.Size != int64(len("# hello")) || f.IsDir {
					t.Errorf("%s: README = %+v", kind, f)
				}
			}
		}

		src, _ := ScanArchive(ix, "src", ScanOptions{SortBy: "name"})
		if got := names(src); !slices.Equal(got, []string{"util", "latest", "main.go"}) {
			t.Errorf("%s: src = %q", kind, got)
		}
		for _, f := range src {
			if f.Name == "latest" && (!f.IsSymlink || f.IsDir) {
				t.Errorf("%s: latest = %+v, want a symlink", kind, f)
			}
		}
		deep, _ := ScanArchive(ix, "docs/日本語", ScanOptions{})
		if got := names(deep); !slices.Equal(got, []string{"résumé.txt"}) {
			t.Errorf("%s: docs/日本語 = %q", kind, got)
		}
		if _, err := ScanArchive(ix, "README.md", ScanOptions{}); err == nil {
			t.Errorf("%s: listed a file as a folder", kind)
		}
		if _, err := ScanArchive(ix, "nope", ScanOptions{}); err == nil {
			t.Errorf("%s: listed a folder that isn't there", kind)
		}

		// A folder without an entry has the time of the newest in it
		e, _ := ix.Entry("docs")
		inside, _ := ix.Entry("docs/日本語/résumé.txt")
		if !e.ModTime.Equal(inside.ModTime) {
			t.Errorf("%s: docs dated %v, want %v", kind, e.ModTime, inside.ModTime)
		}
	}
}

func TestOpenEntry(t *testing.T) {
	for kind, archive := range browseArchives(t) {
		ix, err := ReadArchiveIndex(archive)
		if err != nil {
			t.Fatal(err)
		}
		for inner, want := range map[string]string{"src/util/strings.go": "package util", "docs/日本語/résumé.txt": "café"} {
			e, _ := ix.Entry(inner)
			rc, err := ix.OpenEntry(context.Background(), e)
			if err != nil {
				t.Fatalf("%s %s: %v", kind, inner, err)
			}
			b, _ := io.ReadAll(rc)
			rc.Close()
			if string(b) != want {
				t.Errorf("%s %s = %q, want %q", kind, inner, b, want)
			}
		}
		// Unsafe entries can still be read, to preview them
		for _, f := range mustScan(t, ix, "") {
			if f.Name == "../evil.txt" {
				inner, _ := ix.Inner(f.Path)
				e, _ := ix.Entry(inner)
				rc, err := ix.OpenEntry(context.Background(), e)
				if err != nil {
					t.Fatal(err)
				}
				b, _ := io.ReadAll(rc)
				rc.Close()
				if string(b) != "pwned" {
					t.Errorf("%s: evil = %q", kind, b)
				}
			}
		}
		dir, _ := ix.Entry("src")
		if _, err := ix.OpenEntry(context.Background(), dir); err == nil {
			t.Errorf("%s: opened a folder", kind)
		}
		link, _ := ix.Entry("src/latest")
		if target, err := ix.ReadLink(link); err != nil || target != "main.go" {
			t.Errorf("%s: link = %q, %v", kind, target, err)
		}
	}

	// A hard link reads as what it links to; a cancelled read stops
	archive := filepath.Join(t.TempDir(), "links.tar")
	writeTar(t, archive, false, []entry{{name: "a.txt", body: "shared"}, {name: "b.txt", body: "a.txt", hardlink: true}})
	ix, err := ReadArchiveIndex(archive)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := ix.Entry("b.txt")
	rc, err := ix.OpenEntry(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != "shared" {
		t.Errorf("hard link = %q", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ix.OpenEntry(ctx, b); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled read: %v", err)
	}
}

func mustScan(t *testing.T, ix *ArchiveIndex, inner string) []FileInfo {
	t.Helper()
	files, err := ScanArchive(ix, inner, ScanOptions{ShowHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestArchiveIndexFollowsChanges(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "a.zip")
	writeZip(t, archive, []entry{{name: "one.txt", body: "1"}})
	ix, err := ReadArchiveIndex(archive)
	if err != nil {
		t.Fatal(err)
	}
	if !ix.Fresh() {
		t.Fatal("a new index isn't fresh")
	}
	stamp := ix.Stamp()
	writeZip(t, archive, []entry{{name: "one.txt", body: "1"}, {name: "two.txt", body: "22"}})
	later := time.Now().Add(time.Minute)
	os.Chtimes(archive, later, later)
	if ix.Fresh() {
		t.Fatal("the index is fresh after the archive changed")
	}
	again, _ := ReadArchiveIndex(archive)
	if again.Stamp() == stamp {
		t.Fatal("a changed archive has the same stamp")
	}
	// Reading from an outdated index is refused rather than reading another entry
	e, _ := ix.Entry("one.txt")
	if _, err := ix.OpenEntry(context.Background(), e); err != nil {
		t.Fatalf("one.txt is still where it was: %v", err)
	}
	writeZip(t, archive, []entry{{name: "two.txt", body: "22"}})
	if _, err := ix.OpenEntry(context.Background(), e); !errors.Is(err, errChanged) {
		t.Fatalf("read from a changed archive: %v", err)
	}
}

func TestArchiveIndexLimits(t *testing.T) {
	saved := maxIndexEntries
	maxIndexEntries = 3
	t.Cleanup(func() { maxIndexEntries = saved })
	dir := t.TempDir()
	var many []entry
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		many = append(many, entry{name: n + ".txt", body: n})
	}
	for _, archive := range []string{filepath.Join(dir, "many.zip"), filepath.Join(dir, "many.tar")} {
		if strings.HasSuffix(archive, ".zip") {
			writeZip(t, archive, many)
		} else {
			writeTar(t, archive, false, many)
		}
		ix, err := ReadArchiveIndex(archive)
		if err != nil {
			t.Fatal(err)
		}
		if !ix.Partial || len(mustScan(t, ix, "")) != 3 {
			t.Errorf("%s: partial %v, %d listed", filepath.Base(archive), ix.Partial, len(mustScan(t, ix, "")))
		}
	}
	if _, err := ReadArchiveIndex(filepath.Join(dir, "missing.zip")); err == nil {
		t.Error("indexed a missing archive")
	}
	notZip := filepath.Join(dir, "fake.zip")
	writeFile(t, notZip, "not a zip")
	if _, err := ReadArchiveIndex(notZip); err == nil {
		t.Error("indexed a file that isn't a zip")
	}
}

func TestExtractEntries(t *testing.T) {
	for kind, archive := range browseArchives(t) {
		ix, err := ReadArchiveIndex(archive)
		if err != nil {
			t.Fatal(err)
		}
		root, dest := sandbox(t)
		os.Mkdir(dest, 0755)

		// A file and a folder, with what is in it and its symlink
		made, err := background().ExtractEntries(ix, []string{"docs/日本語/résumé.txt", "src"}, dest)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if want := []string{filepath.Join(dest, "résumé.txt"), filepath.Join(dest, "src")}; !slices.Equal(made, want) {
			t.Errorf("%s: made %q, want %q", kind, made, want)
		}
		if readFile(t, filepath.Join(dest, "résumé.txt")) != "café" || readFile(t, filepath.Join(dest, "src", "util", "strings.go")) != "package util" {
			t.Errorf("%s: contents wrong", kind)
		}
		if target, err := os.Readlink(filepath.Join(dest, "src", "latest")); err != nil || target != "main.go" {
			t.Errorf("%s: link = %q, %v", kind, target, err)
		}
		assertOutsideUntouched(t, root)
		if got := dirNamesIn(t, dest); !slices.Equal(got, []string{"résumé.txt", "src"}) {
			t.Errorf("%s: dest holds %q", kind, got)
		}

		// Names taken in dest, and unsafe entries, are refused before
		// anything is written
		_, err = background().ExtractEntries(ix, []string{"README.md", "src"}, dest)
		if !errors.Is(err, ErrNotCreated) || !strings.Contains(err.Error(), "src already exists") {
			t.Errorf("%s: over an existing name: %v", kind, err)
		}
		if Exists(filepath.Join(dest, "README.md")) {
			t.Errorf("%s: README.md was copied though the copy was refused", kind)
		}
		for _, f := range mustScan(t, ix, "") {
			if strings.HasPrefix(f.Name, "..") || strings.HasPrefix(f.Name, "/") {
				inner, _ := ix.Inner(f.Path)
				if _, err := background().ExtractEntries(ix, []string{inner}, dest); err == nil || !strings.Contains(err.Error(), "unsafe path") {
					t.Errorf("%s: %s copied out: %v", kind, f.Name, err)
				}
			}
		}
		assertOutsideUntouched(t, root)
		if got := dirNamesIn(t, dest); !slices.Equal(got, []string{"résumé.txt", "src"}) {
			t.Errorf("%s: dest holds %q after refusals", kind, got)
		}
		if _, err := background().ExtractEntries(ix, []string{"src", "src/main.go"}, t.TempDir()); err == nil {
			t.Errorf("%s: copied a folder and something inside it together", kind)
		}
	}
}

func TestExtractEntriesRefusesEscapingLinks(t *testing.T) {
	root, dest := sandbox(t)
	os.Mkdir(dest, 0755)
	archive := filepath.Join(root, "a.zip")
	writeZip(t, archive, []entry{
		{name: "pkg/inner/ok", body: "../file.txt", mode: os.ModeSymlink | 0777},
		{name: "pkg/file.txt", body: "fine"},
		{name: "pkg/inner/up", body: "../../outside.txt", mode: os.ModeSymlink | 0777},
	})
	ix, err := ReadArchiveIndex(archive)
	if err != nil {
		t.Fatal(err)
	}
	// Copied with its folder, pkg/inner/up leads out of what is copied
	if _, err := background().ExtractEntries(ix, []string{"pkg/inner"}, dest); err == nil || !errors.Is(err, ErrNotCreated) {
		t.Fatalf("escaping link copied: %v", err)
	}
	if got := dirNamesIn(t, dest); len(got) != 0 {
		t.Fatalf("a refused copy left %q", got)
	}
	// Copied whole, every link stays inside
	if _, err := background().ExtractEntries(ix, []string{"pkg"}, dest); err != nil {
		t.Fatalf("pkg: %v", err)
	}
	if readFile(t, filepath.Join(dest, "pkg", "inner", "ok")) != "fine" {
		t.Fatal("link inside the copy doesn't lead to its file")
	}
	assertOutsideUntouched(t, root)

	// A hard link is copied with what it links to, and refused without it
	tarPath := filepath.Join(root, "a.tar")
	writeTar(t, tarPath, false, []entry{{name: "d/a.txt", body: "shared"}, {name: "d/b.txt", body: "d/a.txt", hardlink: true}})
	tix, err := ReadArchiveIndex(tarPath)
	if err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	if _, err := background().ExtractEntries(tix, []string{"d"}, other); err != nil || readFile(t, filepath.Join(other, "d", "b.txt")) != "shared" {
		t.Fatalf("hard link with its target: %v", err)
	}
	if _, err := background().ExtractEntries(tix, []string{"d/b.txt"}, t.TempDir()); err == nil {
		t.Fatal("hard link copied without what it links to")
	}
}

func TestExtractEntriesCancelled(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "a.tar.gz")
	writeTar(t, archive, true, []entry{{name: "big.txt", body: strings.Repeat("x", 1<<16)}})
	ix, err := ReadArchiveIndex(archive)
	if err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewTask(ctx, 0, nil).ExtractEntries(ix, []string{"big.txt"}, dest); err == nil || !errors.Is(err, ErrNotCreated) {
		t.Fatalf("cancelled copy: %v", err)
	}
	if got := dirNamesIn(t, dest); len(got) != 0 {
		t.Fatalf("a cancelled copy left %q", got)
	}
}

// doneAfter is a context that is done once its Err has been asked for n
// times: a cancel arriving while something is being read
type doneAfter struct {
	context.Context
	n *int
}

func newDoneAfter(n int) doneAfter {
	return doneAfter{context.Background(), &n}
}

func (d doneAfter) Err() error {
	if *d.n <= 0 {
		return context.Canceled
	}
	*d.n--
	return nil
}

// noisyTar writes a tar.gz whose first entry is a megabyte that doesn't
// compress, so reading past it takes many reads of the file
func noisyTar(t *testing.T) string {
	t.Helper()
	noise := make([]byte, 1<<20)
	rand.NewChaCha8([32]byte{1}).Read(noise)
	archive := filepath.Join(t.TempDir(), "noisy.tar.gz")
	writeTar(t, archive, true, []entry{{name: "first.txt", body: "1"}, {name: "big.bin", body: string(noise)}, {name: "last.txt", body: "3"}})
	return archive
}

func TestArchiveReadsStopWithinAnEntry(t *testing.T) {
	archive := noisyTar(t)
	ix, err := ReadArchiveIndex(archive)
	if err != nil || ix.Partial {
		t.Fatalf("index: %v, partial %v", err, ix != nil && ix.Partial)
	}
	last, _ := ix.Entry("last.txt")

	// Reaching last.txt reads through big.bin in one step of the tar
	// reader: a cancel arriving meanwhile stops it there
	if _, err := ix.OpenEntry(newDoneAfter(20), last); !errors.Is(err, context.Canceled) {
		t.Fatalf("OpenEntry cancelled while skipping big.bin: %v", err)
	}
	rc, err := ix.OpenEntry(context.Background(), last)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != "3" {
		t.Fatalf("last.txt = %q", got)
	}
	// Reading the entry itself stops too
	big, _ := ix.Entry("big.bin")
	rc, err = ix.OpenEntry(newDoneAfter(20), big)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(rc); !errors.Is(err, context.Canceled) {
		t.Fatalf("reading big.bin went on after the cancel: %v", err)
	}
	rc.Close()

	// And copying last.txt out, which reads past big.bin too
	dest := t.TempDir()
	if _, err := NewTask(newDoneAfter(20), 0, nil).ExtractEntries(ix, []string{"last.txt"}, dest); !errors.Is(err, ErrNotCreated) {
		t.Fatalf("copy out cancelled while skipping big.bin: %v", err)
	}
	if got := dirNamesIn(t, dest); len(got) != 0 {
		t.Fatalf("a cancelled copy left %q", got)
	}

	// So does indexing, which lists what it has read by then
	slow := &ArchiveIndex{Path: archive, Kind: "tar.gz", byInner: map[string]int{}, children: map[string][]int{}}
	if err := slow.readTar(newDoneAfter(20)); err != nil || !slow.Partial {
		t.Fatalf("index cut short: %v, partial %v", err, slow.Partial)
	}
	if names := names(mustScan(t, slow, "")); slices.Contains(names, "last.txt") || !slices.Contains(names, "first.txt") {
		t.Fatalf("index cut short lists %q", names)
	}
	// One that has read nothing says why
	saved := indexTime
	indexTime = time.Nanosecond
	t.Cleanup(func() { indexTime = saved })
	if _, err := ReadArchiveIndex(archive); err == nil || !strings.Contains(err.Error(), "too slow to read") {
		t.Fatalf("index out of time: %v", err)
	}
}

func TestBrowseTarBz2(t *testing.T) {
	bz, err := exec.LookPath("bzip2")
	if err != nil {
		t.Skip("bzip2 not installed")
	}
	dir := t.TempDir()
	plain := filepath.Join(dir, "a.tar")
	writeTar(t, plain, false, []entry{{name: "x/y.txt", body: "bz"}})
	if out, err := exec.Command(bz, "-k", plain).CombinedOutput(); err != nil {
		t.Fatalf("bzip2: %v %s", err, out)
	}
	ix, err := ReadArchiveIndex(plain + ".bz2")
	if err != nil {
		t.Fatal(err)
	}
	if got := names(mustScan(t, ix, "x")); !slices.Equal(got, []string{"y.txt"}) {
		t.Fatalf("x = %q", got)
	}
	dest := t.TempDir()
	if _, err := background().ExtractEntries(ix, []string{"x/y.txt"}, dest); err != nil || readFile(t, filepath.Join(dest, "y.txt")) != "bz" {
		t.Fatalf("copy out of tar.bz2: %v", err)
	}
}

func dirNamesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}
