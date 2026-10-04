package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// usageTree makes a folder to count: big/ holds 4,000 bytes in two files,
// one a folder down, beside a hidden file, a small file and a link to big
func usageTree(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "root")
	os.MkdirAll(filepath.Join(root, "big", "sub"), 0755)
	writeTestFile(t, filepath.Join(root, "big", "sub", "x.bin"), strings.Repeat("x", 3000))
	writeTestFile(t, filepath.Join(root, "big", "y.bin"), strings.Repeat("y", 1000))
	writeTestFile(t, filepath.Join(root, ".hidden"), strings.Repeat("h", 500))
	writeTestFile(t, filepath.Join(root, "small.txt"), strings.Repeat("s", 100))
	os.Symlink("big", filepath.Join(root, "link"))
	return root
}

// openUsage presses the disk usage key and waits for the count to finish
func openUsage(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := press(t, m, "U")
	if m.mode != ModeDiskUsage {
		t.Fatalf("U opened mode %v", m.mode)
	}
	return drain(t, m, cmd)
}

// usageRows returns the names and sizes the view lists, in order
func usageRows(m Model) string {
	var rows []string
	for _, r := range m.du.rows {
		rows = append(rows, fmt.Sprintf("%s=%d", r.name, r.size))
	}
	return strings.Join(rows, " ")
}

func TestDiskUsageListsLargestFirst(t *testing.T) {
	root := usageTree(t)
	m := cursorTo(t, newTestModel(t, root, nil), "small.txt")
	m = openUsage(t, m)

	// Hidden files count, and the link is the link, not what it points to
	if got := usageRows(m); got != "big=4000 .hidden=500 small.txt=100 link=3" {
		t.Fatalf("rows = %s", got)
	}
	st := m.du.status
	if st.scanning || st.stopped || st.root.files != 5 || st.root.size != 4603 || m.du.here.size != 4603 || st.root.disk == 0 {
		t.Fatalf("status = %+v", st)
	}
	screen := strings.Join(plain(m.View()), "\n")
	for _, want := range []string{"Disk usage", "big/", "3.9 KB", "2 files", "87%", "4.5 KB in 5 files", "scanned in", "DISK USAGE"} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen lacks %q:\n%s", want, screen)
		}
	}

	// Entering a folder uses what was counted
	scan := m.du.scan
	m, _ = press(t, m, "enter")
	if m.du.scan != scan || m.du.path != filepath.Join(root, "big") || usageRows(m) != "sub=3000 y.bin=1000" {
		t.Fatalf("in big: %s at %s", usageRows(m), m.du.path)
	}
	m, _ = press(t, m, "l")
	if usageRows(m) != "x.bin=3000" {
		t.Fatalf("l should enter sub: %s", usageRows(m))
	}
	// Up again, with the cursor on the folder it came from
	m, _ = press(t, m, "h")
	m, _ = press(t, m, "backspace")
	if m.du.path != root || m.du.rows[m.du.cursor].name != "big" || m.du.scan != scan {
		t.Fatalf("back up: %s, cursor on %s", m.du.path, m.du.rows[m.du.cursor].name)
	}

	// g goes to the entry in the file list, showing hidden files for it
	m, _ = press(t, m, "j")
	m, cmd := press(t, m, "g")
	m = drain(t, m, cmd)
	if m.mode != ModeNormal || !m.showHidden || m.tab().Files[m.tab().Cursor].Name != ".hidden" {
		t.Fatalf("g: mode=%v hidden=%v cursor on %s", m.mode, m.showHidden, m.tab().Files[m.tab().Cursor].Name)
	}
}

func TestDiskUsageOfFolderUnderCursor(t *testing.T) {
	root := usageTree(t)
	m := cursorTo(t, newTestModel(t, root, nil), "big")
	m = openUsage(t, m)
	if m.du.path != filepath.Join(root, "big") || usageRows(m) != "sub=3000 y.bin=1000" {
		t.Fatalf("U on big: %s at %s", usageRows(m), m.du.path)
	}
	// Enter on a file goes to it in the file list
	m, _ = press(t, m, "j")
	m, cmd := press(t, m, "enter")
	m = drain(t, m, cmd)
	if m.mode != ModeNormal || m.tab().CurrentPath != filepath.Join(root, "big") || m.tab().Files[m.tab().Cursor].Name != "y.bin" {
		t.Fatalf("enter on a file: mode=%v in %s", m.mode, m.tab().CurrentPath)
	}
}

