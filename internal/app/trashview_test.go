package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/config"
	"github.com/icichainz/sushi/internal/ui"
)

// openTrashView presses ctrl+t and waits for the listing and the sizes
func openTrashView(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := ctrl(t, m, tea.KeyCtrlT)
	if m.mode != ModeTrash {
		t.Fatalf("ctrl+t opened mode %v", m.mode)
	}
	return drain(t, m, cmd)
}

// trashNames returns the names the browser lists, in order
func trashNames(m Model) string {
	var names []string
	for _, i := range m.trash.shown {
		names = append(names, m.trash.items[i].Name)
	}
	return strings.Join(names, ",")
}

// trashCursorTo puts the browser's cursor on the item called name
func trashCursorTo(t *testing.T, m Model, name string) Model {
	t.Helper()
	for pos, i := range m.trash.shown {
		if m.trash.items[i].Name == name {
			m.trash.cursor = pos
			return m
		}
	}
	t.Fatalf("%s is not listed: %s", name, trashNames(m))
	return m
}

// trashWith trashes the named files of dir with d, as the file list does
func trashWith(t *testing.T, m Model, names ...string) Model {
	t.Helper()
	for _, name := range names {
		m = cursorTo(t, m, name)
		m.tab().Selected[filepath.Join(m.tab().CurrentPath, name)] = true
	}
	m, cmd := press(t, m, "d")
	return drain(t, m, cmd)
}

func TestTrashBrowserPutsBack(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "aaa")
	writeTestFile(t, filepath.Join(dir, "keep.txt"), "")
	m := trashWith(t, newTestModel(t, dir, nil), "a.txt")
	if len(m.undo) != 1 {
		t.Fatalf("undo = %d entries", len(m.undo))
	}
	// sushi notes where it came from
	var record struct {
		Items []struct{ Original, Trashed string }
	}
	if err := json.Unmarshal([]byte(readTestFile(t, config.TrashRecordPath())), &record); err != nil || len(record.Items) != 1 || record.Items[0].Original != filepath.Join(dir, "a.txt") {
		t.Fatalf("record: %v %+v", err, record)
	}

	m = openTrashView(t, m)
	it, _ := m.trash.chosen()
	if trashNames(m) != "a.txt" || it.Original != filepath.Join(dir, "a.txt") || it.size != 3 {
		t.Fatalf("listed %s: %+v", trashNames(m), it)
	}
	screen := strings.Join(plain(m.View()), "\n")
	for _, want := range []string{"Trash", "~/.Trash", "1 item, 3 B", "a.txt", "TRASH", "enter put back"} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen lacks %q:\n%s", want, screen)
		}
	}

	m, cmd := press(t, m, "enter")
	m = drain(t, m, cmd)
	if readTestFile(t, filepath.Join(dir, "a.txt")) != "aaa" || !strings.Contains(m.statusMsg, "Put back: a.txt") {
		t.Fatalf("put back: %q", m.statusMsg)
	}
	// The browser shows the trash as it now is, and the cursor is on the
	// file in the list
	if m.mode != ModeTrash || trashNames(m) != "" || m.tab().Files[m.tab().Cursor].Name != "a.txt" {
		t.Fatalf("after putting back: mode=%v listed %q", m.mode, trashNames(m))
	}
	// The trashing can't be undone any more; putting back can
	if len(m.undo) != 1 || m.undo[0].label != "put back a.txt" {
		t.Fatalf("undo = %+v", m.undo)
	}

	m, _ = press(t, m, "esc")
	m = undoNow(t, m)
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err == nil {
		t.Fatal("undo didn't trash a.txt again")
	}
	m = openTrashView(t, m)
	if it, _ := m.trash.chosen(); trashNames(m) != "a.txt" || it.Original != filepath.Join(dir, "a.txt") {
		t.Fatalf("trashed again: %s %+v", trashNames(m), it)
	}
}

func TestTrashBrowserNeverReplaces(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "old")
	m := trashWith(t, newTestModel(t, dir, nil), "a.txt")
	writeTestFile(t, filepath.Join(dir, "a.txt"), "new")

	m = openTrashView(t, m)
	m, cmd := press(t, m, "r")
	m = drain(t, m, cmd)
	if readTestFile(t, filepath.Join(dir, "a.txt")) != "new" || !strings.Contains(m.statusMsg, "Can't put back a.txt") || trashNames(m) != "a.txt" {
		t.Fatalf("put back over a file: %q", m.statusMsg)
	}
	// Nor does restoring here
	m, cmd = press(t, m, "p")
	m = drain(t, m, cmd)
	if readTestFile(t, filepath.Join(dir, "a.txt")) != "new" || trashNames(m) != "a.txt" {
		t.Fatalf("restore here over a file: %q", m.statusMsg)
	}
}

