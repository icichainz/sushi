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
)

// historyDirs makes root/a/b and root/c
func historyDirs(t *testing.T) (root, a, b, c string) {
	t.Helper()
	root = t.TempDir()
	a, c = filepath.Join(root, "a"), filepath.Join(root, "c")
	b = filepath.Join(a, "b")
	os.MkdirAll(b, 0755)
	os.MkdirAll(c, 0755)
	return root, a, b, c
}

// step presses [ or ] and waits for the folder to load
func step(t *testing.T, m Model, k string) Model {
	t.Helper()
	m, cmd := press(t, m, k)
	return drain(t, m, cmd)
}

func TestHistoryBackAndForward(t *testing.T) {
	root, a, b, c := historyDirs(t)
	m := newTestModel(t, root, noWatch())
	m = goHere(t, m, a)
	m = goHere(t, m, b)
	m, cmd := press(t, m, "h") // Keys count as much as any other way
	m = drain(t, m, cmd)

	// Back through b, a and root, in the order they were left
	for _, want := range []string{b, a, root} {
		if m = step(t, m, "["); m.tab().CurrentPath != want {
			t.Fatalf("[ went to %s, want %s", m.tab().CurrentPath, want)
		}
		if !strings.HasPrefix(m.statusMsg, "Back to ") || !strings.HasSuffix(m.statusMsg, filepath.Base(want)) {
			t.Errorf("status %q", m.statusMsg)
		}
	}
	if m = step(t, m, "["); m.tab().CurrentPath != root || m.statusMsg != "Nothing to go back to" {
		t.Fatalf("at the start: in %s, status %q", m.tab().CurrentPath, m.statusMsg)
	}
	// And forward again
	for _, want := range []string{a, b} {
		if m = step(t, m, "]"); m.tab().CurrentPath != want || !strings.HasPrefix(m.statusMsg, "Forward to ") {
			t.Fatalf("] went to %s, want %s (status %q)", m.tab().CurrentPath, want, m.statusMsg)
		}
	}

	// Going somewhere new drops what was ahead
	m = goHere(t, m, c)
	if m = step(t, m, "]"); m.tab().CurrentPath != c || m.statusMsg != "Nothing to go forward to" {
		t.Fatalf("after going elsewhere, ] went to %s", m.tab().CurrentPath)
	}
	if m = step(t, m, "["); m.tab().CurrentPath != b {
		t.Fatalf("back from c went to %s, want b", m.tab().CurrentPath)
	}

	// Folders that are gone are passed over
	os.RemoveAll(c)
	if m = step(t, m, "]"); m.tab().CurrentPath != b || m.statusMsg != "Nothing to go forward to" {
		t.Fatalf("] to a folder that is gone: in %s, status %q", m.tab().CurrentPath, m.statusMsg)
	}
	if m = step(t, m, "["); m.tab().CurrentPath != a {
		t.Fatalf("[ went to %s, want a", m.tab().CurrentPath)
	}

	// Pressed again before the folder loads, [ goes on from where the
	// first was going: root, a, root, b, and back twice is a
	m = goHere(t, m, root)
	m = goHere(t, m, b)
	m, first := press(t, m, "[")
	m, second := press(t, m, "[")
	m = drain(t, drain(t, m, first), second)
	if m.tab().CurrentPath != a {
		t.Fatalf("[[ went to %s, want a", m.tab().CurrentPath)
	}
}

func TestHistoryIsEachPanes(t *testing.T) {
	root, a, _, c := historyDirs(t)
	m := newTestModel(t, root, noWatch())
	m = goHere(t, m, a)
	m, cmd := press(t, m, "w")
	m = drain(t, m, cmd)
	m, _ = ctrl(t, m, tea.KeyCtrlL)
	if m = step(t, m, "["); m.tab().CurrentPath != a || m.statusMsg != "Nothing to go back to" {
		t.Fatalf("the new pane went back to %s", m.tab().CurrentPath)
	}
	m = goHere(t, m, c)
	m, _ = ctrl(t, m, tea.KeyCtrlH)
	if m = step(t, m, "["); m.tab().CurrentPath != root || m.otherPane().CurrentPath != c {
		t.Fatalf("the left pane went back to %s; the right is in %s", m.tab().CurrentPath, m.otherPane().CurrentPath)
	}

	// A bookmark counts as a step
	m.bookmarks.Add("c", c)
	m, cmd = press(t, m, "1")
	m = drain(t, m, cmd)
	if m = step(t, m, "["); m.tab().CurrentPath != root {
		t.Fatalf("back from the bookmark went to %s", m.tab().CurrentPath)
	}
}

func TestHistoryIsBounded(t *testing.T) {
	var list []string
	for i := range historyLimit + 50 {
		list = pushDir(list, fmt.Sprintf("/d%d", i))
	}
	list = pushDir(list, list[len(list)-1]) // Not twice in a row
	if len(list) != historyLimit || list[0] != "/d50" || list[len(list)-1] != fmt.Sprintf("/d%d", historyLimit+49) {
		t.Fatalf("%d kept, from %s to %s", len(list), list[0], list[len(list)-1])
	}
}

