package app

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/icichainz/sushi/internal/config"
	sfs "github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/ui/components"
)

// testEntry is one item of a test archive
type testEntry struct {
	name, body string
	link       bool // A symlink to body
}

// bundleEntries are the test archives' entries: folders with no entries of
// their own, unicode names, a symlink, and a name leading outside
var bundleEntries = []testEntry{
	{name: "README.md", body: "# hello\n"},
	{name: "src/main.go", body: "package main\n"},
	{name: "src/util/strings.go", body: "package util\n"},
	{name: "src/latest", body: "main.go", link: true},
	{name: "docs/日本語/résumé.txt", body: "café\n"},
	{name: "../evil.txt", body: "pwned"},
}

var entryTime = time.Date(2024, 3, 9, 10, 30, 0, 0, time.UTC)

func writeTestZip(t *testing.T, path string, entries []testEntry) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		hdr := &zip.FileHeader{Name: e.name, Method: zip.Deflate, Modified: entryTime}
		hdr.SetMode(0644)
		if e.link {
			hdr.SetMode(os.ModeSymlink | 0777)
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(e.body))
	}
	zw.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
}

func writeTestTarGz(t *testing.T, path string, entries []testEntry) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0644, Typeflag: tar.TypeReg, Size: int64(len(e.body)), ModTime: entryTime}
		if e.link {
			hdr.Typeflag, hdr.Linkname, hdr.Size = tar.TypeSymlink, e.body, 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if !e.link {
			tw.Write([]byte(e.body))
		}
	}
	tw.Close()
	gz.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
}

// archiveDir makes a folder holding bundle.zip and bundle.tar.gz, with a
// file beside it that escaping entries would hit, and points the system's
// temporary folder at one of the test's, where copies of entries go
func archiveDir(t *testing.T) string {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "evil.txt"), "untouched")
	dir := filepath.Join(root, "downloads")
	os.Mkdir(dir, 0755)
	writeTestZip(t, filepath.Join(dir, "bundle.zip"), bundleEntries)
	writeTestTarGz(t, filepath.Join(dir, "bundle.tar.gz"), bundleEntries)
	writeTestFile(t, filepath.Join(dir, "notes.txt"), "notes")
	return dir
}

// back presses h and applies what follows
func back(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := press(t, m, "h")
	return drain(t, m, cmd)
}

// previewNow loads the preview of the file under the cursor
func previewNow(t *testing.T, m Model) Model {
	t.Helper()
	return drain(t, m, m.previewCmd(m.tab()))
}

// statusLine returns the status bar as text
func statusLine(m Model) string {
	return ansi.Strip(m.renderStatusBar())
}

// evilUntouched fails if an entry named outside the archive got out
func evilUntouched(t *testing.T, dir string) {
	t.Helper()
	if readTestFile(t, filepath.Join(filepath.Dir(dir), "evil.txt")) != "untouched" {
		t.Fatal("an entry named outside the archive was written outside it")
	}
}

func TestBrowseInsideArchives(t *testing.T) {
	for _, name := range []string{"bundle.zip", "bundle.tar.gz"} {
		dir := archiveDir(t)
		archive := filepath.Join(dir, name)
		m := newTestModel(t, dir, nil)
		m = enter(t, m, name)

		tab := m.tab()
		if tab.archive == nil || tab.CurrentPath != archive {
			t.Fatalf("%s: enter didn't go inside: path %s, status %q", name, tab.CurrentPath, m.statusMsg)
		}
		// Folders first; the entry named outside is listed, by its name
		if got, want := names(tab), []string{"docs", "src", "../evil.txt", "README.md"}; !slices.Equal(got, want) {
			t.Fatalf("%s: top = %q, want %q", name, got, want)
		}
		for _, f := range tab.Files {
			if f.Name == "README.md" && (f.Size != int64(len("# hello\n")) || !f.ModTime.Equal(entryTime)) {
				t.Errorf("%s: README.md = size %d, time %v", name, f.Size, f.ModTime)
			}
		}
		if h := header(m); !strings.Contains(h, "downloads / "+name) {
			t.Errorf("%s: breadcrumb = %q", name, h)
		}
		if s := statusLine(m); !strings.Contains(s, "ARCHIVE") || !strings.Contains(s, "4 items") {
			t.Errorf("%s: status bar = %q", name, s)
		}
		if hints := ansi.Strip(m.renderBottomRow()); !strings.Contains(hints, "c copy out") {
			t.Errorf("%s: hints = %q", name, hints)
		}

		// Into folders the archive has no entries for, and back out
		m = enter(t, m, "src")
		if h := header(m); !strings.Contains(h, name+" / src") {
			t.Errorf("%s: breadcrumb in src = %q", name, h)
		}
		if got := names(m.tab()); !slices.Equal(got, []string{"util", "latest", "main.go"}) {
			t.Errorf("%s: src = %q", name, got)
		}
		// The parent pane lists the archive's top, with src marked
		if !slices.ContainsFunc(m.tab().ParentFiles, func(f sfs.FileInfo) bool { return f.Path == m.tab().CurrentPath }) {
			t.Errorf("%s: parent pane = %v", name, m.tab().ParentFiles)
		}
		m = enter(t, m, "util")
		if got := names(m.tab()); !slices.Equal(got, []string{"strings.go"}) {
			t.Errorf("%s: src/util = %q", name, got)
		}
		m = back(t, m)
		if cursorName(m) != "util" {
			t.Errorf("%s: going up put the cursor on %s", name, cursorName(m))
		}
		m = back(t, back(t, m))
		if m.tab().archive != nil || m.tab().CurrentPath != dir || cursorName(m) != name {
			t.Fatalf("%s: out at %s, cursor on %s", name, m.tab().CurrentPath, cursorName(m))
		}
		if strings.Contains(statusLine(m), "ARCHIVE") {
			t.Errorf("%s: still says ARCHIVE outside", name)
		}

		m = enter(t, enter(t, enter(t, m, name), "docs"), "日本語")
		if got := names(m.tab()); !slices.Equal(got, []string{"résumé.txt"}) {
			t.Errorf("%s: docs/日本語 = %q", name, got)
		}
		evilUntouched(t, dir)
	}
}