func TestTrashBrowserRestoresHere(t *testing.T) {
	dir := t.TempDir()
	m := newTestModel(t, dir, nil)
	// Finder's items say nothing of where they came from
	os.MkdirAll(trashDir(t), 0700)
	writeTestFile(t, filepath.Join(trashDir(t), "from finder.pdf"), "pdf")
	writeTestFile(t, filepath.Join(trashDir(t), ".DS_Store"), "")

	m = openTrashView(t, m)
	if trashNames(m) != "from finder.pdf" {
		t.Fatalf("listed %s", trashNames(m))
	}
	if screen := strings.Join(plain(m.View()), "\n"); !strings.Contains(screen, "unknown origin") {
		t.Fatalf("screen:\n%s", screen)
	}
	m, _ = press(t, m, "enter")
	if !strings.Contains(m.statusMsg, "isn't known: p restores it") || m.job != nil {
		t.Fatalf("enter: %q", m.statusMsg)
	}

	m, cmd := press(t, m, "p")
	m = drain(t, m, cmd)
	if readTestFile(t, filepath.Join(dir, "from finder.pdf")) != "pdf" {
		t.Fatalf("restore here: %q", m.statusMsg)
	}
	// Undone, it goes back to the trash
	m, _ = press(t, m, "q")
	m = undoNow(t, m)
	if _, err := os.Stat(filepath.Join(trashDir(t), "from finder.pdf")); err != nil {
		t.Fatalf("undo of a restore: %v", err)
	}
}

func TestTrashBrowserDeletesAfterAsking(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "folder"), 0755)
	writeTestFile(t, filepath.Join(dir, "folder", "inside"), "x")
	writeTestFile(t, filepath.Join(dir, "b.txt"), "b")
	m := trashWith(t, newTestModel(t, dir, nil), "folder", "b.txt")
	m = openTrashView(t, m)
	m = trashCursorTo(t, m, "folder")
	trashed := filepath.Join(trashDir(t), "folder")

	// No keeps it
	m, _ = press(t, m, "D")
	if m.trash.confirm != "delete" || !strings.Contains(strings.Join(plain(m.View()), "\n"), "Delete 'folder' for good?") {
		t.Fatalf("D: confirm=%q", m.trash.confirm)
	}
	if m, _ = press(t, m, "n"); m.trash.confirm != "" || m.mode != ModeTrash {
		t.Fatal("n didn't cancel")
	}
	// The mouse can't answer
	m, _ = press(t, m, "D")
	if after, _ := clickAt(m, m.width/2, m.height/2); after.trash.confirm != "delete" {
		t.Fatal("a click answered the question")
	}

	m, cmd := press(t, m, "y")
	m = drain(t, m, cmd)
	if _, err := os.Lstat(trashed); err == nil || !strings.Contains(m.statusMsg, "Deleted for good: folder") || trashNames(m) != "b.txt" {
		t.Fatalf("delete: %q, listed %s", m.statusMsg, trashNames(m))
	}
	// The trashing of b.txt can still be undone, folder's can't, and
	// ctrl+z says the delete was permanent rather than undoing anything
	if len(m.undo) != 2 || len(m.undo[0].steps) != 1 || m.undo[1].reason != "it was permanent" {
		t.Fatalf("undo = %+v", m.undo)
	}
	m, _ = press(t, m, "q")
	if m, _ = ctrl(t, m, tea.KeyCtrlZ); !strings.Contains(m.statusMsg, "Can't undo delete folder from the trash") {
		t.Fatalf("ctrl+z: %q", m.statusMsg)
	}
}

