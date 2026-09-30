package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/icichainz/sushi/internal/config"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/utils"
)

// noWatch is a config with watching off, for tests that send changes by
// hand, so nothing waits on a real watcher
func noWatch() *config.Config {
	cfg := config.DefaultConfig()
	cfg.Watch = false
	return cfg
}

// eventually polls until ok holds, failing the test after a generous wait
func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// startWatching starts m's watcher with the given quiet period and waits
// until it watches the active tab's directory. The returned channel gets
// the watcher's first message.
func startWatching(t *testing.T, m Model, quiet time.Duration) chan tea.Msg {
	t.Helper()
	if m.watch == nil {
		t.Fatal("watching is off")
	}
	m.watch.quiet = quiet
	t.Cleanup(m.Close)
	msgs := make(chan tea.Msg, 1)
	listen := m.watch.listen()
	go func() { msgs <- listen() }()
	dir := m.tab().CurrentPath
	eventually(t, "the watch on "+dir, func() bool { return slices.Contains(m.watch.watching(), dir) })
	return msgs
}

// next waits for the watcher's message
func next(t *testing.T, msgs chan tea.Msg) dirsChangedMsg {
	t.Helper()
	select {
	case msg := <-msgs:
		changed, ok := msg.(dirsChangedMsg)
		if !ok {
			t.Fatalf("got %T, want dirsChangedMsg", msg)
		}
		return changed
	case <-time.After(10 * time.Second):
		t.Fatal("no change reported")
		return dirsChangedMsg{}
	}
}

// collect runs cmd, and the commands of any batch it returns, gathering
// the messages that arrive within d, or until one satisfies last.
// Commands still running are left to finish on their own.
func collect(cmd tea.Cmd, d time.Duration, last func(tea.Msg) bool) []tea.Msg {
	out := make(chan tea.Msg, 64)
	var start func(tea.Cmd)
	start = func(c tea.Cmd) {
		if c == nil {
			return
		}
		go func() {
			msg := c()
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, c := range batch {
					start(c)
				}
				return
			}
			if msg != nil {
				out <- msg
			}
		}()
	}
	start(cmd)

	var msgs []tea.Msg
	deadline := time.After(d)
	for {
		select {
		case msg := <-out:
			msgs = append(msgs, msg)
			if last != nil && last(msg) {
				return msgs
			}
		case <-deadline:
			return msgs
		}
	}
}

func TestWatcherReloadsOnceForABurstOfChanges(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"b.txt", "d.txt", "f.txt"} {
		writeTestFile(t, filepath.Join(dir, name), "")
	}
	m := newTestModel(t, dir, nil)
	m, _ = press(t, m, "j") // d.txt
	m, _ = press(t, m, " ") // Selected; the cursor moves on to f.txt
	msgs := startWatching(t, m, 300*time.Millisecond)

	// Like a build writing many files, all listed before f.txt
	for i := range 50 {
		writeTestFile(t, filepath.Join(dir, fmt.Sprintf("a%02d.txt", i)), "output")
	}
	changed := next(t, msgs)
	if !slices.Contains(changed.dirs, dir) {
		t.Fatalf("changed = %v, want %s", changed.dirs, dir)
	}

	// The batch asks for one reload, and listens for the next batch,
	// which must not come: the burst was one change
	updated, cmd := m.Update(changed)
	m = updated.(Model)
	var loads []dirLoadedMsg
	for _, msg := range collect(cmd, 5*300*time.Millisecond, nil) {
		switch msg := msg.(type) {
		case dirLoadedMsg:
			loads = append(loads, msg)
		case dirsChangedMsg:
			t.Fatalf("a second batch came for the same burst: %v", msg.dirs)
		}
	}
	if len(loads) != 1 {
		t.Fatalf("%d reloads, want exactly 1", len(loads))
	}

	updated, cmd = m.Update(loads[0])
	m = drain(t, updated.(Model), cmd)
	tab := m.tab()
	if len(tab.Files) != 53 || cursorName(m) != "f.txt" || !tab.Selected[filepath.Join(dir, "d.txt")] {
		t.Fatalf("after reload: %d files, cursor on %s, selected %v; want 53, f.txt and d.txt kept", len(tab.Files), cursorName(m), tab.Selected)
	}
}