func TestArchiveScreensFillTheTerminal(t *testing.T) {
	dir := archiveDir(t)
	for _, size := range []tea.WindowSizeMsg{{Width: 140, Height: 40}, {Width: 100, Height: 24}, {Width: 80, Height: 24}, {Width: 60, Height: 15}, {Width: 30, Height: 10}} {
		m := resize(newTestModel(t, dir, nil), size)
		m = enter(t, m, "bundle.zip")
		m = previewNow(t, cursorTo(t, m, "README.md"))
		assertFills(t, "archive top", m)
		assertFills(t, "unsafe entry", previewNow(t, cursorTo(t, detach(m), "../evil.txt")))
		m = enter(t, m, "src")
		assertFills(t, "archive folder", m)
		refused, _ := press(t, detach(m), "d")
		assertFills(t, "refused", refused)
		found := find(t, detach(m), "f", "go")
		assertFills(t, "find", found)
	}
}

func TestArchiveEntryPreview(t *testing.T) {
	dir := archiveDir(t)
	m := newTestModel(t, dir, nil)
	m = enter(t, m, "bundle.tar.gz")

	m = previewNow(t, cursorTo(t, m, "README.md"))
	p := m.tab().Preview
	if p.Path != filepath.Join(dir, "bundle.tar.gz", "README.md") || p.FileInfo.Name != "README.md" || !strings.Contains(p.Content, "# hello") {
		t.Fatalf("preview of %s: %q, %v", p.Path, p.Content, p.Error)
	}
	view := strings.Join(plain(m.View()), "\n")
	if !strings.Contains(view, "# hello") {
		t.Fatalf("the preview isn't on screen:\n%s", view)
	}

	// The copy previewed was made in sushi's folder in the temporary one,
	// and is gone once the preview is loaded: the preview holds what it shows
	root := m.arc.cache.root
	if root == "" || !strings.HasPrefix(root, os.Getenv("TMPDIR")) {
		t.Fatalf("copies are in %q", root)
	}
	if copies := cachedCopies(t, m); len(copies) != 0 {
		t.Fatalf("copies after the preview = %q", copies)
	}

	// A folder lists what is in it; a link says where it points
	m = previewNow(t, cursorTo(t, m, "src"))
	if p := m.tab().Preview; p.Kind != "Directory" || !strings.Contains(p.Content, "main.go") || !strings.Contains(p.Content, "util") {
		t.Fatalf("folder preview: %q %q", p.Kind, p.Content)
	}
	m = enter(t, m, "src")
	m = previewNow(t, cursorTo(t, m, "latest"))
	if p := m.tab().Preview; p.LinkTarget != "main.go" {
		t.Fatalf("link preview: %+v", p)
	}
	// An entry named outside the archive previews safely
	m = back(t, m)
	m = previewNow(t, cursorTo(t, m, "../evil.txt"))
	if p := m.tab().Preview; !strings.Contains(p.Content, "pwned") || p.FileInfo.Name != "../evil.txt" {
		t.Fatalf("unsafe preview: %q", p.Content)
	}
	evilUntouched(t, dir)

	// Leaving the archive removes its copies, and quitting the folder
	m = back(t, m)
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatalf("after leaving, the cache holds %v (%v)", entries, err)
	}
	m.Close()
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("the cache is still there after quitting: %v", err)
	}
}

