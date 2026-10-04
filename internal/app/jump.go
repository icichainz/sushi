package app

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/icichainz/sushi/internal/config"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/ui"
	"github.com/icichainz/sushi/internal/ui/components"
	"github.com/icichainz/sushi/internal/utils"
)

// Every folder a pane shows is counted, with the time, in
// ~/.config/sushi/history.json, and z opens a palette of the folders
// visited, the most "frecent" first (see config.DirVisit.Score), to jump
// back to one by typing a few letters of it, as with zoxide. The file is
// written in the background, a little after a visit, and when sushi quits.
// With history: false nothing is read or written, and z offers this
// session's folders.

// historySaveDelay is how long visits gather before they are written;
// tests shorten it
var historySaveDelay = 2 * time.Second

// frecency is the folders visited, for the z palette
type frecency struct {
	keep    bool                       // Kept in history.json (history: true)
	dirs    map[string]config.DirVisit // What the palette offers: the file as loaded, and this session's visits
	unsaved map[string]config.DirVisit // Visits since the last save, as counts to add to the file's
	gone    map[string]bool            // Folders found gone since the last save
	saving  bool                       // A save is due
	warned  bool                       // A failed save has been reported
}

// newFrecency returns the folders visited, read from history.json if keep
func newFrecency(keep bool) *frecency {
	f := &frecency{keep: keep, dirs: make(map[string]config.DirVisit),
		unsaved: make(map[string]config.DirVisit), gone: make(map[string]bool)}
	if keep {
		for _, v := range config.LoadDirHistory() {
			f.dirs[v.Path] = v
		}
	}
	return f
}

// record counts a visit to dir, now
func (f *frecency) record(dir string) {
	if f == nil {
		return
	}
	now := time.Now().Unix()
	add := func(set map[string]config.DirVisit) {
		v := set[dir]
		v.Path, v.Count, v.Last = dir, v.Count+1, now
		set[dir] = v
	}
	add(f.dirs)
	if f.keep {
		add(f.unsaved)
		delete(f.gone, dir)
	}
}

// visit counts a visit to dir, and returns the command that saves it a
// little later, unless a save is due already
func (f *frecency) visit(dir string) tea.Cmd {
	if f == nil {
		return nil
	}
	f.record(dir)
	return f.saveSoon()
}

// drop forgets folders that are gone, and returns the command that saves
// that, unless a save is due already
func (f *frecency) drop(dirs ...string) tea.Cmd {
	if f == nil || len(dirs) == 0 {
		return nil
	}
	for _, dir := range dirs {
		delete(f.dirs, dir)
		if f.keep {
			delete(f.unsaved, dir)
			f.gone[dir] = true
		}
	}
	return f.saveSoon()
}

// saveSoon returns the command that saves the history after
// historySaveDelay, unless one is due already
func (f *frecency) saveSoon() tea.Cmd {
	if !f.keep || f.saving {
		return nil
	}
	f.saving = true
	return tea.Tick(historySaveDelay, func(time.Time) tea.Msg { return historySaveMsg{} })
}

// take returns what hasn't been saved, and forgets it
func (f *frecency) take() ([]config.DirVisit, []string) {
	visits := slices.Collect(maps.Values(f.unsaved))
	gone := slices.Collect(maps.Keys(f.gone))
	f.unsaved, f.gone = make(map[string]config.DirVisit), make(map[string]bool)
	return visits, gone
}

// flush saves what hasn't been saved, at once: for when sushi quits
func (f *frecency) flush() error {
	if f == nil || !f.keep {
		return nil
	}
	visits, gone := f.take()
	if len(visits) == 0 && len(gone) == 0 {
		return nil
	}
	return config.SaveDirHistory(visits, gone, time.Now())
}

// historySaveMsg says that it is time to save the history
type historySaveMsg struct{}

func (historySaveMsg) apply(m Model) (tea.Model, tea.Cmd) {
	f := m.frecent
	if f == nil {
		return m, nil
	}
	f.saving = false
	visits, gone := f.take()
	if len(visits) == 0 && len(gone) == 0 {
		return m, nil
	}
	// In the background: the file is read again, to add to it
	return m, func() tea.Msg {
		return historySavedMsg{err: config.SaveDirHistory(visits, gone, time.Now())}
	}
}