func TestDiskUsageGoesAboveItsRoot(t *testing.T) {
	root := usageTree(t)
	m := cursorTo(t, newTestModel(t, root, nil), "big")
	m = openUsage(t, m)
	sub := m.du.rows[0].node

	// Above the folder counted, the folder above is counted, taking over
	// what was counted of this one
	m, cmd := press(t, m, "h")
	m = drain(t, m, cmd)
	if m.du.path != root || usageRows(m) != "big=4000 .hidden=500 small.txt=100 link=3" || m.du.rows[m.du.cursor].name != "big" {
		t.Fatalf("above the root: %s at %s", usageRows(m), m.du.path)
	}
	m, _ = press(t, m, "enter")
	if m.du.rows[0].node != sub {
		t.Fatal("big was counted again rather than taken over")
	}
}

func TestDiskUsageTrashesAndUndoes(t *testing.T) {
	root := usageTree(t)
	m := cursorTo(t, newTestModel(t, root, nil), "small.txt")
	m = openUsage(t, m)

	m, cmd := press(t, m, "d")
	m = drain(t, m, cmd)
	big := filepath.Join(root, "big")
	if _, err := os.Stat(big); err == nil || !strings.Contains(m.statusMsg, "Moved to trash: big") {
		t.Fatalf("big wasn't trashed: %q", m.statusMsg)
	}
	if m.mode != ModeDiskUsage || usageRows(m) != ".hidden=500 small.txt=100 link=3" || m.du.here.size != 603 || m.du.status.root.files != 3 {
		t.Fatalf("after trashing: %s, %+v", usageRows(m), m.du.status.root)
	}
	if _, err := os.Stat(filepath.Join(trashDir(t), "big", "sub", "x.bin")); err != nil {
		t.Fatalf("big is not in the trash: %v", err)
	}

	// It was the normal trash, which ctrl+z undoes, in the view too, which
	// counts the folder again with it back
	m, cmd = ctrl(t, m, tea.KeyCtrlZ)
	m = drain(t, m, cmd)
	if readTestFile(t, filepath.Join(big, "y.bin")) != strings.Repeat("y", 1000) {
		t.Fatalf("undo didn't bring big back: %q", m.statusMsg)
	}
	if m.mode != ModeDiskUsage || usageRows(m) != "big=4000 .hidden=500 small.txt=100 link=3" {
		t.Fatalf("after undoing in the view: mode %v, %s", m.mode, usageRows(m))
	}
	// And in the file list, once closed
	m, cmd = press(t, cursorTo(t, m, "big"), "d")
	m = drain(t, m, cmd)
	m, _ = press(t, m, "q")
	m = undoNow(t, m)
	if readTestFile(t, filepath.Join(big, "y.bin")) != strings.Repeat("y", 1000) {
		t.Fatal("undo didn't bring big back")
	}
}

func TestDiskUsageKeysToTheEnds(t *testing.T) {
	root := usageTree(t)
	m := openUsage(t, cursorTo(t, newTestModel(t, root, nil), "small.txt"))
	m, _ = press(t, m, "G")
	if r, _, _ := m.du.chosen(); r.name != "link" {
		t.Fatalf("G went to %s", r.name)
	}
	m, _ = ctrl(t, m, tea.KeyHome)
	if r, _, _ := m.du.chosen(); r.name != "big" {
		t.Fatalf("Home went to %s", r.name)
	}
	m, _ = ctrl(t, m, tea.KeyEnd)
	if r, _, _ := m.du.chosen(); r.name != "link" || m.mode != ModeDiskUsage {
		t.Fatalf("End went to %s", r.name)
	}
}

func TestCancelFromTheViews(t *testing.T) {
	root := usageTree(t)
	m := cursorTo(t, newTestModel(t, root, nil), "small.txt")
	views := map[string]func(Model) Model{
		"disk usage":    func(m Model) Model { return openUsage(t, m) },
		"trash browser": func(m Model) Model { return openTrashView(t, m) },
	}
	for name, open := range views {
		v := open(detach(m))
		mode := v.mode
		if after, _ := ctrl(t, v, tea.KeyCtrlX); after.statusMsg != "Nothing to cancel" || after.mode != mode {
			t.Errorf("%s, nothing running: %q", name, after.statusMsg)
		}
		// The job it started, say deleting for good from the trash
		ctx, cancel := context.WithCancel(context.Background())
		v.job = &job{doing: "Deleting", cancel: cancel}
		after, _ := ctrl(t, v, tea.KeyCtrlX)
		if ctx.Err() == nil || !after.job.cancelled || after.mode != mode {
			t.Errorf("%s: ctrl+x didn't cancel the job (mode %v)", name, after.mode)
		}
		cancel()
	}
}