func TestWatcherRefreshesThePreview(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "log.txt")
	writeTestFile(t, file, "first line\n")
	m := newTestModel(t, dir, nil)
	msgs := startWatching(t, m, 100*time.Millisecond)

	os.WriteFile(file, []byte("first line\nsecond line\n"), 0644)
	updated, cmd := m.Update(next(t, msgs))
	m = updated.(Model)
	isLoad := func(msg tea.Msg) bool { _, ok := msg.(dirLoadedMsg); return ok }
	for _, msg := range collect(cmd, 10*time.Second, isLoad) {
		if load, ok := msg.(dirLoadedMsg); ok {
			updated, cmd := m.Update(load)
			m = drain(t, updated.(Model), cmd)
		}
	}
	if !strings.Contains(m.tab().Preview.Content, "second line") {
		t.Fatalf("preview = %q, want the new content", m.tab().Preview.Content)
	}
}

func TestWatchSurvivesTheDirectoryBeingRecreated(t *testing.T) {
	root := t.TempDir()
	build := filepath.Join(root, "build")
	os.Mkdir(build, 0755)
	m := newTestModel(t, build, nil)
	first := startWatching(t, m, 100*time.Millisecond)

	// Every batch from here on, until the watcher stops
	batches := make(chan dirsChangedMsg, 16)
	go func() {
		<-first
		for {
			msg, ok := m.watch.listen()().(dirsChangedMsg)
			if !ok {
				return
			}
			batches <- msg
		}
	}()

	// A build tool starting afresh: the watch on the old directory goes
	// with it, so the new one has to be watched again
	os.RemoveAll(build)
	os.Mkdir(build, 0755)
	eventually(t, "the new build directory to be watched", func() bool {
		return slices.Contains(m.watch.watching(), build)
	})
	// Let the batches about the re-creation go by
	for settled := false; !settled; {
		select {
		case <-batches:
		case <-time.After(500 * time.Millisecond):
			settled = true
		}
	}

	writeTestFile(t, filepath.Join(build, "out.o"), "")
	deadline := time.After(10 * time.Second)
	for {
		select {
		case msg := <-batches:
			if slices.Contains(msg.dirs, build) {
				return
			}
		case <-deadline:
			t.Fatal("a file written to the new directory was not noticed")
		}
	}
}

func TestWatchFollowsNavigationAndTabs(t *testing.T) {
	root := t.TempDir()
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	os.Mkdir(a, 0755)
	os.Mkdir(b, 0755)

	m := newTestModel(t, a, nil)
	startWatching(t, m, 100*time.Millisecond)
	if got := m.watch.watching(); !slices.Contains(got, root) {
		t.Fatalf("watching %v, want the parent %s too", got, root)
	}

	// Going up watches the new directory and its parent, and drops a
	m, cmd := press(t, m, "h")
	m = drain(t, m, cmd)
	eventually(t, "the watch to follow", func() bool {
		got := m.watch.watching()
		return slices.Contains(got, root) && slices.Contains(got, filepath.Dir(root)) && !slices.Contains(got, a)
	})

	// A new tab adds its directory; closing it takes it away
	updated, cmd := m.createTab(b)
	m = drain(t, updated.(Model), cmd)
	eventually(t, "the new tab's watch", func() bool { return slices.Contains(m.watch.watching(), b) })
	m, _ = pressKey(t, m, tea.KeyCtrlW)
	eventually(t, "the closed tab's watch to go", func() bool { return !slices.Contains(m.watch.watching(), b) })
}

