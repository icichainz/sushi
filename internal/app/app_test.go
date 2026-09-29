package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/icichainz/sushi/internal/config"
	"github.com/icichainz/sushi/internal/ui/components"
)

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

// assertFits checks the layout fits the terminal and that the header and
// status bar both survived, i.e. nothing was pushed off screen. It checks
// the unclipped layout so View's final clip can't hide an oversized pane.
func assertFits(t *testing.T, label string, m Model, wantLast string) {
	t.Helper()
	lines := strings.Split(m.renderMainView(), "\n")
	if len(lines) > m.height {
		t.Errorf("%s: %d lines, terminal has %d", label, len(lines), m.height)
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w > m.width {
			t.Errorf("%s: line %d is %d wide, terminal has %d", label, i, w, m.width)
		}
		if !utf8.ValidString(l) {
			t.Errorf("%s: line %d is not valid UTF-8: %q", label, i, l)
		}
	}
	if !strings.Contains(lines[0], "📁") {
		t.Errorf("%s: header missing, first line = %q", label, lines[0])
	}
	if last := lines[len(lines)-1]; !strings.Contains(last, wantLast) {
		t.Errorf("%s: last line = %q, want it to contain %q", label, last, wantLast)
	}
}

func TestViewFitsTerminal(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, strings.Repeat("very-long-directory-name-", 6))
	os.MkdirAll(deep, 0755)
	longLine := "package main // " + strings.Repeat("x", 150) + "\n"
	writeTestFile(t, filepath.Join(deep, "a.go"), strings.Repeat(longLine, 200))
	writeTestFile(t, filepath.Join(deep, "réservé-élève-"+strings.Repeat("é", 80)+".txt"), "x")
	writeTestFile(t, filepath.Join(deep, "日本語のとても長いファイル名"+strings.Repeat("字", 40)+".txt"), "x")

	for _, size := range []tea.WindowSizeMsg{{Width: 100, Height: 24}, {Width: 60, Height: 15}, {Width: 30, Height: 10}} {
		m := newTestModel(t, deep, nil)
		updated, _ := m.Update(size)
		m = updated.(Model)
		label := fmt.Sprintf("%dx%d", size.Width, size.Height)

		assertFits(t, label+" preview on", m, "files |")

		m.tab().PreviewEnabled = false
		assertFits(t, label+" preview off", m, "files |")
		m.tab().PreviewEnabled = true

		m.statusMsg = strings.Repeat("a long status message ", 10)
		assertFits(t, label+" long status", m, "files |")

		searching, _ := press(t, m, "/")
		for _, r := range strings.Repeat("query", 20) {
			searching, _ = press(t, searching, string(r))
		}
		assertFits(t, label+" search", searching, "[")

		withTabs := m
		for i := 0; i < 12; i++ {
			updated, _ := withTabs.createTab(deep)
			withTabs = updated.(Model)
		}
		assertFits(t, label+" many tabs", withTabs, "files |")
	}
}

func TestHelpFitsTerminal(t *testing.T) {
	m := newTestModel(t, t.TempDir(), nil)
	m, _ = press(t, m, "?")
	lines := strings.Split(m.View(), "\n")
	if len(lines) > m.height {
		t.Fatalf("help is %d lines, terminal has %d", len(lines), m.height)
	}
	if !strings.Contains(m.View(), "Keyboard Shortcuts") || !strings.Contains(m.View(), "Press any key") {
		t.Fatal("help title or footer was cut off")
	}
}

func TestPreviewWidthIsPreviewShare(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), strings.Repeat("x", 300))
	cfg := config.DefaultConfig()
	cfg.PreviewWidth = 70

	m := newTestModel(t, dir, cfg)
	list := m.renderFileList(m.width - m.width*70/100)
	if w := lipgloss.Width(list); w != 30 {
		t.Fatalf("file list is %d wide, want 30 (preview_width: 70)", w)
	}
	if w := lipgloss.Width(m.renderSplitView()); w != m.width {
		t.Fatalf("split view is %d wide, want %d", w, m.width)
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
	if m.theme.Name != "light" || m.theme.Directory != "33" || m.theme.Syntax != "github" {
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
	if line := m.renderFileLine(m.tab().Files[0], false, true, 80); !strings.Contains(line, "*") {
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
	if m.mode != ModeConfirm || !strings.Contains(m.renderConfirmDialog(), "Delete 2 items?") {
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
	if m.mode != ModeConfirm || !strings.Contains(m.renderConfirmDialog(), "2 items already exist") {
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
// the model. Commands that don't finish quickly, such as the timers that
// clear status messages, are skipped.
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
	case <-time.After(200 * time.Millisecond):
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
	if !strings.Contains(m.renderMainView(), "already exists") {
		t.Fatal("error not shown in the prompt bar")
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
