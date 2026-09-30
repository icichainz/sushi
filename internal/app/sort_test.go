package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/icichainz/sushi/internal/config"
)

// names returns the names a tab lists, in order
func names(tab *Tab) []string {
	var out []string
	for _, f := range tab.Files {
		out = append(out, f.Name)
	}
	return out
}

// sizedFiles makes a.txt, b.txt and c.txt of 1, 5 and 3 bytes
func sizedFiles(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "1")
	writeTestFile(t, filepath.Join(dir, "b.txt"), "12345")
	writeTestFile(t, filepath.Join(dir, "c.txt"), "123")
	return dir
}

func TestSortMenuChangesTheOrderOfEveryTab(t *testing.T) {
	dir := sizedFiles(t)
	cfg := config.DefaultConfig()
	m := newTestModel(t, dir, cfg)
	updated, cmd := m.createTab(dir)
	m = drain(t, updated.(Model), cmd)
	m, _ = press(t, m, "tab") // Back to the first tab
	m, _ = press(t, m, "j")   // Cursor on b.txt
	m.width = 300             // Room for the order beside the long temp path

	m, _ = press(t, m, "s")
	view := strings.Join(plain(m.View()), "\n")
	if m.mode != ModeSort || !strings.Contains(view, "Sort by") || !strings.Contains(view, "largest first") || !strings.Contains(view, "SORT") {
		t.Fatalf("sort menu not shown:\n%s", view)
	}

	m, _ = press(t, m, "s") // Size, largest first
	if m.mode != ModeNormal {
		t.Fatalf("mode = %v, want the menu closed", m.mode)
	}
	for i := range m.tabs {
		if got := strings.Join(names(&m.tabs[i]), " "); got != "b.txt c.txt a.txt" {
			t.Fatalf("tab %d order = %s, want largest first", i+1, got)
		}
	}
	if cursorName(m) != "b.txt" {
		t.Fatalf("cursor on %s, want it to stay on b.txt", cursorName(m))
	}
	if header := ansi.Strip(m.renderHeader()); !strings.Contains(header, "sort size ↓") {
		t.Fatalf("breadcrumb = %q, want the new order", header)
	}
	if heading := ansi.Strip(m.renderFileList(80, 5, false)[0]); !strings.Contains(heading, "Size ↓") {
		t.Fatalf("heading = %q, want the arrow on Size", heading)
	}
	if cfg.SortBy != "name" {
		t.Fatalf("config changed to %s; the order is for this session only", cfg.SortBy)
	}

	// S reverses, keeping the cursor on its file
	m, _ = press(t, m, "S")
	if got := strings.Join(names(m.tab()), " "); got != "a.txt c.txt b.txt" || cursorName(m) != "b.txt" || !m.sortReverse {
		t.Fatalf("after S: %s with the cursor on %s", got, cursorName(m))
	}
	if header := ansi.Strip(m.renderHeader()); !strings.Contains(header, "sort size ↑") {
		t.Fatalf("breadcrumb = %q after reversing", header)
	}

	// Choosing another field starts from its own direction
	m, _ = press(t, m, "s")
	m, _ = press(t, m, "n")
	if got := strings.Join(names(m.tab()), " "); got != "a.txt b.txt c.txt" || m.sortReverse {
		t.Fatalf("by name: %s (reverse=%v)", got, m.sortReverse)
	}
}

func TestSortMenuKeys(t *testing.T) {
	m := newTestModel(t, sizedFiles(t), nil)

	// j and k move, Enter chooses
	m, _ = press(t, m, "s")
	for _, k := range []string{"j", "j", "j", "j", "k"} {
		m, _ = press(t, m, k)
	}
	m, _ = press(t, m, "enter")
	if m.sortBy != "modified" || m.mode != ModeNormal {
		t.Fatalf("sortBy = %s, want modified (j j j clamps at type, then k)", m.sortBy)
	}

	// The menu opens on the current order, and Esc changes nothing
	m, _ = press(t, m, "s")
	if sortFields[m.sortCursor].by != "modified" {
		t.Fatalf("menu opened on %s", sortFields[m.sortCursor].by)
	}
	m, _ = press(t, m, "j")
	if m, _ = press(t, m, "esc"); m.mode != ModeNormal || m.sortBy != "modified" {
		t.Fatalf("esc: mode=%v sortBy=%s", m.mode, m.sortBy)
	}

	// S in the menu reverses and closes it
	m, _ = press(t, m, "s")
	if m, _ = press(t, m, "S"); m.mode != ModeNormal || !m.sortReverse {
		t.Fatalf("S in the menu: mode=%v reverse=%v", m.mode, m.sortReverse)
	}
}

func TestSortAppliesToNewLoads(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	os.Mkdir(sub, 0755)
	writeTestFile(t, filepath.Join(sub, "small"), "1")
	writeTestFile(t, filepath.Join(sub, "big"), "12345")

	m := newTestModel(t, root, nil)
	m, _ = press(t, m, "s")
	m, _ = press(t, m, "s")
	m, cmd := press(t, m, "l")
	m = drain(t, m, cmd)
	if got := strings.Join(names(m.tab()), " "); got != "big small" {
		t.Fatalf("entered directory lists %s, want it sorted by size", got)
	}
}

func TestSortAppliesToLoadsInFlight(t *testing.T) {
	dir := sizedFiles(t)
	m := newTestModel(t, dir, nil)

	// A reload started before the change lists files in the old order; it
	// is sorted when it lands, and nothing is left ticking until then, so
	// a load that hangs costs nothing
	load := m.loadDir(m.tab(), dir)
	m, cmd := press(t, m, "S")
	if !m.tab().resortWanted {
		t.Fatal("the loading tab should be marked for sorting")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("changing the order left a command running, giving %T", msg)
	}
	m = run(t, m, load)
	if got := strings.Join(names(m.tab()), " "); got != "c.txt b.txt a.txt" {
		t.Fatalf("the load landed as %s, want it reversed", got)
	}
	if m.tab().resortWanted {
		t.Fatal("still marked for sorting after the load")
	}

	// A load started after the change lists files in the new order anyway
	load = m.loadDir(m.tab(), dir)
	if m.tab().resortWanted {
		t.Fatal("a new load was marked for sorting")
	}
	if m = run(t, m, load); strings.Join(names(m.tab()), " ") != "c.txt b.txt a.txt" {
		t.Fatalf("order = %v", names(m.tab()))
	}
}

func TestResortKeepsSearchResultsInStep(t *testing.T) {
	dir := sizedFiles(t)
	m := newTestModel(t, dir, nil)
	load := m.loadDir(m.tab(), dir)
	m, _ = press(t, m, "S")

	// A search starts before the old load lands in the old order
	m = typeQuery(t, m, "a")
	m = run(t, m, load)
	if names := names(m.tab()); names[0] != "c.txt" {
		t.Fatalf("order = %v, want it re-sorted", names)
	}
	if res := m.tab().SearchResults; len(res) != 1 || m.tab().Files[res[0]].Name != "a.txt" || cursorName(m) != "a.txt" {
		t.Fatalf("search results %v and cursor %s should follow a.txt when re-sorted", res, cursorName(m))
	}
}