// historySavedMsg says how saving the history went
type historySavedMsg struct{ err error }

func (msg historySavedMsg) apply(m Model) (tea.Model, tea.Cmd) {
	// Said once: it would fail again with every folder visited
	if msg.err == nil || m.frecent == nil || m.frecent.warned {
		return m, nil
	}
	m.frecent.warned = true
	cmd := m.setStatus(fmt.Sprintf("Can't save the folder history: %v", msg.err))
	return m, cmd
}

// jumpPalette is the frequent folders palette: the folders visited, the
// best match for what is typed first, then the most frecent
type jumpPalette struct {
	input   components.TextInput
	results []jumpResult
	cursor  int
}

// jumpResult is a folder the palette lists
type jumpResult struct {
	path  string
	shown string       // The path as shown, with ~ for home
	rank  int          // How the query matched, 0 best; see jumpMatch
	score float64      // Frecency
	last  int64        // The latest visit
	hits  map[int]bool // The runes of shown the query matched
}

// openJump opens the frequent folders palette, and looks for folders that
// are gone meanwhile
func (m Model) openJump() (tea.Model, tea.Cmd) {
	m.jumper = jumpPalette{input: components.NewTextInput("")}
	m.mode = ModeJump
	m.filterJumps(false)
	return m, m.checkJumps()
}

// checkJumps looks for folders that are gone, in the background, so one
// on a slow or missing disk holds up nothing
func (m *Model) checkJumps() tea.Cmd {
	if m.frecent == nil || len(m.frecent.dirs) == 0 {
		return nil
	}
	paths := slices.Collect(maps.Keys(m.frecent.dirs))
	return func() tea.Msg {
		var gone []string
		for _, p := range paths {
			if folderGone(p) {
				gone = append(gone, p)
			}
		}
		return jumpCheckedMsg{gone: gone}
	}
}

// folderGone reports whether path is no longer a folder. Other problems,
// such as permissions, don't say it is gone.
func folderGone(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() || errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

// jumpCheckedMsg brings the folders checkJumps found gone
type jumpCheckedMsg struct{ gone []string }

func (msg jumpCheckedMsg) apply(m Model) (tea.Model, tea.Cmd) {
	cmd := m.frecent.drop(msg.gone...)
	if m.mode == ModeJump && len(msg.gone) > 0 {
		m.filterJumps(true)
	}
	return m, cmd
}

// filterJumps lists the folders matching the query, leaving out the one
// the pane shows. With keep, the cursor stays on the folder it is on;
// otherwise it goes to the top.
func (m *Model) filterJumps(keep bool) {
	j := &m.jumper
	current := ""
	if keep && j.cursor < len(j.results) {
		current = j.results[j.cursor].path
	}
	now := time.Now()
	query := strings.ToLower(j.input.Value())
	var out []jumpResult
	if m.frecent != nil {
		for path, v := range m.frecent.dirs {
			if path == m.tab().CurrentPath {
				continue
			}
			shown := displayPath(path)
			if rank, hits, ok := jumpMatch(query, shown); ok {
				out = append(out, jumpResult{path: path, shown: shown, rank: rank, score: v.Score(now), last: v.Last, hits: hits})
			}
		}
	}
	sort.Slice(out, func(a, b int) bool {
		x, y := out[a], out[b]
		switch {
		case x.rank != y.rank:
			return x.rank < y.rank
		case x.score != y.score:
			return x.score > y.score
		}
		return x.path < y.path
	})
	j.results, j.cursor = out, 0
	for i, r := range out {
		if r.path == current {
			j.cursor = i
		}
	}
}

// jumpMatch matches a lowercase query against a folder's path as shown,
// ignoring case, much as the find palette matches names: the folder's own
// name starting with the query ranks 0, containing it 1, the whole path
// containing it 2, and the query's letters in order anywhere in the path
// 3. It returns the runes matched, to underline.
func jumpMatch(query, shown string) (int, map[int]bool, bool) {
	if query == "" {
		return 0, nil, true
	}
	// Lowercasing keeps the number of runes, so positions carry over
	target := strings.ToLower(shown)
	cut := strings.LastIndex(target, "/") + 1
	name := target[cut:]
	run := func(s string, at, offset int) map[int]bool {
		start := offset + utf8.RuneCountInString(s[:at])
		hits := make(map[int]bool)
		for i := range utf8.RuneCountInString(query) {
			hits[start+i] = true
		}
		return hits
	}
	nameAt := utf8.RuneCountInString(target[:cut])
	switch {
	case strings.HasPrefix(name, query):
		return 0, run(name, 0, nameAt), true
	case strings.Contains(name, query):
		return 1, run(name, strings.Index(name, query), nameAt), true
	case strings.Contains(target, query):
		return 2, run(target, strings.Index(target, query), 0), true
	}
	if hits := fuzzyPositions(query, target); hits != nil {
		return 3, hits, true
	}
	return 0, nil, false
}

// handleJumpMode handles keys in the frequent folders palette: typing
// filters, the arrows move, Enter goes there and Esc closes it
func (m Model) handleJumpMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	rows, _ := m.findRows()
	switch msg.Type {
	case tea.KeyEsc:
		m.mode = ModeNormal
		return m, nil
	case tea.KeyEnter:
		return m.jumpToChosen()
	case tea.KeyUp, tea.KeyCtrlP:
		m.moveJumpCursor(-1)
	case tea.KeyDown, tea.KeyCtrlN:
		m.moveJumpCursor(1)
	case tea.KeyPgUp:
		m.moveJumpCursor(-rows)
	case tea.KeyPgDown:
		m.moveJumpCursor(rows)
	default:
		before := m.jumper.input.Value()
		m.jumper.input.Update(msg)
		if m.jumper.input.Value() != before {
			m.filterJumps(false)
		}
	}
	return m, nil
}

