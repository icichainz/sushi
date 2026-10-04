package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/icichainz/sushi/internal/config"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/ui"
	"github.com/icichainz/sushi/internal/ui/components"
	"github.com/icichainz/sushi/internal/utils"
)

// The trash browser (ctrl+t): what is in the trash, the most recently
// trashed first, with where each item came from where that is known and
// its size, counted in the background. Enter or r puts an item back where
// it came from, p restores it into the folder sushi was showing, D deletes
// it for good and E empties the trash, both after asking. These run as
// jobs, with progress, and ctrl+x cancels them. Putting an item back is
// undone by trashing it again; deleting can't be undone.
//
// ~/.Trash doesn't say where its items came from, so sushi notes what it
// trashes there itself (see fs.Trash.Record); items Finder trashed show an
// unknown origin and can only be restored into a folder of choice, with p.

// userTrash returns the user's trash, with sushi's record of what it puts
// there where the trash keeps none of its own
func userTrash() (*fs.Trash, error) {
	tr, err := fs.DefaultTrash()
	if err != nil {
		return nil, err
	}
	if tr.Info == "" {
		tr.Record = config.TrashRecordPath()
	}
	return tr, nil
}

// trashView is the trash browser's state
type trashView struct {
	tr        *fs.Trash
	here      string // The folder sushi was showing, where p restores to
	items     []trashItem
	shown     []int // Indexes into items of those the filter lets through
	cursor    int   // Into shown
	loading   bool
	err       error
	seq       int // Counts listings, so an older one is dropped
	filter    components.TextInput
	filtering bool   // Keys go to the filter
	confirm   string // "delete" or "empty" while asking
	asked     string // The item the delete asks about
	sizer     *trashSizer
}

// trashItem is an item in the trash with how much is in it
type trashItem struct {
	fs.TrashEntry
	size  int64 // Bytes in it; -1 until counted
	files int
}

// trashListedMsg brings what is in the trash
type trashListedMsg struct {
	seq     int
	entries []fs.TrashEntry
	err     error
}

// openTrash opens the trash browser
func (m Model) openTrash() (tea.Model, tea.Cmd) {
	tr, err := userTrash()
	if err != nil {
		cmd := m.setStatus(fmt.Sprintf("Can't find the trash: %v", err))
		return m, cmd
	}
	m.stopTrashSizer()
	// Inside an archive, p restores into the folder holding it
	m.trash = trashView{tr: tr, here: m.tab().realDir(), filter: components.NewTextInput(""), seq: m.trash.seq}
	m.mode = ModeTrash
	cmd := m.listTrash()
	return m, cmd
}

// closeTrash closes the trash browser
func (m Model) closeTrash() (tea.Model, tea.Cmd) {
	m.stopTrashSizer()
	m.trash = trashView{seq: m.trash.seq}
	m.mode = ModeNormal
	return m, nil
}

// listTrash lists the trash in the background
func (m *Model) listTrash() tea.Cmd {
	v := &m.trash
	v.seq++
	v.loading = true
	seq, tr := v.seq, v.tr
	return func() tea.Msg {
		entries, err := tr.List()
		return trashListedMsg{seq: seq, entries: entries, err: err}
	}
}

// reloadTrash lists the trash again, if the browser is still open
func (m *Model) reloadTrash() tea.Cmd {
	if m.mode != ModeTrash || m.trash.tr == nil {
		return nil
	}
	return m.listTrash()
}

func (msg trashListedMsg) apply(m Model) (tea.Model, tea.Cmd) {
	v := &m.trash
	if m.mode != ModeTrash || msg.seq != v.seq {
		return m, nil
	}
	current := ""
	if it, ok := v.chosen(); ok {
		current = it.Path
	}
	// Sizes already counted are kept, for items that are still there
	counted := make(map[string]trashItem, len(v.items))
	for _, it := range v.items {
		counted[it.Path] = it
	}
	v.loading, v.err = false, msg.err
	v.items = make([]trashItem, 0, len(msg.entries))
	for _, e := range msg.entries {
		it := trashItem{TrashEntry: e, size: -1}
		switch old, ok := counted[e.Path]; {
		case ok && old.size >= 0 && old.IsDir == e.IsDir:
			it.size, it.files = old.size, old.files
		case !e.IsDir:
			it.size, it.files = e.Size, 1
		}
		v.items = append(v.items, it)
	}
	v.applyFilter(current)
	cmd := m.sizeTrash()
	return m, cmd
}

