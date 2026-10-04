package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/opener"
	"github.com/icichainz/sushi/internal/search"
	"github.com/icichainz/sushi/internal/ui/components"
	"github.com/icichainz/sushi/internal/utils"
)

// Enter on a zip, jar, tar, tar.gz or tar.bz2 goes inside it as if it were
// a folder: the list shows its entries, read from its index (see
// fs.ArchiveIndex), which the tab keeps while it is inside. The tab's path
// goes on below the archive's, as in "bundle.zip/src", so the breadcrumb,
// the parent pane and going up work as they do on disk, and going up from
// the top leaves the archive with the cursor on it.
//
// Inside, nothing can be changed: what would change files is refused. A
// preview, or opening an entry (o, e, i, enter), copies it out to a folder
// of sushi's own under the system's temporary folder, read-only, which goes
// when no tab is inside the archive any more and when sushi quits. c puts
// entries in the clipboard, and v in a folder on disk extracts them there
// as a background job, with the same checks as extracting a whole archive.
// f finds entries by name. Git badges and Finder tags are off, and the
// watcher watches the folder holding the archive, which a reload reads
// again only if it has changed.

const (
	// maxEntryPreview is the largest entry previewed from inside an archive
	maxEntryPreview = 4 << 20
	// maxEntryOpen is the largest entry opened from inside an archive
	maxEntryOpen = 1 << 30
	// readOnly is what refused changes inside an archive say
	readOnly = "Read-only: inside an archive"
)

// entryPreviewTime bounds reading a tar up to an entry to preview, as a
// compressed one is decompressed from the start; tests may change it
var entryPreviewTime = 5 * time.Second

// archiveView is an archive a tab is inside of, with its index
type archiveView struct {
	ix *fs.ArchiveIndex
}

// holds reports whether path is the archive or inside it
func (a *archiveView) holds(path string) bool {
	if a == nil {
		return false
	}
	_, ok := a.ix.Inner(path)
	return ok
}

// inner returns where path is inside the archive, "" for its top
func (a *archiveView) inner(path string) string {
	in, _ := a.ix.Inner(path)
	return in
}

// entry returns the entry listed at path
func (a *archiveView) entry(path string) (fs.ArchiveEntry, bool) {
	in, ok := a.ix.Inner(path)
	if !ok || in == "" {
		return fs.ArchiveEntry{}, false
	}
	return a.ix.Entry(in)
}

// key names the archive, as it was indexed, in the cache of copies
func (a *archiveView) key() string {
	sum := sha256.Sum256([]byte(a.ix.Stamp()))
	return hex.EncodeToString(sum[:8])
}

// archiveState is what the model keeps for archives
type archiveState struct {
	cache *archiveCache // Shared by the model's copies
	clip  *archiveClip  // Entries c put in the clipboard, while they are there
}

// archiveClip is a clipboard of archive entries, which v extracts
type archiveClip struct {
	view  *archiveView
	paths []string
}

// holds reports whether the clipboard is still these entries, rather than
// files copied since, here or in Finder
func (c *archiveClip) holds(clipboard []string) bool {
	return c != nil && len(clipboard) > 0 && slices.Equal(c.paths, clipboard)
}

// realDir returns the folder on disk the tab is in: its directory, or the
// folder holding the archive it is inside of
func (t *Tab) realDir() string {
	if t.archive != nil {
		return filepath.Dir(t.archive.ix.Path)
	}
	return t.CurrentPath
}

// archiveFor returns the archive a load of path into tab lists from: the
// tab's own, or another tab's, already read. A load of an archive no tab
// is in, or of anything else, goes to loadDirectory, which reads the
// archive when it finds path isn't a directory.
func (m *Model) archiveFor(tab *Tab, path string) *archiveView {
	if tab.archive.holds(path) {
		return tab.archive
	}
	for i := range m.tabs {
		if a := m.tabs[i].archive; a.holds(path) {
			return a
		}
	}
	return nil
}

