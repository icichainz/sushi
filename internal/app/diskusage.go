package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/ui"
	"github.com/icichainz/sushi/internal/utils"
)

// The disk usage view (U), like ncdu: the folder under the cursor, or the
// current folder, is walked in the background, and the entries of the
// folder shown are listed largest first, filling in while the walk goes
// on. Folders are entered without walking them again. Everything is
// counted, hidden files included; symlinks are never followed, and other
// volumes mounted inside are listed but not entered.

// duInterval is the least time between redraws while a walk goes on
var duInterval = 100 * time.Millisecond

// duOpenDir opens a folder to walk; tests replace it to hold a walk up.
// A walk takes it when it starts, so replacing it races with no walk.
var duOpenDir = os.Open

// duNode is a file or folder the walk found. A folder's figures add up
// everything below it. The walk's goroutine changes nodes holding its
// scan's lock, and the interface copies what it shows under the lock.
type duNode struct {
	name     string // The full path for the root
	parent   *duNode
	dir      bool
	link     bool
	mount    bool  // A folder on another volume, not entered
	size     int64 // Apparent size: the bytes in files and links
	disk     int64 // Allocated on disk, each hard-linked file once
	files    int   // Files and links, or 1 for one
	unread   int   // Folders that couldn't be read, this one included
	children []*duNode
	scanned  bool // A folder's contents are all counted
}

// duSum is what a batch of entries adds to the folders above it
type duSum struct {
	size, disk    int64
	files, unread int
}

// addTo adds the sum to n and every folder above it
func (s duSum) addTo(n *duNode) {
	for ; n != nil; n = n.parent {
		n.size += s.size
		n.disk += s.disk
		n.files += s.files
		n.unread += s.unread
	}
}

// sumOf is what n adds to the folders above it
func sumOf(n *duNode) duSum {
	return duSum{n.size, n.disk, n.files, n.unread}
}

// duScan is a walk of a folder in the background
type duScan struct {
	root    *duNode
	path    string // Of the root
	cancel  context.CancelFunc
	changed chan struct{} // Signalled when the figures change
	done    chan struct{} // Closed once the walk has returned
	began   time.Time

	mu      sync.Mutex // Guards the nodes and what follows
	ended   time.Time  // Zero while it walks
	stopped bool       // Cancelled before it was done
	err     error      // The root couldn't be read
}

// duWalker is what one walk keeps to itself
type duWalker struct {
	dev       uint64             // The root's volume
	seen      map[[2]uint64]bool // Hard-linked files counted on disk already
	open      func(string) (*os.File, error)
	reuse     *duNode // A finished walk of a folder below, to take as it is
	reusePath string
}

// startDiskUsage walks root in the background and shows it. A finished
// walk of one of its folders is taken over rather than walked again, and
// focus names the entry to put the cursor on once it is found.
func (m *Model) startDiskUsage(root, focus string, reuse *duScan) tea.Cmd {
	m.stopDiskUsage()
	ctx, cancel := context.WithCancel(context.Background())
	s := &duScan{root: &duNode{name: root, dir: true}, path: root, cancel: cancel,
		changed: make(chan struct{}, 1), done: make(chan struct{}), began: time.Now()}
	w := &duWalker{seen: make(map[[2]uint64]bool), open: duOpenDir}
	if reuse != nil {
		reuse.mu.Lock()
		if !reuse.ended.IsZero() && !reuse.stopped && reuse.err == nil {
			w.reuse, w.reusePath = reuse.root, reuse.path
		}
		reuse.mu.Unlock()
	}
	go s.run(ctx, w)

	m.du = duView{scan: s, path: root, dir: s.root, focus: focus}
	m.du.refresh()
	return waitDU(s)
}

// stopDiskUsage cancels the walk, if one is going on
func (m *Model) stopDiskUsage() {
	if m.du.scan != nil {
		m.du.scan.cancel()
	}
}

// run walks the root and says when it is done
func (s *duScan) run(ctx context.Context, w *duWalker) {
	defer close(s.done)
	info, err := os.Lstat(s.path)
	if err == nil && !info.IsDir() {
		err = fmt.Errorf("%s is not a folder", filepath.Base(s.path))
	}
	if err == nil {
		w.dev, _ = statIDs(info)
		s.walk(ctx, w, s.root, s.path)
	}
	s.mu.Lock()
	s.err = err
	s.ended = time.Now()
	s.stopped = ctx.Err() != nil
	s.mu.Unlock()
}