// moveJumpCursor moves through the folders by delta, stopping at the ends
func (m *Model) moveJumpCursor(delta int) {
	m.jumper.cursor = max(min(m.jumper.cursor+delta, len(m.jumper.results)-1), 0)
}

// jumpToChosen takes the active pane to the folder chosen, or forgets it
// if it is gone
func (m Model) jumpToChosen() (tea.Model, tea.Cmd) {
	j := m.jumper
	if len(j.results) == 0 {
		return m, nil
	}
	r := j.results[j.cursor]
	if folderGone(r.path) {
		drop := m.frecent.drop(r.path)
		m.filterJumps(false)
		m.jumper.cursor = min(j.cursor, max(len(m.jumper.results)-1, 0))
		status := m.setStatus(r.shown + " is gone")
		return m, tea.Batch(drop, status)
	}
	m.mode = ModeNormal
	return m, m.loadDir(m.tab(), r.path)
}

// jumpBox builds the frequent folders palette: the query, the folders and
// a footer
func (m Model) jumpBox() []string {
	t := m.theme
	g := currentGlyphs()
	j := m.jumper
	width := min(100, m.width)
	inner := width - 2
	rows, roomy := m.findRows()

	key := shownKey(m.keys.Frequent)
	if key == "" {
		key = ">"
	}
	prompt := " " + m.fg(t.Title).Bold(true).Render(key) + " "
	body := []string{prompt + j.input.View(max(inner-utils.Width(prompt)-1, 1), m.fg(t.Text), lipgloss.NewStyle().Reverse(true))}
	if roomy {
		body = append(body, m.fg(t.Border).Render(strings.Repeat(g.hline, inner)))
	}

	now := time.Now()
	start := window(j.cursor, len(j.results), rows)
	for i := start; i < start+rows; i++ {
		switch {
		case i < len(j.results):
			body = append(body, m.jumpRow(j.results[i], i == j.cursor, inner, now))
		case i == 0:
			empty := "No matches"
			if j.input.Value() == "" {
				empty = "No folders visited yet"
			}
			body = append(body, " "+m.fg(t.Faint).Render(utils.Truncate(empty, inner-2)))
		default:
			body = append(body, "")
		}
	}
	if roomy {
		body = append(body, m.jumpFooter(inner))
	}
	title := "Frequent folders"
	if m.frecent != nil && !m.frecent.keep {
		title += " this session"
	}
	return m.dialog(title, t.Accent, body, width)
}

