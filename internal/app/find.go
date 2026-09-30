package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/search"
	"github.com/icichainz/sushi/internal/ui"
	"github.com/icichainz/sushi/internal/ui/components"
	"github.com/icichainz/sushi/internal/utils"
)

// findLimit is the most results a search shows
var findLimit = 1000

// findDelay is how long typing must pause before a search starts, so a
// word typed quickly starts one search rather than one per letter
const findDelay = 150 * time.Millisecond

// finder is the search palette: f finds files and folders by name below
// the current directory, F finds lines inside the files there
type finder struct {
	content   bool   // Searching inside files rather than names
	root      string // Directory searched
	input     components.TextInput
	results   []search.Result
	cursor    int
	seq       int      // Counts edits, so only the pause after the last one starts a search
	run       *findRun // Search in progress, or nil
	waiting   bool     // Typed since the last search started
	truncated bool     // The last search found more than findLimit
	err       error
}

// findRun is a search running in the background
type findRun struct {
	results chan search.Result
	cancel  context.CancelFunc
	// Set before results is closed, and read only after
	truncated bool
	err       error
}

// findDelayMsg starts a search once typing has paused
type findDelayMsg struct{ seq int }

// findResultsMsg brings results of a search as they are found
type findResultsMsg struct {
	run       *findRun
	results   []search.Result
	done      bool
	truncated bool
	err       error
}

// previewJump is a line of a file to show once the file's preview loads
type previewJump struct {
	tabID int
	path  string
	line  int
}

// openFind opens the search palette on the current directory
func (m Model) openFind(content bool) (tea.Model, tea.Cmd) {
	m.stopFind()
	m.find = finder{
		content: content,
		root:    m.tab().CurrentPath,
		input:   components.NewTextInput(""),
		seq:     m.find.seq, // Kept, so a pause from before can't start a search
	}
	m.mode = ModeFind
	return m, nil
}

// stopFind cancels the search in progress, if any
func (m *Model) stopFind() {
	if m.find.run != nil {
		m.find.run.cancel()
		m.find.run = nil
	}
}

// handleFindMode handles keys in the search palette. Typing edits the
// query; the arrows move through the results.
func (m Model) handleFindMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	rows, _ := m.findRows()
	switch msg.Type {
	case tea.KeyEsc:
		m.stopFind()
		m.mode = ModeNormal
		return m, nil
	case tea.KeyEnter:
		return m.openFindResult()
	case tea.KeyUp, tea.KeyCtrlP:
		m.moveFindCursor(-1)
	case tea.KeyDown, tea.KeyCtrlN:
		m.moveFindCursor(1)
	case tea.KeyPgUp:
		m.moveFindCursor(-rows)
	case tea.KeyPgDown:
		m.moveFindCursor(rows)
	case tea.KeyTab, tea.KeyShiftTab:
		// The same query, the other kind of search
		m.find.content = !m.find.content
		cmd := m.startFind()
		return m, cmd
	default:
		before := m.find.input.Value()
		m.find.input.Update(msg)
		if m.find.input.Value() != before {
			cmd := m.searchSoon()
			return m, cmd
		}
	}
	return m, nil
}

// moveFindCursor moves through the results by delta, stopping at the ends
func (m *Model) moveFindCursor(delta int) {
	m.find.cursor = max(min(m.find.cursor+delta, len(m.find.results)-1), 0)
}

// searchSoon cancels the search in progress and starts a new one when
// typing pauses
func (m *Model) searchSoon() tea.Cmd {
	m.stopFind()
	m.find.seq++
	m.find.waiting = true
	seq := m.find.seq
	return tea.Tick(findDelay, func(time.Time) tea.Msg { return findDelayMsg{seq: seq} })
}

// handleFindDelay starts the search if nothing was typed since
func (m *Model) handleFindDelay(msg findDelayMsg) tea.Cmd {
	if m.mode != ModeFind || msg.seq != m.find.seq {
		return nil
	}
	return m.startFind()
}