// walk counts what is in the folder dir at path: first all its entries,
// so its listing is whole early on, then its folders, one by one
func (s *duScan) walk(ctx context.Context, w *duWalker, dir *duNode, path string) {
	f, err := w.open(path)
	if err != nil {
		s.mu.Lock()
		duSum{unread: 1}.addTo(dir)
		dir.scanned = true
		s.mu.Unlock()
		s.signal()
		return
	}
	var folders []*duNode
	for {
		if ctx.Err() != nil {
			f.Close()
			return
		}
		entries, rerr := f.ReadDir(256)
		batch := make([]*duNode, 0, len(entries))
		var sum duSum
		for _, e := range entries {
			info, err := e.Info()
			if err != nil {
				// Gone since it was listed, or can't be looked at
				if !errors.Is(err, os.ErrNotExist) {
					sum.unread++
				}
				continue
			}
			n := &duNode{name: e.Name(), parent: dir, dir: info.IsDir(), link: info.Mode()&os.ModeSymlink != 0}
			n.disk = w.allocated(info)
			switch dev, _ := statIDs(info); {
			case !n.dir:
				n.size, n.files = info.Size(), 1
			case dev != w.dev:
				n.mount, n.scanned = true, true
			default:
				folders = append(folders, n)
			}
			sum.size += n.size
			sum.disk += n.disk
			sum.files += n.files
			batch = append(batch, n)
		}
		if rerr != nil && rerr != io.EOF {
			sum.unread++
		}
		s.mu.Lock()
		dir.children = append(dir.children, batch...)
		sum.addTo(dir)
		s.mu.Unlock()
		s.signal()
		if rerr != nil {
			break
		}
	}
	// Closed before going deeper, so a deep tree holds one folder open
	f.Close()

	for _, sub := range folders {
		if ctx.Err() != nil {
			return
		}
		subPath := filepath.Join(path, sub.name)
		if w.reuse != nil && subPath == w.reusePath {
			s.graft(sub, w.reuse)
			w.reuse = nil
			continue
		}
		s.walk(ctx, w, sub, subPath)
	}
	s.mu.Lock()
	dir.scanned = ctx.Err() == nil
	s.mu.Unlock()
}

// graft takes the finished walk of a folder, done before, as sub's
// contents. Its root's own blocks weren't counted; sub's were.
func (s *duScan) graft(sub, done *duNode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub.children = done.children
	for _, c := range sub.children {
		c.parent = sub
	}
	sumOf(done).addTo(sub)
	sub.scanned = true
}

// running reports whether the walk is still going on
func (s *duScan) running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ended.IsZero()
}

// signal says the figures changed, without waiting for anyone to look
func (s *duScan) signal() {
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

// allocated returns the bytes info's entry takes on disk. A file with
// more than one name (a hard link) takes them once, under the first name
// found.
func (w *duWalker) allocated(info os.FileInfo) int64 {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return info.Size()
	}
	if !info.IsDir() && uint64(st.Nlink) > 1 {
		id := [2]uint64{uint64(st.Dev), uint64(st.Ino)}
		if w.seen[id] {
			return 0
		}
		w.seen[id] = true
	}
	return int64(st.Blocks) * 512
}

// statIDs returns the device and inode of info's file, or zeros where the
// system doesn't say
func statIDs(info os.FileInfo) (dev, ino uint64) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0
	}
	return uint64(st.Dev), uint64(st.Ino)
}

// duScanMsg says a walk's figures changed, or that it is done
type duScanMsg struct{ scan *duScan }

// waitDU waits for the walk's figures to change, then for duInterval, so
// a walk redraws the view a few times a second however fast it goes
func waitDU(s *duScan) tea.Cmd {
	return func() tea.Msg {
		select {
		case <-s.done:
		case <-s.changed:
			select {
			case <-s.done:
			case <-time.After(duInterval):
			}
		}
		return duScanMsg{scan: s}
	}
}

func (msg duScanMsg) apply(m Model) (tea.Model, tea.Cmd) {
	if msg.scan != m.du.scan {
		return m, nil // From a walk since replaced or closed
	}
	m.du.refresh()
	if !m.du.status.scanning {
		return m, nil
	}
	return m, waitDU(msg.scan)
}

// duView is the disk usage view: the folder shown, and copies of the
// figures it shows, taken from the walk under its lock
type duView struct {
	scan   *duScan
	path   string  // The folder shown
	dir    *duNode // Its node
	rows   []duRow // Its entries, largest first
	here   duRow   // The folder shown
	cursor int
	focus  string // Entry to put the cursor on once it shows up
	status duStatus
}