func TestWatchStaysWithinItsBudget(t *testing.T) {
	root := t.TempDir()
	big := filepath.Join(root, "big")
	os.Mkdir(big, 0755)
	for i := range 20 {
		writeTestFile(t, filepath.Join(big, fmt.Sprintf("f%d", i)), "")
	}
	m := newTestModel(t, big, nil)
	m.watch.budget = 10 // Room for root (one entry), but not for big's twenty
	m.watch.wanted = nil
	m.watchTabs()
	m.watch.watchDirs([]string{big, root, filepath.Join(root, "missing")})

	t.Cleanup(m.Close)
	go m.watch.listen()()
	eventually(t, "root to be watched", func() bool { return slices.Contains(m.watch.watching(), root) })
	if got := m.watch.watching(); len(got) != 1 {
		t.Fatalf("watching %v, want only %s: big is over budget and missing can't be watched", got, root)
	}
	if n, err := countEntries(big, 5); err != nil || n != 6 {
		t.Fatalf("countEntries = %d, %v; want it to stop at 6", n, err)
	}
}

func TestWatcherStopsOnClose(t *testing.T) {
	m := newTestModel(t, t.TempDir(), nil)
	msgs := startWatching(t, m, 100*time.Millisecond)
	m.Close()
	m.Close() // Twice is fine

	select {
	case <-m.watch.stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("watcher goroutine still running")
	}
	select {
	case msg := <-msgs:
		if msg != nil {
			t.Fatalf("listen returned %T after close, want nil", msg)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("listen still waiting after close")
	}
}

func TestWatchCanBeTurnedOff(t *testing.T) {
	m := newTestModel(t, t.TempDir(), noWatch())
	if m.watch != nil || m.Init() != nil {
		t.Fatal("watch: false should start no watcher")
	}
	m.Close() // Nothing to stop, but safe
}

func TestRefreshKeyReloadsEveryTab(t *testing.T) {
	d1, d2 := t.TempDir(), t.TempDir()
	m := newTestModel(t, d1, noWatch())
	updated, cmd := m.createTab(d2)
	m = drain(t, updated.(Model), cmd)

	writeTestFile(t, filepath.Join(d1, "new1.txt"), "")
	writeTestFile(t, filepath.Join(d2, "new2.txt"), "")
	m, cmd = pressKey(t, m, tea.KeyCtrlR)
	m = drain(t, m, cmd)
	if len(m.tabs[0].Files) != 1 || len(m.tabs[1].Files) != 1 {
		t.Fatalf("tabs list %d and %d files, want the new file in each", len(m.tabs[0].Files), len(m.tabs[1].Files))
	}
	if m.statusMsg != "Refreshed" {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
}

// changeDirs sends a change in dirs as the watcher would, and applies
// everything that follows
func changeDirs(t *testing.T, m Model, dirs ...string) Model {
	t.Helper()
	updated, cmd := m.Update(dirsChangedMsg{dirs: dirs})
	return drain(t, updated.(Model), cmd)
}

func TestReloadLeavesPromptsAndSearchAlone(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "c1.txt", "c2.txt", "c3.txt"} {
		writeTestFile(t, filepath.Join(dir, name), "")
	}
	m := newTestModel(t, dir, noWatch())

	// A rename in progress keeps its text, on the same file
	m, _ = press(t, m, "j")
	m, _ = press(t, m, "r")
	m = typeText(t, m, "-x")
	writeTestFile(t, filepath.Join(dir, "0.txt"), "")
	m = changeDirs(t, m, dir)
	if m.mode != ModeInput || m.prompt.input.Value() != "c1-x.txt" || cursorName(m) != "c1.txt" {
		t.Fatalf("rename: mode=%v value=%q cursor=%s", m.mode, m.prompt.input.Value(), cursorName(m))
	}
	m, _ = press(t, m, "esc")

	// A search keeps its query, and moving on goes from where the cursor is
	m = typeQuery(t, m, "c")
	m, _ = pressKey(t, m, tea.KeyDown) // c2
	writeTestFile(t, filepath.Join(dir, "0c.txt"), "")
	m = changeDirs(t, m, dir)
	if m.mode != ModeSearch || m.tab().SearchQuery != "c" || cursorName(m) != "c2.txt" || len(m.tab().SearchResults) != 4 {
		t.Fatalf("search: mode=%v query=%q cursor=%s results=%v", m.mode, m.tab().SearchQuery, cursorName(m), m.tab().SearchResults)
	}
	m, _ = pressKey(t, m, tea.KeyDown)
	if cursorName(m) != "c3.txt" {
		t.Fatalf("down after the reload went to %s, want c3.txt", cursorName(m))
	}
	m, _ = press(t, m, "esc")

	// Menus and the search palette stay open
	for _, keys := range []string{"s", "b", "fc"} {
		screen := typeText(t, m, keys)
		mode := screen.mode
		screen = changeDirs(t, screen, dir)
		if screen.mode != mode {
			t.Errorf("after %q: mode %v became %v", keys, mode, screen.mode)
		}
	}
}

func TestReloadKeepsThePreviewOfAnUnchangedFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "notes.txt")
	writeTestFile(t, file, "first\n")
	writeTestFile(t, filepath.Join(dir, "other.txt"), "")
	m := newTestModel(t, dir, noWatch())

	// A mark the preview would lose if it were loaded again
	m.tab().Preview.Content = "kept"
	writeTestFile(t, filepath.Join(dir, "new.txt"), "")
	if m = changeDirs(t, m, dir); cursorName(m) != "notes.txt" || m.tab().Preview.Content != "kept" {
		t.Fatalf("on %s, preview %q: the unchanged file's preview was loaded again", cursorName(m), m.tab().Preview.Content)
	}

	// A change to the file itself does reload it: its content, or its
	// permissions, which the heading shows
	os.WriteFile(file, []byte("first\nsecond\n"), 0644)
	if m = changeDirs(t, m, dir); !strings.Contains(m.tab().Preview.Content, "second") {
		t.Fatalf("preview = %q after the file changed", m.tab().Preview.Content)
	}
	if runtime.GOOS != "windows" {
		m.tab().Preview.Content = "kept"
		os.Chmod(file, 0600)
		if m = changeDirs(t, m, dir); m.tab().Preview.Content == "kept" {
			t.Fatal("the preview wasn't loaded again after a chmod")
		}
	}
}

