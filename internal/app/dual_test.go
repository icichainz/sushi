package app

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/utils"
)

func init() {
	// Visits are saved at once, so drained commands don't sit out the
	// delay; see jump.go
	historySaveDelay = 0
}

// twoFolders makes left, holding a.txt, b.txt and a folder, sub, and
// right, holding r-00.txt to r-04.txt
func twoFolders(t *testing.T) (root, left, right string) {
	t.Helper()
	root = t.TempDir()
	left, right = filepath.Join(root, "left"), filepath.Join(root, "right")
	os.MkdirAll(filepath.Join(left, "sub"), 0755)
	os.MkdirAll(right, 0755)
	writeTestFile(t, filepath.Join(left, "a.txt"), "A")
	writeTestFile(t, filepath.Join(left, "b.txt"), "B")
	for i := range 5 {
		writeTestFile(t, filepath.Join(right, fmt.Sprintf("r-%02d.txt", i)), "r")
	}
	return root, left, right
}

// dualModel opens left with right in a second pane, the left one active
func dualModel(t *testing.T, left, right string) Model {
	t.Helper()
	m := newTestModel(t, left, noWatch())
	m, cmd := press(t, m, "w")
	return goThere(t, drain(t, m, cmd), right)
}

// goHere loads dir into the active pane
func goHere(t *testing.T, m Model, dir string) Model {
	t.Helper()
	cmd := m.loadDir(m.tab(), dir)
	return drain(t, m, cmd)
}

// goThere loads dir into the inactive pane
func goThere(t *testing.T, m Model, dir string) Model {
	t.Helper()
	cmd := m.loadDir(m.otherPane(), dir)
	return drain(t, m, cmd)
}

// paneNames returns the names a pane lists
func paneNames(tab *Tab) []string {
	var out []string
	for _, f := range tab.Files {
		out = append(out, f.Name)
	}
	return out
}

func TestDualPaneSplitsAndSwitches(t *testing.T) {
	_, left, right := twoFolders(t)
	var told []string
	setHostDirectory = func(dir string) error { told = append(told, dir); return nil }
	defer func() { setHostDirectory = func(string) error { return nil } }()

	m := newTestModel(t, left, noWatch())
	m, cmd := press(t, m, "w")
	m = drain(t, m, cmd)
	other := m.otherPane()
	if other == nil || other.CurrentPath != left || len(other.Files) != 3 || other.ID == m.tab().ID {
		t.Fatalf("after w: %+v", other)
	}
	// Two lists side by side, without the parent pane, and at 100 columns
	// without the preview
	if l := m.layout(); l.parentW != 0 || l.previewW != 0 || l.listW+l.otherW != 100 || l.otherFirst {
		t.Fatalf("layout %+v", l)
	}

	// ctrl+l makes the right pane active, where it is
	m = goThere(t, m, right)
	m, _ = ctrl(t, m, tea.KeyCtrlL)
	if !m.tab().split.right || m.tab().CurrentPath != right || m.otherPane().CurrentPath != left || !m.layout().otherFirst {
		t.Fatalf("after ctrl+l: active %s, other %s", m.tab().CurrentPath, m.otherPane().CurrentPath)
	}
	if told[len(told)-1] != right {
		t.Errorf("the host was told %v, want the active pane's folder last", told)
	}
	lines := plain(m.View())
	if !strings.Contains(lines[breadcrumbRow], "right") || strings.Contains(lines[breadcrumbRow], "left") {
		t.Errorf("breadcrumb %q should name the active pane's folder", lines[breadcrumbRow])
	}
	// Each list's heading names its folder, on its side
	if head := lines[paneTop]; !strings.Contains(utils.Cells(head, 0, 50), "left") || !strings.Contains(utils.Cells(head, 50, 100), "right") {
		t.Errorf("headings %q", head)
	}
	if m, _ = ctrl(t, m, tea.KeyCtrlL); m.tab().CurrentPath != right {
		t.Fatal("ctrl+l on the right pane should stay there")
	}

	// ctrl+h goes back to the left one
	m, _ = ctrl(t, m, tea.KeyCtrlH)
	if m.tab().split.right || m.tab().CurrentPath != left || told[len(told)-1] != left {
		t.Fatalf("after ctrl+h: active %s, told %v", m.tab().CurrentPath, told)
	}

	// w keeps the active pane alone, and brings the other back where it was
	m, _ = press(t, m, "w")
	if m.tab().split != nil || m.tab().CurrentPath != left || m.layout().otherW != 0 {
		t.Fatalf("after w again: split %v in %s", m.tab().split, m.tab().CurrentPath)
	}
	m, cmd = press(t, m, "w")
	if m = drain(t, m, cmd); m.otherPane() == nil || m.otherPane().CurrentPath != right || len(m.otherPane().Files) != 5 {
		t.Fatalf("w once more: the other pane is in %s", m.otherPane().CurrentPath)
	}

	// Without two panes the pane keys say how to get them
	m, _ = press(t, m, "w")
	for _, k := range []tea.KeyType{tea.KeyCtrlH, tea.KeyCtrlL} {
		if after, _ := ctrl(t, m, k); !strings.Contains(after.statusMsg, "needs two panes: w shows them") {
			t.Errorf("%v with one pane: %q", k, after.statusMsg)
		}
	}
	for _, k := range []string{"W", "=", ">", "<"} {
		if after, _ := press(t, m, k); !strings.Contains(after.statusMsg, "needs two panes") {
			t.Errorf("%s with one pane: %q", k, after.statusMsg)
		}
	}
}