// duRow is a copy of what a node says
type duRow struct {
	node             *duNode
	name             string
	dir, link, mount bool
	size, disk       int64
	files, unread    int
	scanned          bool
}

// duStatus is a copy of how the walk is going, with its totals
type duStatus struct {
	root     duRow
	scanning bool
	stopped  bool // Cancelled before it was done
	took     time.Duration
	err      error
}

func duRowOf(n *duNode) duRow {
	return duRow{node: n, name: n.name, dir: n.dir, link: n.link, mount: n.mount, size: n.size, disk: n.disk,
		files: n.files, unread: n.unread, scanned: n.scanned}
}

// refresh copies the figures of the folder shown from the walk, keeping
// the cursor on the entry it was on as the order changes
func (v *duView) refresh() {
	s := v.scan
	if s == nil {
		return
	}
	var current *duNode
	if v.cursor < len(v.rows) {
		current = v.rows[v.cursor].node
	}

	s.mu.Lock()
	rows := make([]duRow, 0, len(v.dir.children))
	for _, c := range v.dir.children {
		rows = append(rows, duRowOf(c))
	}
	v.here = duRowOf(v.dir)
	v.status = duStatus{root: duRowOf(s.root), scanning: s.ended.IsZero(), err: s.err}
	if !s.ended.IsZero() {
		v.status.took = s.ended.Sub(s.began)
		v.status.stopped = s.stopped
	}
	s.mu.Unlock()

	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.size != b.size {
			return a.size > b.size
		}
		if a.disk != b.disk {
			return a.disk > b.disk
		}
		return a.name < b.name
	})
	v.rows = rows
	v.cursor = min(v.cursor, max(len(rows)-1, 0))
	for i, r := range rows {
		if v.focus != "" && r.name == v.focus {
			v.cursor, v.focus, current = i, "", nil
			break
		}
	}
	for i, r := range rows {
		if current != nil && r.node == current {
			v.cursor = i
			break
		}
	}
}

// move moves the cursor by delta, stopping at the ends
func (v *duView) move(delta int) {
	v.cursor = max(min(v.cursor+delta, len(v.rows)-1), 0)
	v.focus = ""
}

// chosen returns the entry under the cursor and its path
func (v *duView) chosen() (duRow, string, bool) {
	if v.cursor >= len(v.rows) {
		return duRow{}, "", false
	}
	r := v.rows[v.cursor]
	return r, filepath.Join(v.path, r.name), true
}

// openDiskUsage opens the disk usage view on the folder under the cursor,
// or on the current folder when the cursor is on anything else
func (m Model) openDiskUsage() (tea.Model, tea.Cmd) {
	tab := m.tab()
	root := tab.CurrentPath
	if len(tab.Files) > 0 {
		if f := tab.Files[tab.Cursor]; f.IsDir && !f.IsSymlink {
			root = f.Path
		}
	}
	m.mode = ModeDiskUsage
	cmd := m.startDiskUsage(root, "", nil)
	return m, cmd
}

// closeDiskUsage closes the view, stopping its walk
func (m Model) closeDiskUsage() (tea.Model, tea.Cmd) {
	m.stopDiskUsage()
	m.du = duView{}
	m.mode = ModeNormal
	return m, nil
}

// handleDiskUsageMode handles keys in the disk usage view. Its own keys
// come first: esc stops the walk, then closes the view; enter opens; g
// goes to the entry in the file list. Moving, going up and in, trashing,
// Quick Look, showing in Finder and refreshing follow the key map; q
// closes the view unless one of those has taken it.
func (m Model) handleDiskUsageMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	v := &m.du
	k := m.keys
	switch msg.String() {
	case "esc":
		if v.scan.running() {
			v.scan.cancel()
			cmd := m.setStatus("Scan stopped: the sizes are what it had counted")
			return m, cmd
		}
		return m.closeDiskUsage()
	case "enter":
		return m.duOpen()
	case "g":
		return m.duGoTo()
	}
	rows, _ := m.duRows()
	switch {
	case key.Matches(msg, k.Up):
		v.move(-1)
	case key.Matches(msg, k.Down):
		v.move(1)
	case key.Matches(msg, k.PageUp):
		v.move(-rows)
	case key.Matches(msg, k.PageDown):
		v.move(rows)
	case key.Matches(msg, k.Right):
		return m.duOpen()
	case key.Matches(msg, k.Left, k.Back):
		return m.duUp()
	case key.Matches(msg, k.Delete):
		return m.duTrash()
	case key.Matches(msg, k.QuickLook, k.Reveal):
		_, path, ok := v.chosen()
		switch {
		case !ok:
			return m, nil
		case m.job != nil:
			// What another program opens could be what the job works on
			cmd := m.stillBusy()
			return m, cmd
		case key.Matches(msg, k.QuickLook):
			return m.quickLook([]string{path})
		}
		return m.revealPaths([]string{path})
	case key.Matches(msg, k.Refresh):
		cmd := tea.Batch(m.startDiskUsage(v.scan.path, "", nil), m.setStatus("Scanning again"))
		return m, cmd
	case msg.String() == "q":
		return m.closeDiskUsage()
	}
	return m, nil
}