func TestDiskUsageWhileCounting(t *testing.T) {
	root := usageTree(t)
	// The walk is held up entering big, once it has listed the root
	entered, release := make(chan struct{}), make(chan struct{})
	open := duOpenDir
	duOpenDir = func(path string) (*os.File, error) {
		if filepath.Base(path) == "big" {
			close(entered)
			<-release
		}
		return os.Open(path)
	}
	t.Cleanup(func() { duOpenDir = open })

	m := cursorTo(t, newTestModel(t, root, nil), "small.txt")
	m, cmd := press(t, m, "U")
	<-entered
	// The view fills in while it counts
	updated, next := m.Update(cmd())
	m = updated.(Model)
	if !m.du.status.scanning || len(m.du.rows) != 4 || next == nil {
		t.Fatalf("while counting: %+v, rows %s", m.du.status, usageRows(m))
	}
	screen := strings.Join(plain(m.View()), "\n")
	// big is listed, marked as still being counted
	if !strings.Contains(screen, "Scanning… 3 files, 603 B so far") || !strings.Contains(screen, "esc stop scan") || !strings.Contains(screen, "0 files…") {
		t.Fatalf("while counting:\n%s", screen)
	}
	for _, size := range sizes {
		assertFills(t, fmt.Sprintf("%dx%d while counting", size.Width, size.Height), resize(detach(m), size))
	}

	// Nothing is trashed while counting
	if after, _ := press(t, m, "d"); after.job != nil || !strings.Contains(after.statusMsg, "Still scanning") {
		t.Fatalf("d while counting: %q", after.statusMsg)
	}

	// esc stops counting, and keeps what was counted
	m, _ = press(t, m, "esc")
	close(release)
	m = drain(t, m, next)
	if m.mode != ModeDiskUsage || !m.du.status.stopped || m.du.status.scanning {
		t.Fatalf("after esc: mode=%v %+v", m.mode, m.du.status)
	}
	if screen := strings.Join(plain(m.View()), "\n"); !strings.Contains(screen, "Scan stopped: 3 files, 603 B counted") {
		t.Fatalf("stopped:\n%s", screen)
	}
	// Then esc closes it
	if m, _ = press(t, m, "esc"); m.mode != ModeNormal || m.du.scan != nil {
		t.Fatalf("second esc: mode=%v", m.mode)
	}
}

func TestDiskUsageCountsUnreadableFolders(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can read anything")
	}
	root := usageTree(t)
	locked := filepath.Join(root, "locked")
	os.Mkdir(locked, 0755)
	writeTestFile(t, filepath.Join(locked, "secret"), "x")
	os.Chmod(locked, 0)
	t.Cleanup(func() { os.Chmod(locked, 0755) })

	m := cursorTo(t, newTestModel(t, root, nil), "small.txt")
	m = openUsage(t, m)
	screen := strings.Join(plain(m.View()), "\n")
	if m.du.status.root.unread != 1 || !strings.Contains(screen, "1 unreadable") || !strings.Contains(screen, "locked/ !") {
		t.Fatalf("unreadable: %+v\n%s", m.du.status.root, screen)
	}

	// Counted on its own, it says it can't be read
	m, _ = press(t, m, "q")
	m = openUsage(t, cursorTo(t, m, "locked"))
	if screen := strings.Join(plain(m.View()), "\n"); !strings.Contains(screen, "Can't read this folder") {
		t.Fatalf("unreadable root:\n%s", screen)
	}
	for _, size := range sizes {
		assertFills(t, fmt.Sprintf("%dx%d unreadable", size.Width, size.Height), resize(detach(m), size))
	}
}

func TestDiskUsageShowsEntriesInFinder(t *testing.T) {
	f := useFakeMac(t)
	root := usageTree(t)
	m := cursorTo(t, newTestModel(t, root, nil), "small.txt")
	m = openUsage(t, m)
	m, cmd := ctrl(t, m, tea.KeyCtrlO)
	m = drain(t, m, cmd)
	if want := [][]string{{"-R", filepath.Join(root, "big")}}; len(f.opened) != 1 || strings.Join(f.opened[0], " ") != strings.Join(want[0], " ") {
		t.Fatalf("opened %q, want %q", f.opened, want)
	}
	if m.mode != ModeDiskUsage {
		t.Fatal("showing in Finder closed the view")
	}
}