func TestDualPaneFromTheConfig(t *testing.T) {
	_, left, _ := twoFolders(t)
	cfg := noWatch()
	cfg.DualPane = true
	m := newTestModel(t, left, cfg)
	if other := m.otherPane(); other == nil || other.CurrentPath != left || len(other.Files) != 3 {
		t.Fatalf("dual_pane: true starts with %+v", other)
	}
	// Each pane has its own list, as lists are sorted in place
	if &m.tab().Files[0] == &m.otherPane().Files[0] {
		t.Fatal("the panes share their list")
	}
	m, _ = press(t, m, "S")
	if got := paneNames(m.otherPane()); !slices.Equal(got, paneNames(m.tab())) || got[0] != "sub" || got[1] != "b.txt" {
		t.Fatalf("reversed: %v and %v", paneNames(m.tab()), got)
	}
}

func TestDualPaneFillsTheTerminal(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, strings.Repeat("very-long-directory-name-", 6))
	os.MkdirAll(filepath.Join(deep, "folder"), 0755)
	writeTestFile(t, filepath.Join(deep, "a.go"), strings.Repeat("package main // "+strings.Repeat("x", 150)+"\n", 50))
	writeTestFile(t, filepath.Join(deep, "日本語のとても長いファイル名"+strings.Repeat("字", 40)+".txt"), "x")

	for _, size := range []tea.WindowSizeMsg{{Width: 160, Height: 40}, {Width: 120, Height: 30}, {Width: 100, Height: 24},
		{Width: 80, Height: 24}, {Width: 60, Height: 15}, {Width: 30, Height: 10}} {
		m := resize(newTestModel(t, deep, noWatch()), size)
		m, cmd := press(t, m, "w")
		m = drain(t, m, cmd)
		label := fmt.Sprintf("%dx%d", size.Width, size.Height)
		assertFills(t, label+" dual", m)

		// The preview shows from minWidthDualPreview columns
		if l := m.layout(); (l.previewW > 0) != (size.Width >= minWidthDualPreview) || l.listW+l.otherW+l.previewW != size.Width {
			t.Errorf("%s: layout %+v", label, l)
		}

		right, _ := ctrl(t, detach(m), tea.KeyCtrlL)
		assertFills(t, label+" right active", right)
		swapped, _ := press(t, detach(m), "W")
		assertFills(t, label+" swapped", swapped)
		noPreview := detach(m)
		noPreview.tab().PreviewEnabled = false
		assertFills(t, label+" preview off", noPreview)

		for _, keys := range []string{"/" + strings.Repeat("query", 10), " ", "r", "z", "zquery", "?", "d", "s"} {
			screen := typeText(t, detach(m), keys)
			assertFills(t, label+" after "+keys, screen)
		}
	}
}

