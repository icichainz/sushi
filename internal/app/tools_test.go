package app

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/icichainz/sushi/internal/config"
	"github.com/icichainz/sushi/internal/fs"
)

// ctrl sends a control key, such as ctrl+z, to the model
func ctrl(t *testing.T, m Model, k tea.KeyType) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(tea.KeyMsg{Type: k})
	return updated.(Model), cmd
}

// undoNow presses ctrl+z and waits for the undo to finish
func undoNow(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := ctrl(t, m, tea.KeyCtrlZ)
	return drain(t, m, cmd)
}

// trashDir returns where trashed files go for the test's HOME
func trashDir(t *testing.T) string {
	t.Helper()
	tr, err := fs.DefaultTrash()
	if err != nil {
		t.Fatal(err)
	}
	return tr.Files
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", filepath.Base(path), err)
	}
	return string(b)
}

// cursorTo puts the cursor on the file called name
func cursorTo(t *testing.T, m Model, name string) Model {
	t.Helper()
	for i, f := range m.tab().Files {
		if f.Name == name {
			m.tab().Cursor = i
			return m
		}
	}
	t.Fatalf("%s is not listed", name)
	return m
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

func TestTrashDoesNotAskAndUndoRestores(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "aaa")
	writeTestFile(t, filepath.Join(dir, "b.txt"), "bbb")
	writeTestFile(t, filepath.Join(dir, "keep.txt"), "")

	m := newTestModel(t, dir, nil)
	m, _ = press(t, m, " ")
	m, _ = press(t, m, " ")
	m, cmd := press(t, m, "d")
	if m.mode == ModeConfirm {
		t.Fatal("moving to the trash should not ask first")
	}
	m = drain(t, m, cmd)

	if got := dirNames(t, dir); len(got) != 1 || got[0] != "keep.txt" {
		t.Fatalf("left %v, want only keep.txt", got)
	}
	if got := dirNames(t, trashDir(t)); len(got) != 2 {
		t.Fatalf("trash holds %v", got)
	}
	if m.statusMsg != "Moved to trash: 2 items" || m.job != nil {
		t.Fatalf("statusMsg = %q, job = %v", m.statusMsg, m.job)
	}

	m = undoNow(t, m)
	if readTestFile(t, filepath.Join(dir, "a.txt")) != "aaa" || readTestFile(t, filepath.Join(dir, "b.txt")) != "bbb" {
		t.Fatal("undo did not restore the files")
	}
	if m.statusMsg != "Undone: trash 2 items" || len(m.undo) != 0 {
		t.Fatalf("statusMsg = %q, %d entries left", m.statusMsg, len(m.undo))
	}
	if got := dirNames(t, trashDir(t)); len(got) != 0 {
		t.Fatalf("trash still holds %v", got)
	}
}

func TestUndoRefusesWhenOriginalIsTaken(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	writeTestFile(t, f, "old")

	m := newTestModel(t, dir, nil)
	m, cmd := press(t, m, "d")
	m = drain(t, m, cmd)
	writeTestFile(t, f, "new file in the way")

	m = undoNow(t, m)
	if !strings.Contains(m.statusMsg, "a.txt already exists") || !isProblem(m.statusMsg) {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
	if readTestFile(t, f) != "new file in the way" {
		t.Fatal("undo replaced the new file")
	}
	if len(m.undo) != 1 {
		t.Fatal("the refused undo should stay, to try again")
	}

	os.Remove(f)
	m = undoNow(t, m)
	if readTestFile(t, f) != "old" || len(m.undo) != 0 {
		t.Fatalf("second undo: statusMsg = %q", m.statusMsg)
	}
}

func TestImpossibleUndoDoesNotBlockOlderOnes(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "y.txt"), "")

	m := newTestModel(t, dir, nil)
	m, _ = press(t, m, "n")
	m = typeText(t, m, "x.txt")
	m = submit(t, m)
	m = cursorTo(t, m, "y.txt")
	m, _ = press(t, m, "r")
	m, _ = press(t, m, "ctrl+u") // Clears up to the cursor, before ".txt"
	m = typeText(t, m, "z")
	m = submit(t, m)
	// The renamed file is deleted behind sushi's back
	os.Remove(filepath.Join(dir, "z.txt"))

	m = undoNow(t, m)
	if !strings.Contains(m.statusMsg, "no longer") || len(m.undo) != 1 {
		t.Fatalf("statusMsg = %q, %d entries", m.statusMsg, len(m.undo))
	}
	m = undoNow(t, m)
	if fs.Exists(filepath.Join(dir, "x.txt")) || m.statusMsg != "Undone: create x.txt" {
		t.Fatalf("the older undo was blocked: %q", m.statusMsg)
	}
}