// loadArchive lists path, inside the archive view indexes, in the background
func loadArchive(tabID, seq int, view *archiveView, path string, opts fs.ScanOptions) tea.Cmd {
	return func() tea.Msg {
		return readArchive(tabID, seq, view, path, opts)
	}
}

// archiveLoad is what loadDirectory falls back on when path isn't a
// directory it can list: a load of an archive, or of a folder inside one
func archiveLoad(tabID, seq int, path string, opts fs.ScanOptions) (dirLoadedMsg, bool) {
	if archiveAt(path) == "" {
		return dirLoadedMsg{}, false
	}
	return readArchive(tabID, seq, nil, path, opts), true
}

// archiveAt returns the archive path is, or is inside of, or ""
func archiveAt(path string) string {
	for p := path; ; {
		info, err := os.Stat(p)
		if err == nil {
			if info.Mode().IsRegular() && fs.BrowseKind(p) != "" {
				return p
			}
			return ""
		}
		parent := filepath.Dir(p)
		if parent == p {
			return ""
		}
		p = parent
	}
}

// readArchive lists path, the archive or a folder inside it, from view's
// index, or a new one if there is none yet or the archive has changed
// since. A folder a changed archive no longer has lists the nearest one
// above it that it has.
func readArchive(tabID, seq int, view *archiveView, dir string, opts fs.ScanOptions) dirLoadedMsg {
	msg := dirLoadedMsg{tabID: tabID, seq: seq, path: dir}
	if view == nil || !view.ix.Fresh() {
		archive := archiveAt(dir)
		if archive == "" {
			msg.err = fmt.Errorf("%s is no longer there", filepath.Base(dir))
			return msg
		}
		ix, err := fs.ReadArchiveIndex(archive)
		if err != nil {
			msg.err = fmt.Errorf("can't open %s: %w", filepath.Base(archive), err)
			return msg
		}
		view = &archiveView{ix: ix}
	}
	inner := view.inner(dir)
	for !view.ix.IsFolder(inner) {
		inner = innerParent(inner)
	}
	msg.path = view.ix.PathOf(inner)
	files, err := fs.ScanArchive(view.ix, inner, opts)
	if err != nil {
		msg.err = err
		return msg
	}
	msg.files, msg.archive = files, view
	if inner == "" {
		// The folder holding the archive
		msg.parent = scanParent(msg.path, opts)
	} else {
		msg.parent, _ = fs.ScanArchive(view.ix, innerParent(inner), opts)
	}
	return msg
}

// innerParent returns the folder of an archive an inner path is in, ""
// for its top
func innerParent(inner string) string {
	if up := path.Dir(inner); up != "." {
		return up
	}
	return ""
}

// arriveIn records the archive a load has put the tab in, nil for none.
// Going into, out of or between archives clears the selection, as entries
// and files on disk don't mix; copies of entries go once no tab is in
// their archive.
func (m *Model) arriveIn(tab *Tab, view *archiveView) tea.Cmd {
	old := tab.archive
	if old == view {
		return nil
	}
	tab.archive = view
	if old == nil || view == nil || old.ix.Path != view.ix.Path {
		clear(tab.Selected)
	}
	m.sweepArchives()
	if view != nil && view.ix.Partial && (old == nil || old.ix != view.ix) && tab.ID == m.tab().ID {
		return m.setStatus(fmt.Sprintf("%s is too large to list in full: some entries are missing", filepath.Base(view.ix.Path)))
	}
	return nil
}

// sweepArchives removes the copies of entries of archives no tab is in
func (m *Model) sweepArchives() {
	keep := make(map[string]bool)
	for i := range m.tabs {
		if a := m.tabs[i].archive; a != nil {
			keep[a.key()] = true
		}
	}
	m.arc.cache.sweep(keep)
}

// browsable reports whether file is an archive enter goes inside of
func browsable(file fs.FileInfo) bool {
	if file.IsDir || fs.BrowseKind(file.Name) == "" {
		return false
	}
	info, err := os.Stat(file.Path)
	return err == nil && info.Mode().IsRegular()
}

