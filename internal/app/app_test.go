package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/icichainz/sushi/internal/config"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/plugins"
	"github.com/icichainz/sushi/internal/ui/components"
	"github.com/icichainz/sushi/internal/utils"
)

func TestMain(m *testing.M) {
	// Status timers would hold up every test for seconds; fire them at once
	// without a message, so status text stays put for assertions
	statusTimer = func(time.Duration, func(time.Time) tea.Msg) tea.Cmd {
		return func() tea.Msg { return nil }
	}
	os.Exit(m.Run())
}

// newTestModel builds a sized model rooted at dir, with HOME redirected so
// bookmarks and config never touch the real user directory
func newTestModel(t *testing.T, dir string, cfg *config.Config) Model {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	m := NewModelWithConfig(dir, cfg)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	return updated.(Model)
}

// press sends a single key to the model
func press(t *testing.T, m Model, k string) (Model, tea.Cmd) {
	t.Helper()
	var msg tea.KeyMsg
	switch k {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "backspace":
		msg = tea.KeyMsg{Type: tea.KeyBackspace}
	case " ":
		msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	case "ctrl+u":
		msg = tea.KeyMsg{Type: tea.KeyCtrlU}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	updated, cmd := m.Update(msg)
	return updated.(Model), cmd
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestPasteIntoSameDirDoesNotOfferOverwrite(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	writeTestFile(t, f, "important data")

	m := newTestModel(t, dir, nil)
	m, _ = press(t, m, "c")
	m, _ = press(t, m, "v")

	if m.mode == ModeConfirm {
		t.Fatal("paste onto itself opened the overwrite prompt")
	}
	if !strings.Contains(m.statusMsg, "Can't paste") {
		t.Fatalf("statusMsg = %q, want a refusal", m.statusMsg)
	}
	b, _ := os.ReadFile(f)
	if string(b) != "important data" {
		t.Fatalf("content = %q, file was damaged", b)
	}
}

func TestDirLoadGoesToRequestingTab(t *testing.T) {
	d1, d2 := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(d2, "only-in-d2.txt"), "x")

	m := newTestModel(t, d1, nil)
	updated, loadCmd := m.createTab(d2)
	m = updated.(Model)
	// Switch back to tab 1 before tab 2's directory finishes loading
	m, _ = press(t, m, "tab")
	updated, _ = m.Update(loadCmd().(tea.BatchMsg)[0]())
	m = updated.(Model)

	if n := len(m.tabs[0].Files); n != 0 {
		t.Fatalf("tab 1 received %d files from tab 2's load", n)
	}
	if m.tabs[1].Loading || len(m.tabs[1].Files) != 1 {
		t.Fatalf("tab 2: loading=%v files=%d, want loaded with 1 file", m.tabs[1].Loading, len(m.tabs[1].Files))
	}
}

func TestSupersededDirLoadIsDropped(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "a"), 0755)
	os.Mkdir(filepath.Join(root, "b"), 0755)

	m := newTestModel(t, root, nil)
	tab := m.tab()
	first := m.loadDir(tab, filepath.Join(root, "a"))
	second := m.loadDir(tab, filepath.Join(root, "b"))

	// The newer load finishes first; the older one must not overwrite it
	updated, _ := m.Update(second())
	updated, _ = updated.Update(first())
	m = updated.(Model)

	if got := filepath.Base(m.tab().CurrentPath); got != "b" {
		t.Fatalf("CurrentPath = %s, want b", got)
	}
}

func TestStalePreviewIsDropped(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "aaa")
	writeTestFile(t, filepath.Join(dir, "b.txt"), "bbb")

	m := newTestModel(t, dir, nil)
	staleCmd := loadPreview(m.tab().ID, m.tab().Files[0], m.theme.Syntax)
	m, cmd := press(t, m, "j")

	// b.txt's preview lands first, then a late one for a.txt
	updated, _ := m.Update(cmd())
	updated, _ = updated.Update(staleCmd())
	m = updated.(Model)
	if got := filepath.Base(m.tab().Preview.Path); got != "b.txt" {
		t.Fatalf("preview shows %s while the cursor is on b.txt", got)
	}
}

func TestRelativeStartPathCanGoUp(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	os.Mkdir(child, 0755)
	t.Chdir(child)

	m := newTestModel(t, "..", nil)
	if !filepath.IsAbs(m.tab().CurrentPath) {
		t.Fatalf("CurrentPath = %q, want absolute", m.tab().CurrentPath)
	}
	m, cmd := press(t, m, "h")
	updated, _ := m.Update(cmd())
	m = updated.(Model)

	want, _ := filepath.EvalSymlinks(filepath.Dir(root))
	got, _ := filepath.EvalSymlinks(m.tab().CurrentPath)
	if got != want {
		t.Fatalf("after h: %s, want %s", got, want)
	}
}

func TestGoingUpFocusesPreviousDir(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		os.Mkdir(filepath.Join(root, name), 0755)
	}

	m := newTestModel(t, filepath.Join(root, "c"), nil)
	m, cmd := press(t, m, "h")
	updated, _ := m.Update(cmd())
	m = updated.(Model)

	if got := m.tab().Files[m.tab().Cursor].Name; got != "c" {
		t.Fatalf("cursor on %q, want c", got)
	}
}

func TestFailedLoadKeepsCurrentDir(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "x")

	m := newTestModel(t, dir, nil)
	cmd := m.loadDir(m.tab(), filepath.Join(dir, "missing"))
	updated, _ := m.Update(cmd())
	m = updated.(Model)

	if m.tab().CurrentPath != dir || len(m.tab().Files) != 1 || m.tab().Loading {
		t.Fatalf("tab = %s with %d files (loading=%v), want %s unchanged", m.tab().CurrentPath, len(m.tab().Files), m.tab().Loading, dir)
	}
	if !strings.HasPrefix(m.statusMsg, "Error:") {
		t.Fatalf("statusMsg = %q, want an error", m.statusMsg)
	}
}

func TestStatusClearsOnlyItsOwnMessage(t *testing.T) {
	m := newTestModel(t, t.TempDir(), nil)
	m.setStatus("first")
	firstID := m.statusID
	m.setStatus("second")

	updated, _ := m.Update(clearStatusMsg{id: firstID})
	m = updated.(Model)
	if m.statusMsg != "second" {
		t.Fatalf("statusMsg = %q, older timer cleared the newer message", m.statusMsg)
	}

	updated, _ = m.Update(clearStatusMsg{id: m.statusID})
	if msg := updated.(Model).statusMsg; msg != "" {
		t.Fatalf("statusMsg = %q, want cleared", msg)
	}
}

func TestNewTabUsesConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.PreviewEnabled = false
	cfg.PreviewWidth = 30

	m := newTestModel(t, t.TempDir(), cfg)
	updated, _ := m.createTab(t.TempDir())
	tab := updated.(Model).tabs[1]
	if tab.PreviewEnabled || tab.PreviewWidth != 30 {
		t.Fatalf("new tab preview=%v width=%d, want false/30", tab.PreviewEnabled, tab.PreviewWidth)
	}
}

// plain returns a rendered screen as lines without color codes
func plain(view string) []string {
	return strings.Split(ansi.Strip(view), "\n")
}

// assertFills checks that a screen is exactly the size of the terminal, so
// nothing is pushed off screen and nothing of the last frame shows through
func assertFills(t *testing.T, label string, m Model) []string {
	t.Helper()
	// mainLines is checked unclipped, so View's final clip can't hide overflow
	for name, view := range map[string]string{"main": m.renderMainView(), "view": m.View()} {
		lines := strings.Split(view, "\n")
		if len(lines) != m.height {
			t.Errorf("%s %s: %d lines, terminal has %d", label, name, len(lines), m.height)
		}
		for i, l := range lines {
			if w := lipgloss.Width(l); w != m.width {
				t.Errorf("%s %s: line %d is %d wide, terminal is %d: %q", label, name, i, w, m.width, ansi.Strip(l))
			}
			if !utf8.ValidString(l) {
				t.Errorf("%s %s: line %d is not valid UTF-8: %q", label, name, i, l)
			}
		}
	}
	return plain(m.View())
}

func TestEveryScreenFillsTheTerminal(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, strings.Repeat("very-long-directory-name-", 6))
	os.MkdirAll(deep, 0755)
	longLine := "package main // " + strings.Repeat("x", 150) + "\n"
	writeTestFile(t, filepath.Join(deep, "a.go"), strings.Repeat(longLine, 200))
	writeTestFile(t, filepath.Join(deep, "réservé-élève-"+strings.Repeat("é", 80)+".txt"), "x")
	writeTestFile(t, filepath.Join(deep, "日本語のとても長いファイル名"+strings.Repeat("字", 40)+".txt"), "x")
	os.Mkdir(filepath.Join(deep, "folder"), 0755)

	cfg := config.DefaultConfig()
	cfg.Plugins = []plugins.Plugin{{Name: "a-plugin-with-a-long-name", Key: "ctrl+g", Command: "true", Description: strings.Repeat("described ", 20)}}

	sizes := []tea.WindowSizeMsg{{Width: 140, Height: 40}, {Width: 100, Height: 24}, {Width: 80, Height: 24}, {Width: 60, Height: 15}, {Width: 30, Height: 10}}
	for _, size := range sizes {
		m := newTestModel(t, deep, cfg)
		m.bookmarks.Add("deep", deep)
		updated, _ := m.Update(size)
		m = updated.(Model)
		label := fmt.Sprintf("%dx%d", size.Width, size.Height)

		lines := assertFills(t, label+" browse", m)
		if !strings.Contains(lines[0], " 1 ") {
			t.Errorf("%s: tabs missing, first line = %q", label, lines[0])
		}
		if !strings.Contains(lines[len(lines)-2], "NORMAL") {
			t.Errorf("%s: status bar missing, got %q", label, lines[len(lines)-2])
		}

		m.tab().PreviewEnabled = false
		assertFills(t, label+" preview off", m)
		m.tab().PreviewEnabled = true

		long := m
		long.statusMsg = strings.Repeat("a long status message ", 10)
		assertFills(t, label+" long status", long)

		for _, keys := range []string{"/" + strings.Repeat("query", 20), "/a", " j ", "r" + strings.Repeat("name", 30), "nnew", "d", "b", "P", "!" + strings.Repeat("echo ", 30), "?",
			"f" + strings.Repeat("query", 30), "F" + strings.Repeat("text", 30)} {
			screen := m
			for _, r := range keys {
				screen, _ = press(t, screen, string(r))
			}
			assertFills(t, label+" after "+keys[:1], screen)
		}

		withTabs := m
		for i := 0; i < 12; i++ {
			updated, _ := withTabs.createTab(deep)
			withTabs = updated.(Model)
		}
		lines = assertFills(t, label+" many tabs", withTabs)
		if !strings.Contains(lines[0], " 13 ") {
			t.Errorf("%s: active tab 13 not visible in %q", label, lines[0])
		}
	}
}

func TestPanesFollowTerminalWidth(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "current")
	os.Mkdir(dir, 0755)
	os.Mkdir(filepath.Join(root, "sibling"), 0755)
	writeTestFile(t, filepath.Join(dir, "a.txt"), "hello preview")

	for _, c := range []struct {
		width           int
		parent, preview bool
	}{{140, true, true}, {100, true, true}, {99, false, true}, {72, false, true}, {71, false, false}} {
		m := newTestModel(t, dir, nil)
		updated, _ := m.Update(tea.WindowSizeMsg{Width: c.width, Height: 24})
		m = updated.(Model)

		l := m.layout()
		if (l.parentW > 0) != c.parent || (l.previewW > 0) != c.preview {
			t.Errorf("width %d: parent=%d preview=%d, want parent=%v preview=%v", c.width, l.parentW, l.previewW, c.parent, c.preview)
		}
		if l.parentW+l.listW+l.previewW != c.width {
			t.Errorf("width %d: panes add up to %d", c.width, l.parentW+l.listW+l.previewW)
		}
		view := strings.Join(plain(m.View()), "\n")
		if strings.Contains(view, "sibling") != c.parent {
			t.Errorf("width %d: parent pane shown=%v, want %v", c.width, !c.parent, c.parent)
		}
		if strings.Contains(view, "hello preview") != c.preview {
			t.Errorf("width %d: preview shown=%v, want %v", c.width, !c.preview, c.preview)
		}
	}
}

