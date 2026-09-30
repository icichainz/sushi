package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/icichainz/sushi/internal/config"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/plugins"
)

// Regression tests for data-safety problems found in review

// symlinkOrSkip makes a symlink, skipping the test where that needs privileges
func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("can't make symlinks here: %v", err)
	}
}

// withPlugin returns a model in dir with one background plugin on Z
func withPlugin(t *testing.T, dir, command string) Model {
	t.Helper()
	skipWithoutSh(t)
	cfg := config.DefaultConfig()
	cfg.Plugins = []plugins.Plugin{{Name: "pick", Key: "Z", Mode: plugins.ModeBackground, Command: command}}
	return newTestModel(t, dir, cfg)
}

func TestPluginSelectedLinkWithSlashDeletesOnlyTheLink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	os.Mkdir(target, 0755)
	precious := filepath.Join(target, "precious.txt")
	writeTestFile(t, precious, "keep")
	link := filepath.Join(dir, "link")
	symlinkOrSkip(t, target, link)

	// "link/" names the target: Lstat follows it, so a hard delete used to
	// empty the target folder
	m := withPlugin(t, dir, `echo "select $SUSHI_DIR/link/" > "$SUSHI_CMD_FILE"; echo "select $SUSHI_DIR/missing.txt" >> "$SUSHI_CMD_FILE"`)
	m, cmd := press(t, m, "Z")
	m = drain(t, m, cmd)
	if len(m.tab().Selected) != 1 || !m.tab().Selected[link] {
		t.Fatalf("selected %v, want only %s", m.tab().Selected, link)
	}
	if !strings.Contains(m.statusMsg, "can't select") || !strings.Contains(m.statusMsg, "missing.txt") {
		t.Fatalf("statusMsg = %q, want the missing path reported", m.statusMsg)
	}

	m, _ = press(t, m, "D")
	m, cmd = press(t, m, "y")
	m = drain(t, m, cmd)
	if fs.Exists(link) {
		t.Fatalf("the link is still there: %q", m.statusMsg)
	}
	if readTestFile(t, precious) != "keep" {
		t.Fatal("deleting the link emptied its target")
	}
}

func TestPasteOverALinkedFolderLeavesItsTargetAlone(t *testing.T) {
	for _, mode := range []string{"c", "x"} {
		root := t.TempDir()
		src := filepath.Join(root, "src")
		os.MkdirAll(filepath.Join(src, "photos"), 0755)
		writeTestFile(t, filepath.Join(src, "photos", "beach.jpg"), "sand")
		elsewhere := filepath.Join(root, "elsewhere")
		os.Mkdir(elsewhere, 0755)
		writeTestFile(t, filepath.Join(elsewhere, "mine.txt"), "untouched")
		dst := filepath.Join(root, "dst")
		os.Mkdir(dst, 0755)
		symlinkOrSkip(t, elsewhere, filepath.Join(dst, "photos"))

		// dst/photos is a link to elsewhere/: pasting used to copy into
		// elsewhere/ and, for a cut, then delete the source
		m := newTestModel(t, src, nil)
		m, _ = press(t, m, mode)
		m.tab().CurrentPath = dst
		m, _ = press(t, m, "v")
		if m.mode != ModeConfirm {
			t.Fatalf("%s: pasting over the link should ask first", mode)
		}
		m, cmd := press(t, m, "y")
		m = drain(t, m, cmd)

		if got := dirNames(t, elsewhere); len(got) != 1 || got[0] != "mine.txt" {
			t.Fatalf("%s: the paste wrote into the link's target: %v (%q)", mode, got, m.statusMsg)
		}
		if readTestFile(t, filepath.Join(dst, "photos", "beach.jpg")) != "sand" {
			t.Fatalf("%s: the paste isn't where it was asked to go: %q", mode, m.statusMsg)
		}
		if moved := !fs.Exists(filepath.Join(src, "photos")); moved != (mode == "x") {
			t.Fatalf("%s: source moved = %v", mode, moved)
		}
	}
}

// ignoresCase reports whether the filesystem holding dir takes names that
// differ only in case for the same, as macOS and Windows do by default
func ignoresCase(t *testing.T, dir string) bool {
	t.Helper()
	probe := filepath.Join(dir, "CaseProbe")
	writeTestFile(t, probe, "")
	defer os.Remove(probe)
	_, err := os.Lstat(filepath.Join(dir, "caseprobe"))
	return err == nil
}

// reportAndReport sets up x/report.txt and y/Report.txt, and an empty z/
func reportAndReport(t *testing.T) (x, y, z string) {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"x", "y", "z"} {
		os.Mkdir(filepath.Join(root, d), 0755)
	}
	x, y = filepath.Join(root, "x", "report.txt"), filepath.Join(root, "y", "Report.txt")
	writeTestFile(t, x, "x's report")
	writeTestFile(t, y, "y's report")
	return x, y, filepath.Join(root, "z")
}

