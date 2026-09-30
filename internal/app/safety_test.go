package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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
		m = drain(t, m, m.executePaste(z))
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

func TestCancelledUndoCarriesOnWhereItStopped(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "album")
	os.Mkdir(src, 0755)
	for _, name := range []string{"a.jpg", "b.jpg", "c.jpg"} {
		writeTestFile(t, filepath.Join(src, name), name)
	}
	copied := filepath.Join(root, "backup")
	if err := fs.CopyPath(src, copied); err != nil {
		t.Fatal(err)
	}
	e := undoEntry{label: "copy album"}
	e.addCopied(copied, src)

	// Cancelled once the undo has deleted one file of the copy
	ctx, cancel := context.WithCancel(context.Background())
	task := fs.NewTask(ctx, 0, func(p fs.Progress) {
		if p.Files > 0 {
			cancel()
		}
	})
	done := undoWork(task, e, false)
	if !strings.Contains(done.op.message, "undoing again carries on") {
		t.Fatalf("message = %q, err = %v", done.op.message, done.op.err)
	}
	if done.undo == nil || len(done.undo.steps) != 1 {
		t.Fatal("the step that was running was dropped, though the status says undoing again carries on")
	}
	if left := dirNames(t, copied); len(left) != 2 {
		t.Fatalf("left %v, want the cancelled undo to have removed one file", left)
	}

	// Undoing again carries on, although the copy is not as it was made
	done = undoWork(fs.NewTask(context.Background(), 0, nil), *done.undo, false)
	if done.op.err != nil || fs.Exists(copied) {
		t.Fatalf("second undo: %v, %q", done.op.err, done.op.message)
	}
	if len(dirNames(t, src)) != 3 {
		t.Fatal("the original was touched")
	}
}

// modeOf returns the permission bits of path, following links
func modeOf(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func TestChmodLeavesSymlinksAndWhatTheyPointTo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no Unix permissions")
	}
	dir := t.TempDir()
	keys := t.TempDir()
	key := filepath.Join(keys, "id_ed25519")
	writeTestFile(t, key, "secret")
	os.Chmod(key, 0600)
	symlinkOrSkip(t, key, filepath.Join(dir, "key-link"))

	// On the link alone: chmod used to follow it and open up the key
	m := cursorTo(t, newTestModel(t, dir, nil), "key-link")
	m, _ = press(t, m, "m")
	if m.mode != ModeNormal || !strings.Contains(m.statusMsg, "Symlinks have no permissions") {
		t.Fatalf("mode = %v, statusMsg = %q", m.mode, m.statusMsg)
	}

	// With files: the link is left out, and says so
	writeTestFile(t, filepath.Join(dir, "a.sh"), "")
	os.Chmod(filepath.Join(dir, "a.sh"), 0644)
	m = newTestModel(t, dir, nil)
	m, _ = press(t, m, "*")
	m, _ = press(t, m, "m")
	if !strings.Contains(m.prompt.label, "1 symlink left as it is") || m.prompt.input.Value() != "644" {
		t.Fatalf("label = %q, value = %q", m.prompt.label, m.prompt.input.Value())
	}
	m, _ = press(t, m, "ctrl+u")
	m = typeText(t, m, "777")
	m = submit(t, m)
	if modeOf(t, key) != 0600 || modeOf(t, filepath.Join(dir, "a.sh")) != 0777 {
		t.Fatalf("key is %v, a.sh %v: %q", modeOf(t, key), modeOf(t, filepath.Join(dir, "a.sh")), m.statusMsg)
	}
}