// duOpen enters the folder under the cursor, using what was counted. A
// folder on another volume is counted afresh, as a walk of its own; a
// file is shown in the file list.
func (m Model) duOpen() (tea.Model, tea.Cmd) {
	v := &m.du
	r, path, ok := v.chosen()
	switch {
	case !ok:
		return m, nil
	case r.mount:
		cmd := tea.Batch(m.startDiskUsage(path, "", nil), m.setStatus("Scanning "+utils.Printable(r.name)+", on another volume"))
		return m, cmd
	case !r.dir:
		return m.duGoTo()
	}
	v.path, v.dir, v.cursor, v.focus = path, r.node, 0, ""
	v.rows = nil
	v.refresh()
	return m, nil
}

// duUp goes up to the folder above, with the cursor on the one it was in.
// Above the folder walked, the folder above is walked, taking over what
// was counted of this one if the walk was finished.
func (m Model) duUp() (tea.Model, tea.Cmd) {
	v := &m.du
	v.scan.mu.Lock()
	parent := v.dir.parent
	v.scan.mu.Unlock()
	if parent != nil {
		from := v.dir
		v.path, v.dir, v.rows, v.cursor = filepath.Dir(v.path), parent, nil, 0
		v.refresh()
		for i, r := range v.rows {
			if r.node == from {
				v.cursor = i
			}
		}
		return m, nil
	}
	up := filepath.Dir(v.path)
	if up == v.path {
		cmd := m.setStatus("Already at the root directory")
		return m, cmd
	}
	cmd := m.startDiskUsage(up, filepath.Base(v.path), v.scan)
	return m, cmd
}

// duGoTo closes the view and puts the cursor on the entry in the file
// list. A hidden one is shown, as the view shows everything.
func (m Model) duGoTo() (tea.Model, tea.Cmd) {
	r, path, ok := m.du.chosen()
	if !ok {
		return m, nil
	}
	dir := m.du.path
	updated, _ := m.closeDiskUsage()
	m = updated.(Model)
	var cmds []tea.Cmd
	if strings.HasPrefix(r.name, ".") && !m.showHidden {
		m.showHidden = true
		cmds = append(cmds, m.setStatus("Hidden files shown"), m.reloadAll())
	}
	tab := m.tab()
	tab.focusPath = path
	cmds = append(cmds, m.loadDir(tab, dir))
	return m, tea.Batch(cmds...)
}

// duTrash moves the entry under the cursor to the trash, as d does in the
// file list, and takes it out of the figures once it has gone
func (m Model) duTrash() (tea.Model, tea.Cmd) {
	r, path, ok := m.du.chosen()
	switch {
	case !ok:
		return m, nil
	case r.mount:
		// Trashing would copy the whole volume, then empty it; fs.Trash
		// refuses it too
		cmd := m.setStatus(utils.Printable(r.name) + " is a mounted volume; eject it instead")
		return m, cmd
	case m.du.scan.running():
		cmd := m.setStatus("Still scanning: wait for it to finish, or esc to stop it")
		return m, cmd
	case m.job != nil:
		cmd := m.stillBusy()
		return m, cmd
	}
	scan := m.du.scan
	cmd := m.trashJob([]string{path}, func(m *Model) tea.Cmd {
		m.duForget(scan, path)
		return nil
	})
	return m, cmd
}

