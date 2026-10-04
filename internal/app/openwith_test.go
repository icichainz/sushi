package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/icichainz/sushi/internal/config"
	"golang.org/x/sys/unix"
)

func TestOpenWithListsTheAppsAndOpens(t *testing.T) {
	f := useFakeMac(t)
	f.apps = `{"default":"/System/Applications/TextEdit.app","apps":["/Applications/Visual Studio Code.app",` +
		`"/System/Applications/TextEdit.app","/Applications/Xcode.app"]}`
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.md"} {
		writeTestFile(t, filepath.Join(dir, name), "")
	}
	m := resize(newTestModel(t, dir, nil), tea.WindowSizeMsg{Width: 100, Height: 30})

	m, cmd := press(t, m, "O")
	screen := ansi.Strip(m.View())
	if m.mode != ModeOpenWith || !strings.Contains(screen, "Looking up apps") || !strings.Contains(screen, "OPEN WITH") {
		t.Fatalf("mode %v:\n%s", m.mode, screen)
	}
	m = drain(t, m, cmd)
	screen = ansi.Strip(m.View())
	for _, want := range []string{"Open with", "● TextEdit", "/System/Applications", "Visual Studio Code", "Xcode", "Opens a.txt; ● is the default app", "enter open"} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen lacks %q:\n%s", want, screen)
		}
	}
	if !strings.Contains(f.lookups[0], `fileURLWithPath("`+filepath.Join(dir, "a.txt")+`")`) {
		t.Errorf("looked up:\n%s", f.lookups[0])
	}

	m, _ = press(t, m, "j")
	m, _ = press(t, m, "G")
	m, _ = press(t, m, "k")
	m, cmd = press(t, m, "enter")
	if m.mode != ModeNormal || m.statusMsg != "Opening a.txt with Visual Studio Code" {
		t.Fatalf("mode %v, statusMsg %q", m.mode, m.statusMsg)
	}
	m = drain(t, m, cmd)
	if want := [][]string{{"-a", "/Applications/Visual Studio Code.app", filepath.Join(dir, "a.txt")}}; !slices.EqualFunc(f.opened, want, slices.Equal) {
		t.Fatalf("opened %q, want %q", f.opened, want)
	}

	// Known for .txt now; a selection opens together, with the apps of its
	// first file, not its first folder
	os.Mkdir(filepath.Join(dir, "0-folder"), 0755)
	m = at(t, m, dir)
	m.tab().Selected[filepath.Join(dir, "0-folder")] = true
	m.tab().Selected[filepath.Join(dir, "b.txt")] = true
	if m, cmd = press(t, m, "O"); cmd != nil || m.openWith.loading || len(m.openWith.apps) != 3 {
		t.Fatalf("not from the cache: loading %v, apps %v", m.openWith.loading, m.openWith.apps)
	}
	m, cmd = press(t, m, "enter")
	m = drain(t, m, cmd)
	if want := []string{"-a", "/System/Applications/TextEdit.app", filepath.Join(dir, "0-folder"), filepath.Join(dir, "b.txt")}; !slices.Equal(f.opened[1], want) {
		t.Fatalf("opened %q, want %q", f.opened[1], want)
	}
	if len(f.lookups) != 1 {
		t.Fatalf("%d lookups, want 1", len(f.lookups))
	}

	// Another extension is looked up; esc closes without opening
	clear(m.tab().Selected)
	m = cursorTo(t, m, "c.md")
	m, cmd = press(t, m, "O")
	m = drain(t, m, cmd)
	if m, _ = press(t, m, "esc"); m.mode != ModeNormal || len(f.lookups) != 2 || len(f.opened) != 2 {
		t.Fatalf("mode %v, %d lookups, %d opened", m.mode, len(f.lookups), len(f.opened))
	}
}

func TestOpenWithLooksUpAgainWhenTheExtensionIsntEnough(t *testing.T) {
	f := useFakeMac(t)
	f.apps = `{"default":"/System/Applications/TextEdit.app","apps":[]}`
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "own.txt"} {
		writeTestFile(t, filepath.Join(dir, name), "")
	}
	// Get Info's "Open with" for this file only
	if err := unix.Setxattr(filepath.Join(dir, "own.txt"), openWithAttr, []byte("bplist00"), 0); err != nil {
		t.Skipf("can't set attributes here: %v", err)
	}
	m := newTestModel(t, dir, nil)
	open := func(name string) {
		t.Helper()
		m = cursorTo(t, m, name)
		var cmd tea.Cmd
		m, cmd = press(t, m, "O")
		m = drain(t, m, cmd)
		m, _ = press(t, m, "esc")
	}

	open("a.txt")
	open("b.txt") // Cached for .txt
	open("own.txt")
	open("own.txt") // Never cached
	open("a.txt")   // Its own app didn't replace the one for .txt
	if len(f.lookups) != 3 || !strings.Contains(f.lookups[1], "own.txt") || !strings.Contains(f.lookups[2], "own.txt") {
		t.Fatalf("%d lookups", len(f.lookups))
	}

	// ctrl+r forgets them all
	m, cmd := ctrl(t, m, tea.KeyCtrlR)
	m = drain(t, m, cmd)
	open("b.txt")
	if len(f.lookups) != 4 {
		t.Fatalf("after ctrl+r, %d lookups", len(f.lookups))
	}
}