func TestParentPaneFollowsNavigation(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "a", "inner"), 0755)
	os.Mkdir(filepath.Join(root, "b"), 0755)

	m := newTestModel(t, filepath.Join(root, "a"), nil)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m = updated.(Model)
	if len(m.tab().ParentFiles) != 2 {
		t.Fatalf("parent pane has %d entries, want a and b", len(m.tab().ParentFiles))
	}

	m, cmd := press(t, m, "l")
	m = drain(t, m, cmd)
	if names := m.tab().ParentFiles; len(names) != 1 || names[0].Name != "inner" {
		t.Fatalf("after entering inner, parent pane = %v", names)
	}
}

func TestPreviewWidthIsPreviewShare(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), strings.Repeat("x", 300))
	cfg := config.DefaultConfig()
	cfg.PreviewWidth = 70

	m := newTestModel(t, dir, cfg)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 24})
	if l := updated.(Model).layout(); l.previewW != 63 || l.listW != 27 {
		t.Fatalf("preview=%d list=%d, want 63 and 27 (preview_width: 70)", l.previewW, l.listW)
	}
}

func TestBreadcrumbShortensHome(t *testing.T) {
	m := newTestModel(t, t.TempDir(), nil)
	home, _ := os.UserHomeDir()
	project := filepath.Join(home, "code", "sushi")
	os.MkdirAll(project, 0755)
	m.tab().CurrentPath = project

	if got := strings.TrimSpace(ansi.Strip(m.renderHeader())); !strings.HasPrefix(got, "~ / code / sushi") || !strings.Contains(got, "sort name") {
		t.Fatalf("breadcrumb = %q", got)
	}
	// A narrow terminal keeps the current directory
	m.width = 20
	if got := ansi.Strip(m.renderHeader()); !strings.Contains(got, "sushi") || lipgloss.Width(got) != 20 {
		t.Fatalf("narrow breadcrumb = %q", got)
	}
}

func TestStatusBarShowsMode(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "")
	writeTestFile(t, filepath.Join(dir, "b.txt"), "")

	m := newTestModel(t, dir, nil)
	for _, c := range []struct{ keys, badge, hint string }{
		{"", "NORMAL", "rename"},
		{" ", "SELECT", "invert"},
		{"/", "SEARCH", "esc cancel"},
		{"r", "RENAME", "save"},
		{"n", "NEW", "New file:"},
		{"d", "CONFIRM", "keep"},
		{"b", "BOOKMARKS", "close"},
		{"P", "RUN", "run"},
		{"?", "KEYS", "close"},
	} {
		screen := m
		for _, r := range c.keys {
			screen, _ = press(t, screen, string(r))
		}
		lines := plain(screen.View())
		status, bottom := lines[len(lines)-2], lines[len(lines)-1]
		if !strings.Contains(status, c.badge) || !strings.Contains(bottom, c.hint) {
			t.Errorf("after %q: status %q and bottom row %q, want %s and %q", c.keys, strings.TrimSpace(status), strings.TrimSpace(bottom), c.badge, c.hint)
		}
	}

	m, _ = press(t, m, "c")
	if status := ansi.Strip(m.renderStatusBar()); !strings.Contains(status, "2 items") || !strings.Contains(status, "clipboard: copy 1") {
		t.Fatalf("status = %q", status)
	}
}

func TestSearchHidesOtherFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"main.go", "Makefile", "README.md", "go.sum"} {
		writeTestFile(t, filepath.Join(dir, name), "")
	}

	m := typeQuery(t, newTestModel(t, dir, nil), "ma")
	view := strings.Join(plain(m.View()), "\n")
	for _, name := range []string{"main.go", "Makefile"} {
		if !strings.Contains(view, name) {
			t.Errorf("%s matches but is not listed", name)
		}
	}
	list := strings.Join(plain(strings.Join(m.renderFileList(60, 10, false), "\n")), "\n")
	for _, name := range []string{"README.md", "go.sum"} {
		if strings.Contains(list, name) {
			t.Errorf("%s doesn't match but is still listed", name)
		}
	}
	if !strings.Contains(view, "2 of 4 match") || !strings.Contains(view, "/ ma") {
		t.Errorf("status or query missing:\n%s", view)
	}

	// The matched letters are the ones underlined
	if got := fuzzyPositions("ma", "main.go"); !got[0] || !got[1] || len(got) != 2 {
		t.Fatalf("positions in main.go = %v, want 0 and 1", got)
	}
	if fuzzyPositions("zz", "main.go") != nil {
		t.Fatal("no match should give no positions")
	}

	// Enter keeps the cursor on the match and shows everything again
	m, _ = press(t, m, "enter")
	if name := m.tab().Files[m.tab().Cursor].Name; name != "Makefile" && name != "main.go" {
		t.Fatalf("cursor on %s", name)
	}
	if !strings.Contains(strings.Join(plain(m.View()), "\n"), "go.sum") {
		t.Fatal("list still filtered after the search ended")
	}
}

func TestRenameHappensInTheRow(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "draft.txt"), "")
	writeTestFile(t, filepath.Join(dir, "other.txt"), "")

	m := newTestModel(t, dir, nil)
	m, _ = press(t, m, "r")
	m = typeText(t, m, "-v2")
	lines := plain(m.View())
	row := ""
	for _, l := range lines[2 : len(lines)-2] {
		if strings.Contains(l, "draft-v2") {
			row = l
		}
	}
	if row == "" || !strings.Contains(row, "draft-v2") || !strings.Contains(row, ".txt") {
		t.Fatalf("the row being renamed should show the new name:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.Contains(strings.Join(lines, "\n"), "other.txt") {
		t.Fatal("other files should stay visible while renaming")
	}
}