func TestArchivePreviewLimits(t *testing.T) {
	dir := archiveDir(t)
	writeTestZip(t, filepath.Join(dir, "big.zip"), []testEntry{{name: "huge.txt", body: strings.Repeat("a", maxEntryPreview+1)}})
	m := newTestModel(t, dir, nil)
	m = enter(t, m, "big.zip")
	m = previewNow(t, cursorTo(t, m, "huge.txt"))
	if p := m.tab().Preview; p.Kind != "Large file" || !strings.Contains(p.Content, "Too large") {
		t.Fatalf("preview = %q %q", p.Kind, p.Content)
	}
	if entries, _ := os.ReadDir(m.arc.cache.root); len(entries) != 0 {
		t.Fatal("an entry too large to preview was copied out")
	}
}

func TestArchiveIsReadOnly(t *testing.T) {
	dir := archiveDir(t)
	archive := filepath.Join(dir, "bundle.zip")
	before := readTestFile(t, archive)
	m := newTestModel(t, dir, nil)
	m = enter(t, m, "bundle.zip")
	m = cursorTo(t, m, "README.md")

	for _, k := range []string{"d", "D", "r", "R", "M", "n", "N", "m", "y", "V", "a", "X", "x", "v", "L"} {
		after, cmd := press(t, detach(m), k)
		after = drain(t, after, cmd)
		if after.mode != ModeNormal || after.statusMsg != readOnly {
			t.Errorf("%s: mode %v, status %q", k, after.mode, after.statusMsg)
		}
	}
	for k, want := range map[string]string{"P": "Plugins and shell commands", "!": "Plugins and shell commands",
		"F": "f finds them by name", "#": "no Finder tags", "O": "o opens a read-only copy"} {
		if after, _ := press(t, detach(m), k); after.mode != ModeNormal || !strings.Contains(after.statusMsg, want) {
			t.Errorf("%s: mode %v, status %q", k, after.mode, after.statusMsg)
		}
	}
	if readTestFile(t, archive) != before || len(dirNames(t, dir)) != 3 {
		t.Fatalf("something changed: %v", dirNames(t, dir))
	}
	// No git badges, no watching inside
	if !m.tab().git.off || m.gitStatus() != nil {
		t.Error("git runs inside an archive")
	}
	if m.watch != nil && slices.ContainsFunc(m.watch.wanted, func(d string) bool { return strings.HasPrefix(d, archive) }) {
		t.Errorf("watching %q", m.watch.wanted)
	}
	if m.watch != nil && !slices.Contains(m.watch.wanted, dir) {
		t.Errorf("not watching the folder holding the archive: %q", m.watch.wanted)
	}
	// The shell goes to the folder holding the archive
	if got := m.ExitDir(); got != dir {
		t.Errorf("ExitDir = %s", got)
	}
}

func TestCopyOutOfArchive(t *testing.T) {
	for _, name := range []string{"bundle.zip", "bundle.tar.gz"} {
		dir := archiveDir(t)
		dest := filepath.Join(dir, "dest")
		os.Mkdir(dest, 0755)
		m := newTestModel(t, dir, nil)
		m = enter(t, m, name)

		// Named outside the archive: never copied out
		m, _ = press(t, cursorTo(t, m, "../evil.txt"), "c")
		if !strings.Contains(m.statusMsg, "unsafe path") || len(m.clipboard) != 0 {
			t.Fatalf("%s: unsafe entry copied: %q", name, m.statusMsg)
		}

		m, _ = press(t, cursorTo(t, m, "README.md"), " ")
		m, _ = press(t, cursorTo(t, m, "src"), " ")
		m, _ = press(t, m, "c")
		if !strings.Contains(m.statusMsg, "Copied to clipboard: 2 items") || len(m.tab().Selected) != 0 {
			t.Fatalf("%s: copy: %q", name, m.statusMsg)
		}

		m = enter(t, back(t, m), "dest")
		m, cmd := press(t, m, "v")
		m = drain(t, m, cmd)
		if !strings.Contains(m.statusMsg, "Copied 2 items out of "+name) {
			t.Fatalf("%s: paste: %q", name, m.statusMsg)
		}
		if readTestFile(t, filepath.Join(dest, "README.md")) != "# hello\n" || readTestFile(t, filepath.Join(dest, "src", "util", "strings.go")) != "package util\n" {
			t.Fatalf("%s: dest = %v", name, dirNames(t, dest))
		}
		if target, err := os.Readlink(filepath.Join(dest, "src", "latest")); err != nil || target != "main.go" {
			t.Fatalf("%s: link = %q %v", name, target, err)
		}
		if got := dirNames(t, dest); !slices.Equal(got, []string{"README.md", "src"}) {
			t.Fatalf("%s: dest holds %q", name, got)
		}
		evilUntouched(t, dir)

		// The clipboard stays; pasting again would replace, so it is refused
		m, cmd = press(t, m, "v")
		m = drain(t, m, cmd)
		if !strings.Contains(m.statusMsg, "already exists here") {
			t.Fatalf("%s: second paste: %q", name, m.statusMsg)
		}
		if m, _ = press(t, m, "V"); !strings.Contains(m.statusMsg, "Can't link") {
			t.Fatalf("%s: V: %q", name, m.statusMsg)
		}
		// Undo takes the copies away
		m = undoNow(t, m)
		if got := dirNames(t, dest); len(got) != 0 {
			t.Fatalf("%s: after undo dest holds %q (%q)", name, got, m.statusMsg)
		}
		// Copying files on disk replaces the entries in the clipboard
		m, _ = press(t, cursorTo(t, at(t, m, dir), "notes.txt"), "c")
		if m.arc.clip.holds(m.clipboard) {
			t.Fatalf("%s: the entries are still the clipboard", name)
		}
	}
}