func TestTransferToTheOtherPane(t *testing.T) {
	root, src, dst := twoFolders(t)
	writeTestFile(t, filepath.Join(dst, "b.txt"), "old")
	m := dualModel(t, src, dst)
	m = cursorTo(t, m, "a.txt")
	m, _ = press(t, m, "c") // The clipboard is left as it is
	clipboard := slices.Clone(m.clipboard)

	// > copies the file under the cursor into the other pane's folder,
	// which shows it once done
	m = cursorTo(t, m, "a.txt")
	m, cmd := press(t, m, ">")
	m = drain(t, m, cmd)
	if b, err := os.ReadFile(filepath.Join(dst, "a.txt")); err != nil || string(b) != "A" {
		t.Fatalf("dst/a.txt: %q, %v", b, err)
	}
	if !slices.Contains(paneNames(m.otherPane()), "a.txt") || !slices.Contains(paneNames(m.tab()), "a.txt") {
		t.Fatalf("after >: here %v, there %v", paneNames(m.tab()), paneNames(m.otherPane()))
	}
	if !slices.Equal(m.clipboard, clipboard) || m.clipboardMode != "copy" {
		t.Errorf("clipboard %v %s, want it untouched", m.clipboard, m.clipboardMode)
	}
	// Undone like a paste
	if m = undoNow(t, m); fileExists(filepath.Join(dst, "a.txt")) {
		t.Fatal("undo left the copy")
	}

	// < moves; a name taken there is asked about first, and then goes to
	// the other pane's folder
	m = cursorTo(t, m, "b.txt")
	m, _ = press(t, m, "<")
	if m.mode != ModeConfirm || m.pasteDir != dst || !strings.Contains(strings.Join(plain(m.View()), "\n"), "'b.txt' already exists") {
		t.Fatalf("mode %v, into %s", m.mode, m.pasteDir)
	}
	m, cmd = press(t, m, "y")
	m = drain(t, m, cmd)
	if b, _ := os.ReadFile(filepath.Join(dst, "b.txt")); string(b) != "B" || fileExists(filepath.Join(src, "b.txt")) {
		t.Fatalf("after moving b.txt: dst has %q, src has it still: %v", b, fileExists(filepath.Join(src, "b.txt")))
	}
	if slices.Contains(paneNames(m.tab()), "b.txt") {
		t.Errorf("b.txt is still listed here: %v", paneNames(m.tab()))
	}

	// The selection goes, and is cleared
	m, _ = press(t, cursorTo(t, m, "a.txt"), " ")
	m, cmd = press(t, m, ">")
	if m = drain(t, m, cmd); len(m.tab().Selected) != 0 || !fileExists(filepath.Join(dst, "a.txt")) {
		t.Fatalf("selection %v after copying it", m.tab().Selected)
	}

	// The other pane moving on while the dialog asks cancels the transfer
	writeTestFile(t, filepath.Join(src, "c.txt"), "new")
	writeTestFile(t, filepath.Join(dst, "c.txt"), "keep")
	m = goHere(t, m, src)
	m, _ = press(t, cursorTo(t, m, "c.txt"), ">")
	m = goThere(t, m, root)
	m, cmd = press(t, m, "y")
	m = drain(t, m, cmd)
	if b, _ := os.ReadFile(filepath.Join(dst, "c.txt")); string(b) != "keep" || !strings.Contains(m.statusMsg, "The folder shown changed") {
		t.Fatalf("dst/c.txt = %q, status %q", b, m.statusMsg)
	}
	// And so does n, after which a paste is a paste again
	m = goThere(t, m, dst)
	m, _ = press(t, cursorTo(t, m, "c.txt"), ">")
	if m, _ = press(t, m, "n"); m.sending != nil || m.mode != ModeNormal {
		t.Fatalf("after n: mode %v, transfer %+v", m.mode, m.sending)
	}

	// Onto itself is refused, as a paste is
	m = goThere(t, m, src)
	if m, _ = press(t, cursorTo(t, m, "c.txt"), ">"); !strings.Contains(m.statusMsg, "Can't copy") {
		t.Fatalf("copy onto itself: %q", m.statusMsg)
	}

	// While another operation runs it waits
	busy := detach(m)
	busy, _ = press(t, busy, "d") // Started, but its command isn't run
	if busy, _ = press(t, busy, ">"); !strings.Contains(busy.statusMsg, "Still") {
		t.Fatalf("> while busy: %q", busy.statusMsg)
	}
}

