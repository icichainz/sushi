package app

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/config"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/tags"
	"github.com/icichainz/sushi/internal/ui"
	"github.com/icichainz/sushi/internal/utils"
)

// needTags skips a test where files can't have Finder tags
func needTags(t *testing.T) {
	t.Helper()
	if !tags.Supported() {
		t.Skip("no Finder tags here")
	}
}

// tagFile gives the file at path the tags listed
func tagFile(t *testing.T, path string, list ...tags.Tag) {
	t.Helper()
	if err := tags.Write(path, list); err != nil {
		t.Skipf("can't tag files here: %v", err)
	}
}

// tagsOf returns the Finder tags of path, by name
func tagsOf(t *testing.T, path string) []string {
	t.Helper()
	list, err := tags.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tag := range list {
		names = append(names, tag.Name)
	}
	return names
}

// rowOf returns the first screen line below the panes' headings that
// shows name: its row in the file list
func rowOf(t *testing.T, m Model, name string) string {
	t.Helper()
	for _, line := range plain(m.View())[paneTop+1:] {
		if strings.Contains(line, name) {
			return line
		}
	}
	t.Fatalf("%s is not on screen:\n%s", name, strings.Join(plain(m.View()), "\n"))
	return ""
}

var (
	red  = tags.Tag{Name: "Red", Color: tags.Red}
	blue = tags.Tag{Name: "Blue", Color: tags.Blue}
	work = tags.Tag{Name: "Work"}
)

func TestTagDotsFollowNames(t *testing.T) {
	needTags(t)
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "")
	writeTestFile(t, filepath.Join(dir, "b.txt"), "")
	tagFile(t, filepath.Join(dir, "a.txt"), red, work, blue, tags.Tag{Name: "Rouge", Color: tags.Red})

	m := newTestModel(t, dir, nil)
	tagged, plainRow := rowOf(t, m, "a.txt"), rowOf(t, m, "b.txt")
	// A dot per colour, then one for the tags without, after the name
	if !strings.Contains(tagged, "a.txt ●●○ ") {
		t.Fatalf("tagged row = %q", tagged)
	}
	// Within the name's column: the sizes line up
	column := func(row string) int { return utils.Width(row[:max(strings.Index(row, "0 B"), 0)]) }
	if at, want := column(tagged), column(plainRow); at != want || at == 0 {
		t.Fatalf("size at %d in the tagged row, %d in the other:\n%q\n%q", at, want, tagged, plainRow)
	}

	// In ASCII, the marks are plain
	ui.SetIconMode(ui.IconModeASCII)
	t.Cleanup(func() { ui.SetIconMode(ui.IconModeNerd) })
	if row := rowOf(t, m, "a.txt"); !strings.Contains(row, "a.txt **o ") {
		t.Fatalf("ASCII row = %q", row)
	}
}

func TestTagsOff(t *testing.T) {
	needTags(t)
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "")
	tagFile(t, filepath.Join(dir, "a.txt"), red)

	cfg := config.DefaultConfig()
	cfg.Tags = false
	m := newTestModel(t, dir, cfg)
	if row := rowOf(t, m, "a.txt"); strings.Contains(row, "●") || m.tab().Files[0].Tags != nil {
		t.Fatalf("tags shown although off: %q", row)
	}
	for _, k := range []string{"L", "#"} {
		if after, _ := press(t, m, k); after.mode != ModeNormal {
			t.Errorf("%s opened mode %v although tags are off", k, after.mode)
		}
	}
	for _, group := range m.keys.helpGroups() {
		for _, h := range group.keys {
			if strings.Contains(h.label, "tag") {
				t.Errorf("the key panel offers %q %q", h.key, h.label)
			}
		}
	}
}