func TestCopyOutWithFinderSharing(t *testing.T) {
	mac := useFakeMac(t)
	dir := archiveDir(t)
	dest := filepath.Join(dir, "dest")
	os.Mkdir(dest, 0755)
	notes := filepath.Join(dir, "notes.txt")
	m := started(t, newTestModel(t, dir, nil))
	m, cmd := press(t, cursorTo(t, m, "notes.txt"), "c")
	m = drain(t, m, cmd)

	// Finder can't take entries: the pasteboard keeps what it had, and a
	// paste takes the entries, which are newer
	m = enter(t, m, "bundle.zip")
	m, cmd = press(t, cursorTo(t, m, "README.md"), "c")
	m = drain(t, m, cmd)
	if _, files := mac.pasteboard(); !slices.Equal(files, []string{notes}) {
		t.Fatalf("the pasteboard holds %q", files)
	}
	m = enter(t, back(t, m), "dest")
	m, cmd = press(t, m, "v")
	m = drain(t, m, cmd)
	if got := dirNames(t, dest); !slices.Equal(got, []string{"README.md"}) {
		t.Fatalf("dest holds %q: %q", got, m.statusMsg)
	}

	// Copied in Finder since, files win over the entries
	mac.finderCopies(notes)
	m, cmd = press(t, m, "v")
	m = drain(t, m, cmd)
	if got := dirNames(t, dest); !slices.Equal(got, []string{"README.md", "notes.txt"}) {
		t.Fatalf("dest holds %q: %q", got, m.statusMsg)
	}
}

func TestOpenArchiveEntries(t *testing.T) {
	record := fakeOpener(t)
	dir := archiveDir(t)
	cfg := config.DefaultConfig()
	cfg.Opener = "system"
	m := newTestModel(t, dir, cfg)
	m = enter(t, m, "bundle.zip")
	m = enter(t, m, "src")

	m, cmd := press(t, cursorTo(t, m, "main.go"), "o")
	m = drain(t, m, cmd)
	b, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("nothing opened: %q", m.statusMsg)
	}
	opened := string(b)
	if filepath.Base(opened) != "main.go" || !strings.HasPrefix(opened, m.arc.cache.root) || readTestFile(t, opened) != "package main\n" {
		t.Fatalf("opened %s", opened)
	}
	if info, _ := os.Stat(opened); info.Mode().Perm() != 0444 {
		t.Fatalf("the copy is %v", info.Mode())
	}
	if !strings.Contains(m.statusMsg, "read-only copy") {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}

	// Enter opens as o does, by the opener setting
	os.Remove(record)
	m, cmd = press(t, cursorTo(t, m, "main.go"), "enter")
	m = drain(t, m, cmd)
	if b, _ := os.ReadFile(record); string(b) != opened {
		t.Fatalf("enter opened %q", b)
	}

	// Folders and links aren't opened
	for _, entry := range []string{"util", "latest"} {
		if after, _ := press(t, cursorTo(t, detach(m), entry), "o"); !strings.Contains(after.statusMsg, "Can't open "+entry) {
			t.Errorf("%s: %q", entry, after.statusMsg)
		}
	}
}