// openArchived handles enter on a file that is an archive, which it goes
// inside of, or that is inside one, which opens as a copy
func (m Model) openArchived(file fs.FileInfo) (tea.Model, tea.Cmd, bool) {
	tab := m.tab()
	switch {
	case tab.archive != nil && m.job != nil:
		cmd := m.stillBusy()
		return m, cmd, true
	case tab.archive != nil:
		model, cmd := m.openEntries([]string{file.Path}, howAuto)
		return model, cmd, true
	case browsable(file):
		return m, m.loadDir(tab, file.Path), true
	}
	return m, nil, false
}

// inArchive handles the keys that work differently inside an archive:
// those that would change files are refused, and copying, opening and
// finding work on the entries
func (m Model) inArchive(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if m.tab().archive == nil {
		return m, nil, false
	}
	k := m.keys
	why := ""
	switch {
	case key.Matches(msg, k.Delete, k.HardDelete, k.Rename, k.BulkRename, k.PatternRename, k.NewFile, k.NewDir,
		k.Chmod, k.Duplicate, k.Cut, k.Paste, k.PasteLink, k.Archive, k.Extract, k.Tag):
		why = readOnly
	case key.Matches(msg, k.Plugins, k.Shell):
		why = "Plugins and shell commands work on files on disk, not inside an archive"
	case key.Matches(msg, k.OpenWith):
		why = "Open with doesn't work inside an archive"
		if o := shownKey(k.Open); o != "" {
			why += ": " + o + " opens a read-only copy"
		}
	case key.Matches(msg, k.Grep):
		why = "The files inside an archive can't be searched"
		if f := shownKey(k.Find); f != "" {
			why += "; " + f + " finds them by name"
		}
	case key.Matches(msg, k.FindTag):
		why = "Archive entries have no Finder tags"
	case key.Matches(msg, k.Copy):
		model, cmd := m.copyEntries()
		return model, cmd, true
	case key.Matches(msg, k.Open):
		model, cmd := m.openEntries(m.targets(), howSystem)
		return model, cmd, true
	case key.Matches(msg, k.Edit):
		model, cmd := m.openEntries(m.targets(), howEditor)
		return model, cmd, true
	case key.Matches(msg, k.QuickLook):
		model, cmd := m.openEntries(m.targets(), howQuickLook)
		return model, cmd, true
	case key.Matches(msg, k.Reveal):
		model, cmd := m.revealArchive()
		return model, cmd, true
	default:
		return m, nil, false
	}
	cmd := m.setStatus(why)
	return m, cmd, true
}

// copyEntries puts the targets, entries of the archive, in the clipboard.
// Finder can't take them, so the pasteboard is left as it is; see
// claimPasteboard.
func (m Model) copyEntries() (tea.Model, tea.Cmd) {
	paths := m.targets()
	if len(paths) == 0 {
		return m, nil
	}
	view := m.tab().archive
	for _, p := range paths {
		e, ok := view.entry(p)
		switch {
		case !ok:
			cmd := m.setStatus(fmt.Sprintf("Can't copy %s: it is no longer in the archive", filepath.Base(p)))
			return m, cmd
		case e.Unsafe:
			cmd := m.setStatus(fmt.Sprintf("Can't copy %s: unsafe path in archive", e.Name))
			return m, cmd
		}
	}
	m.clipboard, m.clipboardMode = paths, "copy"
	m.arc.clip = &archiveClip{view: view, paths: slices.Clone(paths)}
	clear(m.tab().Selected)
	status := m.setStatus(fmt.Sprintf("Copied to clipboard: %s, to paste into a folder", describe(paths)))
	return m, tea.Batch(status, m.claimPasteboard())
}

// pasteboardClaimedMsg brings the pasteboard's change count when entries
// were copied
type pasteboardClaimedMsg struct {
	seq, count int
	err        error
}