func TestPasteRefusesNamesThatDifferOnlyInCase(t *testing.T) {
	for _, mode := range []string{"copy", "cut"} {
		// On a filesystem that ignores case, the second used to replace the
		// first, "Moved: 2 items" was reported, and undo then moved y's
		// report to x's place: x's report was gone
		x, y, z := reportAndReport(t)
		m := newTestModel(t, filepath.Dir(x), nil)
		m.clipboard, m.clipboardMode = []string{x, y}, mode
		m.tab().CurrentPath = z
		m, cmd := press(t, m, "v")
		m = drain(t, m, cmd)
		if !strings.Contains(m.statusMsg, "Can't paste") || !strings.Contains(m.statusMsg, "differ only in case") {
			t.Fatalf("%s: statusMsg = %q", mode, m.statusMsg)
		}
		if readTestFile(t, x) != "x's report" || readTestFile(t, y) != "y's report" || len(dirNames(t, z)) != 0 {
			t.Fatalf("%s: files were pasted: %v", mode, dirNames(t, z))
		}
	}

	// Accents written as one character or two are the same name too
	if nameKey("re\u0301sume\u0301.txt") != nameKey("R\u00e9sum\u00e9.txt") {
		t.Fatal("names differing only in normalisation should match")
	}
}

func TestPasteNeverReplacesWhatItHasJustPasted(t *testing.T) {
	x, y, z := reportAndReport(t)
	if !ignoresCase(t, z) {
		t.Skip("the filesystem tells Report.txt from report.txt")
	}
	for _, mode := range []string{"copy", "cut"} {
		x, y, z = reportAndReport(t)
		// As if the check before pasting had missed it
		m := newTestModel(t, filepath.Dir(x), nil)
		m.clipboard, m.clipboardMode = []string{x, y}, mode
		m.tab().CurrentPath = z
		m = drain(t, m, m.executePaste())
		if m.statusMsg == "" || !strings.Contains(m.statusMsg, "just put there") {
			t.Fatalf("%s: statusMsg = %q", mode, m.statusMsg)
		}
		if got := readTestFile(t, filepath.Join(z, "report.txt")); got != "x's report" {
			t.Fatalf("%s: z/report.txt holds %q", mode, got)
		}
		if readTestFile(t, y) != "y's report" {
			t.Fatalf("%s: y's report is gone", mode)
		}
		if mode == "cut" {
			m = undoNow(t, m)
			if readTestFile(t, x) != "x's report" || readTestFile(t, y) != "y's report" {
				t.Fatalf("undo put back the wrong file: %q", m.statusMsg)
			}
		}
	}
}

func TestUndoNeverDeletesTheLastCopy(t *testing.T) {
	// With the trash off: copy notes.txt to backup/, delete the original,
	// then press ctrl+z twice. The first press said it couldn't undo the
	// delete and dropped it; the second "undid" the copy, deleting for
	// good the only copy left.
	dir := t.TempDir()
	notes := filepath.Join(dir, "notes.txt")
	writeTestFile(t, notes, "the only notes")
	backup := filepath.Join(dir, "backup")
	os.Mkdir(backup, 0755)
	cfg := config.DefaultConfig()
	cfg.DeleteToTrash = false

	m := cursorTo(t, newTestModel(t, dir, cfg), "notes.txt")
	m, _ = press(t, m, "c")
	m.tab().CurrentPath = backup
	m, cmd := press(t, m, "v")
	m = drain(t, m, cmd)
	m.tab().CurrentPath = dir
	m = drain(t, m, m.reloadAll())
	m = cursorTo(t, m, "notes.txt")
	m, _ = press(t, m, "d")
	m, cmd = press(t, m, "y")
	m = drain(t, m, cmd)
	if fs.Exists(notes) {
		t.Fatal("the original was not deleted")
	}

	copied := filepath.Join(backup, "notes.txt")
	for press := 1; press <= 2; press++ {
		m = undoNow(t, m)
		if !strings.Contains(m.statusMsg, "Can't undo delete notes.txt") || len(m.undo) != 2 {
			t.Fatalf("press %d: statusMsg = %q, %d entries", press, m.statusMsg, len(m.undo))
		}
		if readTestFile(t, copied) != "the only notes" {
			t.Fatalf("press %d deleted the last copy", press)
		}
	}

	// Even with the delete out of the way, the copy is kept while its
	// original is gone
	m.undo = m.undo[:1]
	m = undoNow(t, m)
	if !strings.Contains(m.statusMsg, "its original") || readTestFile(t, copied) != "the only notes" {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
}

func TestUndoStillRemovesACopyWhoseOriginalIsThere(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "notes.txt"), "notes")
	backup := filepath.Join(dir, "backup")
	os.Mkdir(backup, 0755)
	cfg := config.DefaultConfig()
	cfg.DeleteToTrash = false

	m := cursorTo(t, newTestModel(t, dir, cfg), "notes.txt")
	m, _ = press(t, m, "c")
	m.tab().CurrentPath = backup
	m, cmd := press(t, m, "v")
	m = drain(t, m, cmd)
	m = undoNow(t, m)
	if fs.Exists(filepath.Join(backup, "notes.txt")) || m.statusMsg != "Undone: copy notes.txt" {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
}

func TestTargetsAreCleaned(t *testing.T) {
	dir := t.TempDir()
	m := newTestModel(t, dir, nil)
	a := filepath.Join(dir, "a")
	m.tab().Selected[a+string(filepath.Separator)] = true
	m.tab().Selected[a] = true
	if got := m.targets(); len(got) != 1 || got[0] != a {
		t.Fatalf("targets = %q, want just %q", got, a)
	}
}