// duForget takes what was at path out of the walk's figures, once it has
// gone from there
func (m *Model) duForget(scan *duScan, path string) {
	v := &m.du
	if v.scan != scan {
		return
	}
	if _, err := os.Lstat(path); err == nil {
		return // Still there: the trashing failed
	}
	rel, err := filepath.Rel(scan.path, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return
	}

	scan.mu.Lock()
	n := scan.root
	for _, name := range strings.Split(rel, string(filepath.Separator)) {
		var next *duNode
		for _, c := range n.children {
			if c.name == name {
				next = c
				break
			}
		}
		if next == nil {
			scan.mu.Unlock()
			return
		}
		n = next
	}
	parent := n.parent
	for i, c := range parent.children {
		if c == n {
			parent.children = append(parent.children[:i:i], parent.children[i+1:]...)
			break
		}
	}
	gone := sumOf(n)
	duSum{-gone.size, -gone.disk, -gone.files, -gone.unread}.addTo(parent)
	scan.mu.Unlock()

	// The view may have gone into what was trashed meanwhile
	if v.path == path || strings.HasPrefix(v.path, path+string(filepath.Separator)) {
		v.path, v.dir, v.rows, v.cursor = filepath.Dir(path), parent, nil, 0
	}
	v.refresh()
}

// duRows returns how many entries the view shows, and whether there is
// room for the rule under its heading
func (m Model) duRows() (int, bool) {
	// Above the status bar, less the dialog's border, title and padding,
	// and the two heading lines
	room := m.height - 2 - 5 - 2
	if room-1 >= 1 {
		return room - 1, true
	}
	return max(room, 1), false
}

// duBox draws the disk usage view, as wide as the terminal and as tall as
// the panes
func (m Model) duBox() []string {
	t := m.theme
	g := currentGlyphs()
	v := m.du
	inner := max(m.width, 8) - 2
	rows, roomy := m.duRows()

	body := []string{m.duHeading(inner), m.duStatusLine(inner)}
	if roomy {
		body = append(body, m.fg(t.Border).Render(strings.Repeat(g.hline, inner)))
	}
	start := window(v.cursor, len(v.rows), rows)
	for i := start; i < start+rows; i++ {
		switch {
		case i < len(v.rows):
			body = append(body, m.duRow(v.rows[i], i == v.cursor, inner))
		case i == 0:
			text, style := "Empty folder", m.fg(t.Faint)
			switch {
			case v.status.err != nil:
				text, style = fmt.Sprintf("Can't count: %v", v.status.err), m.fg(t.Danger)
			case v.here.unread > 0 && v.here.files == 0:
				text, style = "Can't read this folder", m.fg(t.Danger)
			case v.status.scanning:
				text = "Scanning" + g.more
			}
			body = append(body, " "+style.Render(utils.Truncate(utils.Printable(text), inner-2)))
		default:
			body = append(body, "")
		}
	}
	return m.dialog("Disk usage", t.Accent, body, m.width)
}

// duHeading shows the folder shown, with its size and how many files
func (m Model) duHeading(width int) string {
	t := m.theme
	h := m.du.here
	right := utils.HumanizeSize(h.size) + " in " + countOf(h.files, "file")
	room := width - 2
	if utils.Width(right)+12 > room {
		right = utils.HumanizeSize(h.size)
	}
	path := utils.TruncateLeft(strings.Join(pathSegments(m.du.path), string(filepath.Separator)), max(room-utils.Width(right)-2, 1))
	gap := max(room-utils.Width(path)-utils.Width(right), 0)
	return " " + m.fg(t.HeaderFg).Bold(true).Render(path) + strings.Repeat(" ", gap) + m.fg(t.Text).Render(right)
}

// duStatusLine says how the walk is going, or how it went, with what it
// couldn't read
func (m Model) duStatusLine(width int) string {
	t := m.theme
	g := currentGlyphs()
	st := m.du.status
	r := st.root
	totals := countOf(r.files, "file") + ", " + utils.HumanizeSize(r.size)
	var text string
	style := m.fg(t.Muted)
	switch {
	case st.err != nil:
		text, style = "Can't count: "+st.err.Error(), m.fg(t.Danger)
	case st.scanning:
		text, style = "Scanning"+g.more+" "+totals+" so far", m.fg(t.Highlight)
	case st.stopped:
		text, style = "Scan stopped: "+totals+" counted", m.fg(t.Highlight)
	default:
		text = fmt.Sprintf("%s in %s, %s on disk, scanned in %s", utils.HumanizeSize(r.size), countOf(r.files, "file"),
			utils.HumanizeSize(r.disk), took(st.took))
	}
	line := " " + style.Render(utils.Truncate(utils.Printable(text), width-2))
	if r.unread > 0 && st.err == nil {
		note := g.dot + " " + thousands(r.unread) + " unreadable"
		if utils.Width(line)+2+utils.Width(note) <= width {
			line += "  " + m.fg(t.Danger).Render(note)
		}
	}
	return line
}