func TestPromptsStayWithWhatTheyWereOpenedOn(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	os.Mkdir(sub, 0755)
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		writeTestFile(t, filepath.Join(sub, name), "")
	}

	// Renaming b.txt: a reload that would move the cursor keeps it, and
	// the field, on b.txt
	m := cursorTo(t, newTestModel(t, sub, noWatch()), "b.txt")
	m, _ = press(t, m, "r")
	m = typeText(t, m, "-x")
	m.tab().focusPath = filepath.Join(sub, "a.txt") // As a job finishing does
	m = changeDirs(t, m, sub)
	if m.mode != ModeInput || cursorName(m) != "b.txt" {
		t.Fatalf("rename: mode=%v cursor on %s, want the prompt still on b.txt", m.mode, cursorName(m))
	}
	l := m.layout()
	var list []string
	for _, line := range plain(m.View()) {
		list = append(list, utils.Cells(line, l.parentW, l.parentW+l.listW))
	}
	at := func(s string) int {
		return slices.IndexFunc(list, func(line string) bool { return strings.Contains(line, s) })
	}
	if a, b, c := at("a.txt"), at("b-x.txt"), at("c.txt"); a < 0 || a >= b || b >= c {
		t.Fatalf("the field should be on b's row, between a and c:\n%s", strings.Join(list, "\n"))
	}

	// A job moving b.txt takes the prompt with it
	moved := m
	moved.retarget(filepath.Join(sub, "b.txt"), filepath.Join(sub, "moved.txt"))
	if moved.prompt.target != filepath.Join(sub, "moved.txt") {
		t.Fatalf("prompt target = %s after the file moved", moved.prompt.target)
	}

	// Deleted meanwhile: the prompt closes, saying why
	os.Remove(filepath.Join(sub, "b.txt"))
	m = changeDirs(t, m, sub)
	if m.mode != ModeNormal || m.statusMsg != "b.txt is gone, so it wasn't renamed" {
		t.Fatalf("rename of a deleted file: mode=%v status=%q", m.mode, m.statusMsg)
	}

	// A new file's directory changes under the prompt: nothing is created,
	// in particular not in whatever the list shows now
	m, _ = press(t, m, "n")
	m = typeText(t, m, "new.txt")
	m.tab().CurrentPath = root // As a load started before the prompt landing
	m = submit(t, m)
	if m.mode != ModeNormal || m.statusMsg != "The folder shown changed, so nothing was created" || fs.Exists(filepath.Join(root, "new.txt")) {
		t.Fatalf("new file after the list moved: mode=%v status=%q", m.mode, m.statusMsg)
	}

	// Its directory deleted: the reload moves up, and the prompt closes
	m = newTestModel(t, sub, noWatch())
	m, _ = press(t, m, "N")
	m = typeText(t, m, "newdir")
	os.RemoveAll(sub)
	m = changeDirs(t, m, sub)
	if m.mode != ModeNormal || m.tab().CurrentPath != root || m.statusMsg != "sub is gone, so nothing was created" {
		t.Fatalf("new folder in a deleted directory: mode=%v in %s, status=%q", m.mode, m.tab().CurrentPath, m.statusMsg)
	}
	if m = submit(t, m); fs.Exists(filepath.Join(root, "newdir")) {
		t.Fatal("the folder was created in the parent")
	}
}

