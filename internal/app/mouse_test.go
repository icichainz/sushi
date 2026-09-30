package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/config"
	"github.com/icichainz/sushi/internal/plugins"
	"github.com/icichainz/sushi/internal/utils"
)

// sizes are the terminal sizes the mouse is tried at, from all three
// panes down to the file list alone
var sizes = []tea.WindowSizeMsg{{Width: 140, Height: 40}, {Width: 100, Height: 24}, {Width: 80, Height: 24}, {Width: 60, Height: 15}, {Width: 30, Height: 10}}

// resize gives the model a new terminal size
func resize(m Model, size tea.WindowSizeMsg) Model {
	updated, _ := m.Update(size)
	return updated.(Model)
}

// mouseAt sends a press of button at column x, row y
func mouseAt(m Model, x, y int, button tea.MouseButton, ctrl bool) (Model, tea.Cmd) {
	updated, cmd := m.Update(tea.MouseMsg{X: x, Y: y, Button: button, Action: tea.MouseActionPress, Ctrl: ctrl})
	return updated.(Model), cmd
}

// clickAt left-clicks at column x, row y
func clickAt(m Model, x, y int) (Model, tea.Cmd) {
	return mouseAt(m, x, y, tea.MouseButtonLeft, false)
}

// fakeClock stops the click clock at a time the test moves on by hand
func fakeClock(t *testing.T) *time.Time {
	t.Helper()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	old := clock
	clock = func() time.Time { return now }
	t.Cleanup(func() { clock = old })
	return &now
}

// findOnScreen returns the cell where text is drawn, looking between
// columns from and to of the rendered screen. Clicks aimed with it test
// the hit-testing against what was actually drawn.
func findOnScreen(t *testing.T, m Model, text string, from, to int) (x, y int) {
	t.Helper()
	for y, line := range plain(m.View()) {
		part := utils.Cells(line, from, to)
		if i := strings.Index(part, text); i >= 0 {
			return from + utils.Width(part[:i]), y
		}
	}
	t.Fatalf("%q is not on screen between columns %d and %d:\n%s", text, from, to, strings.Join(plain(m.View()), "\n"))
	return 0, 0
}

// onScreen reports whether text is drawn between columns from and to
func onScreen(m Model, text string, from, to int) bool {
	for _, line := range plain(m.View()) {
		if strings.Contains(utils.Cells(line, from, to), text) {
			return true
		}
	}
	return false
}

// numberedFiles creates n files named file-00.txt and up
func numberedFiles(t *testing.T, dir string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		writeTestFile(t, filepath.Join(dir, fmt.Sprintf("file-%02d.txt", i)), "x")
	}
}

func TestClickPutsCursorOnTheRowClicked(t *testing.T) {
	dir := t.TempDir()
	numberedFiles(t, dir, 60)

	for _, size := range sizes {
		m := resize(newTestModel(t, dir, nil), size)
		label := fmt.Sprintf("%dx%d", size.Width, size.Height)
		l := m.layout()

		// From the top, the middle and the end, so the list is scrolled
		// differently each time
		for _, from := range []int{0, 30, 59} {
			m.tab().Cursor = from
			visible := 0
			for i := 0; i < 60; i++ {
				name := fmt.Sprintf("file-%02d.txt", i)
				if !onScreen(m, name, l.parentW, l.parentW+l.listW) {
					continue
				}
				visible++
				x, y := findOnScreen(t, m, name, l.parentW, l.parentW+l.listW)
				clicked, _ := clickAt(m, x, y)
				if got := clicked.tab().Files[clicked.tab().Cursor].Name; got != name {
					t.Fatalf("%s from %d: clicked %s at (%d,%d), cursor went to %s", label, from, name, x, y, got)
				}
				m.tab().Cursor = from // The click moved the shared tab
			}
			if visible != min(60, l.bodyH-1) {
				t.Fatalf("%s: %d files on screen, want %d", label, visible, min(60, l.bodyH-1))
			}
		}
	}
}