func TestTagPickerTicksTags(t *testing.T) {
	needTags(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "report.pdf")
	writeTestFile(t, path, "")
	writeTestFile(t, filepath.Join(dir, "other.txt"), "")
	tagFile(t, filepath.Join(dir, "other.txt"), tags.Tag{Name: "Project X"})

	m := newTestModel(t, dir, nil)
	m.tab().Cursor = slices.IndexFunc(m.tab().Files, func(f fs.FileInfo) bool { return f.Name == "report.pdf" })
	m, _ = press(t, m, "L")
	if m.mode != ModeTags {
		t.Fatalf("mode = %v, want the tag picker", m.mode)
	}
	view := strings.Join(plain(m.View()), "\n")
	for _, want := range []string{"Tags of report.pdf", "[ ] ● Red", "[ ] ● Gray", "[ ] ○ Project X", "type a new tag", "TAGS"} {
		if !strings.Contains(view, want) {
			t.Errorf("picker is missing %q:\n%s", want, view)
		}
	}

	// Space ticks Red, written at once
	m, _ = press(t, m, " ")
	if got := tagsOf(t, path); !slices.Equal(got, []string{"Red"}) {
		t.Fatalf("after space, tags = %q", got)
	}
	if !strings.Contains(strings.Join(plain(m.View()), "\n"), "[x] ● Red") {
		t.Fatal("Red isn't ticked")
	}

	// Typing goes to the field; enter adds the tag
	m = typeText(t, m, "Urgent")
	m, _ = press(t, m, "enter")
	if got := tagsOf(t, path); !slices.Equal(got, []string{"Red", "Urgent"}) {
		t.Fatalf("after typing a tag, tags = %q", got)
	}
	// One that is offered already is ticked rather than added twice
	m = typeText(t, m, "project x")
	m, _ = press(t, m, "enter")
	if got := tagsOf(t, path); !slices.Equal(got, []string{"Red", "Urgent", "Project X"}) {
		t.Fatalf("after typing an offered tag, tags = %q", got)
	}

	// Enter on a ticked row takes it off
	m, _ = ctrl(t, m, tea.KeyTab) // To the list
	m, _ = press(t, m, "enter")
	if got := tagsOf(t, path); !slices.Equal(got, []string{"Urgent", "Project X"}) {
		t.Fatalf("after unticking Red, tags = %q", got)
	}

	// Enter on the empty field closes; the list shows the tags without a
	// reload, which the watcher wouldn't make
	m, _ = ctrl(t, m, tea.KeyTab)
	m, _ = press(t, m, "enter")
	if m.mode != ModeNormal || !strings.Contains(m.statusMsg, "Changed the tags of report.pdf") {
		t.Fatalf("mode %v, status %q", m.mode, m.statusMsg)
	}
	if row := rowOf(t, m, "report.pdf"); !strings.Contains(row, "report.pdf ○") {
		t.Fatalf("row = %q", row)
	}

	// All of it is undone at once
	m = undoNow(t, m)
	if got := tagsOf(t, path); len(got) != 0 {
		t.Fatalf("after undo, tags = %q", got)
	}
	if row := rowOf(t, m, "report.pdf"); strings.Contains(row, "○") {
		t.Fatalf("after undo, row = %q", row)
	}
}

func TestTagPickerOnASelection(t *testing.T) {
	needTags(t)
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	writeTestFile(t, a, "")
	writeTestFile(t, b, "")
	tagFile(t, a, red)

	m := newTestModel(t, dir, nil)
	m, _ = press(t, m, " ")
	m, _ = press(t, m, " ")
	m, _ = press(t, m, "L")
	if view := strings.Join(plain(m.View()), "\n"); !strings.Contains(view, "Tags of 2 items") || !strings.Contains(view, "[-] ● Red") {
		t.Fatalf("Red should show as on some of the selection:\n%s", view)
	}
	// Ticked for those without it, then taken off all
	m, _ = press(t, m, " ")
	if !slices.Equal(tagsOf(t, a), []string{"Red"}) || !slices.Equal(tagsOf(t, b), []string{"Red"}) {
		t.Fatalf("tags = %q, %q", tagsOf(t, a), tagsOf(t, b))
	}
	m, _ = press(t, m, " ")
	if len(tagsOf(t, a)) != 0 || len(tagsOf(t, b)) != 0 {
		t.Fatalf("tags = %q, %q", tagsOf(t, a), tagsOf(t, b))
	}
	m, _ = press(t, m, "esc")

	// Undo puts back what each had
	m = undoNow(t, m)
	if !slices.Equal(tagsOf(t, a), []string{"Red"}) || len(tagsOf(t, b)) != 0 {
		t.Fatalf("after undo, tags = %q, %q", tagsOf(t, a), tagsOf(t, b))
	}
}

func TestTagUndoKeepsLaterChanges(t *testing.T) {
	needTags(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	writeTestFile(t, path, "")

	m := newTestModel(t, dir, nil)
	m, _ = press(t, m, "L")
	m, _ = press(t, m, " ")
	m, _ = press(t, m, "esc")
	tagFile(t, path, blue) // In Finder, meanwhile

	m = undoNow(t, m)
	if got := tagsOf(t, path); !slices.Equal(got, []string{"Blue"}) {
		t.Fatalf("undo overwrote a later change: tags = %q", got)
	}
	if !strings.Contains(m.statusMsg, "changed since") {
		t.Fatalf("status = %q", m.statusMsg)
	}
}

func TestTagPickerWithoutChangesLeavesNoUndo(t *testing.T) {
	needTags(t)
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "")
	m := newTestModel(t, dir, nil)
	m, _ = press(t, m, "L")
	m, _ = press(t, m, " ")
	m, _ = press(t, m, " ") // Back as it was
	m, _ = press(t, m, "esc")
	if len(m.undo) != 0 || m.statusMsg != "" {
		t.Fatalf("undo %v, status %q", m.undo, m.statusMsg)
	}
}