func TestOpenWithFailures(t *testing.T) {
	f := useFakeMac(t)
	dir := t.TempDir()
	for _, name := range []string{"a.none", "b.fails", "c.stale", "d.zzz"} {
		writeTestFile(t, filepath.Join(dir, name), "")
	}
	m := newTestModel(t, dir, nil)
	lookUp := func(m Model, name, reply string) (Model, tea.Cmd) {
		t.Helper()
		f.apps = reply
		return press(t, cursorTo(t, m, name), "O")
	}

	// No app
	m, cmd := lookUp(m, "a.none", `{"default":"","apps":[]}`)
	m = drain(t, m, cmd)
	if screen := ansi.Strip(m.View()); !strings.Contains(screen, "No app can open a.none") {
		t.Fatalf("screen:\n%s", screen)
	}
	if m, _ = press(t, m, "enter"); m.mode != ModeNormal || len(f.opened) != 0 {
		t.Fatalf("enter with no app: mode %v, opened %q", m.mode, f.opened)
	}

	// The lookup fails, and isn't cached
	m, cmd = lookUp(m, "b.fails", "garbage")
	if m = drain(t, m, cmd); m.mode != ModeNormal || !strings.HasPrefix(m.statusMsg, "Can't find the apps for b.fails") {
		t.Fatalf("mode %v, statusMsg %q", m.mode, m.statusMsg)
	}
	if _, cmd = lookUp(m, "b.fails", "garbage"); cmd == nil {
		t.Fatal("a failed lookup was cached")
	}

	// A lookup that comes after the list closed is only cached
	m, stale := lookUp(m, "c.stale", `{"default":"/Applications/A.app","apps":[]}`)
	m, _ = press(t, m, "esc")
	if m = drain(t, m, stale); m.mode != ModeNormal {
		t.Fatalf("mode %v", m.mode)
	}
	if m, cmd = lookUp(m, "c.stale", ""); cmd != nil || len(m.openWith.apps) != 1 {
		t.Fatalf("not cached: %+v", m.openWith.apps)
	}
	m, _ = press(t, m, "esc")

	// open fails
	m, cmd = lookUp(m, "d.zzz", `{"default":"/Applications/A.app","apps":[]}`)
	m = drain(t, m, cmd)
	f.fail = errors.New("exit status 1")
	m, cmd = press(t, m, "enter")
	if m = drain(t, m, cmd); m.statusMsg != "Open with A failed: exit status 1: LSOpenURLsWithRole() failed" {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
}

func TestOpenWithMouse(t *testing.T) {
	f := useFakeMac(t)
	fakeClock(t)
	var apps []string
	for i := range 30 {
		apps = append(apps, fmt.Sprintf(`"/Applications/App %02d.app"`, i))
	}
	f.apps = `{"default":"","apps":[` + strings.Join(apps, ",") + `]}`
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "")
	m := resize(newTestModel(t, dir, nil), tea.WindowSizeMsg{Width: 80, Height: 24})
	m, cmd := press(t, m, "O")
	m = drain(t, m, cmd)

	// The wheel moves; the list scrolls to keep the app chosen in sight
	for range 20 {
		m, _ = mouseAt(m, 0, 0, tea.MouseButtonWheelDown, false)
	}
	if screen := ansi.Strip(m.View()); m.openWith.cursor != 20 || !strings.Contains(screen, "App 20") || strings.Contains(screen, "App 00") {
		t.Fatalf("cursor %d:\n%s", m.openWith.cursor, screen)
	}

	x, y := findOnScreen(t, m, "App 22", 0, m.width)
	m, _ = clickAt(m, x, y)
	if m.openWith.cursor != 22 || m.mode != ModeOpenWith {
		t.Fatalf("a click picked %d, mode %v", m.openWith.cursor, m.mode)
	}
	m, cmd = clickAt(m, x, y)
	m = drain(t, m, cmd)
	if m.mode != ModeNormal || len(f.opened) != 1 || f.opened[0][1] != "/Applications/App 22.app" {
		t.Fatalf("a double-click opened %q, mode %v", f.opened, m.mode)
	}

	// Clicking outside closes the list
	m, cmd = press(t, m, "O")
	m = drain(t, m, cmd)
	if m, _ = clickAt(m, 0, 0); m.mode != ModeNormal {
		t.Fatalf("mode %v after a click outside", m.mode)
	}
}