func TestDoubleClickOpensWhatTheFirstClickPicked(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 50; i++ {
		os.Mkdir(filepath.Join(root, fmt.Sprintf("dir-%02d", i)), 0755)
	}
	now := fakeClock(t)

	m := resize(newTestModel(t, root, nil), tea.WindowSizeMsg{Width: 80, Height: 24})
	listW := m.layout().listW
	x, y := findOnScreen(t, m, "dir-15", 0, listW)
	m, _ = clickAt(m, x, y)
	if name := m.tab().Files[m.tab().Cursor].Name; name != "dir-15" || onScreen(m, "dir-00", 0, listW) {
		t.Fatalf("cursor on %s; the list should have scrolled to center dir-15", name)
	}

	// Too slow for a double-click: it picks what is under the pointer now
	*now = now.Add(500 * time.Millisecond)
	slow, cmd := clickAt(m, x, y)
	if slow.tab().Loading || slow.tab().Files[slow.tab().Cursor].Name == "dir-15" {
		t.Fatalf("a slow second click opened %s (loading=%v)", slow.tab().Files[slow.tab().Cursor].Name, slow.tab().Loading)
	}
	m.tab().Cursor = 15 // Undo the slow click on the shared tab
	m.lastClick.at = *now

	// Quick enough: the directory the first click picked opens, although
	// the list has moved another one under the pointer
	*now = now.Add(300 * time.Millisecond)
	m, cmd = clickAt(m, x, y)
	m = drain(t, m, cmd)
	if got := filepath.Base(m.tab().CurrentPath); got != "dir-15" {
		t.Fatalf("double-click opened %s, want dir-15", got)
	}

	// A third click doesn't open again
	if m, _ = clickAt(m, x, y); m.tab().Loading {
		t.Fatal("a third click counted as another double-click")
	}
}

func TestDoubleClickOpensFiles(t *testing.T) {
	record := fakeOpener(t)
	dir := t.TempDir()
	photo := filepath.Join(dir, "photo.png")
	writeTestFile(t, photo, "\x00binary")
	writeTestFile(t, filepath.Join(dir, "zz.txt"), "")
	fakeClock(t)

	m := newTestModel(t, dir, nil)
	x, y := findOnScreen(t, m, "photo.png", 0, 50)
	m, _ = clickAt(m, x, y)
	m, cmd := clickAt(m, x, y)
	drain(t, m, cmd)
	if b, err := os.ReadFile(record); err != nil || string(b) != photo {
		t.Fatalf("opener got %q (%v), want %s", b, err, photo)
	}
}

func TestClickInParentPane(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"alpha-dir", "beta-dir"} {
		os.Mkdir(filepath.Join(root, d), 0755)
	}
	writeTestFile(t, filepath.Join(root, "beta-dir", "inside.txt"), "")
	writeTestFile(t, filepath.Join(root, "gamma.txt"), "")

	m := resize(newTestModel(t, filepath.Join(root, "beta-dir"), nil), tea.WindowSizeMsg{Width: 120, Height: 24})
	l := m.layout()

	// A directory is entered
	x, y := findOnScreen(t, m, "alpha-dir", 0, l.parentW)
	m, cmd := clickAt(m, x, y)
	m = drain(t, m, cmd)
	if got := filepath.Base(m.tab().CurrentPath); got != "alpha-dir" {
		t.Fatalf("clicked alpha-dir, now in %s", got)
	}

	// A file is shown in its directory
	x, y = findOnScreen(t, m, "gamma.txt", 0, l.parentW)
	m, cmd = clickAt(m, x, y)
	m = drain(t, m, cmd)
	if m.tab().CurrentPath != root || m.tab().Files[m.tab().Cursor].Name != "gamma.txt" {
		t.Fatalf("clicked gamma.txt: in %s on %s", m.tab().CurrentPath, m.tab().Files[m.tab().Cursor].Name)
	}

	// The heading goes up
	m.tab().Cursor = 0 // alpha-dir
	m, cmd = press(t, m, "l")
	m = drain(t, m, cmd)
	m, cmd = clickAt(m, 1, paneTop)
	m = drain(t, m, cmd)
	if m.tab().CurrentPath != root {
		t.Fatalf("clicking the parent heading went to %s", m.tab().CurrentPath)
	}
}