func TestTagPickerMouse(t *testing.T) {
	needTags(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	writeTestFile(t, path, "")
	m := newTestModel(t, dir, nil)
	m, _ = press(t, m, "L")

	x, y := findOnScreen(t, m, "Green", 0, m.width)
	m, _ = clickAt(m, x, y)
	if got := tagsOf(t, path); !slices.Equal(got, []string{"Green"}) {
		t.Fatalf("after clicking Green, tags = %q", got)
	}
	x, y = findOnScreen(t, m, "type a new tag", 0, m.width)
	if m, _ = clickAt(m, x, y); !m.tagger.onField() {
		t.Fatal("clicking the field didn't go to it")
	}
	if m, _ = clickAt(m, 0, 2); m.mode != ModeNormal {
		t.Fatal("clicking outside didn't close the picker")
	}
}

func TestTagPickerFillsTheTerminal(t *testing.T) {
	needTags(t)
	dir := t.TempDir()
	long := strings.Repeat("a very long tag name ", 8)
	for i, name := range []string{"one.txt", "two.txt", "three.txt"} {
		path := filepath.Join(dir, name)
		writeTestFile(t, path, "")
		tagFile(t, path, tags.Tag{Name: fmt.Sprintf("tag %d", i)}, tags.Tag{Name: long + "日本語"}, tags.Tag{Name: "bad\x1b[31m\nname", Color: tags.Purple})
	}
	for _, size := range []tea.WindowSizeMsg{{Width: 140, Height: 40}, {Width: 100, Height: 24}, {Width: 60, Height: 15}, {Width: 30, Height: 10}, {Width: 20, Height: 8}} {
		for _, keys := range []string{"L", "Ljjjjjjjjjj", "Lnew tag being typed" + strings.Repeat("x", 40)} {
			m := resize(newTestModel(t, dir, nil), size)
			for _, k := range keys {
				if k == 'j' {
					m, _ = ctrl(t, m, tea.KeyDown)
					continue
				}
				m, _ = press(t, m, string(k))
			}
			label := fmt.Sprintf("%dx%d %q", size.Width, size.Height, keys)
			lines := assertFills(t, label, m)
			assertNoControls(t, label, m.View())
			// The row the cursor is on is in view
			if size.Height >= 10 && m.tagger.cursor < len(m.tagger.offer) {
				name := string([]rune(utils.Printable(m.tagger.offer[m.tagger.cursor].Name))[:3])
				if !strings.Contains(strings.Join(lines, "\n"), name) {
					t.Errorf("%s: the cursor's row %q is out of view:\n%s", label, name, strings.Join(lines, "\n"))
				}
			}
		}

		// Tagged files found with #, with their dots
		m := resize(newTestModel(t, dir, nil), size)
		m, cmd := press(t, m, "#")
		m = drain(t, m, cmd)
		label := fmt.Sprintf("%dx%d #", size.Width, size.Height)
		if len(m.find.results) != 3 {
			t.Errorf("%s: %d results", label, len(m.find.results))
		}
		assertFills(t, label, m)
	}
}

func TestFindByTag(t *testing.T) {
	needTags(t)
	root := makeTree(t, map[string]string{
		"report.pdf":      "",
		"redo.txt":        "",
		"sub/budget.xlsx": "",
		"plain.txt":       "",
		"#name.txt":       "", // Found by name only when tags are off
	})
	tagFile(t, filepath.Join(root, "report.pdf"), red)
	tagFile(t, filepath.Join(root, "redo.txt"), tags.Tag{Name: "Redo"})
	tagFile(t, filepath.Join(root, "sub", "budget.xlsx"), work, red)

	// # opens the palette on everything tagged
	m := newTestModel(t, root, nil)
	m, cmd := press(t, m, "#")
	m = drain(t, m, cmd)
	if m.mode != ModeFind || m.find.input.Value() != "#" {
		t.Fatalf("mode %v, query %q", m.mode, m.find.input.Value())
	}
	if got := strings.Join(resultPaths(m), " "); got != "redo.txt report.pdf sub/budget.xlsx" {
		t.Fatalf("# found %s", got)
	}
	view := strings.Join(plain(m.View()), "\n")
	for _, want := range []string{"Find files by tag below this folder", "sub/budget.xlsx ●○"} {
		if !strings.Contains(view, want) {
			t.Errorf("palette is missing %q:\n%s", want, view)
		}
	}

	// Narrowed by a tag's name; exact names first
	m = find(t, newTestModel(t, root, nil), "f", "tag:red")
	if got := strings.Join(resultPaths(m), " "); got != "report.pdf sub/budget.xlsx redo.txt" {
		t.Fatalf("tag:red found %s", got)
	}

	// Inside files, # is just text
	m = find(t, newTestModel(t, root, nil), "F", "#red")
	if m.find.err != nil || len(m.find.results) != 0 {
		t.Fatalf("F #red: %v %v", resultPaths(m), m.find.err)
	}

	// With tags off, it is part of a name
	cfg := config.DefaultConfig()
	cfg.Tags = false
	m = find(t, newTestModel(t, root, cfg), "f", "#name")
	if got := strings.Join(resultPaths(m), " "); got != "#name.txt" {
		t.Fatalf("with tags off, #name found %s", got)
	}
}

func TestCopiesKeepTheirTags(t *testing.T) {
	needTags(t)
	dir, other := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(dir, "report.txt"), "report")
	tagFile(t, filepath.Join(dir, "report.txt"), red, work)

	// Duplicated with y
	m := newTestModel(t, dir, nil)
	m, cmd := press(t, m, "y")
	m = drain(t, m, cmd)
	if got := tagsOf(t, filepath.Join(dir, "report copy.txt")); !slices.Equal(got, []string{"Red", "Work"}) {
		t.Fatalf("the duplicate has tags %q, statusMsg %q", got, m.statusMsg)
	}
	// Copied and pasted elsewhere
	m = cursorTo(t, m, "report.txt")
	m, _ = press(t, m, "c")
	m = at(t, m, other)
	m, cmd = press(t, m, "v")
	m = drain(t, m, cmd)
	if got := tagsOf(t, filepath.Join(other, "report.txt")); !slices.Equal(got, []string{"Red", "Work"}) {
		t.Fatalf("the copy has tags %q, statusMsg %q", got, m.statusMsg)
	}
}

