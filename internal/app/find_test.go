package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/config"
	"github.com/icichainz/sushi/internal/search"
)

// makeTree creates files below a temp dir, making directories as needed
func makeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, path, content)
	}
	return root
}

// find opens the search palette with key, types query, and applies
// everything that follows until the search has finished
func find(t *testing.T, m Model, key, query string) Model {
	t.Helper()
	m, _ = press(t, m, key)
	var cmd tea.Cmd
	for _, r := range query {
		m, cmd = press(t, m, string(r))
	}
	// Only the pause after the last letter starts a search
	return drain(t, m, cmd)
}

// pressKey sends a key that press doesn't name, such as an arrow
func pressKey(t *testing.T, m Model, k tea.KeyType) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(tea.KeyMsg{Type: k})
	return updated.(Model), cmd
}

// cursorName returns the name of the file under the active tab's cursor
func cursorName(m Model) string {
	return m.tab().Files[m.tab().Cursor].Name
}

// resultPaths lists the relative paths of the palette's results
func resultPaths(m Model) []string {
	var out []string
	for _, r := range m.find.results {
		out = append(out, filepath.ToSlash(r.Rel))
	}
	return out
}

// waitClosed waits for a search to end, returning how it ended
func waitClosed(t *testing.T, run *findRun) error {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case _, ok := <-run.results:
			if !ok {
				return run.err
			}
		case <-deadline:
			t.Fatal("search did not stop")
			return nil
		}
	}
}

func TestFindByNameInSubfolders(t *testing.T) {
	root := makeTree(t, map[string]string{
		"src/app/main.go":           "",
		"src/app/model.go":          "",
		"docs/main.md":              "",
		".hidden/main.go":           "",
		"node_modules/pkg/main.js":  "",
		"vendor/lib/main.go":        "",
		"src/app/nothing-here.text": "",
	})
	m := newTestModel(t, root, nil)
	m = find(t, m, "f", "main")

	if got := strings.Join(resultPaths(m), " "); got != "docs/main.md src/app/main.go" {
		t.Fatalf("results = %s, want docs/main.md and src/app/main.go only", got)
	}
	view := strings.Join(plain(m.View()), "\n")
	for _, want := range []string{"Find files", "src/app/main.go", "2 matches", "FIND"} {
		if !strings.Contains(view, want) {
			t.Errorf("palette is missing %q:\n%s", want, view)
		}
	}

	// Enter goes to the file's directory with the cursor on it
	m, _ = pressKey(t, m, tea.KeyDown)
	m, cmd := press(t, m, "enter")
	m = drain(t, m, cmd)
	if m.mode != ModeNormal || m.tab().CurrentPath != filepath.Join(root, "src", "app") || cursorName(m) != "main.go" {
		t.Fatalf("after enter: mode=%v in %s on %s", m.mode, m.tab().CurrentPath, cursorName(m))
	}
}

func TestFindRanksCloserMatchesFirst(t *testing.T) {
	// Walked in this order, so the ranking has to move them
	root := makeTree(t, map[string]string{
		"a/m-o-d.txt":    "",
		"b/the-mod.txt":  "",
		"c/model.go":     "",
		"d/unrelated.go": "",
	})
	m := find(t, newTestModel(t, root, nil), "f", "mod")
	if got := strings.Join(resultPaths(m), " "); got != "c/model.go b/the-mod.txt a/m-o-d.txt" {
		t.Fatalf("results = %s, want starts-with, then contains, then fuzzy", got)
	}

	// A query with a slash matches the path
	m = find(t, newTestModel(t, root, nil), "f", "b/mod")
	if got := strings.Join(resultPaths(m), " "); got != "b/the-mod.txt" {
		t.Fatalf("path query results = %s", got)
	}
}

func TestFindRespectsHiddenSetting(t *testing.T) {
	root := makeTree(t, map[string]string{".config/app.yaml": "", "app.go": ""})
	cfg := config.DefaultConfig()
	cfg.ShowHidden = true
	m := find(t, newTestModel(t, root, cfg), "f", "app")
	if got := strings.Join(resultPaths(m), " "); got != "app.go .config/app.yaml" && got != ".config/app.yaml app.go" {
		t.Fatalf("with hidden files shown, results = %s", got)
	}
}