func TestTrashBrowserEmptiesTheTrash(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can delete anything")
	}
	dir := t.TempDir()
	for _, name := range []string{"a", "b"} {
		writeTestFile(t, filepath.Join(dir, name), strings.Repeat(name, 1000))
	}
	m := trashWith(t, newTestModel(t, dir, nil), "a", "b")
	// Something that can't be deleted
	stuck := filepath.Join(trashDir(t), "stuck")
	os.Mkdir(stuck, 0755)
	writeTestFile(t, filepath.Join(stuck, "inside"), "")
	os.Chmod(stuck, 0555)
	t.Cleanup(func() { os.Chmod(stuck, 0755) })
	writeTestFile(t, filepath.Join(trashDir(t), ".DS_Store"), "finder's")

	m = openTrashView(t, m)
	m, _ = press(t, m, "E")
	screen := strings.Join(plain(m.View()), "\n")
	if m.trash.confirm != "empty" || !strings.Contains(screen, "3 items, 2.0 KB.") || !strings.Contains(screen, "There is no undo") {
		t.Fatalf("E:\n%s", screen)
	}
	m, cmd := press(t, m, "y")
	m = drain(t, m, cmd)
	if !strings.Contains(m.statusMsg, "emptied the trash but for 1 item it can't delete; stuck") {
		t.Fatalf("empty: %q", m.statusMsg)
	}
	left, _ := os.ReadDir(trashDir(t))
	if len(left) != 2 || trashNames(m) != "stuck" {
		t.Fatalf("left %v, listed %s", left, trashNames(m))
	}
	// Nothing trashed can be put back by undo now, and undo says why
	if len(m.undo) != 1 || m.undo[0].reason != "it was permanent" {
		t.Fatalf("undo = %+v", m.undo)
	}
}

func TestTrashBrowserFilters(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"report.pdf", "notes.txt", "photo.jpg"} {
		writeTestFile(t, filepath.Join(dir, name), "")
	}
	m := trashWith(t, newTestModel(t, dir, nil), "report.pdf", "notes.txt", "photo.jpg")
	m = openTrashView(t, m)

	m = typeText(t, m, "/rt")
	if trashNames(m) != "report.pdf" || !m.trash.filtering {
		t.Fatalf("filter rt: %s", trashNames(m))
	}
	// Keys go to the filter while typing it: q and j are letters
	m = typeText(t, m, "q")
	if m.mode != ModeTrash || trashNames(m) != "" {
		t.Fatalf("q while filtering: mode=%v %s", m.mode, trashNames(m))
	}
	m, _ = press(t, m, "backspace")
	m, _ = press(t, m, "enter")
	if m.trash.filtering || trashNames(m) != "report.pdf" {
		t.Fatal("enter should keep the filter")
	}
	// esc clears the filter, then closes
	if m, _ = press(t, m, "esc"); m.mode != ModeTrash || len(m.trash.shown) != 3 {
		t.Fatalf("esc: mode=%v %s", m.mode, trashNames(m))
	}
	if m, _ = press(t, m, "esc"); m.mode != ModeNormal {
		t.Fatal("second esc should close")
	}
}

func TestTrashBrowserWithTheMouse(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"one.txt", "two.txt"} {
		writeTestFile(t, filepath.Join(dir, name), name)
	}
	fakeClock(t)
	m := trashWith(t, newTestModel(t, dir, nil), "one.txt", "two.txt")
	m = openTrashView(t, m)

	x, y := findOnScreen(t, m, "two.txt", 0, m.width)
	if m, _ = clickAt(m, x, y); m.trash.items[m.trash.shown[m.trash.cursor]].Name != "two.txt" {
		t.Fatal("click didn't pick two.txt")
	}
	m, _ = mouseAt(m, x, y, tea.MouseButtonWheelUp, false)
	m, _ = mouseAt(m, x, y, tea.MouseButtonWheelDown, false)
	// A double-click puts it back
	m, _ = clickAt(m, x, y)
	m, cmd := clickAt(m, x, y)
	m = drain(t, m, cmd)
	if readTestFile(t, filepath.Join(dir, "two.txt")) != "two.txt" {
		t.Fatalf("double-click: %q", m.statusMsg)
	}
	// A click outside closes it
	resized := resize(m, tea.WindowSizeMsg{Width: 140, Height: 30})
	if after, _ := clickAt(resized, 1, 5); after.mode != ModeNormal {
		t.Fatal("a click outside didn't close the browser")
	}
}

func TestTrashBrowserWaitsForJobs(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "")
	m := trashWith(t, newTestModel(t, dir, nil), "a.txt")
	m = openTrashView(t, m)
	m.job = &job{doing: "Copying"}
	for _, k := range []string{"enter", "p", "D", "E"} {
		after, cmd := press(t, m, k)
		if cmd == nil || after.trash.confirm != "" || !strings.Contains(after.statusMsg, "Still copying") {
			t.Errorf("%s while a job runs: %q", k, after.statusMsg)
		}
	}
}