// applyFilter lists the items whose names match the filter, keeping the
// cursor on the item at keep if it still shows
func (v *trashView) applyFilter(keep string) {
	query := strings.ToLower(v.filter.Value())
	v.shown = v.shown[:0:0]
	for i, it := range v.items {
		if query == "" || fuzzyMatch(query, strings.ToLower(it.Name)) {
			v.shown = append(v.shown, i)
		}
	}
	v.cursor = min(v.cursor, max(len(v.shown)-1, 0))
	for pos, i := range v.shown {
		if v.items[i].Path == keep {
			v.cursor = pos
		}
	}
}

// chosen returns the item under the cursor
func (v *trashView) chosen() (trashItem, bool) {
	if v.cursor >= len(v.shown) {
		return trashItem{}, false
	}
	return v.items[v.shown[v.cursor]], true
}

// move moves the cursor by delta, stopping at the ends
func (v *trashView) move(delta int) {
	v.cursor = max(min(v.cursor+delta, len(v.shown)-1), 0)
}

// trashSizer counts what is in the items of the trash, one at a time
type trashSizer struct {
	cancel  context.CancelFunc
	results chan trashSize
}

type trashSize struct {
	path  string
	size  int64
	files int
}

// trashSizesMsg brings the sizes counted since the last
type trashSizesMsg struct {
	sizer *trashSizer
	sizes []trashSize
	done  bool
}

// sizeTrash counts the items not counted yet, in the background
func (m *Model) sizeTrash() tea.Cmd {
	m.stopTrashSizer()
	var paths []string
	for _, it := range m.trash.items {
		if it.size < 0 {
			paths = append(paths, it.Path)
		}
	}
	if len(paths) == 0 {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	z := &trashSizer{cancel: cancel, results: make(chan trashSize, 64)}
	go func() {
		defer close(z.results)
		for _, p := range paths {
			size, files := measure(ctx, p)
			select {
			case z.results <- trashSize{p, size, files}:
			case <-ctx.Done():
				return
			}
		}
	}()
	m.trash.sizer = z
	return waitTrashSizes(z)
}

// stopTrashSizer stops counting, if it is going on
func (m *Model) stopTrashSizer() {
	if z := m.trash.sizer; z != nil {
		z.cancel()
		m.trash.sizer = nil
	}
}

// waitTrashSizes waits for sizes, taking all that have come with the first
func waitTrashSizes(z *trashSizer) tea.Cmd {
	return func() tea.Msg {
		msg := trashSizesMsg{sizer: z}
		r, ok := <-z.results
		for ok {
			msg.sizes = append(msg.sizes, r)
			select {
			case r, ok = <-z.results:
			default:
				return msg
			}
		}
		msg.done = true
		return msg
	}
}

func (msg trashSizesMsg) apply(m Model) (tea.Model, tea.Cmd) {
	v := &m.trash
	if msg.sizer != v.sizer {
		return m, nil
	}
	items := slices.Clone(v.items)
	for _, s := range msg.sizes {
		for i := range items {
			if items[i].Path == s.path {
				items[i].size, items[i].files = s.size, s.files
			}
		}
	}
	v.items = items
	if msg.done {
		v.sizer.cancel() // Releases the context
		v.sizer = nil
		return m, nil
	}
	return m, waitTrashSizes(msg.sizer)
}

// measure adds up the bytes and files in path, without following links
func measure(ctx context.Context, path string) (int64, int) {
	var size int64
	files := 0
	filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			size += info.Size()
		}
		files++
		return nil
	})
	return size, files
}