func TestPreviewScrollKeys(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&b, "line number %d\n", i)
	}
	writeTestFile(t, filepath.Join(dir, "a.txt"), b.String())
	writeTestFile(t, filepath.Join(dir, "b.txt"), "short")

	m := newTestModel(t, dir, nil)
	m, _ = press(t, m, "J")
	if m.tab().PreviewScroll == 0 {
		t.Fatal("J did not scroll the preview")
	}
	if view := strings.Join(plain(m.View()), "\n"); strings.Contains(view, "line number 1\n") || !strings.Contains(view, "of 100") {
		t.Fatalf("preview did not move:\n%s", view)
	}
	for i := 0; i < 50; i++ {
		m, _ = press(t, m, "J")
	}
	if want := m.tab().Preview.MaxScroll(m.previewRows()); m.tab().PreviewScroll != want {
		t.Fatalf("scroll = %d, want it to stop at %d", m.tab().PreviewScroll, want)
	}
	m, _ = press(t, m, "K")
	if m.tab().PreviewScroll >= m.tab().Preview.MaxScroll(m.previewRows()) {
		t.Fatal("K did not scroll back")
	}

	// Another file starts from the top
	m, cmd := press(t, m, "j")
	m = drain(t, m, cmd)
	if m.tab().PreviewScroll != 0 {
		t.Fatalf("scroll = %d after moving to another file", m.tab().PreviewScroll)
	}
}

func TestSelectionIsSummarisedInPreview(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "12345")
	writeTestFile(t, filepath.Join(dir, "b.txt"), "12345")
	writeTestFile(t, filepath.Join(dir, "c.txt"), "unselected content")

	m := newTestModel(t, dir, nil)
	m, _ = press(t, m, " ")
	m, _ = press(t, m, " ")
	view := strings.Join(plain(m.View()), "\n")
	if !strings.Contains(view, "2 selected") || !strings.Contains(view, "10 B in 2 here") {
		t.Fatalf("selection summary missing:\n%s", view)
	}
}

func TestDialogsKeepTheBrowserVisible(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "doomed.txt"), "")
	writeTestFile(t, filepath.Join(dir, "zz-bystander.txt"), "")

	m := newTestModel(t, dir, nil)
	m, _ = press(t, m, "d")
	view := strings.Join(plain(m.View()), "\n")
	for _, want := range []string{"Confirm delete", "Delete file 'doomed.txt'?", "There is no undo", "zz-bystander.txt", "CONFIRM"} {
		if !strings.Contains(view, want) {
			t.Errorf("confirm screen is missing %q:\n%s", want, view)
		}
	}
}

func TestKeyPanel(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "")
	writeTestFile(t, filepath.Join(dir, "b.txt"), "")

	for _, size := range []tea.WindowSizeMsg{{Width: 200, Height: 60}, {Width: 100, Height: 24}, {Width: 60, Height: 15}, {Width: 30, Height: 10}} {
		m := newTestModel(t, dir, nil)
		updated, _ := m.Update(size)
		m, _ = press(t, updated.(Model), "?")
		label := fmt.Sprintf("%dx%d", size.Width, size.Height)

		// The panel covers the full width on every row, so nothing behind
		// it shows through
		for i, l := range strings.Split(m.renderHelpView(), "\n") {
			if w := lipgloss.Width(l); w != m.width {
				t.Errorf("%s: panel row %d is %d wide, want %d", label, i, w, m.width)
			}
		}

		// Every shortcut can be reached, by scrolling if need be
		seen := ""
		for i := 0; i <= m.maxHelpScroll(); i++ {
			seen += ansi.Strip(m.renderHelpView()) + "\n"
			m, _ = press(t, m, "j")
		}
		if m.mode != ModeHelp && m.maxHelpScroll() > 0 {
			t.Errorf("%s: j should scroll the panel, not close it", label)
		}
		for _, group := range helpGroups {
			for _, k := range group.keys {
				if !strings.Contains(seen, utils.Truncate(k.key, helpKeyW)) {
					t.Errorf("%s: key %q is unreachable", label, k.key)
				}
			}
		}
	}

	// Every key binding is documented in the panel
	m := newTestModel(t, dir, nil)
	documented := ""
	for _, group := range helpGroups {
		for _, k := range group.keys {
			documented += " " + k.key + " "
		}
	}
	for used := range m.keys.usedKeys() {
		switch used {
		case "up", "down", "left", "right", "home", "end", "pgup", "pgdown", "backspace", "ctrl+c":
			continue // Alternatives to a documented key
		case "ctrl+u", "ctrl+d":
			used = strings.TrimPrefix(used, "ctrl+")
		}
		if len(used) == 1 && used >= "1" && used <= "9" {
			used = "1-9"
		}
		if used == " " {
			used = "space"
		}
		if !strings.Contains(documented, used) {
			t.Errorf("key %q is not in the key panel", used)
		}
	}

	// Esc closes the panel; any other key closes it and does its job
	m, _ = press(t, m, "?")
	if m, _ = press(t, m, "esc"); m.mode != ModeNormal || m.tab().Cursor != 0 {
		t.Fatalf("esc: mode=%v cursor=%d", m.mode, m.tab().Cursor)
	}
	m, _ = press(t, m, "?")
	if m, _ = press(t, m, "j"); m.mode != ModeNormal || m.tab().Cursor != 1 {
		t.Fatalf("j from the panel: mode=%v cursor=%d, want the cursor moved down", m.mode, m.tab().Cursor)
	}
}

func TestTogglingPreviewOnLoadsCurrentFile(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "hello")
	cfg := config.DefaultConfig()
	cfg.PreviewEnabled = false

	m := newTestModel(t, dir, cfg)
	m, cmd := press(t, m, "p")
	if !m.tab().PreviewEnabled || cmd == nil {
		t.Fatal("preview not enabled or no load issued")
	}
	// The first command in the batch is the preview load; the second is the status timer
	updated, _ := m.Update(cmd().(tea.BatchMsg)[0]())
	m = updated.(Model)
	if !strings.Contains(m.tab().Preview.Content, "hello") {
		t.Fatalf("preview content = %q, want the file", m.tab().Preview.Content)
	}
}

func typeQuery(t *testing.T, m Model, query string) Model {
	t.Helper()
	m, _ = press(t, m, "/")
	for _, r := range query {
		m, _ = press(t, m, string(r))
	}
	return m
}

func TestSearchAcceptsSpaces(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "annual report.pdf"), "")
	writeTestFile(t, filepath.Join(dir, "annualreport.pdf"), "")

	m := typeQuery(t, newTestModel(t, dir, nil), "l r")
	if m.tab().SearchQuery != "l r" {
		t.Fatalf("query = %q, want %q", m.tab().SearchQuery, "l r")
	}
	if len(m.tab().SearchResults) != 1 || m.tab().Files[m.tab().Cursor].Name != "annual report.pdf" {
		t.Fatalf("results = %v, want only the name with a space", m.tab().SearchResults)
	}
}