// duRow draws an entry: its name, size, share of the folder shown as a
// bar and a percentage, and how many files are in it. Narrow views lose
// the count, then the bar, then the percentage.
func (m Model) duRow(r duRow, chosen bool, width int) string {
	t := m.theme
	icon := ui.GetFileIcon(fs.FileInfo{Name: r.name, IsDir: r.dir, IsSymlink: r.link})
	nameStyle, meta, iconStyle, warn := m.fg(t.Text), m.fg(t.Muted), m.fg(t.Muted), m.fg(t.Danger)
	if r.dir {
		nameStyle, iconStyle = m.fg(t.Directory).Bold(true), m.fg(t.Directory)
	}
	if chosen {
		sel := lipgloss.NewStyle().Background(t.CursorBg).Foreground(t.CursorFg)
		nameStyle, meta, iconStyle, warn = sel.Bold(r.dir), sel, sel, sel
	}

	pct := 0
	if here := m.du.here.size; here > 0 {
		pct = int((float64(r.size)*100 + float64(here)/2) / float64(here))
	}
	tail := utils.FitRight(utils.HumanizeSize(r.size), sizeW+1)
	switch {
	case width >= 50:
		tail += "  " + progressBar(pct, 8) + utils.FitRight(strconv.Itoa(pct)+"%", 5)
	case width >= 36:
		tail += utils.FitRight(strconv.Itoa(pct)+"%", 5)
	}
	if width >= 70 {
		count := ""
		switch {
		case r.mount:
			count = "other volume"
		case r.dir && r.unread > 0 && r.files == 0:
			count = "unreadable"
		case r.dir && !r.scanned:
			// Still being counted, or the walk stopped before it was
			count = countOf(r.files, "file") + currentGlyphs().more
		case r.dir:
			count = countOf(r.files, "file")
		}
		tail += utils.FitRight(count, 16)
	}
	tail += " "

	lead := " " + icon + "  "
	name := utils.Printable(r.name)
	if r.dir {
		name += string(filepath.Separator)
	}
	mark := ""
	if r.unread > 0 {
		mark = " !"
	}
	room := max(width-utils.Width(lead)-utils.Width(tail), 1)
	name = utils.Truncate(name, max(room-utils.Width(mark), 1))
	pad := strings.Repeat(" ", max(room-utils.Width(name)-utils.Width(mark), 0))
	row := iconStyle.Render(lead) + nameStyle.Render(name) + warn.Render(mark) + meta.Render(pad+tail)
	return utils.Cells(row, 0, width)
}

// took says how long something took, as in 0.4s or 2m13s
func took(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return d.Round(time.Second).String()
}

// countOf returns a count with a noun, as in "1 file" or "12,345 files"
func countOf(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return thousands(n) + " " + noun + "s"
}

// thousands writes n with commas between its thousands, as in 12,345
func thousands(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		return "-" + s
	}
	return s
}

// duHints are the keys shown while the view is open
func (m Model) duHints() []hint {
	k := m.keys
	esc := "close"
	if m.du.status.scanning {
		esc = "stop scan"
	}
	return []hint{{"enter", "open"}, keyHint("up", k.Left), keyHint("trash", k.Delete), {"g", "go to"},
		keyHint("quick look", k.QuickLook), keyHint("reveal", k.Reveal), {"esc", esc}}
}

// mouseDiskUsage handles the mouse over the view: the wheel moves, a click
// picks an entry and a double-click opens it, as enter does
func (m Model) mouseDiskUsage(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	v := &m.du
	if delta := wheelDelta(msg, wheelStep); delta != 0 {
		v.move(delta)
		return m, nil
	}
	if msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	r, inside := m.dialogRowAt(m.duBox(), msg.X, msg.Y)
	if !inside {
		return m, nil
	}
	rows, roomy := m.duRows()
	row := r - dialogBodyRow - 2
	if roomy {
		row--
	}
	i := window(v.cursor, len(v.rows), rows) + row
	if row < 0 || row >= rows || i >= len(v.rows) {
		return m, nil
	}
	// The list may have scrolled to follow the first click: what counts is
	// the same row and the entry that click picked
	if _, path, ok := v.chosen(); ok && m.isDouble(areaDialog, msg.Y, path) {
		m.lastClick = click{}
		return m.duOpen()
	}
	v.cursor, v.focus = i, ""
	m.remember(areaDialog, msg.Y, filepath.Join(v.path, v.rows[i].name))
	return m, nil
}