// handleTrashMode handles keys in the trash browser. A question asked
// takes the keys first, then the filter being typed. The browser's own
// keys come next: esc clears the filter, then closes; enter and r put the
// item back, p restores it here and E empties the trash. Moving, deleting,
// filtering, Quick Look and refreshing follow the key map; q closes the
// browser unless one of those has taken it.
func (m Model) handleTrashMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	v := &m.trash
	k := m.keys
	if v.confirm != "" {
		return m.handleTrashConfirm(msg)
	}
	// ctrl+x stops what the browser started, even while filtering, unless
	// it is bound to a key that types
	if key.Matches(msg, k.Cancel) && (!v.filtering || msg.Type != tea.KeyRunes) {
		return m.cancelJob()
	}
	if v.filtering {
		keep := ""
		if it, ok := v.chosen(); ok {
			keep = it.Path
		}
		switch msg.Type {
		case tea.KeyEsc:
			v.filter, v.filtering = components.NewTextInput(""), false
			v.applyFilter(keep)
		case tea.KeyEnter:
			v.filtering = false
		case tea.KeyUp:
			v.move(-1)
		case tea.KeyDown:
			v.move(1)
		default:
			before := v.filter.Value()
			v.filter.Update(msg)
			if v.filter.Value() != before {
				v.cursor = 0
				v.applyFilter("")
			}
		}
		return m, nil
	}

	switch msg.String() {
	case "esc":
		if v.filter.Value() != "" {
			keep := ""
			if it, ok := v.chosen(); ok {
				keep = it.Path
			}
			v.filter = components.NewTextInput("")
			v.applyFilter(keep)
			return m, nil
		}
		return m.closeTrash()
	case "enter", "r":
		return m.trashPutBack()
	case "p":
		return m.trashRestoreHere()
	case "E":
		return m.trashAsk("empty")
	}
	rows, _ := m.trashRows()
	switch {
	case key.Matches(msg, k.Up):
		v.move(-1)
	case key.Matches(msg, k.Down):
		v.move(1)
	case key.Matches(msg, k.PageUp):
		v.move(-rows)
	case key.Matches(msg, k.PageDown):
		v.move(rows)
	case key.Matches(msg, k.HardDelete):
		return m.trashAsk("delete")
	case key.Matches(msg, k.Search):
		v.filtering = true
	case key.Matches(msg, k.QuickLook):
		it, ok := v.chosen()
		switch {
		case !ok:
			return m, nil
		case m.job != nil:
			cmd := m.stillBusy()
			return m, cmd
		}
		return m.quickLook([]string{it.Path})
	case key.Matches(msg, k.Refresh):
		cmd := m.listTrash()
		return m, cmd
	case msg.String() == "q":
		return m.closeTrash()
	}
	return m, nil
}

// trashPutBack puts the item under the cursor back where it came from
func (m Model) trashPutBack() (tea.Model, tea.Cmd) {
	it, ok := m.trash.chosen()
	if !ok {
		return m, nil
	}
	if it.Original == "" {
		cmd := m.setStatus(fmt.Sprintf("Where %s came from isn't known: p restores it into %s", utils.Printable(it.Name), shortDir(m.trash.here)))
		return m, cmd
	}
	return m.trashRestore(it, it.Original)
}

// trashRestoreHere restores the item under the cursor into the folder
// sushi was showing, under the name it had before it was trashed
func (m Model) trashRestoreHere() (tea.Model, tea.Cmd) {
	it, ok := m.trash.chosen()
	if !ok {
		return m, nil
	}
	name := it.Name
	if it.Original != "" {
		name = filepath.Base(it.Original)
	}
	return m.trashRestore(it, filepath.Join(m.trash.here, name))
}

// trashRestore moves the item out of the trash to to, in the background.
// Nothing at to is ever replaced, as with undo.
func (m Model) trashRestore(it trashItem, to string) (tea.Model, tea.Cmd) {
	if m.job != nil {
		cmd := m.stillBusy()
		return m, cmd
	}
	name := utils.Printable(filepath.Base(to))
	if _, err := os.Lstat(to); err == nil {
		cmd := m.setStatus(fmt.Sprintf("Can't put back %s: something called %s is in %s now", name, name, shortDir(filepath.Dir(to))))
		return m, cmd
	}
	entry := it.TrashEntry
	cmd := m.startJob("Putting back", func(t *fs.Task) jobDoneMsg {
		err := entry.Item(to).Restore(t)
		done := jobDoneMsg{op: fileOperationMsg{operation: "restore"}, focus: to}
		switch {
		case err != nil && t.Err() != nil:
			done.op.message = "Cancelled putting back " + name
		case err != nil:
			done.op.err = fmt.Errorf("can't put back %s: %w", name, err)
		default:
			done.op.message = fmt.Sprintf("Put back: %s in %s", name, shortDir(filepath.Dir(to)))
			done.undo = &undoEntry{label: "put back " + name}
			if stamp, err := fs.TakeStamp(to); err == nil {
				done.undo.steps = []undoStep{{kind: stepTrash, path: to, stamp: stamp}}
			}
		}
		done.after = func(m *Model) tea.Cmd {
			if err == nil {
				m.forgetTrashed(entry.Path)
			}
			return m.reloadTrash()
		}
		return done
	})
	return m, cmd
}