func TestDiskUsageWithTheMouse(t *testing.T) {
	root := usageTree(t)
	fakeClock(t)
	m := cursorTo(t, newTestModel(t, root, nil), "small.txt")
	m = openUsage(t, m)

	x, y := findOnScreen(t, m, "small.txt", 0, m.width)
	if m, _ = clickAt(m, x, y); m.du.rows[m.du.cursor].name != "small.txt" {
		t.Fatalf("click picked %s", m.du.rows[m.du.cursor].name)
	}
	m, _ = mouseAt(m, x, y, tea.MouseButtonWheelUp, false)
	if m.du.cursor != 0 {
		t.Fatalf("wheel: cursor %d", m.du.cursor)
	}
	// A double-click on a folder enters it
	x, y = findOnScreen(t, m, "big/", 0, m.width)
	m, _ = clickAt(m, x, y)
	if m, _ = clickAt(m, x, y); m.du.path != filepath.Join(root, "big") {
		t.Fatalf("double-click: in %s", m.du.path)
	}
	// The status bar and hints do nothing
	if after, _ := clickAt(m, 2, m.height-1); after.mode != ModeDiskUsage || after.du.path != m.du.path {
		t.Fatal("a click on the hints did something")
	}
}

func TestDiskUsageFillsTheTerminal(t *testing.T) {
	root := usageTree(t)
	long := filepath.Join(root, "big", strings.Repeat("a-very-long-name-", 8)+"日本語.txt")
	writeTestFile(t, long, "x")
	m := newTestModel(t, root, nil)

	for _, size := range sizes {
		label := fmt.Sprintf("%dx%d", size.Width, size.Height)
		screen := resize(openUsage(t, cursorTo(t, detach(m), "big")), size)
		assertFills(t, label+" disk usage", screen)
		screen, _ = press(t, screen, "j")
		assertFills(t, label+" disk usage, cursor on the long name", screen)
	}
}

// keyPanelAt100x24 returns the key panel as an 100x24 terminal shows it,
// with runs of spaces read as one, failing if it has to scroll there
func keyPanelAt100x24(t *testing.T) string {
	t.Helper()
	m := resize(newTestModel(t, t.TempDir(), nil), tea.WindowSizeMsg{Width: 100, Height: 24})
	m, _ = press(t, m, "?")
	if m.maxHelpScroll() != 0 {
		t.Fatalf("the key panel scrolls at 100x24:\n%s", m.renderHelpView())
	}
	return strings.Join(strings.Fields(strings.Join(plain(m.renderHelpView()), " ")), " ")
}

func TestDiskUsageKeyInThePanel(t *testing.T) {
	if panel := keyPanelAt100x24(t); !strings.Contains(panel, "U ctrl+t disk usage, trash") {
		t.Errorf("panel lacks U: %s", panel)
	}
}

func TestThousands(t *testing.T) {
	for n, want := range map[int]string{0: "0", 999: "999", 1000: "1,000", 12345: "12,345", 1234567: "1,234,567", -4500: "-4,500"} {
		if got := thousands(n); got != want {
			t.Errorf("thousands(%d) = %s, want %s", n, got, want)
		}
	}
	if countOf(1, "file") != "1 file" || countOf(12340, "file") != "12,340 files" {
		t.Error(countOf(12340, "file"))
	}
}

func TestDiskUsageWontTrashAVolume(t *testing.T) {
	root := usageTree(t)
	m := cursorTo(t, newTestModel(t, root, nil), "small.txt")
	m = openUsage(t, m)

	// big shows as another volume mounted there, as the walk marks one: d
	// would copy all of it into the trash, then empty it
	m.du.rows[0].node.mount = true
	m.du.refresh()
	if r, _, _ := m.du.chosen(); r.name != "big" || !r.mount {
		t.Fatalf("cursor on %+v", r)
	}
	m, cmd := press(t, m, "d")
	m = drain(t, m, cmd)
	if m.job != nil || m.statusMsg != "big is a mounted volume; eject it instead" {
		t.Fatalf("d on a volume: job %v, status %q", m.job, m.statusMsg)
	}
	if readTestFile(t, filepath.Join(root, "big", "y.bin")) != strings.Repeat("y", 1000) || len(m.undo) != 0 {
		t.Fatal("the volume was trashed")
	}
}