func TestClickSwitchesTabs(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"one", "two", "three"} {
		os.Mkdir(filepath.Join(root, d), 0755)
	}
	m := newTestModel(t, filepath.Join(root, "one"), nil)
	for _, d := range []string{"two", "three"} {
		updated, _ := m.createTab(filepath.Join(root, d))
		m = updated.(Model)
	}

	x, y := findOnScreen(t, m, " 2 two ", 0, m.width)
	m, _ = clickAt(m, x+1, y)
	if m.activeTabIdx != 1 {
		t.Fatalf("clicked tab 2, active is %d", m.activeTabIdx+1)
	}
	// Past the last tab is empty bar
	if m, _ = clickAt(m, m.width-1, tabBarRow); m.activeTabIdx != 1 {
		t.Fatalf("clicking the empty bar switched to tab %d", m.activeTabIdx+1)
	}

	// With too many tabs to show, the labels drawn are the ones clicked
	for i := 0; i < 10; i++ {
		updated, _ := m.createTab(filepath.Join(root, "three"))
		m = updated.(Model)
	}
	for _, size := range sizes {
		m = resize(m, size)
		m.activeTabIdx = 12
		for _, span := range m.tabSpans() {
			label := strings.TrimSpace(span.label)
			x, y := findOnScreen(t, m, label, span.x, span.x+span.width)
			clicked, _ := clickAt(m, x, y)
			if clicked.activeTabIdx != span.index {
				t.Fatalf("%dx%d: clicked %q, active is tab %d", size.Width, size.Height, label, clicked.activeTabIdx+1)
			}
		}
	}
}

func TestWheelMovesListAndScrollsPreview(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "files")
	os.Mkdir(dir, 0755)
	var b strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	writeTestFile(t, filepath.Join(dir, "a-long.txt"), b.String())
	numberedFiles(t, dir, 20)

	m := resize(newTestModel(t, dir, nil), tea.WindowSizeMsg{Width: 120, Height: 24})
	l := m.layout()
	wheel := func(m Model, x int, button tea.MouseButton) Model {
		m, _ = mouseAt(m, x, paneTop+3, button, false)
		return m
	}
	list, preview, parent := l.parentW+2, l.parentW+l.listW+2, 1

	// Over the preview, the preview scrolls, as far as it goes
	m = wheel(m, preview, tea.MouseButtonWheelDown)
	if m.tab().PreviewScroll != wheelStep || m.tab().Cursor != 0 {
		t.Fatalf("scroll=%d cursor=%d after the wheel over the preview", m.tab().PreviewScroll, m.tab().Cursor)
	}
	for i := 0; i < 50; i++ {
		m = wheel(m, preview, tea.MouseButtonWheelDown)
	}
	if want := m.tab().Preview.MaxScroll(m.previewRows()); m.tab().PreviewScroll != want {
		t.Fatalf("scroll = %d, want it stopped at %d", m.tab().PreviewScroll, want)
	}
	m = wheel(m, preview, tea.MouseButtonWheelUp)
	if want := m.tab().Preview.MaxScroll(m.previewRows()) - wheelStep; m.tab().PreviewScroll != want {
		t.Fatalf("scroll = %d after the wheel up, want %d", m.tab().PreviewScroll, want)
	}

	// Over the list, the cursor moves, stopping at the ends
	m = wheel(m, list, tea.MouseButtonWheelDown)
	if m.tab().Cursor != wheelStep {
		t.Fatalf("cursor = %d, want %d", m.tab().Cursor, wheelStep)
	}
	m = wheel(m, list, tea.MouseButtonWheelUp)
	m = wheel(m, list, tea.MouseButtonWheelUp)
	if m.tab().Cursor != 0 {
		t.Fatalf("cursor = %d, want it stopped at 0", m.tab().Cursor)
	}

	// Over the parent pane, nothing happens
	before := *m.tab()
	m = wheel(m, parent, tea.MouseButtonWheelDown)
	if m.tab().Cursor != before.Cursor || m.tab().PreviewScroll != before.PreviewScroll || m.tab().Loading {
		t.Fatal("the wheel over the parent pane changed something")
	}
}

