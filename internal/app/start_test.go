package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/icichainz/sushi/internal/config"
	"github.com/icichainz/sushi/internal/fs"
)

// containsName reports whether files has one called name
func containsName(files []fs.FileInfo, name string) bool {
	return slices.ContainsFunc(files, func(f fs.FileInfo) bool { return f.Name == name })
}

// startDir makes a folder holding a.txt, b.txt, c.txt and a folder sub
func startDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		writeTestFile(t, filepath.Join(dir, name), name)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestStartingOnAFileSelectsItInItsFolder(t *testing.T) {
	dir := startDir(t)

	// A folder opens as before, at the top
	m := newTestModel(t, dir, nil)
	if m.tab().CurrentPath != dir || m.tab().Cursor != 0 || m.statusMsg != "" {
		t.Fatalf("a folder: %s, cursor %d, status %q", m.tab().CurrentPath, m.tab().Cursor, m.statusMsg)
	}

	// A file opens its folder with the cursor, and the preview, on it
	m = newTestModel(t, filepath.Join(dir, "c.txt"), nil)
	if m.tab().CurrentPath != dir || cursorName(m) != "c.txt" || m.statusMsg != "" {
		t.Fatalf("a file: %s, cursor on %q, status %q", m.tab().CurrentPath, cursorName(m), m.statusMsg)
	}
	if got := filepath.Base(m.tab().Preview.Path); got != "c.txt" {
		t.Fatalf("the preview shows %s", got)
	}
	if want := filepath.Base(dir); len(m.tab().ParentFiles) == 0 || !containsName(m.tab().ParentFiles, want) {
		t.Fatalf("the parent pane doesn't list %s", want)
	}

	// Given relative to the working directory
	t.Chdir(dir)
	m = newTestModel(t, "b.txt", nil)
	if m.tab().CurrentPath != dir || cursorName(m) != "b.txt" {
		t.Fatalf("a relative file: %s, cursor on %q", m.tab().CurrentPath, cursorName(m))
	}

	// The cursor stays on it once Init's work comes in, and as the folder
	// reloads
	cfg := config.DefaultConfig()
	cfg.Watch = false // Its listener would keep Init's batch from draining
	m = newTestModel(t, filepath.Join(dir, "b.txt"), cfg)
	m = drain(t, m, m.Init())
	m = drain(t, m, m.reloadTab(m.tab()))
	if cursorName(m) != "b.txt" {
		t.Fatalf("after Init and a reload, the cursor is on %q", cursorName(m))
	}
}

func TestStartingOnADotfileShowsHiddenFiles(t *testing.T) {
	dir := startDir(t)
	writeTestFile(t, filepath.Join(dir, ".env"), "SECRET=1")

	m := newTestModel(t, filepath.Join(dir, ".env"), nil)
	if !m.showHidden || cursorName(m) != ".env" {
		t.Fatalf("show hidden %v, cursor on %q", m.showHidden, cursorName(m))
	}

	// Other files leave dotfiles as the config has them
	m = newTestModel(t, filepath.Join(dir, "a.txt"), nil)
	if m.showHidden || containsName(m.tab().Files, ".env") {
		t.Fatal("a file that isn't hidden showed dotfiles")
	}
}

func TestStartingOnAFileNamedInAnotherNormalForm(t *testing.T) {
	dir := startDir(t)
	// Stored composed (é as one character), named decomposed (e and an
	// accent), and the other way round, as Finder and the shell may differ
	for _, tc := range []struct{ stored, given string }{
		{"café.txt", "café.txt"},
		{"résumé.txt", "résumé.txt"},
	} {
		writeTestFile(t, filepath.Join(dir, tc.stored), "x")
		given := filepath.Join(dir, tc.given)
		if _, err := os.Stat(given); err != nil {
			t.Skipf("this filesystem tells the two forms apart: %v", err)
		}
		m := newTestModel(t, given, nil)
		if m.tab().CurrentPath != dir || cursorName(m) != tc.stored || m.statusMsg != "" {
			t.Errorf("%q for %q: %s, cursor on %q, status %q", tc.given, tc.stored, m.tab().CurrentPath, cursorName(m), m.statusMsg)
		}
	}
}