// shortDir names a folder for a status message, with the home folder as ~
func shortDir(dir string) string {
	return strings.Join(pathSegments(dir), string(filepath.Separator))
}

// trashAsk asks before deleting the item under the cursor, or emptying
// the trash, for good
func (m Model) trashAsk(what string) (tea.Model, tea.Cmd) {
	v := &m.trash
	it, ok := v.chosen()
	switch {
	case what == "delete" && !ok, what == "empty" && len(v.items) == 0:
		return m, nil
	case m.job != nil:
		cmd := m.stillBusy()
		return m, cmd
	}
	v.confirm, v.asked = what, it.Path
	return m, nil
}

// handleTrashConfirm answers the question asked: y (or enter) goes ahead,
// n, esc or q don't
func (m Model) handleTrashConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	v := &m.trash
	switch msg.String() {
	case "y", "Y", "enter":
		what, asked := v.confirm, v.asked
		v.confirm, v.asked = "", ""
		if m.job != nil {
			cmd := m.stillBusy()
			return m, cmd
		}
		if what == "empty" {
			cmd := m.emptyTrash()
			return m, cmd
		}
		for _, it := range v.items {
			if it.Path == asked {
				cmd := m.deleteFromTrash(it.TrashEntry)
				return m, cmd
			}
		}
		cmd := m.setStatus(utils.Printable(filepath.Base(asked)) + " is no longer in the trash")
		return m, cmd
	case "n", "N", "esc", "q":
		v.confirm, v.asked = "", ""
	}
	return m, nil
}

// deleteFromTrash deletes an item in the trash for good, in the background
func (m *Model) deleteFromTrash(e fs.TrashEntry) tea.Cmd {
	tr := m.trash.tr
	name := utils.Printable(e.Name)
	return m.startJob("Deleting", func(t *fs.Task) jobDoneMsg {
		t.CountFiles(e.Path)
		err := tr.Delete(t, e)
		done := jobDoneMsg{op: fileOperationMsg{operation: "delete"}}
		switch {
		case err != nil && t.Err() != nil:
			done.op.message = "Cancelled: " + howFar("deleted", t.Progress(), 0, 1)
		case err != nil:
			done.op.err = fmt.Errorf("can't delete %s: %w", name, err)
		default:
			done.op.message = "Deleted for good: " + name
		}
		if err == nil || t.Progress().Files > 0 {
			// Nothing to undo, but ctrl+z says so rather than undoing something older
			done.undo = &undoEntry{label: "delete " + name + " from the trash", reason: "it was permanent"}
		}
		done.after = func(m *Model) tea.Cmd {
			if _, err := os.Lstat(e.Path); err != nil {
				m.forgetTrashed(e.Path)
			}
			return m.reloadTrash()
		}
		return done
	})
}

// emptyTrash deletes everything in the trash for good, in the background
func (m *Model) emptyTrash() tea.Cmd {
	tr := m.trash.tr
	return m.startJob("Emptying trash", func(t *fs.Task) jobDoneMsg {
		deleted, failed, err := tr.Empty(t)
		done := jobDoneMsg{op: fileOperationMsg{operation: "empty"}}
		switch {
		case err != nil && t.Err() != nil:
			done.op.message = fmt.Sprintf("Cancelled emptying the trash: deleted %s", plural(len(deleted), "item"))
		case err != nil:
			done.op.err = fmt.Errorf("can't empty the trash: %w", err)
		case len(failed) > 0:
			done.op.err = fmt.Errorf("emptied the trash but for %s it can't delete; %w", plural(len(failed), "item"), failed[0])
		default:
			done.op.message = "Emptied the trash: " + plural(len(deleted), "item") + " deleted for good"
		}
		if len(deleted) > 0 || t.Progress().Files > 0 {
			done.undo = &undoEntry{label: "empty the trash", reason: "it was permanent"}
		}
		done.after = func(m *Model) tea.Cmd {
			paths := make([]string, len(deleted))
			for i, e := range deleted {
				paths[i] = e.Path
			}
			m.forgetTrashed(paths...)
			return m.reloadTrash()
		}
		return done
	})
}