func TestQuickLookArchiveEntry(t *testing.T) {
	bin := fakeQlmanage(t)
	dir := archiveDir(t)
	m := newTestModel(t, dir, nil)
	m = enter(t, m, "bundle.zip")
	m, cmd := press(t, cursorTo(t, m, "README.md"), "i")
	m = drain(t, m, cmd)
	wantQuickLook(t, bin, m.quickLookWin, filepath.Join(filepath.Dir(m.quickLookWin.paths[0]), "README.md"))
	if !strings.HasPrefix(m.quickLookWin.paths[0], m.arc.cache.root) || !strings.Contains(m.statusMsg, "read-only copy") {
		t.Fatalf("quick look on %q: %q", m.quickLookWin.paths, m.statusMsg)
	}
}

func TestFindInsideArchive(t *testing.T) {
	dir := archiveDir(t)
	m := newTestModel(t, dir, nil)
	m = enter(t, m, "bundle.zip")
	m = find(t, m, "f", "strings")
	if got := resultPaths(m); !slices.Equal(got, []string{"src/util/strings.go"}) {
		t.Fatalf("results = %q (%v)", got, m.find.err)
	}
	m, cmd := press(t, m, "enter")
	m = drain(t, m, cmd)
	if m.tab().CurrentPath != filepath.Join(dir, "bundle.zip", "src", "util") || cursorName(m) != "strings.go" || m.tab().archive == nil {
		t.Fatalf("went to %s, cursor on %s", m.tab().CurrentPath, cursorName(m))
	}

	// Below the folder shown only; searching contents says it can't
	m = find(t, back(t, m), "f", "main")
	if got := resultPaths(m); !slices.Equal(got, []string{"main.go"}) {
		t.Fatalf("results in src = %q", got)
	}
	m, _ = press(t, m, "esc")
	m = find(t, m, "f", "x")
	m, cmd = press(t, m, "tab")
	m = drain(t, m, cmd)
	if m.find.err == nil || !strings.Contains(m.findEmpty(), "can't be searched") {
		t.Fatalf("content search inside: %v / %q", m.find.err, m.findEmpty())
	}
}

func TestArchiveReloads(t *testing.T) {
	dir := archiveDir(t)
	archive := filepath.Join(dir, "bundle.zip")
	m := newTestModel(t, dir, nil)
	m = enter(t, m, "bundle.zip")
	m = previewNow(t, cursorTo(t, m, "README.md"))
	view := m.tab().archive

	// A refresh at the top stays inside, and reads the archive only if it changed
	m, cmd := ctrl(t, m, tea.KeyCtrlR)
	m = drain(t, m, cmd)
	if m.tab().archive != view || m.tab().CurrentPath != archive {
		t.Fatalf("refresh: at %s, archive %p (was %p)", m.tab().CurrentPath, m.tab().archive, view)
	}
	writeTestZip(t, archive, append(slices.Clone(bundleEntries), testEntry{name: "added.txt", body: "new"}))
	later := time.Now().Add(time.Minute)
	os.Chtimes(archive, later, later)
	m, cmd = ctrl(t, m, tea.KeyCtrlR)
	m = drain(t, m, cmd)
	if m.tab().archive == view || !slices.Contains(names(m.tab()), "added.txt") {
		t.Fatalf("after the archive changed: %q", names(m.tab()))
	}

	// A new tab here is inside too, with the same index
	m, cmd = press(t, m, "t")
	m = drain(t, m, cmd)
	if len(m.tabs) != 2 || m.tab().archive != m.tabs[0].archive || m.tab().CurrentPath != archive {
		t.Fatalf("new tab at %s", m.tab().CurrentPath)
	}

	// The archive gone, the tab goes up to its folder
	os.Remove(archive)
	m, cmd = ctrl(t, m, tea.KeyCtrlR)
	m = drain(t, m, cmd)
	if m.tab().archive != nil || m.tab().CurrentPath != dir {
		t.Fatalf("archive gone: at %s", m.tab().CurrentPath)
	}
}

// cachedCopies returns the copies of entries in the cache, by name
func cachedCopies(t *testing.T, m Model) []string {
	t.Helper()
	var out []string
	if m.arc.cache.root == "" {
		return nil
	}
	filepath.WalkDir(m.arc.cache.root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out = append(out, d.Name())
		}
		return nil
	})
	slices.Sort(out)
	return out
}