func TestFindInFilesJumpsToTheLine(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 200; i++ {
		if i == 120 {
			b.WriteString("\tthe NEEDLE is here\n")
			continue
		}
		fmt.Fprintf(&b, "line %d\n", i)
	}
	root := makeTree(t, map[string]string{
		"notes/todo.txt":  b.String(),
		"notes/other.txt": "no match",
		"image.bin":       "needle\x00\x01",
		".git/config":     "needle",
	})
	m := newTestModel(t, root, nil)
	m = find(t, m, "F", "needle")

	if len(m.find.results) != 1 {
		t.Fatalf("results = %v, want the one line in todo.txt", resultPaths(m))
	}
	view := strings.Join(plain(m.View()), "\n")
	if !strings.Contains(view, "Find in files") || !strings.Contains(view, "notes/todo.txt:120: the NEEDLE is here") {
		t.Fatalf("palette should show path:line: text:\n%s", view)
	}

	m, cmd := press(t, m, "enter")
	m = drain(t, m, cmd)
	tab := m.tab()
	if tab.CurrentPath != filepath.Join(root, "notes") || cursorName(m) != "todo.txt" {
		t.Fatalf("after enter: in %s on %s", tab.CurrentPath, cursorName(m))
	}
	rows := m.previewRows()
	if want := 120 - 1 - (rows-1)/2; tab.PreviewScroll != want {
		t.Fatalf("preview scroll = %d, want %d to centre line 120", tab.PreviewScroll, want)
	}
	shown := false
	for _, line := range plain(m.View()) {
		shown = shown || strings.Contains(line, " 120 ") && strings.Contains(line, "the NEEDLE is here")
	}
	if !shown {
		t.Fatalf("line 120 is not in the preview:\n%s", strings.Join(plain(m.View()), "\n"))
	}
	if m.jump != (previewJump{}) {
		t.Fatal("the jump should be used up")
	}
}

func TestFindJumpIsDroppedIfAnotherKeyComesFirst(t *testing.T) {
	root := makeTree(t, map[string]string{"a.txt": strings.Repeat("x\n", 100) + "needle\n"})
	m := find(t, newTestModel(t, root, nil), "F", "needle")
	m, cmd := press(t, m, "enter")
	m, _ = press(t, m, "K") // Scrolls the preview up before the file has loaded
	m = drain(t, m, cmd)
	if m.tab().PreviewScroll != 0 {
		t.Fatalf("preview scroll = %d; a key pressed meanwhile should cancel the jump", m.tab().PreviewScroll)
	}
}

// manyFiles makes a tree with more matches for "a" than the channel holds,
// so a search can't finish until it is read or cancelled
func manyFiles(t *testing.T) string {
	files := map[string]string{}
	for i := range 400 {
		files[fmt.Sprintf("d%d/a%03d.txt", i%4, i)] = ""
	}
	return makeTree(t, files)
}

func TestNewQueryCancelsTheSearch(t *testing.T) {
	m := newTestModel(t, manyFiles(t), nil)
	m, _ = press(t, m, "f")
	m, cmd := press(t, m, "a")
	m = run(t, m, cmd) // The pause: the search starts
	old := m.find.run
	if old == nil {
		t.Fatal("no search started")
	}

	m, _ = press(t, m, "0")
	if m.find.run != nil || !m.find.waiting {
		t.Fatal("typing should cancel the search and wait for a pause")
	}
	if err := waitClosed(t, old); !errors.Is(err, context.Canceled) {
		t.Fatalf("old search ended with %v, want it cancelled", err)
	}

	// Its results are dropped if they arrive late
	before := len(m.find.results)
	updated, _ := m.Update(findResultsMsg{run: old, results: []search.Result{{Rel: "stale"}}})
	if m = updated.(Model); len(m.find.results) != before {
		t.Fatal("results from a cancelled search were shown")
	}

	// A pause from before the last letter doesn't start anything
	updated, _ = m.Update(findDelayMsg{seq: m.find.seq - 1})
	if m = updated.(Model); m.find.run != nil {
		t.Fatal("an old pause started a search")
	}
}

func TestEscCancelsTheSearch(t *testing.T) {
	m := newTestModel(t, manyFiles(t), nil)
	m, _ = press(t, m, "f")
	m, cmd := press(t, m, "a")
	m = run(t, m, cmd)
	running := m.find.run

	m, _ = press(t, m, "esc")
	if m.mode != ModeNormal || m.find.run != nil {
		t.Fatalf("esc: mode=%v", m.mode)
	}
	if err := waitClosed(t, running); !errors.Is(err, context.Canceled) {
		t.Fatalf("search ended with %v, want it cancelled", err)
	}
}