func TestSearchBackspaceRemovesWholeCharacter(t *testing.T) {
	m := typeQuery(t, newTestModel(t, t.TempDir(), nil), "élè")
	m, _ = press(t, m, "backspace")
	if q := m.tab().SearchQuery; q != "él" || !utf8.ValidString(q) {
		t.Fatalf("query = %q, want %q", q, "él")
	}
}

func TestSearchMatchesAccentsExactly(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "élève.txt"), "")
	// "é" is bytes C3 A9; a byte-wise match finds C3 in "è" and A9 in "©"
	writeTestFile(t, filepath.Join(dir, "crème ©.txt"), "")

	m := typeQuery(t, newTestModel(t, dir, nil), "é")
	if len(m.tab().SearchResults) != 1 || m.tab().Files[m.tab().Cursor].Name != "élève.txt" {
		t.Fatalf("results = %v, want only élève.txt", m.tab().SearchResults)
	}
}

func TestEnterSymlinkedDirectory(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	os.Mkdir(target, 0755)
	writeTestFile(t, filepath.Join(target, "inside.txt"), "x")
	sub := filepath.Join(root, "browse")
	os.Mkdir(sub, 0755)
	os.Symlink(target, filepath.Join(sub, "link"))

	m := newTestModel(t, sub, nil)
	m, cmd := press(t, m, "l")
	if cmd == nil {
		t.Fatal("entering a symlinked directory did nothing")
	}
	updated, _ := m.Update(cmd())
	m = updated.(Model)
	if len(m.tab().Files) != 1 || m.tab().Files[0].Name != "inside.txt" {
		t.Fatalf("files = %v, want inside.txt", m.tab().Files)
	}
}

func TestToggleHiddenFiles(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".env"), "")
	writeTestFile(t, filepath.Join(dir, "main.go"), "")

	m := newTestModel(t, dir, nil)
	if len(m.tab().Files) != 1 {
		t.Fatalf("show_hidden: false listed %d files, want 1", len(m.tab().Files))
	}

	m, cmd := press(t, m, ".")
	for _, c := range cmd().(tea.BatchMsg)[1:] { // Skip the status timer
		updated, _ := m.Update(c())
		m = updated.(Model)
	}
	if len(m.tab().Files) != 2 {
		t.Fatalf("after toggling, listed %d files, want 2", len(m.tab().Files))
	}
}

func TestConfirmDeleteSetting(t *testing.T) {
	for _, confirm := range []bool{true, false} {
		dir := t.TempDir()
		f := filepath.Join(dir, "doomed.txt")
		writeTestFile(t, f, "")
		cfg := config.DefaultConfig()
		cfg.ConfirmDelete = confirm

		m := newTestModel(t, dir, cfg)
		m, cmd := press(t, m, "d")
		if confirm {
			if m.mode != ModeConfirm {
				t.Fatal("confirm_delete: true did not ask for confirmation")
			}
			continue
		}
		if m.mode == ModeConfirm || cmd == nil {
			t.Fatal("confirm_delete: false still asked for confirmation")
		}
		cmd()
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Fatal("file was not deleted")
		}
	}
}

func TestBookmarksMoveWithJK(t *testing.T) {
	m := newTestModel(t, t.TempDir(), nil)
	for _, p := range []string{"/a", "/b", "/c"} {
		m.bookmarks.Add(filepath.Base(p), p)
	}
	m, _ = press(t, m, "b")
	for _, k := range []string{"j", "j", "j", "k"} {
		m, _ = press(t, m, k)
	}
	if m.bookmarkCursor != 1 {
		t.Fatalf("bookmarkCursor = %d, want 1 (j j j clamps at 2, then k)", m.bookmarkCursor)
	}
}

func TestThemeFromConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Theme = "light"
	cfg.Colors = map[string]string{"directory": "33"}

	m := newTestModel(t, t.TempDir(), cfg)
	if m.theme.Name != "light" || m.theme.Directory != "33" || m.theme.Syntax != "sushi-light" {
		t.Fatalf("theme = %s directory=%s syntax=%s", m.theme.Name, m.theme.Directory, m.theme.Syntax)
	}
	if m.statusMsg != "" {
		t.Fatalf("unexpected startup status %q", m.statusMsg)
	}

	cfg.SyntaxTheme = "nord"
	if m := newTestModel(t, t.TempDir(), cfg); m.theme.Syntax != "nord" {
		t.Fatalf("syntax_theme ignored: %s", m.theme.Syntax)
	}
}

func TestConfigProblemsShownAtStartup(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Theme = "neon"
	cfg.SyntaxTheme = "nope"

	m := newTestModel(t, t.TempDir(), cfg)
	if !strings.Contains(m.statusMsg, "neon") || !strings.Contains(m.statusMsg, "nope") {
		t.Fatalf("statusMsg = %q, want both problems", m.statusMsg)
	}
	if m.Init() == nil {
		t.Fatal("Init should schedule clearing the startup warning")
	}
}

// run executes cmd and feeds its message back, returning the updated model
func run(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command")
	}
	updated, _ := m.Update(cmd())
	return updated.(Model)
}

func TestSelectCopyPasteMultipleFiles(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		writeTestFile(t, filepath.Join(src, name), name)
	}

	m := newTestModel(t, src, nil)
	m, _ = press(t, m, " ") // a, cursor moves to b
	m, _ = press(t, m, "j") // skip b
	m, _ = press(t, m, " ") // c
	if len(m.tab().Selected) != 2 {
		t.Fatalf("selected %d files, want 2", len(m.tab().Selected))
	}
	if line := m.renderFileLine(m.tab().Files[0], false, nil, listColumns(80, m.tab().Files)); !strings.Contains(line, "●") {
		t.Fatalf("selected file has no marker: %q", line)
	}
	if !strings.Contains(m.renderStatusBar(), "2 selected") {
		t.Fatalf("status bar = %q", m.renderStatusBar())
	}

	m, _ = press(t, m, "c")
	if len(m.clipboard) != 2 || len(m.tab().Selected) != 0 {
		t.Fatalf("clipboard=%v selected=%d, want 2 items and a cleared selection", m.clipboard, len(m.tab().Selected))
	}

	m.tab().CurrentPath = dst
	m, cmd := press(t, m, "v")
	run(t, m, cmd)
	for _, name := range []string{"a.txt", "c.txt"} {
		if b, err := os.ReadFile(filepath.Join(dst, name)); err != nil || string(b) != name {
			t.Fatalf("%s not copied: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dst, "b.txt")); err == nil {
		t.Fatal("unselected b.txt was copied")
	}
}

func TestInvertAndClearSelection(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		writeTestFile(t, filepath.Join(dir, name), "")
	}

	m := newTestModel(t, dir, nil)
	m, _ = press(t, m, " ")
	m, _ = press(t, m, "*")
	if got := len(m.tab().Selected); got != 2 || m.tab().Selected[m.tab().Files[0].Path] {
		t.Fatalf("after invert: %d selected, want b and c", got)
	}
	m, _ = press(t, m, "u")
	if len(m.tab().Selected) != 0 {
		t.Fatal("u did not clear the selection")
	}
}

