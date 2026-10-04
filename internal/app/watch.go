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

// dirsChangedMsg names directories whose contents changed on disk. It
// comes with none when only the directories left unwatched changed, so the
// breadcrumb can say so.
type dirsChangedMsg struct {
	dirs []string
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
	missed []string // Directories wanted but not watched: over budget, or can't be
	broken bool     // Watching couldn't start, so nothing is watched
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

// watchBudget limits the files watches may hold open on macOS, where
// fsnotify uses kqueue: watching a very large directory could otherwise use
// up the files sushi needs to open for itself
func watchBudget() int {
	if runtime.GOOS == "darwin" {
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
		w.mu.Lock()
		w.broken = true
		w.mu.Unlock()
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

// rewatch asks for the directories to be watched again, as a refresh
// does: one that was over budget may fit now
func (w *dirWatcher) rewatch() {
	if w == nil {
		return
	}
	dirs := w.wanted
	w.wanted = nil
	w.watchDirs(dirs)
}

// watching returns the directories being watched
func (w *dirWatcher) watching() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.active)
}

// unwatched reports whether dir was asked for but isn't being watched, so
// its changes show only after a refresh. With watching turned off in the
// config nothing is asked for, and nothing needs saying.
func (w *dirWatcher) unwatched(dir string) bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.broken || slices.Contains(w.missed, dir)
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
	notify := false                  // The directories left unwatched changed
	var orphans []string             // Entries of directories let go, maybe still open

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
	release := func() {
		for _, path := range orphans {
			if _, ok := watched[path]; !ok {
				fsw.Remove(path) // Fails harmlessly if it wasn't opened
			}
		}
		orphans = nil
	}

	for {
		var out chan<- []string
		if len(ready) > 0 || notify {
			out = w.changes
		}
		select {
		case <-w.done:
			return
		case wanted = <-w.want:
			notify = w.watch(fsw, watched, wanted) || notify
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
			// kqueue opens every entry created in a watched directory, so
			// pasting thousands of files would hold thousands open. Count
			// them as they come, and let go of a directory once it takes
			// the total over budget, rather than at the end of the burst.
			// An entry removed or renamed is closed.
			dir := filepath.Dir(ev.Name)
			cost, inWatched := watched[dir]
			switch {
			case w.budget == 0:
			case inWatched && ev.Has(fsnotify.Remove|fsnotify.Rename):
				watched[dir] = max(cost-1, 1)
			case inWatched && ev.Has(fsnotify.Create):
				watched[dir] = cost + 1
				if total(watched) > w.budget {
					notify = w.watch(fsw, watched, wanted) || notify
				}
			case ev.Has(fsnotify.Create):
				if _, ok := watched[ev.Name]; !ok {
					// From a directory let go mid-burst: fsnotify goes on
					// opening the new entries it had already listed there,
					// just after reporting each. Close them once it settles.
					orphans = append(orphans, ev.Name)
					quiet.Reset(w.quiet)
					quietC = quiet.C
				}
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
			release()
			notify = w.watch(fsw, watched, wanted) || notify
		case <-mostC:
			flush()
			release()
			notify = w.watch(fsw, watched, wanted) || notify
		case out <- ready:
			ready, notify = nil, false
		}
	}
}

// watch makes the watches match dirs, which come most important first.
// Directories that can't be watched, or would take the open files over
// budget, are left out. With a budget, directories already watched are
// counted again, as their watches hold a file open for every entry
// created since, and those now over budget are let go. It reports whether
// the directories left out changed.
func (w *dirWatcher) watch(fsw *fsnotify.Watcher, watched map[string]int, dirs []string) bool {
	for dir := range watched {
		if !slices.Contains(dirs, dir) {
			fsw.Remove(dir)
			delete(watched, dir)
		}
	}
	used := 0
	var missed []string
	for _, dir := range dirs {
		_, have := watched[dir]
		cost := 1
		if w.budget > 0 {
			n, err := countEntries(dir, w.budget-used-1)
			if err != nil || used+n+1 > w.budget {
				if have {
					fsw.Remove(dir)
					delete(watched, dir)
				}
				missed = append(missed, dir)
				continue
			}
			cost = n + 1
		}
		if !have {
			if err := fsw.Add(dir); err != nil {
				// Let go of whatever a partly made watch opened
				fsw.Remove(dir)
				missed = append(missed, dir)
				continue
			}
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
	defer w.mu.Unlock()
	changed := !slices.Equal(missed, w.missed)
	w.active, w.missed = active, missed
	return changed
}

// total returns how many files the watches hold open
func total(watched map[string]int) int {
	n := 0
	for _, cost := range watched {
		n += cost
	}
	return n
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
// parents shown beside them: the active tab's first, as they matter most.
// Last, so they are the first left out, come their Git repositories, for
// what git commands run elsewhere change.
func (m *Model) watchTabs() {
	if m.watch == nil {
		return
	}
	dirs := make([]string, 0, 2*len(m.tabs))
	add := func(list ...string) {
		for _, dir := range list {
			if !slices.Contains(dirs, dir) {
				dirs = append(dirs, dir)
			}
		}
	}
	// Inside an archive, the folder holding it; see archive.go
	add(m.tab().realDir(), filepath.Dir(m.tab().realDir()))
	for i := range m.tabs {
		add(m.tabs[i].realDir(), filepath.Dir(m.tabs[i].realDir()))
	}
	for i := range m.tabs {
		if tab := &m.tabs[i]; tab.git.dir == tab.CurrentPath {
			add(tab.git.watch...)
		}
	}
	m.watch.watchDirs(dirs)
}

// handleDirsChanged reloads the tabs showing a directory that changed. The
// reload keeps each tab's cursor and selection, and leaves any open prompt,
// menu or search as it is. A tab that is loading is reloaded once its
// load is in, however many changes come meanwhile, rather than now, which
// would cut its load short. A tab whose Git repository changed reads its
// status again, as a reload does too.
func (m *Model) handleDirsChanged(msg dirsChangedMsg) tea.Cmd {
	changed := make(map[string]bool, len(msg.dirs))
	for _, dir := range msg.dirs {
		changed[dir] = true
	}

	var cmds []tea.Cmd
	for i := range m.tabs {
		tab := &m.tabs[i]
		if dir := tab.realDir(); !changed[dir] && !changed[filepath.Dir(dir)] {
			if tab.gitChanged(changed) && !tab.Loading {
				cmds = append(cmds, m.runGit(tab))
			}
			continue
		}
		if tab.Loading {
			tab.reloadWanted = true
			continue
		}
		cmds = append(cmds, m.reloadTab(tab))
	}
	cmds = append(cmds, m.watch.listen())
	return tea.Batch(cmds...)
}

// reloadTab reloads the tab's directory, or if it has been deleted, the
// nearest directory above it that still exists
func (m *Model) reloadTab(tab *Tab) tea.Cmd {
	dir := existingDir(tab.CurrentPath)
	if a := tab.archive; a != nil && archiveAt(tab.CurrentPath) == a.ix.Path {
		dir = tab.CurrentPath // Inside an archive that is still there; see archive.go
	}
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
// network drives, or when watch: false. It also tries again to watch the
// directories that weren't watched, and to read their Git status, and
// forgets the apps found for Open with, as apps come and go.
func (m Model) refresh() (tea.Model, tea.Cmd) {
	m.watch.rewatch()
	m.retryGit()
	m.openWith.cache = nil
	cmds := []tea.Cmd{m.setStatus("Refreshed")}
	for i := range m.tabs {
		// A load in flight may have read its directory before the change
		// that prompted the refresh, so the tab reloads once it is in
		if tab := &m.tabs[i]; tab.Loading {
			tab.reloadWanted = true
		} else {
			cmds = append(cmds, m.reloadTab(tab))
		}
	}
	cmd := tea.Batch(cmds...)
	return m, cmd
}