func TestStartingOnLinks(t *testing.T) {
	dir := startDir(t)
	elsewhere := t.TempDir()
	writeTestFile(t, filepath.Join(elsewhere, "target.txt"), "x")

	// A link to a file elsewhere opens the link's folder, on the link
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(filepath.Join(elsewhere, "target.txt"), link); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t, link, nil)
	if m.tab().CurrentPath != dir || cursorName(m) != "link.txt" {
		t.Fatalf("a link to a file: %s, cursor on %q", m.tab().CurrentPath, cursorName(m))
	}

	// A link to nothing is still in its folder
	broken := filepath.Join(dir, "broken")
	if err := os.Symlink(filepath.Join(elsewhere, "gone"), broken); err != nil {
		t.Fatal(err)
	}
	m = newTestModel(t, broken, nil)
	if m.tab().CurrentPath != dir || cursorName(m) != "broken" || m.statusMsg != "" {
		t.Fatalf("a broken link: %s, cursor on %q, status %q", m.tab().CurrentPath, cursorName(m), m.statusMsg)
	}

	// A link to a folder opens the folder, by the link's path
	dirLink := filepath.Join(dir, "sub-link")
	if err := os.Symlink(filepath.Join(dir, "sub"), dirLink); err != nil {
		t.Fatal(err)
	}
	m = newTestModel(t, dirLink, nil)
	if m.tab().CurrentPath != dirLink {
		t.Fatalf("a link to a folder: %s", m.tab().CurrentPath)
	}
}

func TestStartingOnAMissingPathOpensTheNearestFolder(t *testing.T) {
	dir := startDir(t)

	missing := filepath.Join(dir, "gone", "deeper", "x.txt")
	m := newTestModel(t, missing, nil)
	if m.tab().CurrentPath != dir || m.statusMsg != "Can't find "+missing {
		t.Fatalf("%s, status %q", m.tab().CurrentPath, m.statusMsg)
	}
	if !isProblem(m.statusMsg) {
		t.Fatal("the note should look like a problem")
	}

	// Under a file, as in a.txt/x: its folder, on the file
	under := filepath.Join(dir, "a.txt", "x")
	m = newTestModel(t, under, nil)
	if m.tab().CurrentPath != dir || cursorName(m) != "a.txt" || m.statusMsg != "Can't find "+under {
		t.Fatalf("under a file: %s, cursor on %q, status %q", m.tab().CurrentPath, cursorName(m), m.statusMsg)
	}
}

func TestStartPlace(t *testing.T) {
	dir := startDir(t)
	for _, tc := range []struct {
		path, dir, focus, note string
	}{
		{dir, dir, "", ""},
		{filepath.Join(dir, "sub"), filepath.Join(dir, "sub"), "", ""},
		{filepath.Join(dir, "a.txt"), dir, filepath.Join(dir, "a.txt"), ""},
		{filepath.Join(dir, "sub", "nope"), filepath.Join(dir, "sub"), "", "Can't find " + filepath.Join(dir, "sub", "nope")},
		{"/", "/", "", ""},
	} {
		d, f, n := startPlace(tc.path)
		if d != tc.dir || f != tc.focus || n != tc.note {
			t.Errorf("startPlace(%s) = %q, %q, %q; want %q, %q, %q", tc.path, d, f, n, tc.dir, tc.focus, tc.note)
		}
	}

	// A path that can't be looked at, rather than one that is missing,
	// opens as it is, so the scan says why
	locked := filepath.Join(dir, "locked")
	os.Mkdir(locked, 0)
	t.Cleanup(func() { os.Chmod(locked, 0755) })
	inside := filepath.Join(locked, "file.txt")
	if _, err := os.Lstat(inside); err == nil || os.IsNotExist(err) {
		t.Skip("the folder can be searched without permission here, as by root")
	}
	if d, f, n := startPlace(inside); d != inside || f != "" || n != "" {
		t.Fatalf("startPlace(%s) = %q, %q, %q; want it as it is", inside, d, f, n)
	}
}
