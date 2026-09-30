package app

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/icichainz/sushi/internal/config"
	"github.com/icichainz/sushi/internal/plugins"
	"gopkg.in/yaml.v3"
)

// withKeys builds a test model whose config remaps keys
func withKeys(t *testing.T, dir string, keys map[string]config.KeyList) Model {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Keys = keys
	return newTestModel(t, dir, cfg)
}

// send delivers a key message press can't describe, such as alt+s
func send(t *testing.T, m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	return updated.(Model), cmd
}

// threeFiles returns a directory holding a.txt, b.txt and c.txt
func threeFiles(t *testing.T) string {
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		writeTestFile(t, filepath.Join(dir, name), "")
	}
	return dir
}

// wantProblems fails unless the status bar mentions every one of want
func wantProblems(t *testing.T, m Model, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(m.statusMsg, w) {
			t.Errorf("status bar lacks %q:\n%s", w, strings.ReplaceAll(m.statusMsg, "; ", "\n"))
		}
	}
}

func TestActionNames(t *testing.T) {
	k := DefaultKeyMap()
	names := make(map[string]bool)
	for _, a := range k.actions() {
		if names[a.name] {
			t.Errorf("two actions are called %s", a.name)
		}
		names[a.name] = true
		if len(a.binding.Keys()) == 0 || a.binding.Help().Desc == "" {
			t.Errorf("%s has no default keys or no description", a.name)
		}
	}
	if len(names) != reflect.TypeOf(k).NumField() {
		t.Errorf("%d actions for %d key map fields", len(names), reflect.TypeOf(k).NumField())
	}
	for _, name := range []string{"up", "page_down", "hard_delete", "paste_link", "preview_down", "new_tab_home", "quit_no_cd"} {
		if !names[name] {
			t.Errorf("no action called %s", name)
		}
	}
}

func TestDefaultKeysAreUnchanged(t *testing.T) {
	keys, problems := loadKeyMap(nil)
	if len(problems) > 0 {
		t.Fatalf("the defaults have problems: %q", problems)
	}
	defaults := DefaultKeyMap()
	for i, a := range keys.actions() {
		if want := defaults.actions()[i].binding.Keys(); !slices.Equal(a.binding.Keys(), want) {
			t.Errorf("%s = %q, want %q", a.name, a.binding.Keys(), want)
		}
	}
}