// claimPasteboard makes sushi's clipboard, now holding entries the
// pasteboard can't, the newer of the two: until the pasteboard changes,
// a paste takes the entries, as it would files sushi put there. Its
// change count is read in the background; until it is in, or if it
// can't be read, sushi's clipboard is the newer.
func (m *Model) claimPasteboard() tea.Cmd {
	board := m.board()
	if board == nil {
		return nil
	}
	m.pb.writeSeq++ // A write still on its way is older
	m.pb.ours = false
	seq := m.pb.writeSeq
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), macTimeout)
		defer cancel()
		count, err := board.Count(ctx)
		return pasteboardClaimedMsg{seq: seq, count: count, err: err}
	}
}

func (msg pasteboardClaimedMsg) apply(m Model) (tea.Model, tea.Cmd) {
	if msg.seq == m.pb.writeSeq && msg.err == nil {
		m.pb.ours, m.pb.count = true, msg.count
	}
	return m, nil
}

// pasteEntries extracts the archive entries in the clipboard into the
// folder shown, in the background. A name taken there is refused before
// anything is written: extracting never replaces anything.
func (m Model) pasteEntries(links bool) (tea.Model, tea.Cmd) {
	tab := m.tab()
	clip := m.arc.clip
	switch {
	case tab.archive != nil:
		cmd := m.setStatus(readOnly)
		return m, cmd
	case links:
		what := "Can't link to what is inside an archive"
		if v := shownKey(m.keys.Paste); v != "" {
			what += ": " + v + " extracts it"
		}
		cmd := m.setStatus(what)
		return m, cmd
	}
	dir := tab.CurrentPath
	inners := make([]string, 0, len(clip.paths))
	for _, p := range clip.paths {
		if name := filepath.Base(p); fs.Exists(filepath.Join(dir, name)) {
			cmd := m.setStatus(fmt.Sprintf("Can't paste: %s already exists here", name))
			return m, cmd
		}
		inners = append(inners, clip.view.inner(p))
	}

	paths := slices.Clone(clip.paths)
	archive := filepath.Base(clip.view.ix.Path)
	ix := clip.view.ix
	cmd := m.startJob("Extracting", func(t *fs.Task) jobDoneMsg {
		made, err := t.ExtractEntries(ix, inners, dir)
		undo := &undoEntry{label: fmt.Sprintf("copy %s out of %s", describe(paths), archive)}
		for _, p := range made {
			undo.addCreated(p)
		}
		done := jobDoneMsg{op: fileOperationMsg{operation: "extract"}, undo: undo}
		switch {
		case t.Err() != nil:
			done.op.message = "Cancelled: nothing was copied out of " + archive
		case err != nil:
			done.op.err = err
		default:
			done.op.message = fmt.Sprintf("Copied %s out of %s", describe(made), archive)
		}
		if len(made) > 0 {
			done.focus = made[0]
		}
		return done
	})
	return m, cmd
}

// openHow is how openEntries opens what it copies out
type openHow int

const (
	howAuto      openHow = iota // As enter opens files: see openFile
	howSystem                   // With the default app
	howEditor                   // In the editor
	howQuickLook                // In Quick Look
)

// entriesCopiedMsg brings the copies of entries made to open them
type entriesCopiedMsg struct {
	how    openHow
	what   string   // The entries, for the status bar
	copies []string // Their copies, in the cache
	err    error
}

