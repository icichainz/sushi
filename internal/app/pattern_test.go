package app

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/fs"
)

// infos describes files called names for applyPattern; a name ending in /
// is a folder
func infos(names ...string) []fs.FileInfo {
	when := time.Date(2024, 5, 6, 12, 0, 0, 0, time.Local)
	files := make([]fs.FileInfo, len(names))
	for i, name := range names {
		dir := strings.HasSuffix(name, "/")
		name = strings.TrimSuffix(name, "/")
		files[i] = fs.FileInfo{Name: name, Path: filepath.Join("/x", name), IsDir: dir, ModTime: when}
	}
	return files
}

func TestApplyPattern(t *testing.T) {
	for _, c := range []struct {
		files         []string
		find, replace string
		want          []string
	}{
		// Nothing typed changes nothing
		{[]string{"a.txt", "b.txt"}, "", "", []string{"a.txt", "b.txt"}},
		// Plain text, wherever it occurs, matching case
		{[]string{"a-a-a.txt"}, "a", "b", []string{"b-b-b.txt"}},
		{[]string{"Report.txt"}, "report", "x", []string{"Report.txt"}},
		{[]string{"draft (copy).txt"}, " (copy)", "", []string{"draft.txt"}},
		// Plain text is never an expression, nor its replacement a group
		{[]string{"a.b.txt"}, ".", "$1", []string{"a$1b$1txt"}},
		{[]string{"a/b"}, "/", "x", []string{"axb"}},
		// With Find empty, Replace is the whole name
		{[]string{"IMG_0001.jpg", "IMG_0002.jpg"}, "", "holiday-{n:3}{ext}", []string{"holiday-001.jpg", "holiday-002.jpg"}},
		{[]string{"a.txt", "b.txt"}, "", "{n}", []string{"1", "2"}},
		// Regular expressions, with their groups
		{[]string{"2024-05-06 notes.md"}, `/^(\d+)-(\d+)-(\d+) (.*)$/`, "$4 ${3}.${2}.$1", []string{"notes.md 06.05.2024"}},
		{[]string{"photo.JPG", "JPG.txt"}, `/\.JPG$/`, ".jpg", []string{"photo.jpg", "JPG.txt"}},
		{[]string{"track1.mp3", "track12.mp3"}, `/^track(\d+)/`, "$1-{n:2}", []string{"1-01.mp3", "12-02.mp3"}},
		// The tokens: compound extensions are one, folders have none
		{[]string{"notes.tar.gz", "v1.2/", ".profile"}, "", "{name}-{n}{ext}", []string{"notes-1.tar.gz", "v1.2-2", ".profile-3"}},
		{[]string{"a.txt"}, "", "{date} {name}{ext}", []string{"2024-05-06 a.txt"}},
		{[]string{"a.txt"}, "", "{n:99}", []string{"000000000000000001"}},
		{[]string{"a.txt"}, "", "{x}{N}{n:}{name", []string{"{x}{N}{n:}{name"}},
		// A $ in a file's own name stays a $, never a group
		{[]string{"cost$1.txt"}, "/^(.*)$/", "{name}!", []string{"cost$1!"}},
		{[]string{"a$1.txt"}, "a", "{name}", []string{"a$1$1.txt"}},
	} {
		got, err := applyPattern(infos(c.files...), c.find, c.replace)
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("%v with %q → %q: got %q, %v; want %q", c.files, c.find, c.replace, got, err, c.want)
		}
	}

	if _, err := applyPattern(infos("a.txt"), "/(/", "x"); err == nil || !strings.Contains(err.Error(), "invalid regular expression") {
		t.Errorf("an invalid expression: err = %v", err)
	}
}

// openPattern presses M and types find, then replace, into the dialog
func openPattern(t *testing.T, m Model, find, replace string) Model {
	t.Helper()
	m, _ = press(t, m, "M")
	if m.mode != ModePattern {
		t.Fatalf("M didn't open the dialog: statusMsg = %q", m.statusMsg)
	}
	m = typeText(t, m, find)
	m, _ = press(t, m, "tab")
	return typeText(t, m, replace)
}