func TestRightAndCtrlClickSelect(t *testing.T) {
	dir := t.TempDir()
	numberedFiles(t, dir, 5)
	m := newTestModel(t, dir, nil)
	path := func(i int) string { return m.tab().Files[i].Path }
	// Only the list, as the preview names the selection
	l := m.layout()
	from, to := l.parentW, l.parentW+l.listW

	x, y := findOnScreen(t, m, "file-03.txt", from, to)
	m, _ = mouseAt(m, x, y, tea.MouseButtonRight, false)
	if !m.tab().Selected[path(3)] || len(m.tab().Selected) != 1 || m.tab().Cursor != 3 {
		t.Fatalf("right-click: selected=%v cursor=%d", m.tab().Selected, m.tab().Cursor)
	}
	m, _ = mouseAt(m, x, y, tea.MouseButtonRight, false)
	if len(m.tab().Selected) != 0 {
		t.Fatalf("a second right-click should deselect: %v", m.tab().Selected)
	}

	x, y = findOnScreen(t, m, "file-01.txt", from, to)
	m, _ = mouseAt(m, x, y, tea.MouseButtonLeft, true)
	if !m.tab().Selected[path(1)] {
		t.Fatalf("ctrl-click: selected=%v", m.tab().Selected)
	}
}

func TestClicksAwayFromRowsDoNothing(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "here")
	os.Mkdir(dir, 0755)
	numberedFiles(t, dir, 3)

	for _, size := range sizes {
		m := resize(newTestModel(t, dir, nil), size)
		l := m.layout()
		m.tab().Cursor = 1
		spots := [][2]int{
			{l.parentW + 2, paneTop},     // The list heading
			{l.parentW + 2, paneTop + 4}, // Below the last file
			{2, breadcrumbRow},
			{2, m.height - 2}, // Status bar
			{2, m.height - 1}, // Hints
			{-1, 3}, {m.width, 3}, {3, -1}, {3, m.height + 5},
		}
		if l.previewW > 0 {
			spots = append(spots, [2]int{m.width - 2, paneTop + 1})
		}
		for _, s := range spots {
			for _, button := range []tea.MouseButton{tea.MouseButtonLeft, tea.MouseButtonRight, tea.MouseButtonMiddle, tea.MouseButtonWheelLeft} {
				after, cmd := mouseAt(m, s[0], s[1], button, false)
				if after.tab().Cursor != 1 || after.mode != ModeNormal || after.tab().Loading || len(after.tab().Selected) != 0 || cmd != nil {
					t.Fatalf("%dx%d: button %v at %v changed something", size.Width, size.Height, button, s)
				}
			}
		}

		// Releases and drags never act
		for _, action := range []tea.MouseAction{tea.MouseActionRelease, tea.MouseActionMotion} {
			x, y := findOnScreen(t, m, "file-02.txt", l.parentW, l.parentW+l.listW)
			updated, _ := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: action})
			if after := updated.(Model); after.tab().Cursor != 1 {
				t.Fatalf("%dx%d: action %v moved the cursor", size.Width, size.Height, action)
			}
		}
	}
}