func TestSplitInsideAnArchive(t *testing.T) {
	dir := archiveDir(t)
	var told []string
	setHostDirectory = func(dir string) error { told = append(told, dir); return nil }
	t.Cleanup(func() { setHostDirectory = func(string) error { return nil } })
	m := newTestModel(t, dir, noWatch())
	m = enter(t, enter(t, m, "bundle.zip"), "src")

	// The second pane opens in the same folder of the archive, and is inside
	// it as the first is
	m, cmd := press(t, m, "w")
	m = drain(t, m, cmd)
	other := m.otherPane()
	if other == nil || other.archive != m.tab().archive || other.CurrentPath != filepath.Join(dir, "bundle.zip", "src") {
		t.Fatalf("second pane: %+v", other)
	}
	m, cmd = ctrl(t, m, tea.KeyCtrlL)
	m = drain(t, m, cmd)
	if !strings.Contains(statusLine(m), "ARCHIVE") {
		t.Errorf("status bar %q", statusLine(m))
	}
	if !m.tab().git.off {
		t.Error("git runs inside the archive in the second pane")
	}
	m = previewNow(t, cursorTo(t, m, "main.go"))
	if p := m.tab().Preview; p.Error != nil || !strings.Contains(p.Content, "package main") {
		t.Fatalf("preview in the second pane: %q, %v", p.Content, p.Error)
	}
	for _, k := range []string{"M", "n", "a", "d"} {
		if after, _ := press(t, detach(m), k); after.mode != ModeNormal || after.statusMsg != readOnly {
			t.Errorf("%s in the second pane: mode %v, status %q", k, after.mode, after.statusMsg)
		}
	}
	if after, _ := press(t, detach(m), "!"); after.mode != ModeNormal || !strings.Contains(after.statusMsg, "Plugins and shell commands") {
		t.Errorf("! in the second pane: %q", after.statusMsg)
	}
	// The shell and the window go to the folder holding the archive
	if got := m.ExitDir(); got != dir {
		t.Errorf("ExitDir = %s", got)
	}
	if len(told) == 0 || told[len(told)-1] != dir {
		t.Errorf("the host was told %q", told)
	}
}

func TestInactivePaneKeepsItsArchive(t *testing.T) {
	fakeOpener(t)
	dir := archiveDir(t)
	cfg := noWatch()
	cfg.Opener = "system"
	m := newTestModel(t, dir, cfg)
	m = enter(t, m, "bundle.zip")
	m, cmd := press(t, cursorTo(t, m, "README.md"), "o")
	m = drain(t, m, cmd)
	if got := cachedCopies(t, m); !slices.Equal(got, []string{"README.md"}) {
		t.Fatalf("copies after o: %q (%s)", got, m.statusMsg)
	}

	// Both panes inside; the active one leaves, and the other is still in
	// there, so the copy opened stays
	m, cmd = press(t, m, "w")
	m = back(t, drain(t, m, cmd))
	if m.tab().archive != nil || m.otherPane().archive == nil {
		t.Fatalf("active in %s, other in %s", m.tab().CurrentPath, m.otherPane().CurrentPath)
	}
	if got := cachedCopies(t, m); !slices.Equal(got, []string{"README.md"}) {
		t.Fatalf("the inactive pane's copies went: %q", got)
	}
	// Going back in reads nothing again: the other pane's index serves
	m = enter(t, m, "bundle.zip")
	if m.tab().archive != m.otherPane().archive {
		t.Error("the archive was read again rather than taken from the other pane")
	}
	m = back(t, m)

	// One pane again: the copies of the archive only the other was in go
	m, _ = press(t, m, "w")
	if got := cachedCopies(t, m); len(got) != 0 {
		t.Fatalf("copies after leaving dual pane: %q", got)
	}
}

func TestArchiveCopiesAreCapped(t *testing.T) {
	fakeOpener(t)
	saved := archiveCacheLimit
	archiveCacheLimit = 25
	t.Cleanup(func() { archiveCacheLimit = saved })
	dir := archiveDir(t)
	ten := strings.Repeat("x", 10)
	writeTestZip(t, filepath.Join(dir, "tens.zip"), []testEntry{{name: "a.txt", body: ten}, {name: "b.txt", body: ten},
		{name: "c.txt", body: ten}, {name: "d.txt", body: ten}})
	cfg := config.DefaultConfig()
	cfg.Opener = "system"
	m := newTestModel(t, dir, cfg)
	m = enter(t, m, "tens.zip")
	open := func(name string) {
		t.Helper()
		var cmd tea.Cmd
		m, cmd = press(t, cursorTo(t, m, name), "o")
		m = drain(t, m, cmd)
		if !strings.Contains(m.statusMsg, "read-only copy") {
			t.Fatalf("o on %s: %q", name, m.statusMsg)
		}
	}

	open("a.txt")
	open("b.txt")
	if got := cachedCopies(t, m); !slices.Equal(got, []string{"a.txt", "b.txt"}) {
		t.Fatalf("copies = %q", got)
	}
	// a.txt, used again, is now more recent than b.txt, which makes room
	open("a.txt")
	open("c.txt")
	if got := cachedCopies(t, m); !slices.Equal(got, []string{"a.txt", "c.txt"}) || m.arc.cache.size != 20 {
		t.Fatalf("copies past the limit = %q (%d bytes)", got, m.arc.cache.size)
	}
	// A preview of an entry opened reads its copy, and leaves it
	m = previewNow(t, cursorTo(t, m, "c.txt"))
	if p := m.tab().Preview; p.Content != ten || !slices.Equal(cachedCopies(t, m), []string{"a.txt", "c.txt"}) {
		t.Fatalf("preview %q, copies %q", p.Content, cachedCopies(t, m))
	}
	// Entries opened together never make room for one another, even past
	// the limit
	m, _ = press(t, cursorTo(t, m, "b.txt"), " ")
	m, _ = press(t, cursorTo(t, m, "d.txt"), " ")
	m, cmd := press(t, m, "o")
	m = drain(t, m, cmd)
	if got := cachedCopies(t, m); !slices.Equal(got, []string{"b.txt", "d.txt"}) {
		t.Fatalf("copies after opening two = %q (%s)", got, m.statusMsg)
	}
}