func TestChmodOfSeveralSaysTheyAllGetTheMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no Unix permissions")
	}
	dir := t.TempDir()
	public, private := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.key")
	writeTestFile(t, public, "")
	writeTestFile(t, private, "")
	os.Chmod(public, 0644)
	os.Chmod(private, 0600)

	// Different modes: the prompt used to start from the first one's, so
	// pressing Enter made the private file 644 as well
	m := newTestModel(t, dir, nil)
	m, _ = press(t, m, "*")
	m, _ = press(t, m, "m")
	if m.prompt.input.Value() != "" || !strings.Contains(m.prompt.label, "Set all 2 items, now 644, 600, to") {
		t.Fatalf("label = %q, value = %q", m.prompt.label, m.prompt.input.Value())
	}
	m, _ = press(t, m, "enter")
	if m.mode != ModeInput || modeOf(t, private) != 0600 {
		t.Fatal("an empty mode was accepted")
	}

	// The same mode: offered, and the label still says it applies to all
	os.Chmod(private, 0644)
	m = newTestModel(t, dir, nil)
	m, _ = press(t, m, "*")
	m, _ = press(t, m, "m")
	if m.prompt.input.Value() != "644" || !strings.Contains(m.prompt.label, "Set all 2 items, now 644, to") {
		t.Fatalf("label = %q, value = %q", m.prompt.label, m.prompt.input.Value())
	}
}

func TestUndoChmodDoesNotFollowALinkPutInItsPlace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no Unix permissions")
	}
	dir := t.TempDir()
	f := filepath.Join(dir, "a.sh")
	writeTestFile(t, f, "")
	os.Chmod(f, 0755)
	key := filepath.Join(t.TempDir(), "key")
	writeTestFile(t, key, "secret")
	os.Chmod(key, 0600)

	m := newTestModel(t, dir, nil)
	m, _ = press(t, m, "m")
	m, _ = press(t, m, "ctrl+u")
	m = typeText(t, m, "700")
	m = submit(t, m)
	os.Remove(f)
	symlinkOrSkip(t, key, f)
	m = undoNow(t, m)
	if modeOf(t, key) != 0600 || !strings.Contains(m.statusMsg, "symlink") {
		t.Fatalf("key is %v: %q", modeOf(t, key), m.statusMsg)
	}
}

func TestDeleteConfirmationShowsItemsInOtherFolders(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "here.txt"), "")
	other := filepath.Join(t.TempDir(), "elsewhere")
	os.Mkdir(other, 0755)
	thesis := filepath.Join(other, "thesis.tex")
	writeTestFile(t, thesis, "")

	// A plugin selects a file here and one in another folder: the dialog
	// used to list both by name alone, as if both were here
	m := withPlugin(t, dir, `printf 'select here.txt\nselect %s\n' "`+thesis+`" > "$SUSHI_CMD_FILE"`)
	m, cmd := press(t, m, "Z")
	m = drain(t, m, cmd)
	m, _ = press(t, m, "D")
	dialog := strings.Join(plain(m.renderConfirmDialog()), "\n")
	if !strings.Contains(dialog, "Delete 2 items?") || !strings.Contains(dialog, "elsewhere/thesis.tex") ||
		!strings.Contains(dialog, "1 of them is in another folder") {
		t.Fatalf("dialog:\n%s", dialog)
	}

	// One item, elsewhere
	m, _ = press(t, m, "n")
	m.tab().Selected = map[string]bool{thesis: true}
	m, _ = press(t, m, "D")
	dialog = strings.Join(plain(m.renderConfirmDialog()), "\n")
	if !strings.Contains(dialog, "Delete file 'thesis.tex'?") || !strings.Contains(dialog, "It is in another folder") || !strings.Contains(dialog, "elsewhere/thesis.tex") {
		t.Fatalf("dialog:\n%s", dialog)
	}
}

// busy returns a model in dir with a job started but not run, as the
// moment after pressing d
func busy(t *testing.T, dir string, cfg *config.Config) Model {
	t.Helper()
	m := newTestModel(t, dir, cfg)
	m, _ = press(t, m, "d")
	if m.job == nil {
		t.Fatal("no job started")
	}
	return m
}