func TestHardDeleteAlwaysAsksAndCantBeUndone(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	writeTestFile(t, f, "")
	cfg := config.DefaultConfig()
	cfg.ConfirmDelete = false

	m := newTestModel(t, dir, cfg)
	m, _ = press(t, m, "D")
	if m.mode != ModeConfirm || !strings.Contains(ansi.Strip(m.renderConfirmDialog()), "There is no undo") {
		t.Fatal("D should always ask, with confirm_delete off too")
	}
	m, cmd := press(t, m, "y")
	m = drain(t, m, cmd)
	if fs.Exists(f) || len(dirNames(t, trashDir(t))) != 0 {
		t.Fatal("D should delete for good, not to the trash")
	}

	m = undoNow(t, m)
	if m.statusMsg != "Can't undo delete a.txt: it was permanent" {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
}

func TestDeleteToTrashOff(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "")
	cfg := config.DefaultConfig()
	cfg.DeleteToTrash = false

	m := newTestModel(t, dir, cfg)
	m, _ = press(t, m, "d")
	if m.mode != ModeConfirm {
		t.Fatal("with delete_to_trash off, d deletes and so asks first")
	}
	m, cmd := press(t, m, "y")
	drain(t, m, cmd)
	if len(dirNames(t, dir)) != 0 || len(dirNames(t, trashDir(t))) != 0 {
		t.Fatal("file not deleted permanently")
	}
}

func TestUndoRename(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "draft.txt"), "")

	m := newTestModel(t, dir, nil)
	m, _ = press(t, m, "c")
	m, _ = press(t, m, "r")
	m = typeText(t, m, "-v2")
	m = submit(t, m)

	m = undoNow(t, m)
	if got := dirNames(t, dir); len(got) != 1 || got[0] != "draft.txt" {
		t.Fatalf("after undo: %v", got)
	}
	if m.statusMsg != "Undone: rename draft.txt → draft-v2.txt" || m.clipboard[0] != filepath.Join(dir, "draft.txt") {
		t.Fatalf("statusMsg = %q, clipboard = %v", m.statusMsg, m.clipboard)
	}
}

func TestUndoMove(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(src, "a.txt"), "moved")

	m := newTestModel(t, src, nil)
	m, _ = press(t, m, "x")
	m.tab().CurrentPath = dst
	m, cmd := press(t, m, "v")
	m = drain(t, m, cmd)
	if !fs.Exists(filepath.Join(dst, "a.txt")) {
		t.Fatal("not moved")
	}

	m = undoNow(t, m)
	if readTestFile(t, filepath.Join(src, "a.txt")) != "moved" || fs.Exists(filepath.Join(dst, "a.txt")) {
		t.Fatalf("undo did not move it back: %q", m.statusMsg)
	}
}