// forgetTrashed drops the undo steps that would put back items that have
// left the trash by another way, put back or deleted from the browser: they
// would fail, and the browser has its own undo. An operation left with
// nothing to undo is forgotten.
func (m *Model) forgetTrashed(paths ...string) {
	if len(paths) == 0 {
		return
	}
	gone := make(map[string]bool, len(paths))
	for _, p := range paths {
		gone[filepath.Clean(p)] = true
	}
	kept := make([]undoEntry, 0, len(m.undo))
	for _, e := range m.undo {
		steps := slices.DeleteFunc(slices.Clone(e.steps), func(s undoStep) bool {
			return s.kind == stepRestore && s.trashed && gone[filepath.Clean(s.from)]
		})
		if len(steps) == 0 && len(e.steps) > 0 && e.reason == "" && e.lost == 0 {
			continue
		}
		e.steps = steps
		kept = append(kept, e)
	}
	m.undo = kept
}

// trashAgain undoes putting an item back, by trashing it again, if it is
// still as it was put back
func trashAgain(t *fs.Task, s undoStep) error {
	now, err := fs.TakeStamp(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil // Gone already
	}
	if err != nil {
		return err
	}
	if now != s.stamp {
		return fmt.Errorf("%s has changed since it was put back, so it was kept", filepath.Base(s.path))
	}
	tr, err := userTrash()
	if err != nil {
		return err
	}
	_, err = tr.Put(t, s.path)
	return err
}

// trashRows returns how many items the browser shows, and whether there
// is room for the rule under its heading
func (m Model) trashRows() (int, bool) {
	// Above the status bar, less the dialog's border, title and padding,
	// and the heading and the filter or column line
	room := m.height - 2 - 5 - 2
	if room-1 >= 1 {
		return room - 1, true
	}
	return max(room, 1), false
}

// trashWidth is the width of the browser
func (m Model) trashWidth() int {
	return min(m.width, 120)
}

// trashColumns sizes the columns of a row of width cells: narrow rows
// lose where the item came from, then when it was deleted
type trashColumns struct {
	nameW, fromW int
	date         bool
}

func trashCols(width int) trashColumns {
	c := trashColumns{date: width >= 44}
	// Margin, icon and spaces, the size, the date, a margin
	rest := width - 4 - (sizeW + 1) - 1
	if c.date {
		rest -= dateW
	}
	if width >= 70 {
		c.nameW = rest * 2 / 5
		c.fromW = rest - c.nameW - 2
	} else {
		c.nameW = rest
	}
	c.nameW = max(c.nameW, 1)
	return c
}

// trashBox draws the browser
func (m Model) trashBox() []string {
	t := m.theme
	g := currentGlyphs()
	v := m.trash
	width := m.trashWidth()
	inner := max(width, 8) - 2
	rows, roomy := m.trashRows()
	c := trashCols(inner)

	body := []string{m.trashHeading(inner), m.trashSecondLine(inner, c)}
	if roomy {
		body = append(body, m.fg(t.Border).Render(strings.Repeat(g.hline, inner)))
	}
	// With nothing to list, why, over as many lines as it takes
	var why []string
	if len(v.shown) == 0 {
		text, style := "The trash is empty", m.fg(t.Faint)
		switch {
		case v.err != nil:
			text, style = trashError(v.err), m.fg(t.Danger)
		case v.loading:
			text = "Looking in the trash" + g.more
		case len(v.items) > 0:
			text = "No matches"
		}
		for _, line := range strings.Split(ansi.Wordwrap(utils.Printable(text), inner-2, ""), "\n") {
			why = append(why, " "+style.Render(utils.Truncate(line, inner-2)))
		}
	}
	start := window(v.cursor, len(v.shown), rows)
	for i := start; i < start+rows; i++ {
		switch {
		case i < len(v.shown):
			body = append(body, m.trashRow(v.items[v.shown[i]], i == v.cursor, inner, c))
		case i < len(why):
			body = append(body, why[i])
		default:
			body = append(body, "")
		}
	}
	return m.dialog("Trash", t.Accent, body, width)
}