func TestEveryWayOutStopsTheJobFirst(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "")

	// Q and closing the last tab used to quit at once, leaving the job's
	// partial files behind
	for _, k := range []string{"q", "Q", "ctrl+w"} {
		m := busy(t, dir, nil)
		var cmd tea.Cmd
		if k == "ctrl+w" {
			m, cmd = ctrl(t, m, tea.KeyCtrlW)
		} else {
			m, cmd = press(t, m, k)
		}
		if quits(cmd) || !m.job.quit || !strings.Contains(m.statusMsg, "Stopping moving to trash before quitting") {
			t.Fatalf("%s: statusMsg = %q", k, m.statusMsg)
		}
		if k == "Q" && m.ExitDir() != "" {
			t.Fatal("Q should still quit without changing the shell's directory")
		}
	}
}

func TestBusyRefusesPluginsCommandsAndOpeningFiles(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "")
	os.Mkdir(filepath.Join(dir, "sub"), 0755)
	ran := filepath.Join(t.TempDir(), "ran")
	cfg := config.DefaultConfig()
	cfg.Plugins = []plugins.Plugin{{Name: "touch", Key: "Z", Mode: plugins.ModeBackground, Command: "touch " + ran}}

	for _, k := range []string{"Z", "P", "!", "e", "o"} {
		m := busy(t, dir, cfg)
		m, cmd := press(t, m, k)
		m = drain(t, m, cmd)
		if m.mode != ModeNormal || !strings.Contains(m.statusMsg, "Still moving to trash") {
			t.Fatalf("%s while busy: mode = %v, statusMsg = %q", k, m.mode, m.statusMsg)
		}
	}
	if fs.Exists(ran) {
		t.Fatal("a plugin ran while the job did")
	}

	// A plugin started some other way, as from the Run palette
	m := busy(t, dir, cfg)
	m2, _ := m.runPlugin(cfg.Plugins[0])
	if m = m2.(Model); !strings.Contains(m.statusMsg, "Still moving to trash") {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}

	// Enter opens a file in another program: refused; a folder: fine
	m = cursorTo(t, busy(t, dir, nil), "a.txt")
	m, _ = press(t, m, "enter")
	if !strings.Contains(m.statusMsg, "Still moving to trash") {
		t.Fatalf("enter on a file: statusMsg = %q", m.statusMsg)
	}
	m = cursorTo(t, busy(t, dir, nil), "sub")
	m, cmd := press(t, m, "enter")
	m = drain(t, m, cmd)
	if m.tab().CurrentPath != filepath.Join(dir, "sub") {
		t.Fatal("entering a folder should still work while busy")
	}
}

func TestShutdownWaitsForTheJobToCleanUp(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(src, "big"), strings.Repeat("x", 64<<20))

	m := newTestModel(t, src, nil)
	m, _ = press(t, m, "c")
	m.tab().CurrentPath = dst
	m, cmd := press(t, m, "v")
	go cmd() // The program runs it and then quits, say on a second q
	<-m.job.started

	if err := m.Shutdown(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	if got := dirNames(t, dst); len(got) != 0 {
		t.Fatalf("left in the destination: %v", got)
	}

	// A job whose command never ran isn't waited for
	m = busy(t, src, nil)
	start := time.Now()
	if err := m.Shutdown(5 * time.Second); err != nil || time.Since(start) > 2*time.Second {
		t.Fatalf("err = %v after %v", err, time.Since(start))
	}
}