func TestReloadOfDeletedDirectoryMovesUp(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub", "deeper")
	os.MkdirAll(sub, 0755)
	m := newTestModel(t, sub, noWatch())

	os.RemoveAll(filepath.Join(root, "sub"))
	m = changeDirs(t, m, sub)
	if m.tab().CurrentPath != root || !strings.Contains(m.statusMsg, "no longer exists") {
		t.Fatalf("in %s with status %q, want %s", m.tab().CurrentPath, m.statusMsg, root)
	}
}

func TestReloadWaitsForALoadingTab(t *testing.T) {
	dir := t.TempDir()
	m := newTestModel(t, dir, noWatch())
	load := m.loadDir(m.tab(), dir) // In flight
	seq := m.tab().loadSeq

	// However many changes come meanwhile, nothing reloads yet, and nothing
	// is left ticking to try again, so a load that hangs costs nothing
	for range 5 {
		updated, cmd := m.Update(dirsChangedMsg{dirs: []string{dir}})
		m = updated.(Model)
		if m.tab().loadSeq != seq {
			t.Fatal("a reload cut the tab's load short")
		}
		if cmd != nil {
			t.Fatalf("a change during a load left a command running, giving %T", cmd())
		}
	}
	// A refresh meanwhile waits too, rather than being dropped
	m, _ = ctrl(t, m, tea.KeyCtrlR)
	if m.tab().loadSeq != seq {
		t.Fatal("a refresh cut the tab's load short")
	}

	// Once the load is in, the tab reloads, once
	updated, cmd := m.Update(load())
	if m = updated.(Model); m.tab().loadSeq != seq+1 {
		t.Fatalf("%d reloads once the load was in, want 1", m.tab().loadSeq-seq)
	}
	if m = drain(t, m, cmd); m.tab().loadSeq != seq+1 || m.tab().Loading {
		t.Fatalf("the reload was followed by %d more", m.tab().loadSeq-seq-1)
	}

	// Likewise after a load that failed: the tab stays, and reloads
	load = m.loadDir(m.tab(), filepath.Join(dir, "missing"))
	m = changeDirs(t, m, dir)
	updated, cmd = m.Update(load())
	if m = drain(t, updated.(Model), cmd); m.tab().loadSeq != seq+3 || m.tab().CurrentPath != dir {
		t.Fatalf("after a failed load: %d loads in %s, want 3 in %s", m.tab().loadSeq-seq, m.tab().CurrentPath, dir)
	}
}