func TestRevealInFinder(t *testing.T) {
	f := useFakeMac(t)
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "")
	writeTestFile(t, filepath.Join(dir, "b.txt"), "")
	m := newTestModel(t, dir, nil)

	m, cmd := ctrl(t, m, tea.KeyCtrlO)
	if m.statusMsg != "Showing a.txt in Finder" {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
	m = drain(t, m, cmd)
	if want := [][]string{{"-R", filepath.Join(dir, "a.txt")}}; !slices.EqualFunc(f.opened, want, slices.Equal) {
		t.Fatalf("opened %q, want %q", f.opened, want)
	}

	// Several are all selected in Finder, which open -R can't do
	m, _ = press(t, m, " ")
	m, _ = press(t, m, " ")
	m, cmd = ctrl(t, m, tea.KeyCtrlO)
	m = drain(t, m, cmd)
	if want := []string{filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")}; len(f.revealed) != 1 || !slices.Equal(f.revealed[0], want) {
		t.Fatalf("revealed %q, want %q", f.revealed, want)
	}

	f.fail = errors.New("exit status 1")
	clear(m.tab().Selected)
	m, cmd = ctrl(t, m, tea.KeyCtrlO)
	if m = drain(t, m, cmd); !strings.HasPrefix(m.statusMsg, "Show in Finder failed: exit status 1") {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
}

func TestOpenWithAndFinderNeedMacOS(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "")
	m := newTestModel(t, dir, nil)
	if m, _ = press(t, m, "O"); m.mode != ModeNormal || m.statusMsg != "Open with needs macOS" {
		t.Fatalf("mode %v, statusMsg %q", m.mode, m.statusMsg)
	}
	if m, _ = ctrl(t, m, tea.KeyCtrlO); m.statusMsg != "Showing files in Finder needs macOS" {
		t.Fatalf("statusMsg %q", m.statusMsg)
	}
}

func TestBusyRefusesOpenWithAndFinder(t *testing.T) {
	f := useFakeMac(t)
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.txt"), "")
	writeTestFile(t, filepath.Join(dir, "b.txt"), "")
	m := newTestModel(t, dir, nil)
	m, _ = press(t, m, "d")
	for _, k := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune{'O'}}, {Type: tea.KeyCtrlO}} {
		m.statusMsg = ""
		m2, cmd := send(t, m, k)
		if m2 = drain(t, m2, cmd); m2.mode != ModeNormal || !strings.Contains(m2.statusMsg, "Still moving to trash") {
			t.Fatalf("%s while busy: mode %v, statusMsg %q", k, m2.mode, m2.statusMsg)
		}
	}
	if len(f.opened)+len(f.lookups)+len(f.revealed) != 0 {
		t.Fatal("something ran while busy")
	}
}

func TestOpenWithFillsTheTerminal(t *testing.T) {
	f := useFakeMac(t)
	dir := t.TempDir()
	long := strings.Repeat("long-name-", 8) + ".txt"
	writeTestFile(t, filepath.Join(dir, long), "")
	var apps []string
	for i := range 25 {
		apps = append(apps, fmt.Sprintf(`"/Applications/%s/App with a rather long name %02d.app"`, strings.Repeat("Folder/", i%4), i))
	}
	for _, size := range sizes {
		label := fmt.Sprintf("%dx%d", size.Width, size.Height)
		for state, reply := range map[string]string{
			"loading": "",
			"none":    `{"default":"","apps":[]}`,
			"many":    `{"default":"/Applications/日本語のアプリ.app","apps":[` + strings.Join(apps, ",") + `]}`,
		} {
			f.apps = reply
			m := resize(newTestModel(t, dir, nil), size)
			m, cmd := press(t, m, "O")
			if state != "loading" {
				m = drain(t, m, cmd)
			}
			m, _ = press(t, m, "G")
			lines := assertFills(t, label+" open with "+state, m)
			if !strings.Contains(strings.Join(lines, "\n"), "Open with") && size.Height > 10 {
				t.Errorf("%s %s: no dialog", label, state)
			}
		}
	}
}

func TestOpenWithAndRevealCanBeRemapped(t *testing.T) {
	f := useFakeMac(t)
	f.apps = `{"default":"/Applications/A.app","apps":[]}`
	m := withKeys(t, threeFiles(t), map[string]config.KeyList{"open_with": {"A"}, "reveal": {"alt+r"}})
	if m.statusMsg != "" {
		t.Fatalf("unexpected problems: %s", m.statusMsg)
	}
	panel := strings.Join(strings.Fields(ansi.Strip(strings.Join(m.helpLines(), "\n"))), " ")
	if !strings.Contains(panel, "A alt+r open with, reveal") {
		t.Fatalf("panel:\n%s", strings.Join(m.helpLines(), "\n"))
	}
	if same, _ := press(t, m, "O"); same.mode != ModeNormal {
		t.Fatal("O still opens the list")
	}
	m, cmd := press(t, m, "A")
	if m = drain(t, m, cmd); m.mode != ModeOpenWith || len(m.openWith.apps) != 1 {
		t.Fatalf("W: mode %v", m.mode)
	}
	m, _ = press(t, m, "esc")
	m, cmd = send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}, Alt: true})
	if m = drain(t, m, cmd); len(f.opened) != 1 || f.opened[0][0] != "-R" {
		t.Fatalf("alt+r opened %q", f.opened)
	}
}