// patternRowsOf returns the dialog's rows as "from → to (problem)"
func patternRowsOf(m Model) []string {
	var rows []string
	for _, r := range m.pattern.rows {
		row := r.from + " → " + r.to
		if r.problem != "" {
			row += " (" + r.problem + ")"
		}
		rows = append(rows, row)
	}
	return rows
}

func TestPatternRenameRenamesAndUndoes(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"IMG_0001.jpg", "IMG_0002.jpg", "notes.txt"} {
		writeTestFile(t, filepath.Join(dir, name), name)
	}

	m := newTestModel(t, dir, nil)
	m.bookmarks.Add("first", filepath.Join(dir, "IMG_0001.jpg"))
	m, _ = press(t, m, " ")
	m, _ = press(t, m, " ")
	m = openPattern(t, m, "IMG_", "holiday-")

	want := []string{"IMG_0001.jpg → holiday-0001.jpg", "IMG_0002.jpg → holiday-0002.jpg"}
	if got := patternRowsOf(m); !slices.Equal(got, want) || m.pattern.changes != 2 || m.pattern.problems != 0 {
		t.Fatalf("rows = %q, %d changes, %d problems", got, m.pattern.changes, m.pattern.problems)
	}
	screen := strings.Join(plain(m.View()), "\n")
	for _, s := range []string{"Rename 2 items by pattern", "IMG_0001.jpg → holiday-0001.jpg", "2 of 2 names change", "RENAME"} {
		if !strings.Contains(screen, s) {
			t.Errorf("%q is not on screen:\n%s", s, screen)
		}
	}

	m = submit(t, m)
	if got := dirNames(t, dir); strings.Join(got, ",") != "holiday-0001.jpg,holiday-0002.jpg,notes.txt" {
		t.Fatalf("after renaming: %v, statusMsg = %q", got, m.statusMsg)
	}
	if readTestFile(t, filepath.Join(dir, "holiday-0002.jpg")) != "IMG_0002.jpg" {
		t.Fatal("the files were renamed out of order")
	}
	if m.mode != ModeNormal || m.statusMsg != "Renamed 2 items" || len(m.tab().Selected) != 0 {
		t.Fatalf("mode = %v, statusMsg = %q, %d selected", m.mode, m.statusMsg, len(m.tab().Selected))
	}
	if got := m.bookmarks.Get(0).Path; got != filepath.Join(dir, "holiday-0001.jpg") {
		t.Fatalf("bookmark = %s, want it to follow the rename", got)
	}

	// One undo puts every name back
	m = undoNow(t, m)
	if got := dirNames(t, dir); strings.Join(got, ",") != "IMG_0001.jpg,IMG_0002.jpg,notes.txt" {
		t.Fatalf("after undo: %v, statusMsg = %q", got, m.statusMsg)
	}
}

func TestPatternRenameWithoutSelection(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		writeTestFile(t, filepath.Join(dir, name), "")
	}
	m := cursorTo(t, newTestModel(t, dir, nil), "b.txt")
	m = openPattern(t, m, "b", "z")
	if got := patternRowsOf(m); !slices.Equal(got, []string{"b.txt → z.txt"}) {
		t.Fatalf("rows = %q, want only the file under the cursor", got)
	}
	if !strings.Contains(strings.Join(plain(m.View()), "\n"), "Rename b.txt by pattern") {
		t.Error("the dialog doesn't name the file")
	}

	m = submit(t, m)
	if got := dirNames(t, dir); strings.Join(got, ",") != "a.txt,c.txt,z.txt" {
		t.Fatalf("after renaming: %v, statusMsg = %q", got, m.statusMsg)
	}
	if got := cursorName(m); got != "z.txt" {
		t.Fatalf("cursor on %s, want it to follow the file to z.txt", got)
	}
}