func TestParseKey(t *testing.T) {
	good := map[string]string{
		"k": "k", "G": "G", "?": "?", "é": "é", "+": "+", "1": "1",
		"space": " ", "Space": " ", " ": " ", "alt+space": "alt+ ",
		"enter": "enter", "Enter": "enter", "esc": "esc", "tab": "tab", "shift+tab": "shift+tab",
		"ctrl+r": "ctrl+r", "CTRL+R": "ctrl+r", "ctrl+@": "ctrl+@", "f5": "f5", "pgdown": "pgdown",
		"backspace": "backspace", "delete": "delete", "alt+x": "alt+x", "alt+X": "alt+X", "Alt+Enter": "alt+enter", "alt++": "alt++",
	}
	for in, want := range good {
		if got, err := parseKey(in); err != nil || got != want {
			t.Errorf("parseKey(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "kk", "ctrl+", "alt+", "ctrlr+k", "runes", "ctrl+shift+x", "\x01", "shift+a"} {
		if got, err := parseKey(in); err == nil {
			t.Errorf("parseKey(%q) = %q, want an error", in, got)
		}
	}
	if _, err := parseKey("shift+a"); err == nil || !strings.Contains(err.Error(), `"A"`) {
		t.Errorf("shift+a should suggest A: %v", err)
	}
}

func TestKeysLabel(t *testing.T) {
	b := func(keys ...string) key.Binding { return key.NewBinding(key.WithKeys(keys...)) }
	tests := []struct {
		sep      string
		bindings []key.Binding
		want     string
	}{
		{" ", []key.Binding{b("down", "j"), b("up", "k")}, "j k"},
		{" ", []key.Binding{b("pgup", "ctrl+u"), b("pgdown", "ctrl+d")}, "ctrl+u d"},
		{" ", []key.Binding{b("up")}, "up"}, // Only an arrow: then it is shown
		{" ", []key.Binding{b(" ")}, "space"},
		{" ", []key.Binding{b("c"), b(), b("v")}, "c v"}, // Unbound actions are left out
		{" ", []key.Binding{b("k"), b("ctrl+n")}, "k ctrl+n"},
		{" ", []key.Binding{b("alt++"), b("alt+-")}, "alt++ -"},
		// "ctrl+n k" would read as ctrl+n and ctrl+k
		{" ", []key.Binding{b("ctrl+n"), b("k")}, "ctrl+n, k"},
		{"/", []key.Binding{b("j"), b("k")}, "j/k"},
		{"/", []key.Binding{b("/"), b("k")}, "/, k"},
		{" ", []key.Binding{b(), b()}, ""},
	}
	for _, tt := range tests {
		if got := keysLabel(tt.sep, tt.bindings...); got != tt.want {
			t.Errorf("keysLabel(%q, %v) = %q, want %q", tt.sep, tt.bindings, got, tt.want)
		}
	}

	// A line for actions some of which are unbound describes those left
	k := DefaultKeyMap()
	k.PreviewUp.SetKeys()
	if h := keyHint("scroll preview", k.PreviewDown, k.PreviewUp); h != (hint{"J", "scroll preview down"}) {
		t.Errorf("keyHint = %+v", h)
	}
}

func TestRemappedKeysTriggerActions(t *testing.T) {
	m := withKeys(t, threeFiles(t), map[string]config.KeyList{
		"down":   {"ctrl+n", "down"},
		"hidden": {"H"}, // A single key, as the config can write it
		"quit":   {"ctrl+q"},
	})
	if m.statusMsg != "" {
		t.Fatalf("unexpected problems: %s", m.statusMsg)
	}

	// The old key does nothing; the new ones move
	if m, _ = press(t, m, "j"); m.tab().Cursor != 0 {
		t.Fatal("j still moves down")
	}
	if m, _ = ctrl(t, m, tea.KeyCtrlN); m.tab().Cursor != 1 {
		t.Fatal("ctrl+n doesn't move down")
	}
	if m, _ = ctrl(t, m, tea.KeyDown); m.tab().Cursor != 2 {
		t.Fatal("the down arrow doesn't move down")
	}

	if m, _ = press(t, m, "."); m.showHidden {
		t.Fatal(". still toggles hidden files")
	}
	if m, _ = press(t, m, "H"); !m.showHidden {
		t.Fatal("H doesn't toggle hidden files")
	}

	if _, cmd := press(t, m, "q"); cmd != nil {
		t.Fatal("q still quits")
	}
	if _, cmd := ctrl(t, m, tea.KeyCtrlQ); cmd == nil || cmd() != tea.Quit() {
		t.Fatal("ctrl+q doesn't quit")
	}

	// Dialogs move with the same keys
	for _, p := range []string{"/a", "/b", "/c"} {
		m.bookmarks.Add(filepath.Base(p), p)
	}
	m, _ = press(t, m, "b")
	m, _ = press(t, m, "j")
	m, _ = ctrl(t, m, tea.KeyCtrlN)
	if m.bookmarkCursor != 1 {
		t.Fatalf("bookmarkCursor = %d, want 1: j shouldn't move, ctrl+n should", m.bookmarkCursor)
	}
}

func TestKeyProblemsAreReported(t *testing.T) {
	m := withKeys(t, threeFiles(t), map[string]config.KeyList{
		"jump":        {"z"},
		"hard-delete": {"Z"},
		"up":          {"ctrlr+k", "k"},
		"down":        {"shift+j"},
		"refresh":     nil, // Written with no value
	})
	wantProblems(t, m,
		`keys: unknown action "jump" (sushi --list-keys lists them)`,
		`keys: unknown action "hard-delete" (did you mean hard_delete?)`,
		`keys: up: unknown key "ctrlr+k"`,
		`keys: down: unknown key "shift+j" (shifted letters are capitals, like "J")`,
		`keys: refresh has no keys (write refresh: [] to unbind it)`)

	// Up keeps the key it could read; down and refresh, with none, keep
	// their defaults
	if !slices.Equal(m.keys.Up.Keys(), []string{"k"}) || !slices.Equal(m.keys.Down.Keys(), []string{"down", "j"}) ||
		!slices.Equal(m.keys.Refresh.Keys(), []string{"ctrl+r"}) {
		t.Fatalf("up=%q down=%q refresh=%q", m.keys.Up.Keys(), m.keys.Down.Keys(), m.keys.Refresh.Keys())
	}
	if !isProblem(m.statusMsg) {
		t.Fatal("key problems should be shown as problems")
	}
}

func TestKeyConflictsAreReported(t *testing.T) {
	dir := threeFiles(t)
	m := withKeys(t, dir, map[string]config.KeyList{
		"find":     {"d"},        // Delete's key
		"new_tab":  {"3"},        // A bookmark's digit
		"down":     {"j", "esc"}, // Esc closes dialogs
		"reverse":  {"n"},        // New file's key, and a letter of the sort menu
		"up":       {"x"},        // Both set in the config: the first in the key map wins
		"cut":      {"x", "ctrl+k"},
		"new_file": {"alt+n"},
	})
	wantProblems(t, m,
		`keys: conflict: "d" is bound to both find and delete; find keeps it`,
		`keys: conflict: "x" is bound to both up and cut; up keeps it`,
		`keys: conflict: "3" for new_tab hides bookmark 3`,
		`keys: conflict: "esc" for down is ignored in the key panel, where it closes it`,
		`keys: conflict: "esc" for down is ignored in the bookmark list, where it closes it`,
		`keys: conflict: "esc" for down is ignored in the sort menu, where it closes it`,
		`keys: conflict: "esc" for down is ignored in the Run palette, where it closes it`,
		`keys: conflict: "n" for reverse is ignored in the sort menu, where it sorts by name`)
	if strings.Contains(m.statusMsg, "new_file") {
		t.Errorf("new_file moved to alt+n, so it doesn't clash: %s", m.statusMsg)
	}
	if !isProblem(`keys: conflict: "3" for new_tab hides bookmark 3`) {
		t.Error("conflicts should be shown as problems")
	}

	// The key does what the problem says: d finds, and delete has no key
	if len(m.keys.Delete.Keys()) != 0 || !slices.Equal(m.keys.Cut.Keys(), []string{"ctrl+k"}) {
		t.Fatalf("delete=%q cut=%q", m.keys.Delete.Keys(), m.keys.Cut.Keys())
	}
	if found, _ := press(t, m, "d"); found.mode != ModeFind {
		t.Fatal("d doesn't find")
	}
	m, _ = press(t, m, "j")
	if up, _ := press(t, m, "x"); up.tab().Cursor != 0 || len(up.clipboard) != 0 {
		t.Fatal("x should move up, and not cut")
	}

	// 3 opens a tab instead of going to bookmark 3
	for _, p := range []string{"/a", "/b", "/c"} {
		m.bookmarks.Add(filepath.Base(p), p)
	}
	if m, _ = press(t, m, "3"); len(m.tabs) != 2 {
		t.Fatalf("3 made %d tabs, want a new one", len(m.tabs))
	}

	// Esc still closes the bookmark list, and n still sorts by name
	m, _ = press(t, m, "b")
	if m, _ = press(t, m, "esc"); m.mode != ModeNormal {
		t.Fatal("esc should close the bookmark list")
	}
	m, _ = press(t, m, "s")
	m, _ = press(t, m, "j")
	if m, _ = press(t, m, "n"); m.mode != ModeNormal || m.sortBy != "name" || m.sortReverse {
		t.Fatalf("n in the sort menu: mode=%v by=%s reverse=%v", m.mode, m.sortBy, m.sortReverse)
	}
}

func TestPluginKeysFollowRemaps(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Keys = map[string]config.KeyList{"cut": {"ctrl+k"}, "hidden": {"Z"}}
	cfg.Plugins = []plugins.Plugin{
		{Name: "freed", Key: "x", Command: "true"}, // Cut gave x up
		{Name: "taken", Key: "Z", Command: "true"}, // Hidden took Z
		{Name: "digit", Key: "5", Command: "true"},
		{Name: "dot", Key: ".", Command: "true"}, // Hidden gave . up
	}
	m := newTestModel(t, t.TempDir(), cfg)

	wantProblems(t, m, `plugin taken: key "Z" is used by sushi for hidden`, `plugin digit: key "5" is used by sushi for bookmark 5`)
	for _, k := range []string{"x", "."} {
		if _, ok := m.pluginKeys[k]; !ok {
			t.Errorf("no plugin on %s, which no action uses", k)
		}
	}
	if strings.Contains(m.statusMsg, "freed") || strings.Contains(m.statusMsg, "dot") {
		t.Errorf("unexpected problems: %s", m.statusMsg)
	}
}

func TestPanelAndHintsShowRemaps(t *testing.T) {
	m := withKeys(t, threeFiles(t), map[string]config.KeyList{
		"down":         {"ctrl+n", "down"},
		"up":           {"ctrl+p", "up"},
		"copy":         {"C"},
		"cut":          {"ctrl+k"},
		"delete":       {"x"},
		"reverse":      {"alt+s"},
		"add_bookmark": {"ctrl+b"},
		"help":         {"H"},
		"preview_up":   {},
	})
	if m.statusMsg != "" {
		t.Fatalf("unexpected problems: %s", m.statusMsg)
	}
	bottom := func(m Model) string { return ansi.Strip(m.renderBottomRow()) }

	hints := bottom(resize(m, tea.WindowSizeMsg{Width: 200, Height: 24}))
	for _, want := range []string{"C copy", "ctrl+k cut", "x delete", "H all keys"} {
		if !strings.Contains(hints, want) {
			t.Errorf("hints lack %q: %s", want, hints)
		}
	}
	if strings.Contains(hints, "c copy") || strings.Contains(hints, "? all keys") {
		t.Errorf("hints show the old keys: %s", hints)
	}

	// The key panel, with runs of spaces read as one
	m = resize(m, tea.WindowSizeMsg{Width: 100, Height: 12})
	m, _ = press(t, m, "H")
	panel := strings.Join(strings.Fields(ansi.Strip(strings.Join(m.helpLines(), "\n"))), " ")
	for _, want := range []string{"ctrl+n p down, up", "C, ctrl+k, v copy, cut, paste", "x D trash, delete", "s alt+s sort by, reverse",
		"b ctrl+b bookmarks, add", "J scroll preview down", "H this panel"} {
		if !strings.Contains(panel, want) {
			t.Errorf("panel lacks %q:\n%s", want, strings.Join(m.helpLines(), "\n"))
		}
	}
	if !strings.Contains(ansi.Strip(m.renderHelpView()), "ctrl+n/p to scroll") || !strings.Contains(bottom(m), "ctrl+n/p scroll") {
		t.Errorf("scroll keys: %s / %s", strings.Split(ansi.Strip(m.renderHelpView()), "\n")[0], bottom(m))
	}

	// ctrl+n scrolls it, j is no longer special, and the help key closes it
	if m, _ = ctrl(t, m, tea.KeyCtrlN); m.mode != ModeHelp || m.helpScroll != 1 {
		t.Fatalf("ctrl+n: mode=%v scroll=%d", m.mode, m.helpScroll)
	}
	if m, _ = press(t, m, "H"); m.mode != ModeNormal {
		t.Fatal("H should close the panel")
	}
	m, _ = press(t, m, "H")
	if m, _ = press(t, m, "j"); m.mode != ModeNormal || m.tab().Cursor != 0 {
		t.Fatalf("j: mode=%v cursor=%d; it should close the panel and do nothing", m.mode, m.tab().Cursor)
	}

	// Dialogs
	if !strings.Contains(ansi.Strip(strings.Join(m.bookmarksBox(), "\n")), "Press ctrl+b to bookmark") {
		t.Error("the empty bookmark list names the old key")
	}
	for _, p := range []string{"/a", "/b"} {
		m.bookmarks.Add(filepath.Base(p), p)
	}
	m, _ = press(t, m, "b")
	if !strings.Contains(bottom(m), "x remove") {
		t.Errorf("bookmark hints: %s", bottom(m))
	}
	if m, _ = press(t, m, "d"); m.bookmarks.Len() != 2 {
		t.Fatal("d still removes bookmarks")
	}
	if m, _ = press(t, m, "x"); m.bookmarks.Len() != 1 {
		t.Fatal("x doesn't remove bookmarks")
	}
	m, _ = press(t, m, "esc")

	m, _ = press(t, m, "s")
	if !strings.Contains(bottom(m), "alt+s reverse") {
		t.Errorf("sort menu hints: %s", bottom(m))
	}
	if m, _ = press(t, m, "S"); m.mode != ModeSort || m.sortReverse {
		t.Fatal("S still reverses")
	}
	if m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}, Alt: true}); m.mode != ModeNormal || !m.sortReverse {
		t.Fatal("alt+s doesn't reverse")
	}
}