// trashError explains why the trash can't be read. macOS lets a terminal
// read ~/.Trash only with Full Disk Access.
func trashError(err error) string {
	why := err.Error()
	// The trash's path says nothing the heading doesn't
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		why = pathErr.Err.Error()
	}
	if errors.Is(err, os.ErrPermission) && onMac {
		return "Can't read the trash: " + why + ". macOS shows the trash only to a terminal given Full Disk Access, in System Settings > Privacy & Security."
	}
	return "Can't read the trash: " + why
}

// trashHeading shows where the trash is, and how many items and bytes are
// in it
func (m Model) trashHeading(width int) string {
	t := m.theme
	v := m.trash
	var total int64
	counting := false
	for _, it := range v.items {
		if it.size < 0 {
			counting = true
			continue
		}
		total += it.size
	}
	right := plural(len(v.items), "item")
	if len(v.shown) != len(v.items) {
		right = fmt.Sprintf("%d of %s", len(v.shown), right)
	}
	if len(v.items) > 0 {
		right += ", " + utils.HumanizeSize(total)
		if counting {
			right += " so far"
		}
	}
	room := width - 2
	where := ""
	if v.tr != nil {
		where = utils.TruncateLeft(shortDir(v.tr.Files), max(room-utils.Width(right)-2, 1))
	}
	gap := max(room-utils.Width(where)-utils.Width(right), 0)
	return " " + m.fg(t.HeaderFg).Bold(true).Render(where) + strings.Repeat(" ", gap) + m.fg(t.Text).Render(right)
}

// trashSecondLine is the filter while there is one, and otherwise the
// column headings
func (m Model) trashSecondLine(width int, c trashColumns) string {
	t := m.theme
	v := m.trash
	if v.filtering || v.filter.Value() != "" {
		prompt := " " + m.fg(t.Title).Bold(true).Render("/") + " "
		room := max(width-utils.Width(prompt)-1, 1)
		if v.filtering {
			return prompt + v.filter.View(room, m.fg(t.Text), lipgloss.NewStyle().Reverse(true))
		}
		return prompt + m.fg(t.Text).Render(utils.Truncate(utils.Printable(v.filter.Value()), room))
	}
	head := strings.Repeat(" ", 4) + utils.Fit("Name", c.nameW)
	if c.fromW > 0 {
		head += "  " + utils.Fit("From", c.fromW)
	}
	head += utils.FitRight("Size", sizeW+1)
	if c.date {
		head += utils.FitRight("Deleted", dateW)
	}
	return m.fg(t.Faint).Render(utils.Fit(head, width))
}

// trashRow draws an item: its name, where it came from, its size and when
// it was trashed
func (m Model) trashRow(it trashItem, chosen bool, width int, c trashColumns) string {
	t := m.theme
	g := currentGlyphs()
	nameStyle, meta, faint, iconStyle := m.fg(t.Text), m.fg(t.Muted), m.fg(t.Faint).Italic(true), m.fg(t.Muted)
	if it.IsDir {
		nameStyle, iconStyle = m.fg(t.Directory).Bold(true), m.fg(t.Directory)
	}
	if chosen {
		sel := lipgloss.NewStyle().Background(t.CursorBg).Foreground(t.CursorFg)
		nameStyle, meta, faint, iconStyle = sel.Bold(it.IsDir), sel, sel.Italic(true), sel
	}

	icon := ui.GetFileIcon(fs.FileInfo{Name: it.Name, IsDir: it.IsDir, IsSymlink: it.IsLink})
	row := iconStyle.Render(utils.Fit(" "+icon, 4))
	// The letters the filter matched are underlined, as in the file list,
	// short of the ellipsis of a name cut short
	full := utils.Printable(it.Name)
	name := []rune(utils.Truncate(full, c.nameW))
	var hits map[int]bool
	if q := strings.ToLower(m.trash.filter.Value()); q != "" {
		hits = fuzzyPositions(q, strings.ToLower(it.Name))
		if string(name) != full {
			for pos := range hits {
				if pos >= len(name)-3 {
					delete(hits, pos)
				}
			}
		}
	}
	row += paint(name, 0, func(int) int { return 0 }, []lipgloss.Style{nameStyle}, hits)
	row += meta.Render(strings.Repeat(" ", max(c.nameW-utils.Width(string(name)), 0)))
	if c.fromW > 0 {
		from := faint.Render(utils.Fit("unknown origin", c.fromW))
		if it.Original != "" {
			from = meta.Render(utils.Fit(utils.TruncateLeft(shortDir(filepath.Dir(it.Original)), c.fromW), c.fromW))
		}
		row += meta.Render("  ") + from
	}
	size := g.more
	if it.size >= 0 {
		size = utils.HumanizeSize(it.size)
	}
	tail := utils.FitRight(size, sizeW+1)
	if c.date {
		tail += utils.FitRight(trashDate(it.Deleted), dateW)
	}
	row += meta.Render(tail + " ")
	return utils.Cells(row, 0, width)
}