// jumpRow draws a folder, highlighted if chosen: its path, cut from the
// left so its name stays in view, and how long ago it was last visited
func (m Model) jumpRow(r jumpResult, chosen bool, width int, now time.Time) string {
	t := m.theme
	g := currentGlyphs()
	text, muted, faint, icon := m.fg(t.Directory).Bold(true), m.fg(t.Muted), m.fg(t.Faint), m.fg(t.Directory)
	if chosen {
		sel := lipgloss.NewStyle().Background(t.CursorBg).Foreground(t.CursorFg)
		text, muted, faint, icon = sel.Bold(true), sel, sel, sel
	}
	lead := " " + ui.GetFileIcon(fs.FileInfo{Name: filepath.Base(r.path), IsDir: true}) + "  "
	when := ago(r.last, now) + " "
	room := width - utils.Width(lead) - utils.Width(when) - 2
	if room < 12 {
		// Too narrow for both: the path matters more
		when, room = "", width-utils.Width(lead)-1
	}

	runes := []rune(r.shown)
	drop, ellipsis := 0, ""
	if utils.Width(r.shown) > room {
		ellipsis = g.more
		drop = cutLeft(runes, room-utils.Width(ellipsis))
	}
	nameAt := 0
	for i, c := range runes {
		if c == '/' && i < len(runes)-1 {
			nameAt = i + 1
		}
	}
	class := func(i int) int {
		if i < nameAt {
			return 0
		}
		return 1
	}
	row := icon.Render(lead) + muted.Render(ellipsis) + paint(runes[drop:], drop, class, []lipgloss.Style{muted, text}, r.hits)
	pad := max(width-utils.Width(row)-utils.Width(when), 0)
	return utils.Cells(row+muted.Render(strings.Repeat(" ", pad))+faint.Render(when), 0, width)
}

// ago says briefly how long ago a Unix time was, as in "5m ago"
func ago(unix int64, now time.Time) string {
	d := now.Sub(time.Unix(unix, 0))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m ago"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h ago"
	case d < 60*24*time.Hour:
		return strconv.Itoa(int(d.Hours()/24)) + "d ago"
	}
	return time.Unix(unix, 0).Format("Jan 2006")
}

// jumpFooter says how many folders match, and how they are ranked
func (m Model) jumpFooter(width int) string {
	t := m.theme
	left := plural(len(m.jumper.results), "folder")
	right := "most visited and most recent first"
	if m.frecent != nil && !m.frecent.keep {
		right = "history is off: this session's"
	}
	room := width - 2
	left = utils.Truncate(left, room)
	right = utils.Truncate(right, max(room-utils.Width(left)-3, 0))
	gap := max(room-utils.Width(left)-utils.Width(right), 0)
	return " " + m.fg(t.Muted).Render(left) + strings.Repeat(" ", gap) + m.fg(t.Faint).Render(right)
}

// jumpHints are the keys shown while the palette is open
func (m Model) jumpHints() []hint {
	g := currentGlyphs()
	return []hint{{"enter", "go"}, {g.up + "/" + g.down, "move"}, {"esc", "close"}, {"type", "to filter"}}
}

// mouseJump handles the mouse over the palette: the wheel moves through
// the folders, a click picks one and a double-click goes there, and
// clicking outside closes it, as Esc does
func (m Model) mouseJump(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	j := &m.jumper
	if delta := wheelDelta(msg, 1); delta != 0 {
		m.moveJumpCursor(delta)
		return m, nil
	}
	if msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	r, inside := m.dialogRowAt(m.jumpBox(), msg.X, msg.Y)
	if !inside {
		m.mode = ModeNormal
		return m, nil
	}
	// The body is the query, a rule if there is room, then the folders
	rows, roomy := m.findRows()
	row := r - dialogBodyRow - 1
	if roomy {
		row--
	}
	i := window(j.cursor, len(j.results), rows) + row
	if row < 0 || row >= rows || i >= len(j.results) {
		return m, nil
	}
	// The list may have scrolled to follow the first click, so what counts
	// is the same row and the folder that click picked
	if m.isDouble(areaDialog, msg.Y, j.results[j.cursor].path) {
		m.lastClick = click{}
		return m.jumpToChosen()
	}
	j.cursor = i
	m.remember(areaDialog, msg.Y, j.results[i].path)
	return m, nil
}