// historyHome points HOME at a new folder holding history.json with
// visits, as sushi would have saved them
func historyHome(t *testing.T, visits ...config.DirVisit) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	if len(visits) > 0 {
		if err := config.SaveDirHistory(visits, nil, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
}

// startAt starts sushi in dir with HOME as it is, unlike newTestModel
func startAt(t *testing.T, dir string, cfg *config.Config) Model {
	t.Helper()
	return resize(NewModelWithConfig(dir, cfg), tea.WindowSizeMsg{Width: 100, Height: 24})
}

// visitsOf returns the count history.json has for each folder
func visitsOf() map[string]float64 {
	counts := make(map[string]float64)
	for _, v := range config.LoadDirHistory() {
		counts[v.Path] = v.Count
	}
	return counts
}

func TestFoldersVisitedAreSaved(t *testing.T) {
	root, a, b, _ := historyDirs(t)
	historyHome(t)
	m := startAt(t, root, noWatch())
	m = goHere(t, m, a)
	m = goHere(t, m, b)
	m = goHere(t, m, a)
	m = goHere(t, m, a) // A reload isn't a visit

	// Saved in the background as visits come; the start counts
	got := visitsOf()
	if got[root] != 1 || got[a] != 2 || got[b] != 1 || len(got) != 3 {
		t.Fatalf("history.json has %v", got)
	}

	// What hasn't been saved is saved on quitting
	m.frecent.record(root)
	m.Close()
	if got := visitsOf(); got[root] != 2 {
		t.Fatalf("after Close: %v", got)
	}

	// A save that fails says so, once
	fail := historySavedMsg{err: fmt.Errorf("disk full")}
	updated, _ := m.Update(fail)
	m = updated.(Model)
	if !strings.Contains(m.statusMsg, "Can't save the folder history: disk full") {
		t.Fatalf("status %q", m.statusMsg)
	}
	m.statusMsg = ""
	if updated, _ = m.Update(fail); updated.(Model).statusMsg != "" {
		t.Fatal("a second failure was reported too")
	}
}

func TestHistoryOff(t *testing.T) {
	root, a, _, c := historyDirs(t)
	elsewhere := t.TempDir()
	historyHome(t, config.DirVisit{Path: elsewhere, Count: 5, Last: time.Now().Unix()})
	before, _ := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".config", "sushi", "history.json"))

	cfg := noWatch()
	cfg.History = false
	m := startAt(t, root, cfg)
	m = goHere(t, m, a)
	m = goHere(t, m, c)
	m, cmd := press(t, m, "z")
	m = drain(t, m, cmd)
	var listed []string
	for _, r := range m.jumper.results {
		listed = append(listed, r.path)
	}
	// This session's folders but the one shown, and nothing from the file
	if len(listed) != 2 || listed[0] == elsewhere || listed[1] == elsewhere {
		t.Fatalf("z lists %v", listed)
	}
	if !strings.Contains(strings.Join(plain(m.View()), "\n"), "this session") {
		t.Error("the palette doesn't say it holds this session's folders only")
	}
	m.Close()
	after, _ := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".config", "sushi", "history.json"))
	if string(after) != string(before) {
		t.Fatal("history: false wrote history.json")
	}
}