func TestFindByNameStartingWithATagSign(t *testing.T) {
	root := makeTree(t, map[string]string{"#autosave#.txt": "", "tag:notes.md": "", "plain.txt": "", "\\odd.txt": ""})

	// A backslash makes # and tag: part of the name
	for query, want := range map[string]string{`\#autosave#`: "#autosave#.txt", `\tag:notes`: "tag:notes.md", `\odd`: "\\odd.txt"} {
		m := find(t, newTestModel(t, root, nil), "f", query)
		if got := strings.Join(resultPaths(m), " "); got != want {
			t.Errorf("%s found %q, want %q", query, got, want)
		}
		if title := strings.Join(plain(m.View()), "\n"); !strings.Contains(title, "Find files below this folder") {
			t.Errorf("%s isn't a search by name:\n%s", query, title)
		}
	}
	// The name shows the match underlined from its first letter
	m := find(t, newTestModel(t, root, nil), "f", `\#auto`)
	if hits := namePositions(m.nameQuery(), "#autosave#.txt"); !hits[0] || !hits[4] || hits[5] {
		t.Errorf("positions %v", hits)
	}
	// Without it, a search by tag
	if m := find(t, newTestModel(t, root, nil), "f", "#autosave"); len(m.find.results) != 0 {
		t.Errorf("#autosave found %q", resultPaths(m))
	}
}

func TestFindByNameShowsTags(t *testing.T) {
	needTags(t)
	root := makeTree(t, map[string]string{"report.pdf": "", "report.txt": ""})
	tagFile(t, filepath.Join(root, "report.pdf"), red, work)

	m := find(t, newTestModel(t, root, nil), "f", "report")
	view := strings.Join(plain(m.View()), "\n")
	if !strings.Contains(view, "report.pdf ●○") || strings.Contains(view, "report.txt ●") {
		t.Fatalf("palette:\n%s", view)
	}
	// Not with tags off
	cfg := config.DefaultConfig()
	cfg.Tags = false
	m = find(t, newTestModel(t, root, cfg), "f", "report")
	if view := strings.Join(plain(m.View()), "\n"); strings.Contains(view, "●") {
		t.Fatalf("tags off:\n%s", view)
	}
}