func TestPatternRenameChains(t *testing.T) {
	// 0.txt becomes 1.txt as 1.txt becomes 2.txt: names taken by files
	// renamed away in the same batch are free
	dir := t.TempDir()
	for _, name := range []string{"0.txt", "1.txt"} {
		writeTestFile(t, filepath.Join(dir, name), name)
	}
	m := newTestModel(t, dir, nil)
	m, _ = press(t, m, "*")
	m = openPattern(t, m, "", "{n}{ext}")
	if m.pattern.problems != 0 {
		t.Fatalf("rows = %q", patternRowsOf(m))
	}
	m = submit(t, m)
	if readTestFile(t, filepath.Join(dir, "1.txt")) != "0.txt" || readTestFile(t, filepath.Join(dir, "2.txt")) != "1.txt" {
		t.Fatalf("after renaming: %v, statusMsg = %q", dirNames(t, dir), m.statusMsg)
	}
	m = undoNow(t, m)
	if readTestFile(t, filepath.Join(dir, "0.txt")) != "0.txt" || readTestFile(t, filepath.Join(dir, "1.txt")) != "1.txt" {
		t.Fatalf("after undo: %v, statusMsg = %q", dirNames(t, dir), m.statusMsg)
	}
}

func TestPatternRenameAcrossFolders(t *testing.T) {
	// Files selected in another folder come after those listed, and stay
	// in their own folders
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "sub"), 0755)
	writeTestFile(t, filepath.Join(dir, "a.txt"), "a")
	writeTestFile(t, filepath.Join(dir, "sub", "z.txt"), "z")

	m := enter(t, newTestModel(t, dir, nil), "sub")
	m, _ = press(t, m, " ")
	m = at(t, m, dir)
	m, _ = press(t, cursorTo(t, m, "a.txt"), " ")
	m = openPattern(t, m, "", "{n}{ext}")
	if got := patternRowsOf(m); !slices.Equal(got, []string{"a.txt → 1.txt", "z.txt → 2.txt"}) {
		t.Fatalf("rows = %q", got)
	}
	m = submit(t, m)
	if readTestFile(t, filepath.Join(dir, "1.txt")) != "a" || readTestFile(t, filepath.Join(dir, "sub", "2.txt")) != "z" {
		t.Fatalf("after renaming: %v and %v, statusMsg = %q", dirNames(t, dir), dirNames(t, filepath.Join(dir, "sub")), m.statusMsg)
	}
}

func TestPatternRenameMarksNamesThatCantBeUsed(t *testing.T) {
	for _, c := range []struct {
		files         []string
		taken         string // Created once the dialog is open
		find, replace string
		want          []string // What each file's problem says, if anything
	}{
		{[]string{"a.txt", "b.txt"}, "", "", "same.txt", []string{"another file gets this name", "another file gets this name"}},
		// Names that differ only in case are the same name on macOS
		{[]string{"Xa.txt", "xb.txt"}, "", "/[ab]/", "", []string{"another file gets this name", "another file gets this name"}},
		{[]string{"a.txt", "b.txt"}, "", "", "{n}/x", []string{"path separator", "path separator"}},
		{[]string{"a.txt", "b.txt"}, "", "/.*/", "", []string{"name is empty", "name is empty"}},
		{[]string{"a.txt", "b.txt"}, "", "", "..", []string{"not a valid name", "not a valid name"}},
		{[]string{"a.txt", "b.txt"}, "taken.txt", "a", "taken", []string{"already exists", ""}},
	} {
		label := fmt.Sprintf("%q → %q", c.find, c.replace)
		dir := t.TempDir()
		for _, name := range c.files {
			writeTestFile(t, filepath.Join(dir, name), "")
		}
		m := newTestModel(t, dir, nil)
		m, _ = press(t, m, "*")
		m = openPattern(t, m, c.find, c.replace)
		if c.taken != "" {
			// Typed while the name was free; Enter checks again
			if m.pattern.problems != 0 {
				t.Fatalf("%s: rows = %q before %s exists", label, patternRowsOf(m), c.taken)
			}
			writeTestFile(t, filepath.Join(dir, c.taken), "")
		}
		m, _ = press(t, m, "enter")

		problems := 0
		for i, r := range m.pattern.rows {
			if (c.want[i] == "") != (r.problem == "") || !strings.Contains(r.problem, c.want[i]) {
				t.Errorf("%s: %s gets %q, problem %q, want %q", label, r.from, r.to, r.problem, c.want[i])
			}
			if c.want[i] != "" {
				problems++
			}
		}
		if m.pattern.problems != problems || m.mode != ModePattern || m.pattern.failed != "Change the names in red first" {
			t.Errorf("%s: %d problems, mode %v, failed %q", label, m.pattern.problems, m.mode, m.pattern.failed)
		}
		if screen := strings.Join(plain(m.View()), "\n"); !strings.Contains(screen, c.want[0]) {
			t.Errorf("%s: the problem is not on screen:\n%s", label, screen)
		}
		want := slices.Sorted(slices.Values(append(slices.Clone(c.files), c.taken)))
		want = slices.DeleteFunc(want, func(s string) bool { return s == "" })
		if got := dirNames(t, dir); !slices.Equal(got, want) {
			t.Errorf("%s: files changed to %v", label, got)
		}

		// Typing again clears what Enter said, and Esc leaves it all as it was
		m = typeText(t, m, "q")
		if m.pattern.failed != "" {
			t.Errorf("%s: failed = %q after an edit", label, m.pattern.failed)
		}
		m, _ = press(t, m, "esc")
		if m.mode != ModeNormal || m.pattern.rows != nil {
			t.Errorf("%s: esc left mode %v", label, m.mode)
		}
	}
}