func TestShutdownRemovesUnfinishedPluginsFiles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	m := withPlugin(t, t.TempDir(), "sleep 1")
	m, _ = press(t, m, "Z") // Started, still running when sushi quits
	if left, _ := filepath.Glob(filepath.Join(tmp, "sushi-cmd-*")); len(left) != 1 {
		t.Fatalf("instruction files: %v", left)
	}
	if err := m.Shutdown(time.Second); err != nil {
		t.Fatal(err)
	}
	if left, _ := filepath.Glob(filepath.Join(tmp, "sushi-cmd-*")); len(left) != 0 {
		t.Fatalf("left behind: %v", left)
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

func TestOverwriteConfirmationStaysWithItsFolder(t *testing.T) {
	root := t.TempDir()
	src, sub := filepath.Join(root, "src"), filepath.Join(root, "sub")
	os.Mkdir(src, 0755)
	os.Mkdir(sub, 0755)
	writeTestFile(t, filepath.Join(src, "a.txt"), "new")
	writeTestFile(t, filepath.Join(sub, "a.txt"), "old")
	writeTestFile(t, filepath.Join(root, "a.txt"), "the parent's")

	// The dialog asks about sub/a.txt
	ask := func(t *testing.T) Model {
		t.Helper()
		m := newTestModel(t, src, noWatch())
		m, _ = press(t, m, "c")
		m = drain(t, m, m.loadDir(m.tab(), sub))
		if m, _ = press(t, m, "v"); m.mode != ModeConfirm {
			t.Fatalf("mode = %v, want the overwrite dialog", m.mode)
		}
		return m
	}
	unchanged := func(t *testing.T) {
		t.Helper()
		for path, want := range map[string]string{filepath.Join(root, "a.txt"): "the parent's", filepath.Join(sub, "a.txt"): "old"} {
			if got := readTestFile(t, path); got != want {
				t.Fatalf("%s = %q, want %q: the paste went ahead", path, got, want)
			}
		}
	}

	// Meanwhile a load lands the tab in the parent, which has an a.txt of
	// its own that nobody was asked about
	m := ask(t)
	m = drain(t, m, m.loadDir(m.tab(), root))
	m, cmd := press(t, m, "y")
	m = drain(t, m, cmd)
	unchanged(t)
	if m.mode != ModeNormal || !strings.Contains(m.statusMsg, "nothing was pasted") {
		t.Fatalf("mode=%v status=%q, want the paste called off", m.mode, m.statusMsg)
	}

	// The folder is deleted while the dialog is open: the watcher's reload
	// moves the tab up
	m = ask(t)
	os.Rename(sub, filepath.Join(root, "gone"))
	m = changeDirs(t, m, sub)
	if m.tab().CurrentPath != root {
		t.Fatalf("in %s, want the reload to have moved up to %s", m.tab().CurrentPath, root)
	}
	m, cmd = press(t, m, "y")
	m = drain(t, m, cmd)
	if got := readTestFile(t, filepath.Join(root, "a.txt")); got != "the parent's" || !strings.Contains(m.statusMsg, "nothing was pasted") {
		t.Fatalf("the parent's a.txt = %q, status %q", got, m.statusMsg)
	}
	os.Rename(filepath.Join(root, "gone"), sub)

	// What would be overwritten changes: another name is taken meanwhile
	m = ask(t)
	writeTestFile(t, filepath.Join(src, "b.txt"), "new b")
	m.clipboard = append(m.clipboard, filepath.Join(src, "b.txt"))
	writeTestFile(t, filepath.Join(sub, "b.txt"), "old b")
	m, cmd = press(t, m, "y")
	m = drain(t, m, cmd)
	unchanged(t)
	if got := readTestFile(t, filepath.Join(sub, "b.txt")); got != "old b" || !strings.Contains(m.statusMsg, "nothing was pasted") {
		t.Fatalf("b.txt = %q, status %q", got, m.statusMsg)
	}
	os.Remove(filepath.Join(src, "b.txt"))
	os.Remove(filepath.Join(sub, "b.txt"))

	// Nothing changed: y overwrites what the dialog named, and only that
	m = ask(t)
	m, cmd = press(t, m, "y")
	m = drain(t, m, cmd)
	if got := readTestFile(t, filepath.Join(sub, "a.txt")); got != "new" {
		t.Fatalf("sub/a.txt = %q after y, want it overwritten; status %q", got, m.statusMsg)
	}
	if got := readTestFile(t, filepath.Join(root, "a.txt")); got != "the parent's" {
		t.Fatalf("the parent's a.txt = %q", got)
	}
}
