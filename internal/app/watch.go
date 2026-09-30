package app

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fsnotify/fsnotify"
)

const (
	// watchQuiet is how long changes must pause before tabs reload, so a
	// build writing many files causes one reload rather than hundreds
	watchQuiet = 200 * time.Millisecond
	// watchMaxWait is the longest a reload waits while changes keep coming,
	// so a file being written to continuously still shows progress
	watchMaxWait = 2 * time.Second
	// kqueueBudget is how many files watches may hold open where fsnotify
	// uses kqueue, which opens every entry of a watched directory
	kqueueBudget = 2048
)

// dirsChangedMsg names directories whose contents changed on disk
type dirsChangedMsg struct {
	dirs  []string
	retry bool // Put off while a tab was loading, rather than sent by the watcher
}

// dirWatcher watches the directories the tabs show and reports changes in
// debounced batches. Events are gathered by its own goroutine; listen
// hands each batch to Bubble Tea. Watching is best effort: a directory
// that can't be watched still refreshes with ctrl+r.
type dirWatcher struct {
	quiet, maxWait time.Duration
	budget         int // Files the watches may hold open; 0 for no limit

	want    chan []string // Directories to watch, most important first; the latest set wins
	changes chan []string // Directories that changed, one batch per reload
	done    chan struct{} // Closed by stop
	stopped chan struct{} // Closed once the goroutine has finished
	started sync.Once
	closed  sync.Once
	failed  bool     // Watching couldn't start; set within started
	wanted  []string // Last set sent on want; used only by the UI goroutine

	mu     sync.Mutex
	active []string // Directories being watched, for tests
}

func newDirWatcher() *dirWatcher {
	return &dirWatcher{
		quiet:   watchQuiet,
		maxWait: watchMaxWait,
		budget:  watchBudget(),
		want:    make(chan []string, 1),
		changes: make(chan []string),
		done:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

// watchBudget limits the files watches may hold open where fsnotify uses
// kqueue: watching a very large directory could otherwise use up the
// files sushi needs to open for itself
func watchBudget() int {
	switch runtime.GOOS {
	case "darwin", "freebsd", "openbsd", "netbsd", "dragonfly":
		return kqueueBudget
	}
	return 0
}

// listen starts watching if it hasn't yet, then waits for the next batch
// of changes. It is issued again after every batch.
func (w *dirWatcher) listen() tea.Cmd {
	if w == nil {
		return nil
	}
	return func() tea.Msg {
		w.started.Do(w.start)
		if w.failed {
			return nil
		}
		select {
		case dirs := <-w.changes:
			return dirsChangedMsg{dirs: dirs}
		case <-w.done:
			return nil
		}
	}
}

// start creates the watcher and its goroutine
func (w *dirWatcher) start() {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		// Out of inotify instances, say: tabs refresh with ctrl+r only
		w.failed = true
		close(w.stopped)
		return
	}
	go w.run(fsw)
}

// stop ends watching. It is safe to call more than once, and before
// watching has started.
func (w *dirWatcher) stop() {
	if w != nil {
		w.closed.Do(func() { close(w.done) })
	}
}

// watchDirs asks for dirs to be watched instead of the current set
func (w *dirWatcher) watchDirs(dirs []string) {
	if slices.Equal(dirs, w.wanted) {
		return
	}
	w.wanted = dirs
	for {
		select {
		case w.want <- dirs:
			return
		default:
			// Replace a set the goroutine hasn't picked up yet
			select {
			case <-w.want:
			default:
			}
		}
	}
}

// watching returns the directories being watched
func (w *dirWatcher) watching() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.active)
}

// run follows fsnotify's events until stop, gathering the directories that
// changed into batches: one is sent once changes pause for w.quiet, or
// after w.maxWait if they don't
func (w *dirWatcher) run(fsw *fsnotify.Watcher) {
	defer close(w.stopped)
	defer fsw.Close()

	watched := make(map[string]int)  // Directory → files its watch holds open
	pending := make(map[string]bool) // Changed since the last batch
	var wanted []string              // What the UI asked to watch
	var ready []string               // A batch the UI hasn't taken yet

	quiet := time.NewTimer(w.quiet)
	most := time.NewTimer(w.maxWait)
	quiet.Stop()
	most.Stop()
	var quietC, mostC <-chan time.Time
	changed := func(dir string) {
		if _, ok := watched[dir]; !ok {
			return
		}
		pending[dir] = true
		quiet.Reset(w.quiet)
		quietC = quiet.C
		if mostC == nil {
			most.Reset(w.maxWait)
			mostC = most.C
		}
	}
	flush := func() {
		for dir := range pending {
			if !slices.Contains(ready, dir) {
				ready = append(ready, dir)
			}
		}
		clear(pending)
		quiet.Stop()
		most.Stop()
		quietC, mostC = nil, nil
	}

	for {
		var out chan<- []string
		if len(ready) > 0 {
			out = w.changes
		}
		select {
		case <-w.done:
			return
		case wanted = <-w.want:
			w.watch(fsw, watched, wanted)
		case ev, ok := <-fsw.Events:
			if !ok {
				return
			}
			// Only permissions or times changed: indexers and editors do
			// this all the time, and the lists don't show it
			if ev.Op == fsnotify.Chmod {
				continue
			}
			// The directory holding what changed, or the directory itself
			// if it was removed or renamed
			changed(filepath.Dir(ev.Name))
			changed(ev.Name)
			if _, ok := watched[ev.Name]; ok && ev.Has(fsnotify.Remove|fsnotify.Rename) {
				// Its watch has gone with it. If a directory of that name
				// comes back, as build directories do, the next batch
				// watches it again.
				fsw.Remove(ev.Name)
				delete(watched, ev.Name)
			}
		case err, ok := <-fsw.Errors:
			if !ok {
				return
			}
			if errors.Is(err, fsnotify.ErrEventOverflow) {
				// Events were lost, so anything may have changed
				for dir := range watched {
					changed(dir)
				}
			}
		case <-quietC:
			flush()
			w.watch(fsw, watched, wanted)
		case <-mostC:
			flush()
			w.watch(fsw, watched, wanted)
		case out <- ready:
			ready = nil
		}
	}
}