func TestLabelsFollowRemaps(t *testing.T) {
	m := withKeys(t, threeFiles(t), map[string]config.KeyList{
		"plugins": {"alt+p"}, "bookmark": {"ctrl+b"}, "reverse": {"alt+s"}, "find": {"ctrl+f"}, "grep": {"alt+f"},
	})
	if m.statusMsg != "" {
		t.Fatalf("unexpected problems: %s", m.statusMsg)
	}
	if bar := ansi.Strip(m.renderTabBar()); !strings.Contains(bar, "alt+p run  ctrl+b bookmarks") || strings.Contains(bar, "P run") {
		t.Errorf("tab bar = %q", bar)
	}
	if menu := ansi.Strip(strings.Join(m.sortBox(), "\n")); !strings.Contains(menu, "alt+s reverses") || strings.Contains(menu, "S reverses") {
		t.Errorf("sort menu:\n%s", menu)
	}
	// The search palette is marked with the key that opens it
	for _, c := range []struct {
		key  tea.KeyMsg
		mark string
	}{
		{tea.KeyMsg{Type: tea.KeyCtrlF}, " ctrl+f "},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}, Alt: true}, " alt+f "},
	} {
		palette, _ := send(t, m, c.key)
		if box := ansi.Strip(strings.Join(palette.findBox(), "\n")); palette.mode != ModeFind || !strings.Contains(box, c.mark) {
			t.Errorf("palette opened with %s:\n%s", c.key, box)
		}
		assertFills(t, "palette opened with "+c.key.String(), palette)
	}

	// Unbound, they are left out
	m = withKeys(t, threeFiles(t), map[string]config.KeyList{"plugins": {}, "bookmark": {}, "reverse": {}})
	if bar := ansi.Strip(m.renderTabBar()); strings.Contains(bar, "run") || strings.Contains(bar, "bookmarks") {
		t.Errorf("tab bar = %q", bar)
	}
	if menu := ansi.Strip(strings.Join(m.sortBox(), "\n")); strings.Contains(menu, "reverses") {
		t.Errorf("sort menu:\n%s", menu)
	}
}