// trashDate shows when an item was trashed: the day and time this year,
// the day and year before
func trashDate(d time.Time) string {
	switch {
	case d.IsZero():
		return ""
	case d.Year() == time.Now().Year():
		return d.Format("Jan 02 15:04")
	}
	return d.Format("Jan 02  2006")
}

// trashConfirmBox asks before deleting for good
func (m Model) trashConfirmBox() []string {
	t := m.theme
	v := m.trash
	var lines []string
	title := "Empty the trash"
	if v.confirm == "delete" {
		title = "Delete for good"
		for _, it := range v.items {
			if it.Path != v.asked {
				continue
			}
			lines = append(lines, fmt.Sprintf("Delete '%s' for good?", utils.Printable(it.Name)))
			if it.size >= 0 && it.IsDir {
				lines = append(lines, "", fmt.Sprintf("It holds %s, %s.", countOf(it.files, "file"), utils.HumanizeSize(it.size)))
			}
		}
	} else {
		var total int64
		counting := false
		for _, it := range v.items {
			if it.size < 0 {
				counting = true
			}
			total += max(it.size, 0)
		}
		size := utils.HumanizeSize(total)
		if counting {
			size = "at least " + size
		}
		lines = append(lines, "Delete everything in the trash for good?", "", fmt.Sprintf("%s, %s.", plural(len(v.items), "item"), size))
	}
	body := make([]string, 0, len(lines)+2)
	for _, line := range lines {
		body = append(body, " "+m.fg(t.Text).Render(line))
	}
	body = append(body, "", " "+m.fg(t.Muted).Render("This is permanent. There is no undo."))
	return m.dialog(title, t.Danger, body, 60)
}

// trashHints are the keys shown while the browser is open
func (m Model) trashHints() []hint {
	k := m.keys
	g := currentGlyphs()
	v := m.trash
	switch {
	case v.confirm != "":
		verb := "delete"
		if v.confirm == "empty" {
			verb = "empty"
		}
		return []hint{{"y", verb}, {"n", "keep"}, {"esc", "cancel"}}
	case v.filtering:
		return []hint{{"enter", "keep filter"}, {g.up + "/" + g.down, "move"}, {"esc", "clear filter"}}
	}
	esc := "close"
	if v.filter.Value() != "" {
		esc = "clear filter"
	}
	return []hint{{"enter", "put back"}, {"p", "restore here"}, keyHint("delete", k.HardDelete), {"E", "empty"},
		keyHint("filter", k.Search), {"esc", esc}}
}

// mouseTrash handles the mouse over the browser: the wheel moves, a click
// picks an item and a double-click puts it back, as enter does, and a
// click outside closes it. While it asks something, the mouse does
// nothing, so a stray click can't answer.
func (m Model) mouseTrash(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	v := &m.trash
	if v.confirm != "" {
		return m, nil
	}
	if delta := wheelDelta(msg, 1); delta != 0 {
		v.move(delta)
		return m, nil
	}
	if msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	r, inside := m.dialogRowAt(m.trashBox(), msg.X, msg.Y)
	if !inside {
		return m.closeTrash()
	}
	rows, roomy := m.trashRows()
	row := r - dialogBodyRow - 2
	if roomy {
		row--
	}
	i := window(v.cursor, len(v.shown), rows) + row
	if row < 0 || row >= rows || i >= len(v.shown) {
		return m, nil
	}
	if it, ok := v.chosen(); ok && m.isDouble(areaDialog, msg.Y, it.Path) {
		m.lastClick = click{}
		return m.trashPutBack()
	}
	v.cursor = i
	m.remember(areaDialog, msg.Y, v.items[v.shown[i]].Path)
	return m, nil
}