func TestFrequentFoldersPalette(t *testing.T) {
	root := t.TempDir()
	sushi, other, music := filepath.Join(root, "projects", "sushi"), filepath.Join(root, "projects", "other"), filepath.Join(root, "music")
	gone := filepath.Join(root, "gone")
	for _, d := range []string{sushi, other, music} {
		os.MkdirAll(d, 0755)
	}
	now := time.Now().Unix()
	historyHome(t,
		config.DirVisit{Path: sushi, Count: 10, Last: now},
		config.DirVisit{Path: other, Count: 1, Last: now - 3600*24*30},
		config.DirVisit{Path: music, Count: 5, Last: now - 3600*3},
		config.DirVisit{Path: gone, Count: 50, Last: now})
	m := startAt(t, root, noWatch())

	// The most frecent first, but not the folder shown; gone folders are
	// dropped once checked
	m, cmd := press(t, m, "z")
	if m.mode != ModeJump || len(m.jumper.results) != 4 || m.jumper.results[0].path != gone {
		t.Fatalf("before the check: mode %v, %d folders", m.mode, len(m.jumper.results))
	}
	m = drain(t, m, cmd)
	var listed []string
	for _, r := range m.jumper.results {
		listed = append(listed, filepath.Base(r.path))
	}
	if strings.Join(listed, " ") != "sushi music other" {
		t.Fatalf("listed %v", listed)
	}
	if _, kept := visitsOf()[gone]; kept {
		t.Error("the gone folder is still in history.json")
	}
	assertFills(t, "palette", m)

	// Typing filters, the folder's own name first, then the path, then
	// letters in order
	m = typeText(t, m, "o")
	if got := m.jumper.results; len(got) != 3 || got[0].path != other {
		t.Fatalf("'o': %v", got)
	}
	m = typeText(t, m, "th")
	if got := m.jumper.results; got[0].path != other || got[0].rank != 0 || !got[0].hits[len([]rune(got[0].shown))-5] {
		t.Fatalf("'oth': %+v", got)
	}
	m, _ = press(t, m, "backspace")
	m, _ = press(t, m, "backspace")
	m, _ = press(t, m, "backspace")
	m = typeText(t, m, "projects")
	// In the paths of both, sushi the more frecent; any other match is by
	// letters scattered through the temporary folder's path
	if got := m.jumper.results; len(got) < 2 || got[0].path != sushi || got[1].path != other || got[1].rank != 2 || len(got) > 2 && got[2].rank != 3 {
		t.Fatalf("'projects': %+v", got)
	}

	// Enter takes the active pane there, as a visit
	m, _ = ctrl(t, m, tea.KeyDown)
	chosen := m.jumper.results[1].path
	m, cmd = press(t, m, "enter")
	if m = drain(t, m, cmd); m.mode != ModeNormal || m.tab().CurrentPath != chosen {
		t.Fatalf("enter: mode %v, in %s, want %s", m.mode, m.tab().CurrentPath, chosen)
	}
	if visitsOf()[chosen] < 2 {
		t.Errorf("the jump wasn't counted: %v", visitsOf())
	}
	if m = step(t, m, "["); m.tab().CurrentPath != root {
		t.Errorf("back from the jump went to %s", m.tab().CurrentPath)
	}

	// Esc closes it
	m, _ = press(t, m, "z")
	if m, _ = press(t, m, "esc"); m.mode != ModeNormal {
		t.Fatal("esc didn't close the palette")
	}

	// A folder gone by the time it is chosen is dropped, and the palette
	// stays
	m, _ = press(t, m, "z") // Its check isn't run
	os.RemoveAll(music)
	for range m.jumper.results {
		if m.jumper.results[m.jumper.cursor].path == music {
			break
		}
		m, _ = ctrl(t, m, tea.KeyDown)
	}
	m, cmd = press(t, m, "enter")
	if m = drain(t, m, cmd); m.mode != ModeJump || m.tab().CurrentPath != root || !strings.HasSuffix(m.statusMsg, "music is gone") {
		t.Fatalf("enter on a gone folder: mode %v in %s, status %q", m.mode, m.tab().CurrentPath, m.statusMsg)
	}
	for _, r := range m.jumper.results {
		if r.path == music {
			t.Fatal("the gone folder is still listed")
		}
	}
}

func TestFrequentFoldersMouse(t *testing.T) {
	root := t.TempDir()
	one, two := filepath.Join(root, "one"), filepath.Join(root, "two")
	os.MkdirAll(one, 0755)
	os.MkdirAll(two, 0755)
	now := time.Now().Unix()
	historyHome(t, config.DirVisit{Path: one, Count: 3, Last: now}, config.DirVisit{Path: two, Count: 1, Last: now})
	fakeClock(t)
	m := startAt(t, root, noWatch())
	m, cmd := press(t, m, "z")
	m = drain(t, m, cmd)

	// A click picks, a second goes there
	x, y := findOnScreen(t, m, "two", 0, m.width)
	m, _ = clickAt(m, x, y)
	if m.jumper.results[m.jumper.cursor].path != two {
		t.Fatalf("the click picked %d", m.jumper.cursor)
	}
	m, cmd = clickAt(m, x, y)
	if m = drain(t, m, cmd); m.mode != ModeNormal || m.tab().CurrentPath != two {
		t.Fatalf("double-click: mode %v in %s", m.mode, m.tab().CurrentPath)
	}

	// The wheel moves, and a click outside closes it
	m, cmd = press(t, m, "z")
	m = drain(t, m, cmd)
	if m, _ = mouseAt(m, x, y, tea.MouseButtonWheelDown, false); m.jumper.cursor != 1 {
		t.Fatalf("wheel: cursor %d", m.jumper.cursor)
	}
	if m, _ = clickAt(m, 0, m.height-1); m.mode != ModeNormal {
		t.Fatal("a click outside didn't close the palette")
	}

	// Every size
	for _, size := range sizes {
		screen, _ := press(t, resize(detach(m), size), "z")
		assertFills(t, fmt.Sprintf("palette %dx%d", size.Width, size.Height), screen)
	}
}

func TestJumpMatch(t *testing.T) {
	for _, c := range []struct {
		query, shown string
		rank         int
		ok           bool
	}{
		{"", "~/src/sushi", 0, true},
		{"su", "~/src/sushi", 0, true},
		{"sh", "~/src/sushi", 1, true},
		{"src/s", "~/src/sushi", 2, true},
		{"srsh", "~/src/sushi", 3, true},
		{"x", "~/src/sushi", 0, false},
	} {
		rank, _, ok := jumpMatch(c.query, c.shown)
		if rank != c.rank || ok != c.ok {
			t.Errorf("jumpMatch(%q, %q) = %d %v, want %d %v", c.query, c.shown, rank, ok, c.rank, c.ok)
		}
	}
	if _, hits, _ := jumpMatch("sh", "~/src/sushi"); len(hits) != 2 || !hits[8] || !hits[9] {
		t.Errorf("hits %v, want the s and h of sushi", hits)
	}
}