func TestPatternRenameInvalidExpression(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "")
	m := openPattern(t, newTestModel(t, dir, nil), "/(/", "x")
	if !strings.Contains(m.pattern.err, "invalid regular expression") || m.pattern.changes != 0 {
		t.Fatalf("err = %q, %d changes", m.pattern.err, m.pattern.changes)
	}
	if screen := strings.Join(plain(m.View()), "\n"); !strings.Contains(screen, "invalid regular expression") {
		t.Errorf("the error is not on screen:\n%s", screen)
	}
	m, _ = press(t, m, "enter")
	if m.mode != ModePattern || !slices.Equal(dirNames(t, dir), []string{"a.txt"}) {
		t.Fatalf("enter with an invalid expression: mode %v, files %v", m.mode, dirNames(t, dir))
	}
}

func TestPatternRenameNothingToDo(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "")

	// No names changed
	m := openPattern(t, newTestModel(t, dir, nil), "nowhere", "x")
	m = submit(t, m)
	if m.mode != ModeNormal || m.statusMsg != "No names changed" || len(m.undo) != 0 {
		t.Fatalf("mode %v, statusMsg = %q", m.mode, m.statusMsg)
	}

	// Nothing to rename
	m = newTestModel(t, t.TempDir(), nil)
	m, _ = press(t, m, "M")
	if m.mode != ModeNormal {
		t.Fatal("M opened the dialog in an empty folder")
	}

	// Names the rename can't take, as it goes through a list of lines
	odd := filepath.Join(dir, "two\nlines.txt")
	if err := os.WriteFile(odd, nil, 0644); err != nil {
		t.Skipf("this file system refuses line breaks in names: %v", err)
	}
	m = cursorTo(t, newTestModel(t, dir, nil), "two\nlines.txt")
	m, _ = press(t, m, "M")
	if m.mode != ModeNormal || !strings.Contains(m.statusMsg, "line break") {
		t.Fatalf("mode %v, statusMsg = %q", m.mode, m.statusMsg)
	}
}

func TestPatternRenameWaitsForTheJob(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "")
	m := busy(t, dir, nil)
	m, _ = press(t, m, "M")
	if m.mode == ModePattern || !strings.HasPrefix(m.statusMsg, "Still ") {
		t.Fatalf("mode %v, statusMsg = %q", m.mode, m.statusMsg)
	}
}