func TestUndoCopyRemovesOnlyUnchangedCopies(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(src, "a.txt"), "a")
	writeTestFile(t, filepath.Join(src, "b.txt"), "b")
	writeTestFile(t, filepath.Join(dst, "a.txt"), "was here first")

	paste := func() Model {
		m := newTestModel(t, src, nil)
		m, _ = press(t, m, "*")
		m, _ = press(t, m, "c")
		m.tab().CurrentPath = dst
		m, _ = press(t, m, "v")
		m, cmd := press(t, m, "y") // Overwrite a.txt
		return drain(t, m, cmd)
	}

	// Undo removes the copy of b.txt; the overwritten a.txt is gone for good
	m := undoNow(t, paste())
	if fs.Exists(filepath.Join(dst, "b.txt")) || readTestFile(t, filepath.Join(dst, "a.txt")) != "a" {
		t.Fatalf("after undo: %v, statusMsg = %q", dirNames(t, dst), m.statusMsg)
	}
	if !strings.Contains(m.statusMsg, "1 item it replaced can't be brought back") {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
	// Copies with content go to the trash, in case they're wanted after all
	if got := dirNames(t, trashDir(t)); len(got) != 1 || got[0] != "b.txt" {
		t.Fatalf("trash = %v", got)
	}

	// An edited copy is kept, and that can't change, so the step is dropped
	m = paste()
	later := time.Now().Add(time.Hour)
	writeTestFile(t, filepath.Join(dst, "b.txt"), "edited")
	os.Chtimes(filepath.Join(dst, "b.txt"), later, later)
	m = undoNow(t, m)
	if !strings.Contains(m.statusMsg, "b.txt has changed since, so it was kept") || readTestFile(t, filepath.Join(dst, "b.txt")) != "edited" {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
	if len(m.undo) != 0 {
		t.Fatal("a step that can never work should not block older undos")
	}
}

func TestUndoPasteThatOnlyReplaced(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(src, "a"), "new")
	writeTestFile(t, filepath.Join(dst, "a"), "old")

	m := newTestModel(t, src, nil)
	m, _ = press(t, m, "c")
	m.tab().CurrentPath = dst
	m, _ = press(t, m, "v")
	m, cmd := press(t, m, "y")
	m = drain(t, m, cmd)

	m = undoNow(t, m)
	if m.statusMsg != "Can't undo copy a: it replaced what was there" {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
}

func TestUndoCreate(t *testing.T) {
	dir := t.TempDir()
	m := newTestModel(t, dir, nil)

	m, _ = press(t, m, "n")
	m = typeText(t, m, "notes.md")
	m = submit(t, m)
	m = undoNow(t, m)
	if fs.Exists(filepath.Join(dir, "notes.md")) || m.statusMsg != "Undone: create notes.md" {
		t.Fatalf("new file not removed: %q", m.statusMsg)
	}

	// Undoing "src/main.go" removes src, which it created
	os.Mkdir(filepath.Join(dir, "existing"), 0755)
	m, _ = press(t, m, "n")
	m = typeText(t, m, "src/main.go")
	m = submit(t, m)
	m, _ = press(t, m, "n")
	m = typeText(t, m, "existing/inner.go")
	m = submit(t, m)
	m = undoNow(t, m)
	m = undoNow(t, m)
	if fs.Exists(filepath.Join(dir, "src")) || fs.Exists(filepath.Join(dir, "existing", "inner.go")) || !fs.Exists(filepath.Join(dir, "existing")) {
		t.Fatalf("after undoing both: %v", dirNames(t, dir))
	}

	// A folder something was put in since is kept
	m, _ = press(t, m, "N")
	m = typeText(t, m, "assets")
	m = submit(t, m)
	writeTestFile(t, filepath.Join(dir, "assets", "logo.png"), "png")
	m = undoNow(t, m)
	if !fs.Exists(filepath.Join(dir, "assets", "logo.png")) || !strings.Contains(m.statusMsg, "changed since") {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
}

func TestUndoStackIsLimited(t *testing.T) {
	m := newTestModel(t, t.TempDir(), nil)
	m, _ = ctrl(t, m, tea.KeyCtrlZ)
	if m.statusMsg != "Nothing to undo" {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
	for i := 0; i < 25; i++ {
		m.pushUndo(&undoEntry{label: fmt.Sprint(i), reason: "test"})
	}
	if len(m.undo) != undoLimit || m.undo[0].label != "5" {
		t.Fatalf("kept %d entries from %s", len(m.undo), m.undo[0].label)
	}
	// Operations that did nothing aren't remembered
	m.pushUndo(&undoEntry{label: "nothing"})
	if m.undo[len(m.undo)-1].label == "nothing" {
		t.Fatal("an empty operation was remembered")
	}
}

func TestProgressShowsWhileRunning(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(src, "a.txt"), "")

	m := newTestModel(t, src, nil)
	m, _ = press(t, m, "c")
	m.tab().CurrentPath = dst
	m, _ = press(t, m, "v") // Started, but its command isn't run
	updated, _ := m.Update(jobProgressMsg{id: m.job.id, progress: fs.Progress{Files: 3, TotalFiles: 120, Bytes: 45, TotalBytes: 100}})
	m = updated.(Model)

	status := ansi.Strip(m.renderStatusBar())
	if !strings.Contains(status, "Copying 3/120 files 45% ████░░░░░░") {
		t.Fatalf("status = %q", status)
	}
	if bottom := ansi.Strip(m.renderBottomRow()); !strings.Contains(bottom, "ctrl+x cancel copying") {
		t.Fatalf("hints = %q", bottom)
	}
	for _, size := range []tea.WindowSizeMsg{{Width: 140, Height: 40}, {Width: 60, Height: 15}, {Width: 30, Height: 10}} {
		updated, _ := m.Update(size)
		assertFills(t, fmt.Sprintf("%dx%d busy", size.Width, size.Height), updated.(Model))
	}

	counting := m
	counting.changeJob(func(j *job) { j.progress = fs.Progress{Counting: true, TotalFiles: 1500} })
	if s := counting.job.status(); s != "Copying: counting, 1500 files so far" {
		t.Fatalf("while counting: %q", s)
	}
}

func TestProgressUpdatesArriveThenTheResult(t *testing.T) {
	progressInterval = 0
	t.Cleanup(func() { progressInterval = time.Hour })
	src, dst := t.TempDir(), t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		writeTestFile(t, filepath.Join(src, name), name)
	}

	m := newTestModel(t, src, nil)
	m, _ = press(t, m, "*")
	m, _ = press(t, m, "c")
	m.tab().CurrentPath = dst
	m, cmd := press(t, m, "v")

	updates := 0
	for {
		msg := cmd()
		if _, ok := msg.(jobProgressMsg); !ok {
			updated, next := m.Update(msg)
			m = drain(t, updated.(Model), next)
			break
		}
		updates++
		var updated tea.Model
		updated, cmd = m.Update(msg)
		m = updated.(Model)
	}
	if updates == 0 {
		t.Fatal("no progress was shown")
	}
	if m.job != nil || m.statusMsg != "Copied: 3 items" || len(dirNames(t, dst)) != 3 {
		t.Fatalf("job=%v statusMsg=%q copied=%v", m.job, m.statusMsg, dirNames(t, dst))
	}
}

func TestCancelPaste(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(src, "big"), bytes.Repeat([]byte("x"), 1<<20), 0644)

	m := newTestModel(t, src, nil)
	m, _ = press(t, m, "c")
	m.tab().CurrentPath = dst
	m, cmd := press(t, m, "v")
	m, _ = ctrl(t, m, tea.KeyCtrlX)
	if !strings.Contains(ansi.Strip(m.renderStatusBar()), "Copying, cancelling") {
		t.Fatalf("status = %q", ansi.Strip(m.renderStatusBar()))
	}
	m = drain(t, m, cmd)

	// Cancelled before counting, so it goes by items rather than files
	if m.statusMsg != "Cancelled: copied 0 of 1 item" || m.job != nil {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
	if got := dirNames(t, dst); len(got) != 0 {
		t.Fatalf("destination has %v", got)
	}
	m, _ = ctrl(t, m, tea.KeyCtrlX)
	if m.statusMsg != "Nothing to cancel" {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
}

func TestBusyRefusesChangesButNotMoving(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "")
	writeTestFile(t, filepath.Join(dir, "b.txt"), "")

	m := newTestModel(t, dir, nil)
	m, _ = press(t, m, "d")
	running := m.job.id
	for _, k := range []string{"d", "D", "r", "n", "N", "v"} {
		m.statusMsg = ""
		m, _ = press(t, m, k)
		if m.mode != ModeNormal || m.job.id != running || !strings.Contains(m.statusMsg, "Still moving to trash") {
			t.Fatalf("%s while busy: mode=%v statusMsg=%q", k, m.mode, m.statusMsg)
		}
	}
	m, _ = ctrl(t, m, tea.KeyCtrlZ)
	if len(m.undo) != 0 || !strings.Contains(m.statusMsg, "Still moving to trash") {
		t.Fatalf("undo while busy: statusMsg=%q", m.statusMsg)
	}
	m, _ = press(t, m, "j")
	if m.tab().Cursor != 1 {
		t.Fatal("moving should still work while an operation runs")
	}
	m, _ = press(t, m, "/")
	if m.mode != ModeSearch {
		t.Fatal("searching should still work while an operation runs")
	}
}

func TestQuitStopsTheRunningJobFirst(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "")

	m := newTestModel(t, dir, nil)
	m, jobCmd := press(t, m, "d")
	m, _ = press(t, m, "q")
	if !m.job.quit || !strings.Contains(m.statusMsg, "Stopping moving to trash before quitting") {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
	updated, cmd := m.Update(jobCmd())
	if cmd == nil {
		t.Fatal("no quit after the job stopped")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok || updated.(Model).job != nil {
		t.Fatal("should quit once the job has stopped")
	}
	if !fs.Exists(filepath.Join(dir, "a.txt")) {
		t.Fatal("the job should have been cancelled")
	}

	// Asked twice, it quits without waiting
	m = newTestModel(t, dir, nil)
	m, _ = press(t, m, "d")
	m, _ = press(t, m, "q")
	if _, cmd = press(t, m, "q"); cmd == nil {
		t.Fatal("second q should quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("second q should quit")
	}
}