func TestUndoForgetsWhatLeftTheTrash(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "a")
	writeTestFile(t, filepath.Join(dir, "b.txt"), "b")
	m := trashWith(t, newTestModel(t, dir, nil), "a.txt", "b.txt")
	if len(m.undo) != 1 || len(m.undo[0].steps) != 2 {
		t.Fatalf("undo = %+v", m.undo)
	}

	// a.txt is put back from the browser; ctrl+z still puts back b.txt
	m = openTrashView(t, m)
	m = trashCursorTo(t, m, "a.txt")
	m, cmd := press(t, m, "enter")
	m = drain(t, m, cmd)
	m, _ = press(t, m, "esc")
	if len(m.undo) != 2 || len(m.undo[0].steps) != 1 {
		t.Fatalf("undo = %+v", m.undo)
	}
	m.undo = m.undo[:1] // Leave out the put back, to undo the trashing
	m = undoNow(t, m)
	if readTestFile(t, filepath.Join(dir, "b.txt")) != "b" || !strings.Contains(m.statusMsg, "Undone") {
		t.Fatalf("undo: %q", m.statusMsg)
	}
}

func TestTrashBrowserExplainsAnUnreadableTrash(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can read anything")
	}
	useFakeMac(t)
	m := newTestModel(t, t.TempDir(), nil)
	// As macOS refuses ~/.Trash to a terminal without Full Disk Access
	os.MkdirAll(trashDir(t), 0700)
	os.Chmod(trashDir(t), 0)
	t.Cleanup(func() { os.Chmod(trashDir(t), 0700) })

	m = openTrashView(t, m)
	screen := strings.Join(plain(resize(m, tea.WindowSizeMsg{Width: 200, Height: 24}).View()), "\n")
	if m.trash.err == nil || !strings.Contains(screen, "Can't read the trash: permission denied.") || !strings.Contains(screen, "Full Disk Access") {
		t.Fatalf("unreadable trash:\n%s", screen)
	}
	for _, size := range sizes {
		assertFills(t, fmt.Sprintf("%dx%d unreadable trash", size.Width, size.Height), resize(detach(m), size))
	}
	// Nothing to act on
	for _, k := range []string{"enter", "p", "D", "E"} {
		if after, cmd := press(t, m, k); cmd != nil || after.trash.confirm != "" {
			t.Errorf("%s did something", k)
		}
	}
}

func TestTrashBrowserFillsTheTerminal(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "small.txt"), "x")
	// Something in the trash, from where it was, and something not
	m := trashWith(t, newTestModel(t, dir, nil), "small.txt")
	os.MkdirAll(trashDir(t), 0700)
	writeTestFile(t, filepath.Join(trashDir(t), strings.Repeat("finder-", 20)+"日本語.txt"), "x")

	for _, size := range sizes {
		label := fmt.Sprintf("%dx%d", size.Width, size.Height)
		trash := openTrashView(t, resize(detach(m), size))
		if len(trash.trash.items) != 2 {
			t.Fatalf("%s: %d items", label, len(trash.trash.items))
		}
		assertFills(t, label+" trash", trash)
		for _, keys := range []string{"j", "D", "E", "/sm", "/zz"} {
			screen := detach(trash)
			for _, r := range keys {
				screen, _ = press(t, screen, string(r))
			}
			assertFills(t, label+" trash after "+keys, screen)
		}
	}
}

func TestTrashKeyInThePanel(t *testing.T) {
	if panel := keyPanelAt100x24(t); !strings.Contains(panel, "U ctrl+t disk usage, trash") {
		t.Errorf("panel lacks ctrl+t: %s", panel)
	}
}

func TestTrashBrowserFitsWideIcons(t *testing.T) {
	ui.SetIconMode(ui.IconModeASCII)
	t.Cleanup(func() { ui.SetIconMode(ui.IconModeNerd) })
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "dir1"), 0755)
	writeTestFile(t, filepath.Join(dir, "foreign.txt"), "f")
	m := trashWith(t, newTestModel(t, dir, nil), "dir1", "foreign.txt")
	m = openTrashView(t, m)

	// --ascii's icons take three cells, as [D]: the names start after a
	// space, and line up with their heading
	screen := plain(m.View())
	nameAt := -1
	for _, line := range screen {
		if i := strings.Index(line, "Name"); i >= 0 && strings.Contains(line, "Deleted") {
			nameAt = i
		}
	}
	for _, name := range []string{"dir1", "foreign.txt"} {
		found := false
		for _, line := range screen {
			if i := strings.Index(line, name); i >= 0 {
				found = true
				if line[i-1] != ' ' || i != nameAt {
					t.Errorf("%s at %d, its heading at %d: %q", name, i, nameAt, line)
				}
			}
		}
		if !found {
			t.Errorf("%s isn't listed:\n%s", name, strings.Join(screen, "\n"))
		}
	}
}