// watch makes the watches match dirs. Directories that can't be watched,
// or would take the open files over budget, are left out.
func (w *dirWatcher) watch(fsw *fsnotify.Watcher, watched map[string]int, dirs []string) {
	used := 0
	for dir, cost := range watched {
		if slices.Contains(dirs, dir) {
			used += cost
			continue
		}
		fsw.Remove(dir)
		delete(watched, dir)
	}
	for _, dir := range dirs {
		if _, ok := watched[dir]; ok {
			continue
		}
		cost := 1
		if w.budget > 0 {
			n, err := countEntries(dir, w.budget-used-1)
			if err != nil || used+n+1 > w.budget {
				continue
			}
			cost = n + 1
		}
		if err := fsw.Add(dir); err != nil {
			// Let go of whatever a partly made watch opened
			fsw.Remove(dir)
			continue
		}
		watched[dir] = cost
		used += cost
	}

	active := make([]string, 0, len(watched))
	for dir := range watched {
		active = append(active, dir)
	}
	sort.Strings(active)
	w.mu.Lock()
	w.active = active
	w.mu.Unlock()
}

// countEntries counts the entries of dir, stopping once there are more
// than limit
func countEntries(dir string, limit int) (int, error) {
	if limit < 1 {
		return 1, nil
	}
	f, err := os.Open(dir)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	names, err := f.Readdirnames(limit + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return 0, err
	}
	return len(names), nil
}

// watchTabs points the watcher at the directories the tabs show, and the
// parents shown beside them: the active tab's first, as they matter most
func (m *Model) watchTabs() {
	if m.watch == nil {
		return
	}
	dirs := make([]string, 0, 2*len(m.tabs))
	add := func(tab *Tab) {
		for _, dir := range []string{tab.CurrentPath, filepath.Dir(tab.CurrentPath)} {
			if !slices.Contains(dirs, dir) {
				dirs = append(dirs, dir)
			}
		}
	}
	add(m.tab())
	for i := range m.tabs {
		add(&m.tabs[i])
	}
	m.watch.watchDirs(dirs)
}

// handleDirsChanged reloads the tabs showing a directory that changed. The
// reload keeps each tab's cursor and selection, and leaves any open prompt,
// menu or search as it is. A tab that is loading is retried shortly
// rather than reloaded, which would cut its load short.
func (m *Model) handleDirsChanged(msg dirsChangedMsg) tea.Cmd {
	changed := make(map[string]bool, len(msg.dirs))
	for _, dir := range msg.dirs {
		changed[dir] = true
	}

	var cmds []tea.Cmd
	var later []string
	for i := range m.tabs {
		tab := &m.tabs[i]
		if !changed[tab.CurrentPath] && !changed[filepath.Dir(tab.CurrentPath)] {
			continue
		}
		if tab.Loading {
			later = append(later, tab.CurrentPath)
			continue
		}
		cmds = append(cmds, m.reloadTab(tab))
	}
	if len(later) > 0 {
		cmds = append(cmds, tea.Tick(watchQuiet, func(time.Time) tea.Msg {
			return dirsChangedMsg{dirs: later, retry: true}
		}))
	}
	if !msg.retry {
		cmds = append(cmds, m.watch.listen())
	}
	return tea.Batch(cmds...)
}

// reloadTab reloads the tab's directory, or if it has been deleted, the
// nearest directory above it that still exists
func (m *Model) reloadTab(tab *Tab) tea.Cmd {
	dir := existingDir(tab.CurrentPath)
	load := m.loadDir(tab, dir)
	if dir != tab.CurrentPath && tab.ID == m.tab().ID {
		return tea.Batch(load, m.setStatus(filepath.Base(tab.CurrentPath)+" no longer exists"))
	}
	return load
}

// existingDir returns path, or its nearest ancestor if path no longer
// exists. Other problems, such as a lack of permission, are left for the
// directory load to report.
func existingDir(path string) string {
	for {
		info, err := os.Stat(path)
		if err == nil && info.IsDir() || err != nil && !errors.Is(err, os.ErrNotExist) {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return path
		}
		path = parent
	}
}

// refresh reloads every tab, for changes the watcher can't see, such as on
// network drives, or when watch: false
func (m Model) refresh() (tea.Model, tea.Cmd) {
	cmds := []tea.Cmd{m.setStatus("Refreshed")}
	for i := range m.tabs {
		// A tab that is loading is about to be up to date anyway
		if tab := &m.tabs[i]; !tab.Loading {
			cmds = append(cmds, m.reloadTab(tab))
		}
	}
	cmd := tea.Batch(cmds...)
	return m, cmd
}