// openEntries copies entries of the archive out, in the background, and
// opens the copies. They are read-only, and changes to them aren't saved
// to the archive, which the status bar says.
func (m Model) openEntries(paths []string, how openHow) (tea.Model, tea.Cmd) {
	if len(paths) == 0 {
		return m, nil
	}
	view := m.tab().archive
	entries := make([]fs.ArchiveEntry, 0, len(paths))
	for _, p := range paths {
		e, ok := view.entry(p)
		name := filepath.Base(p)
		if ok && e.Unsafe {
			name = e.Name
		}
		why := ""
		switch {
		case !ok:
			why = "it is no longer in the archive"
		case e.IsDir():
			why = "folders inside an archive can't be opened"
			if c, v := shownKey(m.keys.Copy), shownKey(m.keys.Paste); c != "" && v != "" {
				why += "; " + c + " and " + v + " copy them out"
			}
		case e.Mode&os.ModeSymlink != 0:
			why = "it is a link inside the archive"
		case !e.Mode.IsRegular():
			why = "it isn't a file"
		case e.Size > maxEntryOpen:
			why = "it is too large to open from inside the archive"
		}
		if why != "" {
			cmd := m.setStatus(fmt.Sprintf("Can't open %s: %s", name, why))
			return m, cmd
		}
		entries = append(entries, e)
	}

	cache := m.arc.cache
	msg := entriesCopiedMsg{how: how, what: describe(paths)}
	status := m.setStatus(fmt.Sprintf("Copying %s out of %s%s", msg.what, filepath.Base(view.ix.Path), currentGlyphs().more))
	run := func() tea.Msg {
		for _, e := range entries {
			p, err := cache.extract(context.Background(), view, e, maxEntryOpen)
			if err != nil {
				msg.err = err
				return msg
			}
			msg.copies = append(msg.copies, p)
		}
		return msg
	}
	return m, tea.Batch(status, run)
}

func (msg entriesCopiedMsg) apply(m Model) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		cmd := m.setStatus(fmt.Sprintf("Can't open %s: %v", msg.what, msg.err))
		return m, cmd
	}
	how := msg.how
	if how == howAuto {
		how = howSystem
		if m.useEditor(fs.FileInfo{Path: msg.copies[0]}) {
			how = howEditor
		}
	}
	var updated tea.Model
	var cmd tea.Cmd
	switch how {
	case howEditor:
		updated, cmd = m.edit(msg.copies)
	case howQuickLook:
		updated, cmd = m.quickLook(msg.copies)
	default:
		updated, cmd = m.openWithSystem(msg.copies)
	}
	m = updated.(Model)
	note := "Opened a read-only copy of " + msg.what + ": changes aren't saved to the archive"
	if how == howQuickLook {
		// Unless it closed the window, or failed
		if !strings.HasPrefix(m.statusMsg, "Quick Look: ") {
			return m, cmd
		}
		note = "Quick Look: a read-only copy of " + msg.what
	}
	status := m.setStatus(note)
	return m, tea.Batch(cmd, status)
}

// revealArchive shows the archive the tab is inside of in Finder
func (m Model) revealArchive() (tea.Model, tea.Cmd) {
	archive := m.tab().archive.ix.Path
	if !onMac {
		cmd := m.setStatus("Showing files in Finder needs macOS")
		return m, cmd
	}
	status := m.setStatus("Showing " + filepath.Base(archive) + " in Finder")
	run := func() tea.Msg {
		const label = "Show in Finder"
		if out, err := runOpen(opener.RevealArgs(archive)...); err != nil {
			return externalDoneMsg{label: label, err: withOutput(err, out)}
		}
		return externalDoneMsg{label: label}
	}
	return m, tea.Batch(status, run)
}

// archiveHints are the keys shown in the bottom row inside an archive
func (m Model) archiveHints() []hint {
	k := m.keys
	return []hint{keyHint("open", k.Enter), keyHint("up", k.Left), keyHint("select", k.Select), keyHint("copy out", k.Copy),
		keyHint("open a copy", k.Open), keyHint("quick look", k.QuickLook), keyHint("find", k.Find), keyHint("all keys", k.Help)}
}

// entryPreview loads the preview of an archive entry in the background
func (m *Model) entryPreview(tab *Tab, file fs.FileInfo, cfg components.PreviewConfig) tea.Cmd {
	view, cache, id := tab.archive, m.arc.cache, tab.ID
	return func() tea.Msg {
		return previewLoadedMsg{tabID: id, preview: previewEntry(cache, view, file, cfg)}
	}
}

// maxEntryList is how many entries a folder's preview lists, as for a
// directory on disk
const maxEntryList = 200