func TestQuitKeyInTheKeyPanel(t *testing.T) {
	// q closes the panel rather than quitting; ctrl+c still quits
	m := newTestModel(t, threeFiles(t), nil)
	m, _ = press(t, m, "?")
	if closed, cmd := press(t, m, "q"); closed.mode != ModeNormal || cmd != nil {
		t.Fatal("q should close the panel, and only that")
	}
	if _, cmd := ctrl(t, m, tea.KeyCtrlC); cmd == nil || cmd() != tea.Quit() {
		t.Fatal("ctrl+c should quit from the panel")
	}

	// The quit key the panel shows does that, whatever it is, and q does
	// what it is bound to
	m = withKeys(t, threeFiles(t), map[string]config.KeyList{"quit": {"x", "ctrl+c"}, "cut": {"ctrl+k"}, "find": {"q"}})
	m, _ = press(t, m, "?")
	if closed, cmd := press(t, m, "x"); closed.mode != ModeNormal || cmd != nil {
		t.Fatal("x, the quit key, should close the panel, and only that")
	}
	if m, _ = press(t, m, "q"); m.mode != ModeFind {
		t.Fatalf("q bound to find: mode=%v", m.mode)
	}
}

func TestMouseDoesNotNeedTheKeys(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "alpha"), 0755)
	writeTestFile(t, filepath.Join(root, "alpha", "inside.txt"), "")
	fakeClock(t)

	// Neither Enter nor the left arrow does anything in the browser now
	m := withKeys(t, root, map[string]config.KeyList{"enter": {}, "left": {"h"}, "back": {}})
	m = resize(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	x, y := findOnScreen(t, m, "alpha", 0, m.layout().listW)
	m, _ = clickAt(m, x, y)
	m, cmd := clickAt(m, x, y)
	if m = drain(t, m, cmd); filepath.Base(m.tab().CurrentPath) != "alpha" {
		t.Fatalf("a double-click went to %s, want alpha", m.tab().CurrentPath)
	}

	m = resize(m, tea.WindowSizeMsg{Width: 120, Height: 24})
	m, cmd = clickAt(m, 1, paneTop)
	if m = drain(t, m, cmd); m.tab().CurrentPath != root {
		t.Fatalf("clicking the parent heading went to %s", m.tab().CurrentPath)
	}
}

func TestWriteKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := config.DefaultConfig()
	cfg.Keys = map[string]config.KeyList{"up": {"i", "up"}, "refresh": {}, "hidden": {"alt+space"}, "bogus": {"b"}}
	cfg.Plugins = []plugins.Plugin{{Name: "git status", Key: "Z", Command: "true"}}

	var out strings.Builder
	problems, err := WriteKeys(&out, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], `unknown action "bogus"`) {
		t.Fatalf("problems = %q", problems)
	}
	text := out.String()
	for _, want := range []string{"  up: [i, up]", "  hard_delete: [D]", "  select: [space]", `  help: ["?"]`, "  refresh: []",
		"  hidden: [alt+space]", "  quit: [q, ctrl+c]", "# move up", "#   Z  git status", "1-9"} {
		if !strings.Contains(text, want) {
			t.Errorf("output lacks %q:\n%s", want, text)
		}
	}

	// It is a keys: section that gives the same keys back
	var parsed struct {
		Keys map[string]config.KeyList `yaml:"keys"`
	}
	if err := yaml.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("%v:\n%s", err, text)
	}
	again, problems := loadKeyMap(parsed.Keys)
	if len(problems) > 0 {
		t.Fatalf("reading it back: %q", problems)
	}
	want, _ := loadKeyMap(cfg.Keys)
	for i, a := range again.actions() {
		if w := want.actions()[i].binding.Keys(); !slices.Equal(a.binding.Keys(), w) {
			t.Errorf("%s read back as %q, want %q", a.name, a.binding.Keys(), w)
		}
	}
	if len(parsed.Keys) != len(again.actions()) {
		t.Errorf("%d actions listed, want %d", len(parsed.Keys), len(again.actions()))
	}
}