func TestMouseInDialogs(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"bm-one", "bm-two", "bm-three"} {
		os.Mkdir(filepath.Join(root, d), 0755)
	}
	writeTestFile(t, filepath.Join(root, "doomed.txt"), "")
	t.Setenv("TMPDIR", t.TempDir()) // Plugins leave a command file there
	fakeClock(t)
	cfg := config.DefaultConfig()
	cfg.Plugins = []plugins.Plugin{
		{Name: "first-plugin", Command: "true", Mode: plugins.ModeBackground},
		{Name: "second-plugin", Command: "true", Mode: plugins.ModeBackground},
	}

	for _, size := range sizes {
		label := fmt.Sprintf("%dx%d", size.Width, size.Height)
		m := resize(newTestModel(t, root, cfg), size)
		for _, d := range []string{"bm-one", "bm-two", "bm-three"} {
			m.bookmarks.Add(d, filepath.Join(root, d))
		}

		// Bookmarks: click picks, double-click goes, clicking outside
		// closes. The list behind the dialog names them too, without
		// their number.
		m, _ = press(t, m, "b")
		x, y := findOnScreen(t, m, "2 bm-two", 0, m.width)
		m, _ = clickAt(m, x, y)
		if m.mode != ModeBookmarks || m.bookmarkCursor != 1 {
			t.Fatalf("%s: mode=%v cursor=%d after clicking bm-two", label, m.mode, m.bookmarkCursor)
		}
		m, _ = mouseAt(m, x, y, tea.MouseButtonWheelDown, false)
		if m.bookmarkCursor != 2 {
			t.Fatalf("%s: the wheel left the bookmark cursor at %d", label, m.bookmarkCursor)
		}
		m, _ = clickAt(m, x, y)
		m, cmd := clickAt(m, x, y)
		m = drain(t, m, cmd)
		if m.mode != ModeNormal || filepath.Base(m.tab().CurrentPath) != "bm-two" {
			t.Fatalf("%s: double-click: mode=%v dir=%s", label, m.mode, m.tab().CurrentPath)
		}
		m, _ = press(t, m, "b")
		if m, _ = clickAt(m, 0, m.height-2); m.mode != ModeNormal {
			t.Fatalf("%s: clicking outside the bookmarks left them open", label)
		}

		// The Run palette: the command line, the plugins
		m, _ = press(t, m, "P")
		x, y = findOnScreen(t, m, "second-plugin", 0, m.width)
		m, _ = clickAt(m, x, y)
		if m.mode != ModePlugins || m.pluginCursor != 1 || m.runTyping {
			t.Fatalf("%s: mode=%v plugin=%d typing=%v", label, m.mode, m.pluginCursor, m.runTyping)
		}
		if size.Width >= 60 {
			cx, cy := findOnScreen(t, m, "press tab", 0, m.width)
			if typing, _ := clickAt(m, cx, cy); !typing.runTyping {
				t.Fatalf("%s: clicking the command line didn't start typing", label)
			}
		}
		m, cmd = clickAt(m, x, y)
		if m.mode != ModeNormal || cmd == nil {
			t.Fatalf("%s: double-clicking a plugin: mode=%v cmd=%v", label, m.mode, cmd != nil)
		}
		m, _ = press(t, m, "P")
		if m, _ = clickAt(m, 0, m.height-2); m.mode != ModeNormal {
			t.Fatalf("%s: clicking outside the palette left it open", label)
		}
	}
}