// previewEntry previews an entry: a folder lists what is in it, and a file
// up to maxEntryPreview is copied out and previewed as any file is, under
// the entry's own name
func previewEntry(cache *archiveCache, view *archiveView, file fs.FileInfo, cfg components.PreviewConfig) components.PreviewContent {
	p := components.PreviewContent{Path: file.Path, FileInfo: file}
	say := func(kind string, err error, lines ...string) components.PreviewContent {
		p.Kind, p.Lines, p.Content, p.Error = kind, lines, strings.Join(lines, "\n"), err
		return p
	}
	e, ok := view.entry(file.Path)
	switch {
	case !ok:
		err := errors.New("no longer in the archive")
		return say("Entry", err, "No longer in the archive")
	case e.IsDir():
		files, err := fs.ScanArchive(view.ix, e.Inner(), fs.ScanOptions{ShowHidden: true, SortBy: "name"})
		if err != nil {
			return say("Directory", err, err.Error())
		}
		p.Kind = "Directory"
		p.More = len(files) > maxEntryList
		names := make([]string, 0, min(len(files), maxEntryList))
		for _, f := range files[:min(len(files), maxEntryList)] {
			p.Entries = append(p.Entries, components.Entry{Name: f.Name, IsDir: f.IsDir})
			names = append(names, f.Name)
		}
		p.Content = strings.Join(names, "\n")
		return p
	case e.Mode&os.ModeSymlink != 0:
		if target, err := view.ix.ReadLink(e); err == nil {
			p.LinkTarget = utils.Printable(target)
		}
		return say("Symlink", nil, "A symlink inside the archive")
	case !e.Mode.IsRegular():
		return say("Special file", nil, "Devices and pipes can't be previewed")
	case e.Size > maxEntryPreview:
		return say("Large file", nil, fmt.Sprintf("Too large to preview from inside the archive (over %s)", utils.HumanizeSize(maxEntryPreview)))
	}

	ctx, cancel := context.WithTimeout(context.Background(), entryPreviewTime)
	defer cancel()
	copied, err := cache.extract(ctx, view, e, maxEntryPreview)
	if err != nil {
		return say("File", err, fmt.Sprintf("Can't read it from the archive: %v", err))
	}
	local := file
	local.Path, local.IsSymlink = copied, false
	p = components.LoadPreviewWithConfig(local, cfg)
	p.Path, p.FileInfo = file.Path, file
	return p
}

// archiveCache keeps the copies of entries made to preview and open them,
// in a folder of sushi's own under the system's temporary folder, made on
// first use, with a folder in it for each archive
type archiveCache struct {
	mu   sync.Mutex
	root string
}

// errTooLarge says an entry is larger than what it is copied out for allows
var errTooLarge = errors.New("too large to copy out of the archive")