func TestFindShowsWhenResultsAreCapped(t *testing.T) {
	old := findLimit
	findLimit = 25
	t.Cleanup(func() { findLimit = old })

	m := find(t, newTestModel(t, manyFiles(t), nil), "f", "a")
	if len(m.find.results) != 25 || !m.find.truncated {
		t.Fatalf("%d results (truncated=%v), want 25 and a note", len(m.find.results), m.find.truncated)
	}
	if view := strings.Join(plain(m.View()), "\n"); !strings.Contains(view, "25 shown, more results not shown") {
		t.Fatalf("no note about the missing results:\n%s", view)
	}
}

func TestFindTabSwitchesKind(t *testing.T) {
	root := makeTree(t, map[string]string{"notes.txt": "remember the milk", "milk.txt": ""})
	m := find(t, newTestModel(t, root, nil), "f", "milk")
	if got := strings.Join(resultPaths(m), " "); got != "milk.txt" {
		t.Fatalf("by name: %s", got)
	}
	m, cmd := press(t, m, "tab")
	m = drain(t, m, cmd)
	if !m.find.content || len(m.find.results) != 1 || m.find.results[0].Line != 1 || m.find.input.Value() != "milk" {
		t.Fatalf("after tab: content=%v results=%v", m.find.content, resultPaths(m))
	}
}

func TestFindCursorMoves(t *testing.T) {
	m := find(t, newTestModel(t, manyFiles(t), nil), "f", "a")
	for _, k := range []tea.KeyType{tea.KeyDown, tea.KeyDown, tea.KeyCtrlN, tea.KeyUp} {
		m, _ = pressKey(t, m, k)
	}
	if m.find.cursor != 2 {
		t.Fatalf("cursor = %d, want 2", m.find.cursor)
	}
	if m, _ = pressKey(t, m, tea.KeyPgDown); m.find.cursor <= 2 {
		t.Fatal("page down did not move")
	}
	for range 100 {
		m, _ = pressKey(t, m, tea.KeyPgUp)
	}
	if m.find.cursor != 0 {
		t.Fatalf("cursor = %d after paging up past the top", m.find.cursor)
	}
}

func TestFindPaletteFillsTheTerminal(t *testing.T) {
	root := t.TempDir()
	long := strings.Repeat("very-long-directory-name/", 8) + "日本語のファイル名" + strings.Repeat("字", 30) + ".txt"
	names := []search.Result{
		{Rel: "main.go", Path: filepath.Join(root, "main.go")},
		{Rel: filepath.FromSlash(long), Path: filepath.Join(root, long)},
		{Rel: "src", Path: filepath.Join(root, "src"), IsDir: true},
		{Rel: "bad\nname\x1b[31m.txt", Path: filepath.Join(root, "bad")},
	}
	lines := []search.Result{
		{Rel: "a.go", Path: filepath.Join(root, "a.go"), Line: 12, Text: "func main() { needle }", Col: 14},
		{Rel: filepath.FromSlash(long), Path: filepath.Join(root, long), Line: 99999, Text: strings.Repeat("é", 300) + "needle" + strings.Repeat("字", 100), Col: 300},
	}

	for _, size := range []tea.WindowSizeMsg{{Width: 140, Height: 40}, {Width: 100, Height: 24}, {Width: 60, Height: 15}, {Width: 30, Height: 10}, {Width: 20, Height: 8}} {
		for _, c := range []struct {
			key     string
			results []search.Result
		}{{"f", names}, {"F", lines}, {"f", nil}} {
			m := newTestModel(t, root, nil)
			updated, _ := m.Update(size)
			m, _ = press(t, updated.(Model), c.key)
			m = typeText(t, m, "needle")
			m.find.waiting = false
			m.find.results = c.results
			m.find.truncated = c.results != nil
			m.find.cursor = len(c.results) - 1
			label := fmt.Sprintf("%dx%d %s with %d results", size.Width, size.Height, c.key, len(c.results))
			lines := assertFills(t, label, m)
			if size.Height >= 15 && c.key == "F" && !strings.Contains(strings.Join(lines, "\n"), "needle") {
				t.Errorf("%s: the match is out of view:\n%s", label, strings.Join(lines, "\n"))
			}
		}
	}
}

func TestFindBadgeAndHints(t *testing.T) {
	m := newTestModel(t, t.TempDir(), nil)
	m, _ = press(t, m, "F")
	lines := plain(m.View())
	status, bottom := lines[len(lines)-2], lines[len(lines)-1]
	if !strings.Contains(status, "FIND") || !strings.Contains(bottom, "find by name") || !strings.Contains(bottom, "esc") {
		t.Fatalf("status %q, hints %q", status, bottom)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "Type to search inside the files") {
		t.Fatalf("empty palette should say what to do:\n%s", strings.Join(lines, "\n"))
	}
}