func TestDeleteSelection(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"keep", "x1", "x2"} {
		writeTestFile(t, filepath.Join(dir, name), "")
	}

	m := newTestModel(t, dir, nil)
	m, _ = press(t, m, "j")
	m, _ = press(t, m, " ")
	m, _ = press(t, m, " ")
	m, _ = press(t, m, "d")
	if m.mode != ModeConfirm || !strings.Contains(ansi.Strip(m.renderConfirmDialog()), "Delete 2 items?") {
		t.Fatalf("mode=%v dialog:\n%s", m.mode, m.renderConfirmDialog())
	}
	m, cmd := press(t, m, "y")
	run(t, m, cmd)

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "keep" {
		t.Fatalf("left %v, want only keep", entries)
	}
}

func TestPasteAsksOnceForAllConflicts(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	for _, name := range []string{"a", "b"} {
		writeTestFile(t, filepath.Join(src, name), "new")
		writeTestFile(t, filepath.Join(dst, name), "old")
	}

	m := newTestModel(t, src, nil)
	m, _ = press(t, m, "*")
	m, _ = press(t, m, "c")
	m.tab().CurrentPath = dst
	m, _ = press(t, m, "v")
	if m.mode != ModeConfirm || !strings.Contains(ansi.Strip(m.renderConfirmDialog()), "2 items already exist") {
		t.Fatalf("dialog:\n%s", m.renderConfirmDialog())
	}
	m, cmd := press(t, m, "y")
	run(t, m, cmd)
	if b, _ := os.ReadFile(filepath.Join(dst, "b")); string(b) != "new" {
		t.Fatalf("b = %q, want overwritten", b)
	}
}

func TestPasteRefusesDuplicateNames(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"one", "two", "dest"} {
		os.Mkdir(filepath.Join(root, d), 0755)
	}
	writeTestFile(t, filepath.Join(root, "one", "same.txt"), "1")
	writeTestFile(t, filepath.Join(root, "two", "same.txt"), "2")

	m := newTestModel(t, root, nil)
	m.clipboard = []string{filepath.Join(root, "one", "same.txt"), filepath.Join(root, "two", "same.txt")}
	m.clipboardMode = "copy"
	m.tab().CurrentPath = filepath.Join(root, "dest")
	m, _ = press(t, m, "v")
	if !strings.Contains(m.statusMsg, "same name") {
		t.Fatalf("statusMsg = %q, want a same-name refusal", m.statusMsg)
	}
}

func TestBatchResultReportsPartialFailure(t *testing.T) {
	msg := batchResult("delete", "Deleted", []string{"/a", "/b", "/c"}, func(p string) error {
		if p == "/b" {
			return os.ErrPermission
		}
		return nil
	})
	if msg.err == nil || !strings.Contains(msg.err.Error(), "Deleted 2 of 3 items") || !strings.Contains(msg.err.Error(), "b:") {
		t.Fatalf("err = %v", msg.err)
	}
}

func TestCutClearsMovedItemsFromClipboard(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(src, "a"), "")

	m := newTestModel(t, src, nil)
	m, _ = press(t, m, "x")
	m.tab().CurrentPath = dst
	m, cmd := press(t, m, "v")
	m = run(t, m, cmd)
	if len(m.clipboard) != 0 || m.clipboardMode != "" {
		t.Fatalf("clipboard = %v (%s), want empty after the move", m.clipboard, m.clipboardMode)
	}
}

// typeText types s into the model one character at a time
func typeText(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m, _ = press(t, m, string(r))
	}
	return m
}

// drain runs cmd and everything it leads to, feeding each result back into
// the model. A command that hangs fails the test rather than blocking it.
func drain(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				m = drain(t, m, c)
			}
			return m
		}
		updated, next := m.Update(msg)
		return drain(t, updated.(Model), next)
	case <-time.After(5 * time.Second):
		t.Fatal("command did not finish")
		return m
	}
}

// submit presses Enter and applies everything that follows
func submit(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := press(t, m, "enter")
	return drain(t, m, cmd)
}

func TestRenameFile(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "draft.txt"), "")

	m := newTestModel(t, dir, nil)
	m.bookmarks.Add("draft", filepath.Join(dir, "draft.txt"))
	m, _ = press(t, m, "c") // Clipboard should follow the rename
	m, _ = press(t, m, "r")
	if m.mode != ModeInput || m.prompt.input.Value() != "draft.txt" {
		t.Fatalf("mode=%v value=%q", m.mode, m.prompt.input.Value())
	}
	// The cursor starts before the extension
	m = typeText(t, m, "-v2")
	m = submit(t, m)

	renamed := filepath.Join(dir, "draft-v2.txt")
	if _, err := os.Stat(renamed); err != nil {
		t.Fatalf("rename failed: %v (status %q)", err, m.statusMsg)
	}
	if m.mode != ModeNormal || m.tab().Files[m.tab().Cursor].Path != renamed {
		t.Fatal("prompt not closed or cursor not on the renamed file")
	}
	if m.clipboard[0] != renamed || m.bookmarks.Get(0).Path != renamed {
		t.Fatalf("clipboard=%v bookmark=%s, want both retargeted", m.clipboard, m.bookmarks.Get(0).Path)
	}
}