// noisyTarGz writes a tar.gz whose first entry is a megabyte that doesn't
// compress, so reading past it takes a while, and a small one after it
func noisyTarGz(t *testing.T, path string) {
	t.Helper()
	noise := make([]byte, 1<<20)
	rand.NewChaCha8([32]byte{1}).Read(noise)
	writeTestTarGz(t, path, []testEntry{{name: "noise.bin", body: string(noise)}, {name: "last.txt", body: "the end\n"}})
}

func TestEntryPreviewCancelledWhenTheCursorMoves(t *testing.T) {
	dir := archiveDir(t)
	noisyTarGz(t, filepath.Join(dir, "noisy.tar.gz"))
	m := newTestModel(t, dir, nil)
	m = enter(t, m, "noisy.tar.gz")
	m = cursorTo(t, m, "last.txt")

	// The preview of last.txt is on its way when the cursor moves on: it is
	// cancelled, and brings nothing
	slow := m.previewCmd(m.tab())
	m, next := press(t, m, "j")
	if cursorName(m) == "last.txt" {
		t.Fatal("the cursor didn't move")
	}
	if msg := slow(); msg != nil {
		t.Fatalf("the cancelled preview brought %#v", msg)
	}
	m = drain(t, m, next)
	if p := m.tab().Preview; p.Path != m.tab().Files[m.tab().Cursor].Path {
		t.Fatalf("preview of %s with the cursor on %s", p.Path, cursorName(m))
	}
	m = previewNow(t, cursorTo(t, m, "last.txt"))
	if p := m.tab().Preview; p.Content != "the end" {
		t.Fatalf("preview of last.txt: %q, %v", p.Content, p.Error)
	}

	// One that runs out of time says so
	saved := entryPreviewTime
	entryPreviewTime = time.Nanosecond
	t.Cleanup(func() { entryPreviewTime = saved })
	m.tab().Preview = components.PreviewContent{}
	m = previewNow(t, m)
	if p := m.tab().Preview; p.Error == nil || !strings.Contains(p.Content, "took over") {
		t.Fatalf("preview out of time: %q, %v", p.Content, p.Error)
	}
}

func TestViewsFromInsideAnArchive(t *testing.T) {
	dir := archiveDir(t)
	m := newTestModel(t, dir, noWatch())
	m = enter(t, m, "bundle.zip")

	// U counts the folder holding the archive, with the cursor on it
	du, cmd := press(t, detach(m), "U")
	du = drain(t, du, cmd)
	if r, _, _ := du.du.chosen(); du.mode != ModeDiskUsage || du.du.path != dir || r.name != "bundle.zip" || du.du.status.err != nil {
		t.Fatalf("U inside: mode %v at %s on %s, %v", du.mode, du.du.path, r.name, du.du.status.err)
	}

	// p in the trash browser restores into the folder holding the archive
	os.MkdirAll(trashDir(t), 0700)
	writeTestFile(t, filepath.Join(trashDir(t), "from finder.pdf"), "pdf")
	m = openTrashView(t, m)
	m, cmd = press(t, m, "p")
	m = drain(t, m, cmd)
	if readTestFile(t, filepath.Join(dir, "from finder.pdf")) != "pdf" {
		t.Fatalf("restore here, inside an archive: %q", m.statusMsg)
	}
}