// openFiles counts the files the test process has open, or returns -1
// where that can't be seen. Only the names are read: stat fails on the
// descriptor listing them.
func openFiles() int {
	f, err := os.Open("/dev/fd")
	if err != nil {
		return -1
	}
	defer f.Close()
	names, err := f.Readdirnames(-1)
	if err != nil {
		return -1
	}
	return len(names)
}

func TestWatchLetsGoOfADirectoryThatOutgrowsItsBudget(t *testing.T) {
	dir := t.TempDir()
	for i := range 5 {
		writeTestFile(t, filepath.Join(dir, fmt.Sprintf("old%d", i)), "")
	}
	m := newTestModel(t, dir, nil)
	m.watch.budget = 40 // Room for the directory and its parent, until the paste
	msgs := startWatching(t, m, 50*time.Millisecond)
	// Where fsnotify uses kqueue, which opens every file it watches
	before := -1
	if watchBudget() > 0 {
		before = openFiles()
	}
	if strings.Contains(ansi.Strip(m.renderHeader()), "not watched") {
		t.Fatalf("a watched directory is said not to be: %s", ansi.Strip(m.renderHeader()))
	}

	// Like a paste: kqueue would open each new file, so the watch goes
	for i := range 200 {
		writeTestFile(t, filepath.Join(dir, fmt.Sprintf("new%03d", i)), "")
	}
	eventually(t, "the watch to be let go", func() bool { return !slices.Contains(m.watch.watching(), dir) })
	if !m.watch.unwatched(dir) {
		t.Fatal("the directory let go isn't reported as unwatched")
	}
	if before >= 0 {
		eventually(t, "the files the watch opened to be closed", func() bool { return openFiles() <= before+10 })
	}

	// The interface hears of it, and says so beside the path
	updated, _ := m.Update(next(t, msgs))
	m = updated.(Model)
	if header := ansi.Strip(m.renderHeader()); !strings.Contains(header, "not watched: ctrl+r refreshes") {
		t.Fatalf("header = %q, want a note that the directory isn't watched", header)
	}

	// Once it has room again, a refresh watches it again
	for i := range 200 {
		os.Remove(filepath.Join(dir, fmt.Sprintf("new%03d", i)))
	}
	m, _ = ctrl(t, m, tea.KeyCtrlR)
	eventually(t, "the directory to be watched again", func() bool { return slices.Contains(m.watch.watching(), dir) })
	if header := ansi.Strip(m.renderHeader()); m.watch.unwatched(dir) || strings.Contains(header, "not watched") {
		t.Fatalf("watched again, but header = %q", header)
	}
}

func TestReloadIgnoresOtherDirectories(t *testing.T) {
	dir := t.TempDir()
	m := newTestModel(t, dir, noWatch())
	seq := m.tab().loadSeq
	updated, _ := m.Update(dirsChangedMsg{dirs: []string{t.TempDir()}})
	if m = updated.(Model); m.tab().loadSeq != seq {
		t.Fatal("a change elsewhere reloaded the tab")
	}
	// The parent is shown too, so a change there reloads
	updated, _ = m.Update(dirsChangedMsg{dirs: []string{filepath.Dir(dir)}})
	if m = updated.(Model); m.tab().loadSeq != seq+1 {
		t.Fatal("a change in the parent did not reload the tab")
	}
}