func TestRenameErrorKeepsPromptOpen(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a"), "")
	writeTestFile(t, filepath.Join(dir, "b"), "")

	m := newTestModel(t, dir, nil)
	m, _ = press(t, m, "r")
	m, _ = press(t, m, "backspace")
	m = typeText(t, m, "b")
	m, _ = press(t, m, "enter")
	if m.mode != ModeInput || !strings.Contains(m.prompt.err, "already exists") {
		t.Fatalf("mode=%v err=%q", m.mode, m.prompt.err)
	}
	if !strings.Contains(ansi.Strip(m.renderMainView()), "already exists") {
		t.Fatal("error not shown beside the name")
	}
	m, _ = press(t, m, "esc")
	if m.mode != ModeNormal {
		t.Fatal("Esc did not close the prompt")
	}
}

func TestRenamingDirectoryMovesTabsInsideIt(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "old", "sub"), 0755)

	m := newTestModel(t, root, nil)
	updated, _ := m.createTab(filepath.Join(root, "old", "sub"))
	m = updated.(Model)
	m, _ = press(t, m, "tab") // Back to tab 1, cursor on "old"
	m, _ = press(t, m, "r")
	m, _ = press(t, m, "ctrl+u")
	m.prompt.input = components.NewTextInput("new")
	m = submit(t, m)

	if got := m.tabs[1].CurrentPath; got != filepath.Join(root, "new", "sub") {
		t.Fatalf("tab 2 path = %s, want it moved with the rename", got)
	}
}

func TestCreateFileAndDirectory(t *testing.T) {
	dir := t.TempDir()
	m := newTestModel(t, dir, nil)

	m, _ = press(t, m, "n")
	m = typeText(t, m, "notes.md")
	m = submit(t, m)
	if _, err := os.Stat(filepath.Join(dir, "notes.md")); err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if m.tab().Files[m.tab().Cursor].Name != "notes.md" {
		t.Fatal("cursor not on the new file")
	}

	m, _ = press(t, m, "N")
	m = typeText(t, m, "assets")
	m = submit(t, m)
	if info, err := os.Stat(filepath.Join(dir, "assets")); err != nil || !info.IsDir() {
		t.Fatalf("directory not created: %v", err)
	}

	m, _ = press(t, m, "n")
	m = typeText(t, m, "build/")
	m = submit(t, m)
	if info, err := os.Stat(filepath.Join(dir, "build")); err != nil || !info.IsDir() {
		t.Fatal("trailing slash did not create a directory")
	}
}

func TestOpenerChoosesEditorForText(t *testing.T) {
	dir := t.TempDir()
	text := filepath.Join(dir, "notes.txt")
	empty := filepath.Join(dir, "empty")
	binary := filepath.Join(dir, "photo.png")
	writeTestFile(t, text, "hello")
	writeTestFile(t, empty, "")
	writeTestFile(t, binary, "\x89PNG\x00\x00")

	info := func(path string) fs.FileInfo {
		st, _ := os.Stat(path)
		return fs.NewFileInfo(path, st)
	}
	for _, c := range []struct {
		opener string
		path   string
		editor bool
	}{
		{"auto", text, true},
		{"auto", empty, true},
		{"auto", binary, false},
		{"editor", binary, true},
		{"system", text, false},
	} {
		cfg := config.DefaultConfig()
		cfg.Opener = c.opener
		m := newTestModel(t, dir, cfg)
		if got := m.useEditor(info(c.path)); got != c.editor {
			t.Errorf("opener=%s %s: editor=%v, want %v", c.opener, filepath.Base(c.path), got, c.editor)
		}
	}
}

// fakeOpener puts a stand-in for the system opener on PATH that records the
// path it was asked to open
func fakeOpener(t *testing.T) (record string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script")
	}
	bin := t.TempDir()
	record = filepath.Join(bin, "opened")
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	script := "#!/bin/sh\nprintf '%s' \"$1\" > " + record + "\n"
	if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	return record
}

func TestOpenWithSystemApp(t *testing.T) {
	record := fakeOpener(t)
	dir := t.TempDir()
	photo := filepath.Join(dir, "photo.png")
	writeTestFile(t, photo, "\x00binary")

	m := newTestModel(t, dir, nil)
	m, cmd := press(t, m, "enter") // Binary, so auto uses the system opener
	m = drain(t, m, cmd)

	if b, err := os.ReadFile(record); err != nil || string(b) != photo {
		t.Fatalf("opener got %q (%v), want %s", b, err, photo)
	}
	if strings.Contains(m.statusMsg, "failed") {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
}

func TestOpenFailureIsReported(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // No opener at all
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.bin"), "\x00")

	m := newTestModel(t, dir, nil)
	m, cmd := press(t, m, "o")
	m = drain(t, m, cmd)
	if !strings.Contains(m.statusMsg, "failed") {
		t.Fatalf("statusMsg = %q, want the failure reported", m.statusMsg)
	}
}

func TestEnterOnDirectoryStillNavigates(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "sub"), 0755)

	m := newTestModel(t, root, nil)
	m, cmd := press(t, m, "enter")
	m = drain(t, m, cmd)
	if filepath.Base(m.tab().CurrentPath) != "sub" {
		t.Fatalf("CurrentPath = %s", m.tab().CurrentPath)
	}
}

func skipWithoutSh(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("plugins here are sh commands")
	}
}

func TestBackgroundPluginSendsInstructions(t *testing.T) {
	skipWithoutSh(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	os.Mkdir(target, 0755)
	writeTestFile(t, filepath.Join(target, "found.txt"), "")

	cfg := config.DefaultConfig()
	cfg.Plugins = []plugins.Plugin{{
		Name: "jump", Key: "Z", Mode: plugins.ModeBackground,
		Command: `echo "cd target/found.txt" > "$SUSHI_CMD_FILE"; echo "working..."; echo "jumped"`,
	}}
	m := newTestModel(t, dir, cfg)
	m, cmd := press(t, m, "Z")
	m = drain(t, m, cmd)

	if m.tab().CurrentPath != target || m.tab().Files[m.tab().Cursor].Name != "found.txt" {
		t.Fatalf("path=%s, want %s with found.txt focused", m.tab().CurrentPath, target)
	}
	if m.statusMsg != "jumped" {
		t.Fatalf("statusMsg = %q, want the last line of output", m.statusMsg)
	}
}

func TestPluginGetsSelection(t *testing.T) {
	skipWithoutSh(t)
	dir := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		writeTestFile(t, filepath.Join(dir, name), "")
	}
	out := filepath.Join(t.TempDir(), "args")

	cfg := config.DefaultConfig()
	cfg.Plugins = []plugins.Plugin{{
		Name: "list", Key: "L", Mode: plugins.ModeBackground,
		Command: `printf '%s\n' "$@" > ` + out + `; echo "select c" > "$SUSHI_CMD_FILE"`,
	}}
	m := newTestModel(t, dir, cfg)
	m, _ = press(t, m, " ") // a
	m, _ = press(t, m, " ") // b
	m, cmd := press(t, m, "L")
	m = drain(t, m, cmd)

	b, _ := os.ReadFile(out)
	want := filepath.Join(dir, "a") + "\n" + filepath.Join(dir, "b") + "\n"
	if string(b) != want {
		t.Fatalf("plugin got %q, want %q", b, want)
	}
	if !m.tab().Selected[filepath.Join(dir, "c")] {
		t.Fatal("select instruction was ignored")
	}
}