func TestPromptsIgnoreTheMouse(t *testing.T) {
	dir := t.TempDir()
	numberedFiles(t, dir, 5)
	fakeClock(t)

	for _, size := range sizes {
		m := resize(newTestModel(t, dir, nil), size)
		for _, keys := range []string{"d", "r", "n"} {
			screen, _ := press(t, m, keys)
			mode := screen.mode
			for y := 0; y < m.height; y++ {
				for _, x := range []int{0, m.width / 2, m.width - 1} {
					for _, button := range []tea.MouseButton{tea.MouseButtonLeft, tea.MouseButtonRight, tea.MouseButtonWheelDown} {
						after, cmd := mouseAt(screen, x, y, button, false)
						if after.mode != mode || after.tab().Cursor != 0 || len(after.tab().Selected) != 0 || cmd != nil {
							t.Fatalf("%dx%d after %q: button %v at (%d,%d) acted", size.Width, size.Height, keys, button, x, y)
						}
					}
				}
			}
		}
		if _, err := os.Stat(filepath.Join(dir, "file-00.txt")); err != nil {
			t.Fatal("a click went through to the delete confirmation")
		}
	}
}

func TestMouseOverTheKeyPanel(t *testing.T) {
	dir := t.TempDir()
	numberedFiles(t, dir, 3)
	m := resize(newTestModel(t, dir, nil), tea.WindowSizeMsg{Width: 30, Height: 10})
	m, _ = press(t, m, "?")
	if m.maxHelpScroll() == 0 {
		t.Fatal("the panel should need scrolling at 30x10")
	}
	m, _ = mouseAt(m, 5, 5, tea.MouseButtonWheelDown, false)
	if m.helpScroll != 1 || m.mode != ModeHelp {
		t.Fatalf("wheel: scroll=%d mode=%v", m.helpScroll, m.mode)
	}
	if m, _ = clickAt(m, 5, 5); m.mode != ModeNormal || m.tab().Cursor != 0 {
		t.Fatalf("click: mode=%v cursor=%d, want the panel closed and nothing else", m.mode, m.tab().Cursor)
	}
}

func TestClickWhileSearching(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"apple.txt", "banana.txt", "cherry.txt", "grape.txt"} {
		writeTestFile(t, filepath.Join(dir, name), "")
	}
	fakeClock(t)

	for _, size := range sizes {
		m := resize(newTestModel(t, dir, nil), size)
		updated, _ := m.createTab(dir)
		m = updated.(Model)
		m.activeTabIdx = 0
		m = typeQuery(t, m, "ape")

		// Only apple and grape are listed, so grape is on the second row
		x, y := findOnScreen(t, m, "grape.txt", 0, m.width)
		m, _ = clickAt(m, x, y)
		if m.mode != ModeSearch || m.tab().Files[m.tab().Cursor].Name != "grape.txt" || m.tab().SearchResultIdx != 1 {
			t.Fatalf("%dx%d: mode=%v cursor on %s", size.Width, size.Height, m.mode, m.tab().Files[m.tab().Cursor].Name)
		}
		// Tabs stay put while searching, as they do for the keyboard
		tx, ty := findOnScreen(t, m, " 2 ", 0, m.width)
		if after, _ := clickAt(m, tx+1, ty); after.activeTabIdx != 0 {
			t.Fatalf("%dx%d: a tab click switched tabs during a search", size.Width, size.Height)
		}
		// A double-click keeps the match, ends the search and opens it
		m, cmd := clickAt(m, x, y)
		if m.mode != ModeNormal || m.tab().SearchQuery != "" || m.tab().Files[m.tab().Cursor].Name != "grape.txt" || cmd == nil {
			t.Fatalf("%dx%d: double-click: mode=%v query=%q cmd=%v", size.Width, size.Height, m.mode, m.tab().SearchQuery, cmd != nil)
		}
	}
}

func TestMouseCanBeTurnedOff(t *testing.T) {
	dir := t.TempDir()
	numberedFiles(t, dir, 5)
	cfg := config.DefaultConfig()
	cfg.Mouse = false

	m := newTestModel(t, dir, cfg)
	x, y := findOnScreen(t, m, "file-03.txt", 0, m.width)
	if m, _ = clickAt(m, x, y); m.tab().Cursor != 0 {
		t.Fatal("a click moved the cursor with mouse: false")
	}
}