func TestTransfersAndArchives(t *testing.T) {
	dir := archiveDir(t)
	out := filepath.Join(t.TempDir(), "out")
	os.Mkdir(out, 0755)
	archive := filepath.Join(dir, "bundle.zip")
	before := readTestFile(t, archive)
	m := newTestModel(t, dir, noWatch())
	m = enter(t, m, "bundle.zip")
	m, cmd := press(t, m, "w")
	m = goThere(t, drain(t, m, cmd), out)

	// > copies entries out into the other pane's folder, as c and v would
	m = cursorTo(t, m, "src")
	m, _ = press(t, m, " ")
	m = cursorTo(t, m, "README.md")
	m, _ = press(t, m, " ")
	m, cmd = press(t, m, ">")
	m = drain(t, m, cmd)
	if !fileExists(filepath.Join(out, "README.md")) {
		t.Fatalf("> from inside an archive: %q", m.statusMsg)
	}
	if readTestFile(t, filepath.Join(out, "README.md")) != "# hello\n" || readTestFile(t, filepath.Join(out, "src", "main.go")) != "package main\n" {
		t.Fatalf("> from inside an archive: %q, out holds %q", m.statusMsg, dirNames(t, out))
	}
	if len(m.tab().Selected) != 0 || !slices.Contains(names(m.otherPane()), "README.md") {
		t.Fatalf("after >: selection %v, other pane %q", m.tab().Selected, names(m.otherPane()))
	}
	// Never over what is there, and ctrl+z takes it back
	if again, _ := press(t, detach(m), ">"); !strings.Contains(again.statusMsg, "README.md already exists in the other pane") {
		t.Errorf("> again: %q", again.statusMsg)
	}
	m = undoNow(t, m)
	if got := dirNames(t, out); len(got) != 0 {
		t.Fatalf("after undo, out holds %q", got)
	}

	// < would take entries out of the archive
	if after, _ := press(t, detach(m), "<"); after.statusMsg != readOnly || after.job != nil {
		t.Errorf("< from inside an archive: %q", after.statusMsg)
	}
	// Nothing goes into a pane inside an archive
	m, _ = ctrl(t, m, tea.KeyCtrlL)
	writeTestFile(t, filepath.Join(out, "new.txt"), "new")
	m, cmd = ctrl(t, m, tea.KeyCtrlR)
	m = drain(t, m, cmd)
	m = cursorTo(t, m, "new.txt")
	for _, k := range []string{">", "<"} {
		if after, _ := press(t, detach(m), k); after.statusMsg != "Read-only: the other pane is inside an archive" || after.job != nil {
			t.Errorf("%s into an archive: %q", k, after.statusMsg)
		}
	}
	if readTestFile(t, archive) != before || readTestFile(t, filepath.Join(out, "new.txt")) != "new" {
		t.Fatal("the archive or the file changed")
	}
}

func TestTransfersWaitForTheFolderToOpen(t *testing.T) {
	_, left, right := twoFolders(t)
	m := dualModel(t, left, right)
	m = cursorTo(t, m, "a.txt")

	// The other pane is leaving right for somewhere else: > would put the
	// copy in right
	other := m.otherPane()
	other.Loading, other.loadingTo = true, filepath.Dir(right)
	for _, k := range []string{">", "<"} {
		if after, _ := press(t, detach(m), k); after.statusMsg != "Wait for the other pane to open its folder" || after.job != nil {
			t.Errorf("%s while the other pane opens a folder: %q", k, after.statusMsg)
		}
	}
	// Reloading the folder it shows is no reason to wait
	other.loadingTo = right
	if after, cmd := press(t, detach(m), ">"); after.job == nil {
		t.Errorf("> while the other pane reloads: %q", after.statusMsg)
	} else {
		drain(t, after, cmd)
	}

	// Nor does a paste go to the folder the pane is leaving
	other.Loading = false
	m, _ = press(t, m, "c")
	tab := m.tab()
	tab.Loading, tab.loadingTo = true, right
	if after, _ := press(t, detach(m), "v"); after.statusMsg != stillOpening || after.job != nil || after.mode != ModeNormal {
		t.Errorf("v while leaving: %q", after.statusMsg)
	}
}

func TestCopyOutRefusedWhole(t *testing.T) {
	dir := archiveDir(t)
	out := t.TempDir()
	writeTestZip(t, filepath.Join(dir, "links.zip"), []testEntry{{name: "d/ok.txt", body: "ok"},
		{name: "d/away", body: "../../../outside", link: true}})
	m := newTestModel(t, dir, noWatch())
	m = enter(t, m, "links.zip")
	m, _ = press(t, cursorTo(t, m, "d"), "c")
	m = goHere(t, m, out)
	m, cmd := press(t, m, "v")
	m = drain(t, m, cmd)
	if !strings.Contains(m.statusMsg, "nothing was copied out of links.zip") || !strings.Contains(m.statusMsg, "away") {
		t.Fatalf("status %q", m.statusMsg)
	}
	if got := dirNames(t, out); len(got) != 0 {
		t.Fatalf("a refused copy left %q", got)
	}
}