// dir returns the folder for copies of view's entries, making it if need be
func (c *archiveCache) dir(view *archiveView) (string, error) {
	if c == nil {
		return "", errors.New("nowhere to copy archive entries to")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if info, err := os.Stat(c.root); c.root == "" || err != nil || !info.IsDir() {
		root, err := os.MkdirTemp("", "sushi-archives-")
		if err != nil {
			return "", fmt.Errorf("can't make a folder for copies of archive entries: %w", err)
		}
		c.root = root
	}
	dir := filepath.Join(c.root, view.key())
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

// extract copies entry e of view's archive to the cache, up to limit
// bytes, and returns where, reusing a copy made before. The copy has the
// entry's own name and time, and is read-only: changes to it would go
// nowhere.
func (c *archiveCache) extract(ctx context.Context, view *archiveView, e fs.ArchiveEntry, limit int64) (string, error) {
	if e.Size > limit {
		return "", errTooLarge
	}
	dir, err := c.dir(view)
	if err != nil {
		return "", err
	}
	// A folder for each entry, so copies keep their names without clashing
	h := fnv.New64a()
	io.WriteString(h, e.Inner())
	dir = filepath.Join(dir, strconv.FormatUint(h.Sum64(), 16))
	dst := filepath.Join(dir, copyName(e))
	if info, err := os.Lstat(dst); err == nil && info.Mode().IsRegular() {
		return dst, nil
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}

	rc, err := view.ix.OpenEntry(ctx, e)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	f, err := os.CreateTemp(dir, ".partial-*")
	if err != nil {
		return "", err
	}
	n, err := io.Copy(f, io.LimitReader(rc, limit+1))
	if err == nil && n > limit {
		err = errTooLarge
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(f.Name(), 0444)
	}
	if err == nil {
		os.Chtimes(f.Name(), time.Time{}, e.ModTime)
		err = os.Rename(f.Name(), dst)
	}
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return dst, nil
}

// copyName is the name an entry's copy gets: its own, or for one named
// outside the archive, the last part of that name
func copyName(e fs.ArchiveEntry) string {
	name := path.Base(e.Inner())
	if e.Unsafe {
		name = path.Base(strings.ReplaceAll(e.Name, "\x00", ""))
	}
	if name == "" || name == "." || name == ".." || name == "/" {
		name = "entry"
	}
	return name
}

// sweep removes the copies of every archive but those keep names
func (c *archiveCache) sweep(keep map[string]bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.root == "" {
		return
	}
	entries, err := os.ReadDir(c.root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !keep[e.Name()] {
			os.RemoveAll(filepath.Join(c.root, e.Name()))
		}
	}
}

// removeAll removes every copy, and the cache's folder
func (c *archiveCache) removeAll() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.root != "" {
		os.RemoveAll(c.root)
		c.root = ""
	}
}

// searchEngine returns the engine the search palette uses on root: inside
// an archive, one that finds its entries by name
func (m Model) searchEngine(root string) search.Engine {
	if view := m.tab().archive; view.holds(root) {
		return archiveEngine{view: view, base: findEngine}
	}
	return findEngine
}

// archiveEngine finds the entries of an archive by name, below the folder
// of it the palette opened on. What is inside them can't be searched, nor
// can tags; searching everywhere is Spotlight's, as anywhere else.
type archiveEngine struct {
	view *archiveView
	base search.Engine
}

// Search finds the entries whose names match
func (e archiveEngine) Search(ctx context.Context, opts search.Options, q search.Query, emit func(search.Result)) (search.Report, error) {
	if opts.Everywhere {
		opts.Root = filepath.Dir(e.view.ix.Path)
		return e.base.Search(ctx, opts, q, emit)
	}
	switch {
	case q.Tagged:
		return search.Report{}, errors.New("archive entries have no Finder tags")
	case q.Content:
		return search.Report{}, errors.New("the files inside an archive can't be searched, only their names")
	}
	match := q.Match
	if match == nil {
		text := strings.ToLower(q.Text)
		match = func(_, name string) (int, bool) { return 0, strings.Contains(strings.ToLower(name), text) }
	}
	root := e.view.inner(opts.Root)
	ix := e.view.ix
	found, truncated := 0, false
	ix.Walk(root, func(entry fs.ArchiveEntry) bool {
		if ctx.Err() != nil {
			return false
		}
		info := ix.FileInfo(entry)
		rel := info.Name
		if !entry.Unsafe {
			rel = strings.TrimPrefix(entry.Inner(), root+"/")
			parts := strings.Split(rel, "/")
			for i, part := range parts {
				if !opts.ShowHidden && strings.HasPrefix(part, ".") || i < len(parts)-1 && slices.Contains(opts.Skip, part) {
					return true
				}
			}
		}
		score, ok := match(rel, info.Name)
		if !ok {
			return true
		}
		if opts.Limit > 0 && found >= opts.Limit {
			truncated = true
			return false
		}
		emit(search.Result{Path: info.Path, Rel: rel, IsDir: info.IsDir, Score: score})
		found++
		return true
	})
	return search.Report{Truncated: truncated}, ctx.Err()
}