func TestPluginFailureIsReported(t *testing.T) {
	skipWithoutSh(t)
	cfg := config.DefaultConfig()
	cfg.Plugins = []plugins.Plugin{{Name: "broken", Key: "Z", Mode: plugins.ModeBackground, Command: "echo boom >&2; exit 3"}}

	m := newTestModel(t, t.TempDir(), cfg)
	m, cmd := press(t, m, "Z")
	m = drain(t, m, cmd)
	if !strings.Contains(m.statusMsg, "broken failed") || !strings.Contains(m.statusMsg, "boom") {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
}

func TestPluginKeyConflictsAreReported(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Plugins = []plugins.Plugin{
		{Name: "quitter", Key: "q", Command: "true"},
		{Name: "first", Key: "Z", Command: "true"},
		{Name: "second", Key: "Z", Command: "true"},
	}
	m := newTestModel(t, t.TempDir(), cfg)

	if !strings.Contains(m.statusMsg, `key "q" is used by sushi`) || !strings.Contains(m.statusMsg, "already used by first") {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
	if _, ok := m.pluginKeys["q"]; ok {
		t.Fatal("a plugin took over q")
	}
	if m.plugins[m.pluginKeys["Z"]].Name != "first" {
		t.Fatal("Z should stay with the first plugin")
	}
}

func TestScriptPluginsAreDiscovered(t *testing.T) {
	skipWithoutSh(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".config", "sushi", "plugins")
	os.MkdirAll(dir, 0755)
	script := "#!/bin/sh\n# sushi-key: ctrl+g\n# sushi-mode: background\n# sushi-description: Says hi\necho hi from script\n"
	os.WriteFile(filepath.Join(dir, "greet.sh"), []byte(script), 0755)

	updated, _ := NewModelWithConfig(t.TempDir(), config.DefaultConfig()).Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m := updated.(Model)
	if len(m.plugins) != 1 || m.plugins[0].Name != "greet" {
		t.Fatalf("plugins = %+v", m.plugins)
	}

	// Run it from the plugin menu
	m, _ = press(t, m, "P")
	menu := ansi.Strip(m.View())
	for _, want := range []string{"ctrl+g", "greet", "Says hi", "background"} {
		if !strings.Contains(menu, want) {
			t.Fatalf("menu is missing %q:\n%s", want, menu)
		}
	}
	m, cmd := press(t, m, "enter")
	m = drain(t, m, cmd)
	if m.mode != ModeNormal || m.statusMsg != "hi from script" {
		t.Fatalf("mode=%v statusMsg=%q", m.mode, m.statusMsg)
	}
}

func TestEmptyPluginMenuExplainsSetup(t *testing.T) {
	m := newTestModel(t, t.TempDir(), nil)
	m, _ = press(t, m, "P")
	if !strings.Contains(ansi.Strip(m.View()), "No plugins yet") {
		t.Fatalf("menu:\n%s", ansi.Strip(m.View()))
	}
	if m, _ = press(t, m, "esc"); m.mode != ModeNormal {
		t.Fatal("Esc did not close the menu")
	}
}

func TestRunPaletteTakesShellCommands(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Plugins = []plugins.Plugin{{Name: "first", Command: "true"}, {Name: "second", Command: "true"}}

	// ! starts on the command line, where every key is text
	m := newTestModel(t, t.TempDir(), cfg)
	m, _ = press(t, m, "!")
	m = typeText(t, m, "jq . | kq")
	if m.mode != ModePlugins || m.runInput.Value() != "jq . | kq" {
		t.Fatalf("mode=%v typed=%q", m.mode, m.runInput.Value())
	}
	if !strings.Contains(ansi.Strip(m.View()), "! jq . | kq") {
		t.Fatalf("command not shown:\n%s", ansi.Strip(m.View()))
	}
	// The command runs in the terminal via tea.Exec, which needs a real
	// program; check it was handed over
	m, cmd := press(t, m, "enter")
	if m.mode != ModeNormal || cmd == nil {
		t.Fatalf("mode=%v cmd=%v", m.mode, cmd)
	}

	// P starts on the plugin list, where j and k move
	m, _ = press(t, m, "P")
	m, _ = press(t, m, "j")
	if m.runInput.Value() != "" || m.pluginCursor != 1 {
		t.Fatalf("typed=%q cursor=%d, want j to move to the second plugin", m.runInput.Value(), m.pluginCursor)
	}
	// Tab moves to the command line and back
	m, _ = press(t, m, "tab")
	m = typeText(t, m, "ls")
	if m.runInput.Value() != "ls" {
		t.Fatalf("typed=%q after tab", m.runInput.Value())
	}
	m, _ = press(t, m, "esc")
	if m.mode != ModeNormal {
		t.Fatal("esc did not close the palette")
	}
}

func TestWaitCommandWaitsForEnter(t *testing.T) {
	skipWithoutSh(t)
	var out strings.Builder
	w := &waitCommand{Cmd: exec.Command("sh", "-c", "echo hello; exit 2")}
	w.SetStdin(strings.NewReader("\n"))
	w.SetStdout(&out)
	w.SetStderr(&out)

	err := w.Run()
	if err == nil {
		t.Fatal("exit status should be returned")
	}
	if got := out.String(); !strings.Contains(got, "hello") || !strings.Contains(got, "Press Enter") || !strings.Contains(got, "exit status 2") {
		t.Fatalf("output = %q", got)
	}
}