// fileExists reports whether something is at path
func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestOtherPaneHereAndSwap(t *testing.T) {
	_, left, right := twoFolders(t)
	m := dualModel(t, left, right)
	m = cursorTo(t, m, "b.txt")
	active := m.tab().ID

	// = shows the active pane's folder in the other, on the same file
	m, cmd := press(t, m, "=")
	m = drain(t, m, cmd)
	other := m.otherPane()
	if other.CurrentPath != left || other.Files[other.Cursor].Name != "b.txt" || m.tab().ID != active {
		t.Fatalf("after =: other in %s on %d", other.CurrentPath, other.Cursor)
	}

	// W swaps the sides; the active pane stays active
	m = goThere(t, m, right)
	m, _ = press(t, m, "W")
	if m.tab().ID != active || !m.tab().split.right || !m.layout().otherFirst {
		t.Fatalf("after W: active %d (was %d), right %v", m.tab().ID, active, m.tab().split.right)
	}
	if head := plain(m.View())[paneTop]; !strings.Contains(utils.Cells(head, 0, 50), "right") || !strings.Contains(utils.Cells(head, 50, 100), "left") {
		t.Errorf("headings after W: %q", head)
	}
}

func TestDualPaneMouse(t *testing.T) {
	_, left, right := twoFolders(t)
	os.Mkdir(filepath.Join(right, "deeper"), 0755)
	fakeClock(t)
	m := dualModel(t, left, right)

	// What spotAt finds is where it is drawn, on either side
	for _, m := range []Model{m, func() Model { s, _ := press(t, detach(m), "W"); return s }()} {
		x, y := findOnScreen(t, m, "r-03.txt", 0, m.width)
		if s := m.spotAt(x, y); s.area != areaOther || m.otherPane().Files[s.index].Name != "r-03.txt" {
			t.Errorf("r-03.txt at %d,%d: %+v", x, y, s)
		}
		x, y = findOnScreen(t, m, "b.txt", 0, m.width)
		if s := m.spotAt(x, y); s.area != areaList || m.tab().Files[s.index].Name != "b.txt" {
			t.Errorf("b.txt at %d,%d: %+v", x, y, s)
		}
	}

	// A click on the inactive list makes it active, on the row clicked
	x, y := findOnScreen(t, m, "r-03.txt", 50, 100)
	m, cmd := clickAt(m, x, y)
	m = drain(t, m, cmd)
	if m.tab().CurrentPath != right || m.tab().Files[m.tab().Cursor].Name != "r-03.txt" {
		t.Fatalf("after the click: in %s on %d", m.tab().CurrentPath, m.tab().Cursor)
	}
	// The wheel over the other list moves its cursor, leaving it inactive;
	// it stops at the last of its three entries
	m, _ = mouseAt(m, 5, y, tea.MouseButtonWheelDown, false)
	if m.tab().CurrentPath != right || m.otherPane().Cursor != 2 {
		t.Fatalf("wheel: active %s, other cursor %d", m.tab().CurrentPath, m.otherPane().Cursor)
	}
	// A double-click on a folder there makes it active and opens the folder
	x, y = findOnScreen(t, m, "sub", 0, 50)
	m, cmd = clickAt(m, x, y)
	m = drain(t, m, cmd)
	m, cmd = clickAt(m, x, y)
	if m = drain(t, m, cmd); m.tab().CurrentPath != filepath.Join(left, "sub") || m.otherPane().CurrentPath != right {
		t.Fatalf("double-click: active %s, other %s", m.tab().CurrentPath, m.otherPane().CurrentPath)
	}

	// While searching, the other list ignores clicks
	m, _ = press(t, m, "/")
	x, y = findOnScreen(t, m, "deeper", 50, 100)
	if after, _ := clickAt(m, x, y); after.tab().CurrentPath != filepath.Join(left, "sub") || after.mode != ModeSearch {
		t.Fatalf("click while searching: in %s, mode %v", after.tab().CurrentPath, after.mode)
	}
}