func TestCtrlCAlwaysQuits(t *testing.T) {
	dir := threeFiles(t)

	// From every mode, whatever the mode does with other keys
	m := newTestModel(t, dir, nil)
	m.bookmarks.Add("a", dir)
	for _, keys := range []string{"", "/a", "r", "n", "D", "b", "P", "!", "s", "f", "Fx", "?"} {
		screen := typeText(t, detach(m), keys)
		if _, cmd := ctrl(t, screen, tea.KeyCtrlC); !quits(cmd) {
			t.Errorf("ctrl+c after %q (mode %v) doesn't quit", keys, screen.mode)
		}
	}

	// Quit on another key: ctrl+c still quits, and that is no problem
	m = withKeys(t, dir, map[string]config.KeyList{"quit": {"ctrl+q"}})
	if m.statusMsg != "" {
		t.Fatalf("unexpected problems: %s", m.statusMsg)
	}
	for _, k := range []tea.KeyType{tea.KeyCtrlQ, tea.KeyCtrlC} {
		if _, cmd := ctrl(t, m, k); !quits(cmd) {
			t.Errorf("%v doesn't quit", k)
		}
	}

	// No quit key at all is a problem, at startup and for --list-keys, but
	// ctrl+c still quits
	none := map[string]config.KeyList{"quit": {}, "quit_no_cd": {}, "close_tab": {}}
	m = withKeys(t, dir, none)
	wantProblems(t, m, "keys: invalid: quit has no key; only ctrl+c quits")
	if !isProblem(m.statusMsg) {
		t.Error("a missing quit key should be shown as a problem")
	}
	if _, cmd := ctrl(t, m, tea.KeyCtrlC); !quits(cmd) {
		t.Error("ctrl+c doesn't quit without a quit key")
	}
	cfg := config.DefaultConfig()
	cfg.Keys = none
	var out strings.Builder
	if problems, _ := WriteKeys(&out, cfg); len(problems) != 1 || !strings.Contains(problems[0], "quit has no key") {
		t.Errorf("--list-keys problems = %q", problems)
	}
	if !strings.Contains(out.String(), "ctrl+c always quits") {
		t.Errorf("--list-keys doesn't say ctrl+c always quits:\n%s", out.String())
	}

	// Another action can't take ctrl+c
	m = withKeys(t, dir, map[string]config.KeyList{"copy": {"ctrl+c", "C"}})
	wantProblems(t, m, `keys: conflict: "ctrl+c" for copy is ignored, as it always quits`)
	if !slices.Equal(m.keys.Copy.Keys(), []string{"C"}) {
		t.Errorf("copy = %q", m.keys.Copy.Keys())
	}
	if _, cmd := ctrl(t, m, tea.KeyCtrlC); !quits(cmd) {
		t.Error("ctrl+c copies rather than quitting")
	}

	// Listed first, ctrl+c isn't the quit key the panel shows and closes
	// with: q is. ctrl+c quits from the panel.
	m = withKeys(t, dir, map[string]config.KeyList{"quit": {"ctrl+c", "q"}})
	if h := keyHint("quit", m.keys.Quit); h.key != "q" {
		t.Errorf("quit is labelled %q, want q", h.key)
	}
	m, _ = press(t, m, "?")
	if closed, cmd := press(t, m, "q"); closed.mode != ModeNormal || cmd != nil {
		t.Error("q should close the panel, and only that")
	}
	if _, cmd := ctrl(t, m, tea.KeyCtrlC); !quits(cmd) {
		t.Error("ctrl+c should quit from the panel")
	}

	// While a job runs, ctrl+c does what the quit key does, in the browser
	// and in a dialog: the job is stopped first
	m = newTestModel(t, dir, nil)
	m, _ = press(t, m, "d") // Started, but its command isn't run
	viaQ, qCmd := press(t, detach(m), "q")
	for _, keys := range []string{"", "s", "?"} {
		viaC, cCmd := ctrl(t, typeText(t, detach(m), keys), tea.KeyCtrlC)
		if quits(cCmd) != quits(qCmd) || (viaC.job == nil) != (viaQ.job == nil) || viaC.job != nil && viaC.job.quit != viaQ.job.quit {
			t.Errorf("busy, after %q: ctrl+c quits=%v job=%+v; q quits=%v job=%+v", keys, quits(cCmd), viaC.job, quits(qCmd), viaQ.job)
		}
	}
}