func TestPatternDialogFillsTheTerminal(t *testing.T) {
	dir := t.TempDir()
	numberedFiles(t, dir, 30)
	writeTestFile(t, filepath.Join(dir, "日本語の"+strings.Repeat("字", 40)+".txt"), "")
	// Names with escape codes are shown, never sent to the terminal
	hostile := "esc\x1b[2Jape\x07.txt"
	_ = os.WriteFile(filepath.Join(dir, hostile), nil, 0644)

	for _, size := range sizes {
		label := fmt.Sprintf("%dx%d", size.Width, size.Height)
		m := resize(newTestModel(t, dir, nil), size)
		m, _ = press(t, m, "*")
		for _, c := range []struct{ name, find, replace string }{
			{"names", "file-", "{n:3}-" + strings.Repeat("long", 20)},
			{"problems", "", "same"},
			{"error", "/(" + strings.Repeat("x", 100) + "/", ""},
		} {
			screen := openPattern(t, detach(m), c.find, c.replace)
			lines := assertFills(t, label+" "+c.name, screen)
			assertNoControls(t, label+" "+c.name, screen.View())
			if c.name == "names" && size.Height >= 24 && !strings.Contains(strings.Join(lines, "\n"), "more") {
				t.Errorf("%s: the names left over aren't counted:\n%s", label, strings.Join(lines, "\n"))
			}
		}
	}
}

func TestPatternDialogScrolls(t *testing.T) {
	dir := t.TempDir()
	numberedFiles(t, dir, 30)
	m := newTestModel(t, dir, nil)
	m, _ = press(t, m, "*")
	m = openPattern(t, m, "file", "doc")
	rows, _ := m.patternRows()
	last := 30 - rows

	for _, step := range []struct {
		key  tea.KeyType
		want int
	}{
		{tea.KeyDown, 1}, {tea.KeyPgDown, rows}, {tea.KeyPgDown, min(2*rows-1, last)},
		{tea.KeyPgDown, last}, {tea.KeyUp, last - 1}, {tea.KeyPgUp, last - rows}, {tea.KeyPgUp, 0}, {tea.KeyUp, 0},
	} {
		m, _ = ctrl(t, m, step.key)
		if m.pattern.scroll != step.want {
			t.Fatalf("after %v: scroll = %d, want %d", step.key, m.pattern.scroll, step.want)
		}
	}

	m, _ = mouseAt(m, 50, 10, tea.MouseButtonWheelDown, false)
	m, _ = mouseAt(m, 50, 10, tea.MouseButtonWheelDown, false)
	if m.pattern.scroll != 2 {
		t.Fatalf("after the wheel: scroll = %d", m.pattern.scroll)
	}
	lines := plain(m.View())
	if screen := strings.Join(lines, "\n"); !strings.Contains(screen, "file-02.txt → doc-02.txt") || strings.Contains(screen, "file-01.txt → ") {
		t.Fatalf("scrolled two names down:\n%s", screen)
	}
	if m.pattern.find.Value() != "file" || m.pattern.replace.Value() != "doc" {
		t.Fatal("scrolling changed the fields")
	}
}

func TestPatternDialogClicks(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "")
	m := openPattern(t, newTestModel(t, dir, nil), "qwe", "z")
	if m.pattern.field != fieldReplace {
		t.Fatal("tab didn't move to Replace")
	}

	// A click on the text of Find types there, where it was clicked
	x, y := findOnScreen(t, m, "qwe", 0, m.width)
	m, _ = clickAt(m, x+1, y)
	if m.pattern.field != fieldFind {
		t.Fatal("the click didn't move to Find")
	}
	m = typeText(t, m, "X")
	if got := m.pattern.find.Value(); got != "qXwe" {
		t.Fatalf("find = %q, want the X where clicked", got)
	}

	// A click on Replace's label moves there, and one outside does nothing
	x, y = findOnScreen(t, m, "Replace", 0, m.width)
	m, _ = clickAt(m, x, y)
	if m.pattern.field != fieldReplace {
		t.Fatal("the click didn't move to Replace")
	}
	m, _ = clickAt(m, 0, 0)
	if m.mode != ModePattern || m.pattern.find.Value() != "qXwe" || m.pattern.replace.Value() != "z" {
		t.Fatalf("a click outside: mode %v, find %q, replace %q", m.mode, m.pattern.find.Value(), m.pattern.replace.Value())
	}
}