func TestDualPaneKeepsEverythingWorking(t *testing.T) {
	_, left, right := twoFolders(t)
	writeTestFile(t, filepath.Join(right, ".hidden"), "")
	m := dualModel(t, left, right)

	// Sorting and hidden files apply to both panes
	m, _ = press(t, m, "S")
	if got := paneNames(m.otherPane()); got[0] != "r-04.txt" {
		t.Errorf("reverse sort: the other pane lists %v", got)
	}
	m, cmd := press(t, m, ".")
	if m = drain(t, m, cmd); !slices.Contains(paneNames(m.otherPane()), ".hidden") {
		t.Errorf("hidden files: the other pane lists %v", paneNames(m.otherPane()))
	}

	// A change on disk reloads the inactive pane
	writeTestFile(t, filepath.Join(right, "new.txt"), "")
	cmd = m.handleDirsChanged(dirsChangedMsg{dirs: []string{right}})
	if m = drain(t, m, cmd); !slices.Contains(paneNames(m.otherPane()), "new.txt") {
		t.Errorf("after a change: the other pane lists %v", paneNames(m.otherPane()))
	}

	// Selections, searches and previews are each pane's
	m, _ = press(t, cursorTo(t, m, "a.txt"), " ")
	m, _ = ctrl(t, m, tea.KeyCtrlL)
	if len(m.tab().Selected) != 0 || len(m.otherPane().Selected) != 1 {
		t.Errorf("selections: here %v, there %v", m.tab().Selected, m.otherPane().Selected)
	}
	if !strings.Contains(plain(m.View())[paneTop], "1 selected") {
		t.Errorf("the other pane's heading doesn't count its selection: %q", plain(m.View())[paneTop])
	}
	m = typeText(t, m, "/r-03")
	if v := m.visibleFiles(); len(v) != 1 || m.otherView().visibleFiles()[0] != 0 || len(m.otherView().visibleFiles()) != 3 {
		t.Errorf("search: here %v, there %v", v, m.otherView().visibleFiles())
	}
	m, _ = press(t, m, "esc")
	if m.previewCmd(m.otherPane()) != nil || m.previewCmd(m.tab()) == nil {
		t.Error("only the active pane should load previews")
	}

	// Prompts, bookmarks and the find palette are the active pane's
	m, _ = press(t, cursorTo(t, m, "r-01.txt"), "r")
	if m.prompt.target != filepath.Join(right, "r-01.txt") {
		t.Errorf("rename: %s", m.prompt.target)
	}
	m, _ = press(t, m, "esc")
	m, _ = press(t, m, "n")
	if m.prompt.dir != right {
		t.Errorf("new file in %s", m.prompt.dir)
	}
	m, _ = press(t, m, "esc")
	if m, _ = press(t, m, "B"); m.bookmarks.Len() != 1 || m.bookmarks.Get(0).Path != right {
		t.Errorf("bookmarked %+v", m.bookmarks.Get(0))
	}
	if m, _ = press(t, m, "f"); m.find.root != right {
		t.Errorf("find searches %s", m.find.root)
	}
	m, _ = press(t, m, "esc")

	// A tab of its own, and closing it, leave the split tab as it was
	m, cmd = press(t, m, "t")
	m = drain(t, m, cmd)
	if m.tab().split != nil || m.tab().CurrentPath != right {
		t.Fatalf("new tab: split %v in %s", m.tab().split, m.tab().CurrentPath)
	}
	m, _ = ctrl(t, m, tea.KeyCtrlW)
	if m.tab().split == nil || m.otherPane().CurrentPath != left {
		t.Fatal("closing the new tab lost the split")
	}

	// A folder renamed in the other pane is followed there
	moved := left + "-moved"
	m = goHere(t, m, filepath.Dir(left))
	m, _ = press(t, cursorTo(t, m, "left"), "r")
	m, _ = press(t, m, "ctrl+u")
	m = submit(t, typeText(t, m, filepath.Base(moved)))
	if m.otherPane().CurrentPath != moved {
		t.Errorf("after renaming its folder the other pane is in %s", m.otherPane().CurrentPath)
	}
}

func TestDualPaneGitBadges(t *testing.T) {
	repo := gitRepo(t)
	m := gitModel(t, repo, noWatch())
	m, cmd := press(t, m, "w")
	m = drain(t, m, cmd)
	if got := badgeOf(t, m.otherView(), "tracked.txt"); got != "M" {
		t.Fatalf("the other pane's badge for tracked.txt is %q", got)
	}
	// Out of the repository, the active pane has no badges; the other keeps its
	m, cmd = press(t, m, "h")
	m = drain(t, m, cmd)
	if m.gitStatus() != nil || m.otherView().gitStatus() == nil {
		t.Fatalf("active %v, other %v", m.gitStatus(), m.otherView().gitStatus())
	}
}