// startFind searches for the query in the background, replacing any
// search in progress
func (m *Model) startFind() tea.Cmd {
	m.stopFind()
	f := &m.find
	f.waiting = false
	f.results, f.cursor, f.truncated, f.err = nil, 0, false, nil
	query := f.input.Value()
	if query == "" {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	run := &findRun{results: make(chan search.Result, 256), cancel: cancel}
	opts := search.Options{Root: f.root, ShowHidden: m.showHidden, Skip: search.DefaultSkip, Limit: findLimit}
	content := f.content
	go func() {
		defer close(run.results)
		emit := func(r search.Result) {
			select {
			case run.results <- r:
			case <-ctx.Done():
			}
		}
		if content {
			run.truncated, run.err = search.Contents(ctx, opts, query, emit)
		} else {
			run.truncated, run.err = search.Names(ctx, opts, nameMatcher(query), emit)
		}
	}()
	f.run = run
	return waitFind(run)
}

// waitFind waits for results of run, taking whatever else has arrived
// with the first, so a fast search doesn't cost a redraw per result
func waitFind(run *findRun) tea.Cmd {
	return func() tea.Msg {
		msg := findResultsMsg{run: run}
		for len(msg.results) < 512 {
			var r search.Result
			var ok bool
			if len(msg.results) == 0 {
				r, ok = <-run.results
			} else {
				select {
				case r, ok = <-run.results:
				default:
					return msg
				}
			}
			if !ok {
				msg.done, msg.truncated, msg.err = true, run.truncated, run.err
				return msg
			}
			msg.results = append(msg.results, r)
		}
		return msg
	}
}

// handleFindResults adds results from the current search, and waits for
// more until it is done
func (m *Model) handleFindResults(msg findResultsMsg) tea.Cmd {
	f := &m.find
	if msg.run != f.run {
		return nil // From a search since replaced or cancelled
	}
	f.add(msg.results)
	if !msg.done {
		return waitFind(msg.run)
	}
	msg.run.cancel() // Releases the context
	f.run = nil
	f.truncated = msg.truncated
	if msg.err != nil && !errors.Is(msg.err, context.Canceled) {
		f.err = msg.err
	}
	return nil
}

// add adds results as they arrive. Names are kept best match first; the
// cursor stays on the result it is on, or at the top if it hasn't moved.
func (f *finder) add(results []search.Result) {
	current := ""
	if f.cursor > 0 {
		current = f.results[f.cursor].Path
	}
	f.results = append(f.results, results...)
	if f.content {
		return
	}
	sort.SliceStable(f.results, func(i, j int) bool { return f.results[i].Score < f.results[j].Score })
	for i, r := range f.results {
		if current != "" && r.Path == current {
			f.cursor = i
			break
		}
	}
}

// openFindResult goes to the chosen result: its directory, with the
// cursor on it, and for a line of a file, the preview showing that line
func (m Model) openFindResult() (tea.Model, tea.Cmd) {
	if len(m.find.results) == 0 {
		return m, nil
	}
	r := m.find.results[m.find.cursor]
	m.stopFind()
	m.mode = ModeNormal

	tab := m.tab()
	tab.focusPath = r.Path
	if r.Line > 0 {
		m.jump = previewJump{tabID: tab.ID, path: r.Path, line: r.Line}
	}
	return m, m.loadDir(tab, filepath.Dir(r.Path))
}

// showJump scrolls the preview to a search result's line once the file's
// preview has loaded
func (m *Model) showJump(msg previewLoadedMsg) {
	j := m.jump
	tab := m.tabByID(msg.tabID)
	if j.line == 0 || j.tabID != msg.tabID || j.path != msg.preview.Path || tab == nil || tab.Preview.Path != j.path {
		return
	}
	// Centred, so the lines around it show too
	rows := m.previewRows()
	tab.PreviewScroll = max(min(j.line-1-(rows-1)/2, tab.Preview.MaxScroll(rows)), 0)
	m.jump = previewJump{}
}

// findQuery prepares a name query: lowercase, with slashes as separators,
// and whether it names a path rather than just a name
func findQuery(query string) (string, bool) {
	q := strings.ToLower(filepath.ToSlash(query))
	return q, strings.Contains(q, "/")
}

// nameMatcher matches names for f, ignoring case, with the fuzzy match of
// the / search. Names starting with the query rank first, then names
// containing it. A query with a slash, such as "src/main", matches the
// path below the search root instead.
func nameMatcher(query string) search.Matcher {
	q, byPath := findQuery(query)
	return func(rel, name string) (int, bool) {
		target := name
		if byPath {
			target = filepath.ToSlash(rel)
		}
		target = strings.ToLower(target)
		switch {
		case strings.HasPrefix(target, q):
			return 0, true
		case strings.Contains(target, q):
			return 1, true
		case fuzzyMatch(q, target):
			return 2, true
		}
		return 0, false
	}
}

// namePositions returns which runes of a result's path (with slashes) the
// query matched: a run of them if the name contains the query, otherwise
// the letters of the fuzzy match
func namePositions(query, rel string) map[int]bool {
	q, byPath := findQuery(query)
	target := strings.ToLower(rel)
	offset := 0
	if !byPath {
		cut := strings.LastIndex(target, "/") + 1
		offset = utf8.RuneCountInString(target[:cut])
		target = target[cut:]
	}

	// Lowercasing keeps the number of runes, so positions carry over to rel
	hits := fuzzyPositions(q, target)
	if at := strings.Index(target, q); at >= 0 {
		hits = make(map[int]bool)
		start := utf8.RuneCountInString(target[:at])
		for i := range utf8.RuneCountInString(q) {
			hits[start+i] = true
		}
	}
	shifted := make(map[int]bool, len(hits))
	for pos := range hits {
		shifted[pos+offset] = true
	}
	return shifted
}

// findRows returns how many results the palette shows, and whether there
// is also room for its divider and footer
func (m Model) findRows() (int, bool) {
	// Above the status bar, less the dialog's border, title and padding
	room := m.height - 2 - 5
	if room-3 >= 1 {
		return min(room-3, 20), true
	}
	return max(room-1, 1), false
}

// findBox builds the search palette: the query, the results and a footer
// saying how the search is going
func (m Model) findBox() []string {
	t := m.theme
	g := currentGlyphs()
	f := m.find
	width := min(100, m.width)
	inner := width - 2
	rows, roomy := m.findRows()

	title, key := "Find files", "f"
	if f.content {
		title, key = "Find in files", "F"
	}
	prompt := " " + m.fg(t.Title).Bold(true).Render(key) + " "
	body := []string{prompt + f.input.View(max(inner-4, 1), m.fg(t.Text), lipgloss.NewStyle().Reverse(true))}
	if roomy {
		body = append(body, m.fg(t.Border).Render(strings.Repeat(g.hline, inner)))
	}

	start := window(f.cursor, len(f.results), rows)
	for i := start; i < start+rows; i++ {
		switch {
		case i < len(f.results):
			body = append(body, m.findRow(f.results[i], i == f.cursor, inner))
		case i == 0:
			style := m.fg(t.Faint)
			if f.err != nil {
				style = m.fg(t.Danger)
			}
			body = append(body, " "+style.Render(utils.Truncate(utils.Printable(m.findEmpty()), inner-2)))
		default:
			body = append(body, "")
		}
	}
	if roomy {
		body = append(body, m.findFooter(inner))
	}
	return m.dialog(title, t.Accent, body, width)
}

// findEmpty says why there are no results to list
func (m Model) findEmpty() string {
	f := m.find
	switch {
	case f.input.Value() == "" && f.content:
		return "Type to search inside the files below this folder"
	case f.input.Value() == "":
		return "Type to find files and folders below this folder"
	case f.run != nil || f.waiting:
		return "Searching" + currentGlyphs().more
	case f.err != nil:
		return fmt.Sprintf("Can't search: %v", f.err)
	}
	return "No matches"
}

// findFooter shows how many results there are and where the search looks
func (m Model) findFooter(width int) string {
	t := m.theme
	f := m.find
	n := len(f.results)

	left, style := "", m.fg(t.Muted)
	switch {
	case f.input.Value() == "", n == 0 && f.run == nil && !f.waiting:
	case f.run != nil || f.waiting:
		left = fmt.Sprintf("Searching%s %d found", currentGlyphs().more, n)
	case f.truncated:
		left, style = fmt.Sprintf("%d shown, more results not shown", n), m.fg(t.Highlight)
	case n == 1:
		left = "1 match"
	default:
		left = fmt.Sprintf("%d matches", n)
	}

	where := "in " + strings.Join(pathSegments(f.root), string(filepath.Separator))
	room := width - 2
	left = utils.Truncate(left, room)
	where = utils.TruncateLeft(where, max(room-utils.Width(left)-3, 0))
	gap := max(room-utils.Width(left)-utils.Width(where), 0)
	return " " + style.Render(left) + strings.Repeat(" ", gap) + m.fg(t.Faint).Render(where)
}

// findHints are the keys shown while the palette is open
func (m Model) findHints() []hint {
	g := currentGlyphs()
	other := "search inside files"
	if m.find.content {
		other = "find by name"
	}
	return []hint{{"enter", "go"}, {g.up + "/" + g.down, "move"}, {"tab", other}, {"esc", "close"}}
}

// findRow draws one result, highlighted if chosen: a path for a name
// match, or "path:line: text" for a line inside a file
func (m Model) findRow(r search.Result, chosen bool, width int) string {
	t := m.theme
	g := currentGlyphs()
	text, muted, faint, accent := m.fg(t.Text), m.fg(t.Muted), m.fg(t.Faint), m.fg(t.Highlight)
	if r.IsDir {
		text = m.fg(t.Directory).Bold(true)
	}
	if chosen {
		sel := lipgloss.NewStyle().Background(t.CursorBg).Foreground(t.CursorFg)
		text, muted, faint, accent = sel.Bold(r.IsDir), sel, sel, sel
	}

	rel := []rune(utils.Printable(filepath.ToSlash(r.Rel)))
	var row string
	if r.Line == 0 {
		icon := ui.GetFileIcon(fs.FileInfo{Name: filepath.Base(r.Path), IsDir: r.IsDir})
		lead := " " + icon + "  "
		iconStyle := muted
		if r.IsDir && !chosen {
			iconStyle = m.fg(t.Directory)
		}
		row = iconStyle.Render(lead)

		// Cut from the left, so the name stays in view
		room := width - utils.Width(lead) - 1
		drop, ellipsis := 0, ""
		if utils.Width(string(rel)) > room {
			ellipsis = g.more
			drop = cutLeft(rel, room-utils.Width(ellipsis))
		}
		nameAt := 0
		for i, c := range rel {
			if c == '/' {
				nameAt = i + 1
			}
		}
		class := func(i int) int {
			if i < nameAt {
				return 0
			}
			return 1
		}
		hits := namePositions(m.find.input.Value(), string(rel))
		row += muted.Render(ellipsis) + paint(rel[drop:], drop, class, []lipgloss.Style{muted, text}, hits)
	} else {
		// The path gets up to two fifths of the row, the text the rest
		path := utils.TruncateLeft(string(rel), max(width*2/5, 8))
		row = " " + muted.Render(path) + faint.Render(":") + accent.Render(fmt.Sprint(r.Line)) + faint.Render(": ")
		room := width - utils.Width(row) - 1

		// Keep the match in view on long lines
		line := []rune(utils.Printable(r.Text))
		size := utf8.RuneCountInString(m.find.input.Value())
		from, ellipsis := 0, ""
		if utils.Width(string(line[:min(r.Col+size, len(line))])) > room {
			from, ellipsis = max(r.Col-room/3, 0), g.more
		}
		line = line[from:]
		line = line[:cutRight(line, max(room-utils.Width(ellipsis), 0))]
		hits := make(map[int]bool, size)
		for i := range size {
			hits[r.Col+i] = true
		}
		row += faint.Render(ellipsis) + paint(line, from, func(int) int { return 0 }, []lipgloss.Style{text}, hits)
	}

	pad := max(width-utils.Width(row), 0)
	return utils.Cells(row+muted.Render(strings.Repeat(" ", pad)), 0, width)
}

// paint draws runes, which start at position offset of the whole text,
// in the style class gives each position, underlining those in hits
func paint(runes []rune, offset int, class func(int) int, styles []lipgloss.Style, hits map[int]bool) string {
	var b strings.Builder
	for i := 0; i < len(runes); {
		c, hit := class(offset+i), hits[offset+i]
		j := i + 1
		for j < len(runes) && class(offset+j) == c && hits[offset+j] == hit {
			j++
		}
		style := styles[c]
		if hit {
			style = style.Underline(true).Bold(true)
		}
		b.WriteString(style.Render(string(runes[i:j])))
		i = j
	}
	return b.String()
}

// cutLeft returns how many runes to drop from the start of runes for the
// rest to fit in width cells
func cutLeft(runes []rune, width int) int {
	total := utils.Width(string(runes))
	i := 0
	for i < len(runes) && total > width {
		total -= utils.Width(string(runes[i]))
		i++
	}
	return i
}

// cutRight returns how many runes from the start of runes fit in width cells
func cutRight(runes []rune, width int) int {
	used := 0
	for i, r := range runes {
		used += utils.Width(string(r))
		if used > width {
			return i
		}
	}
	return len(runes)
}
