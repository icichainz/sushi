package app

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/ui/components"
	"github.com/icichainz/sushi/internal/utils"
)

// Rename by pattern (M) renames the selection, or the file under the
// cursor, with a Find and a Replace field, showing each new name as it is
// typed. Find is plain text, matched case-sensitively wherever it occurs,
// or a regular expression between slashes, as in /(\d+)-(\w+)/, whose
// groups the replacement takes as $1 or ${1}, even with a letter, digit or
// _ after them (see bracedGroups), and $$ for a $. With Find empty,
// Replace is the whole new name. The replacement also takes tokens:
//
//	{n}     the file's number, from 1, in the order of the list
//	{n:3}   the same, zero-padded to 3 digits
//	{name}  the name without its extension
//	{ext}   the extension with its dot, as ".jpg", or nothing
//	{date}  the modification date, as 2006-01-02
//
// Names that can't be used, or that two files would get, or that are
// taken, are marked in red, and Enter does nothing until there are none.
// The renames go through fs.PlanRenames and fs.RenameAll, as the bulk
// rename's do, so swaps work and a failure part way puts every name back;
// one ctrl+z undoes them all.

// patternField is a field of the dialog
type patternField int

const (
	fieldFind patternField = iota
	fieldReplace
)

// patternRename is the dialog's state
type patternRename struct {
	files         []fs.FileInfo // What is renamed, in the order of the list
	find, replace components.TextInput
	field         patternField
	scroll        int // First name of the list shown
	rows          []patternRow
	changes       int    // Names that change
	problems      int    // New names that can't be used
	err           string // Find isn't a valid expression
	failed        string // Why Enter didn't rename, until the next edit
}

// patternRow is a name and what it becomes
type patternRow struct {
	from, to string
	problem  string // Why to can't be used, if it can't
}

// patternTokens are the tokens the replacement takes
var patternTokens = regexp.MustCompile(`\{(?:n(?::(\d+))?|name|ext|date)\}`)

// maxPad is the widest {n:width} pads to
const maxPad = 18

// startPatternRename opens the dialog on the selection, or the file under
// the cursor
func (m Model) startPatternRename() (tea.Model, tea.Cmd) {
	paths := m.targets()
	if len(paths) == 0 {
		return m, nil
	}
	files := listOrder(m.tab(), paths)
	for _, f := range files {
		// The renames go through the bulk rename's list of lines
		if strings.ContainsAny(f.Name, "\n\r") {
			cmd := m.setStatus(fmt.Sprintf("Can't rename %q by pattern: its name has a line break", f.Name))
			return m, cmd
		}
	}
	m.pattern = patternRename{files: files, find: components.NewTextInput(""), replace: components.NewTextInput("")}
	m.pattern.update()
	m.mode = ModePattern
	return m, nil
}

// listOrder returns the files at paths as the list shows them, then any
// selected in other folders, by path
func listOrder(tab *Tab, paths []string) []fs.FileInfo {
	want := make(map[string]bool, len(paths))
	for _, p := range paths {
		want[p] = true
	}
	var files []fs.FileInfo
	for _, f := range tab.Files {
		if want[f.Path] {
			files = append(files, f)
			delete(want, f.Path)
		}
	}
	var rest []string
	for p := range want {
		rest = append(rest, p)
	}
	sort.Strings(rest)
	for _, p := range rest {
		f := fs.FileInfo{Name: filepath.Base(p), Path: p}
		if info, err := os.Lstat(p); err == nil {
			f = fs.NewFileInfo(p, info)
		}
		files = append(files, f)
	}
	return files
}

// applyPattern returns the names files get from find and replace, as the
// dialog describes them, or an error if find is an invalid expression
func applyPattern(files []fs.FileInfo, find, replace string) ([]string, error) {
	var re *regexp.Regexp
	if len(find) >= 2 && strings.HasPrefix(find, "/") && strings.HasSuffix(find, "/") {
		var err error
		if re, err = regexp.Compile(find[1 : len(find)-1]); err != nil {
			return nil, fmt.Errorf("invalid regular expression: %w", err)
		}
	}
	names := make([]string, len(files))
	for i, f := range files {
		with := expandTokens(replace, i+1, f, re != nil)
		if re != nil {
			with = bracedGroups(with)
		}
		switch {
		case find == "" && replace == "":
			names[i] = f.Name
		case find == "":
			names[i] = with
		case re != nil:
			names[i] = re.ReplaceAllString(f.Name, with)
		default:
			names[i] = strings.ReplaceAll(f.Name, find, with)
		}
	}
	return names, nil
}

// expandTokens puts file's values, as the nth file, in place of the tokens
// in s. For a regular expression's replacement, a $ in them is doubled, so
// it stays a $ rather than naming a group.
func expandTokens(s string, n int, f fs.FileInfo, regex bool) string {
	stem, ext := fs.SplitExt(f.Name)
	if f.IsDir {
		// Folders keep their dots, as in "v1.2"
		stem, ext = f.Name, ""
	}
	return patternTokens.ReplaceAllStringFunc(s, func(token string) string {
		var v string
		switch token {
		case "{name}":
			v = stem
		case "{ext}":
			v = ext
		case "{date}":
			v = f.ModTime.Format("2006-01-02")
		default:
			width := 0
			if _, digits, ok := strings.Cut(strings.Trim(token, "{}"), ":"); ok {
				width, _ = strconv.Atoi(digits)
			}
			v = fmt.Sprintf("%0*d", min(width, maxPad), n)
		}
		if regex {
			v = strings.ReplaceAll(v, "$", "$$")
		}
		return v
	})
}

// bracedGroups writes each $N of a regular expression's replacement as
// ${N}: Go takes the longest name it can, so $2_$1 would be the group
// named "2_", then group 1, where group 2, an underscore and group 1 are
// meant. $$ stays, for a $ of its own, and so do ${...} and $name.
func bracedGroups(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '$' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		switch next := s[i+1]; {
		case next == '$':
			b.WriteString("$$")
			i++
		case next >= '0' && next <= '9':
			j := i + 1
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			b.WriteString("${" + s[i+1:j] + "}")
			i = j - 1
		default:
			b.WriteByte('$')
		}
	}
	return b.String()
}

// update works out the new names and checks them as fs.PlanRenames will:
// valid names, none given to two files, none taken by a file that isn't
// being renamed away. Names are compared as the file system compares them,
// ignoring case and accents' composition, so what would clash on macOS is
// caught here.
func (p *patternRename) update() {
	p.rows, p.changes, p.problems, p.err, p.failed = nil, 0, 0, "", ""
	names, err := applyPattern(p.files, p.find.Value(), p.replace.Value())
	if err != nil {
		p.err = err.Error()
		for _, f := range p.files {
			p.rows = append(p.rows, patternRow{from: f.Name, to: f.Name})
		}
		return
	}

	// A name a file is renamed away from is free, however it is spelled
	moving := make(fs.Leaving)
	for i, f := range p.files {
		if names[i] != f.Name {
			moving.Add(f.Path)
		}
	}
	claimed := make(map[string]int) // Folder and folded name → row
	for i, f := range p.files {
		row := patternRow{from: f.Name, to: names[i]}
		if row.to != row.from {
			p.changes++
			dir := filepath.Dir(f.Path)
			dst := filepath.Join(dir, row.to)
			key := dir + "\x00" + nameKey(row.to)
			if err := fs.ValidateName(row.to); err != nil {
				row.problem = err.Error()
			} else if strings.ContainsAny(row.to, "\n\r") {
				row.problem = "name can't contain a line break"
			} else if other, ok := claimed[key]; ok {
				row.problem = "another file gets this name"
				if p.rows[other].problem == "" {
					p.rows[other].problem = row.problem
				}
			} else if !moving.Holds(dst) && occupied(dst, f.Path) {
				row.problem = "already exists"
			}
			if _, ok := claimed[key]; !ok {
				claimed[key] = i
			}
		}
		p.rows = append(p.rows, row)
	}
	for _, r := range p.rows {
		if r.problem != "" {
			p.problems++
		}
	}
}

// handlePatternMode handles keys in the dialog: typing edits the field,
// tab switches fields, the arrows scroll the names, enter renames
func (m Model) handlePatternMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := &m.pattern
	rows, _ := m.patternRows()
	switch msg.Type {
	case tea.KeyEsc:
		m.mode = ModeNormal
		m.pattern = patternRename{}
		return m, nil
	case tea.KeyEnter:
		return m.applyPatternRename()
	case tea.KeyTab, tea.KeyShiftTab:
		p.field = 1 - p.field
		return m, nil
	case tea.KeyUp:
		p.scrollBy(-1, rows)
		return m, nil
	case tea.KeyDown:
		p.scrollBy(1, rows)
		return m, nil
	case tea.KeyPgUp:
		p.scrollBy(-max(rows-1, 1), rows)
		return m, nil
	case tea.KeyPgDown:
		p.scrollBy(max(rows-1, 1), rows)
		return m, nil
	}
	input := &p.find
	if p.field == fieldReplace {
		input = &p.replace
	}
	before := input.Value()
	if input.Update(msg) && input.Value() != before {
		p.update()
	}
	return m, nil
}

// scrollBy scrolls the list of names, which shows rows at a time
func (p *patternRename) scrollBy(delta, rows int) {
	p.scroll = max(min(p.scroll+delta, len(p.rows)-rows), 0)
}

// applyPatternRename renames the files, if every new name can be used.
// The files may have changed since the names were checked, so the batch is
// planned again from what is there now.
func (m Model) applyPatternRename() (tea.Model, tea.Cmd) {
	p := &m.pattern
	p.update()
	switch {
	case p.err != "":
		return m, nil
	case p.problems > 0:
		p.failed = "Change the names in red first"
		return m, nil
	case p.changes == 0:
		m.mode = ModeNormal
		m.pattern = patternRename{}
		cmd := m.setStatus("No names changed")
		return m, cmd
	}
	paths := make([]string, len(p.files))
	names := make([]string, len(p.rows))
	for i, f := range p.files {
		paths[i], names[i] = f.Path, p.rows[i].to
	}
	pairs, err := fs.PlanRenames(paths, strings.Join(names, "\n"), occupied)
	if err == nil {
		err = fs.RenameAll(pairs)
	}
	if err != nil {
		p.failed = "Can't rename: " + err.Error()
		return m, nil
	}
	m.mode = ModeNormal
	m.pattern = patternRename{}
	return m.renamed(pairs)
}

// patternRows returns how many names the dialog lists, and whether it has
// room for its rules and the line on tokens too
func (m Model) patternRows() (int, bool) {
	// Above the status bar, less the dialog's border, title and padding,
	// then the two fields and the summary
	room := m.height - 2 - 5 - 3
	if room >= 3 {
		return min(room-2, 14), true
	}
	return max(room, 0), false
}

// patternLabelW is the width of the fields' labels
const patternLabelW = 10

// patternBox builds the dialog: the fields, the names as they would be,
// and how many change
func (m Model) patternBox() []string {
	t := m.theme
	g := currentGlyphs()
	p := m.pattern
	width := min(84, m.width)
	inner := width - 2
	rows, roomy := m.patternRows()

	field := func(f patternField, label string, input components.TextInput) string {
		lead := " " + m.fg(t.Muted).Render(utils.Fit(label, patternLabelW-1))
		room := max(inner-patternLabelW-1, 1)
		if p.field == f {
			lead = " " + m.fg(t.Title).Bold(true).Render(utils.Fit(label, patternLabelW-1))
			return lead + input.View(room, m.fg(t.Text), lipgloss.NewStyle().Reverse(true))
		}
		return lead + m.fg(t.Text).Render(utils.Truncate(utils.Printable(input.Value()), room))
	}
	body := []string{field(fieldFind, "Find", p.find), field(fieldReplace, "Replace", p.replace)}
	if roomy {
		body = append(body, m.fg(t.Border).Render(strings.Repeat(g.hline, inner)))
	}

	// The names, with what is left over on the last row
	start := min(p.scroll, max(len(p.rows)-rows, 0))
	for i := start; i < start+rows; i++ {
		switch {
		case i >= len(p.rows):
			body = append(body, "")
		case i == start+rows-1 && i < len(p.rows)-1:
			body = append(body, " "+m.fg(t.Faint).Render(fmt.Sprintf("+%d more", len(p.rows)-i)))
		default:
			body = append(body, m.patternRow(p.rows[i], inner))
		}
	}

	var summary string
	switch {
	case p.err != "":
		summary = m.fg(t.Danger).Render(utils.Truncate(utils.Printable(p.err), inner-2))
	case p.failed != "":
		summary = m.fg(t.Danger).Render(utils.Truncate(utils.Printable(p.failed), inner-2))
	default:
		text := fmt.Sprintf("%d of %s change", p.changes, plural(len(p.rows), "name"))
		summary = m.fg(t.Muted).Render(utils.Truncate(text, inner-2))
		if p.problems > 0 {
			trouble := fmt.Sprintf("  %s %d can't be used", g.dot, p.problems)
			summary += m.fg(t.Danger).Render(utils.Truncate(trouble, max(inner-2-utils.Width(text), 0)))
		}
	}
	body = append(body, " "+summary)
	if roomy {
		help := "/regex/ with $1, $$ for $   {n} {n:3} {name} {ext} {date}"
		body = append(body, " "+m.fg(t.Faint).Render(utils.Truncate(help, inner-2)))
	}

	title := "Rename " + plural(len(p.files), "item") + " by pattern"
	if len(p.files) == 1 {
		title = "Rename " + utils.Printable(p.files[0].Name) + " by pattern"
	}
	return m.dialog(utils.Truncate(title, inner-2), t.Accent, body, width)
}

// patternRow draws a name and what it becomes: faint if it stays, red if
// the new name can't be used, with why
func (m Model) patternRow(r patternRow, width int) string {
	t := m.theme
	room := width - 2
	from := utils.Printable(r.from)
	if r.to == r.from {
		return " " + m.fg(t.Faint).Render(utils.Truncate(from, room))
	}
	arrow := " → "
	if currentGlyphs().more == "..." {
		arrow = " -> "
	}
	to, why := utils.Printable(r.to), ""
	if r.problem != "" {
		why = "  " + r.problem
	}
	// The old name gets up to two fifths, the new one and why the rest
	fromW := min(utils.Width(from), max(room*2/5, 4))
	rest := max(room-fromW-utils.Width(arrow), 1)
	toStyle := m.fg(t.Text)
	if r.problem != "" {
		toStyle = m.fg(t.Danger)
	}
	toText := utils.Truncate(to+why, rest)
	return " " + m.fg(t.Muted).Render(utils.Truncate(from, fromW)) + m.fg(t.Faint).Render(arrow) + toStyle.Render(toText)
}

// patternHints are the keys shown while the dialog is open
func (m Model) patternHints() []hint {
	g := currentGlyphs()
	return []hint{{"enter", "rename"}, {"tab", "other field"}, {g.up + "/" + g.down, "scroll"}, {"esc", "cancel"}, {"ctrl+u", "clear"}}
}

// mousePattern handles the mouse over the dialog: a click on a field
// types there, at the column clicked, and the wheel scrolls the names. A
// click outside does nothing, so a stray one can't lose what was typed.
func (m Model) mousePattern(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	p := &m.pattern
	rows, _ := m.patternRows()
	if delta := wheelDelta(msg, 1); delta != 0 {
		p.scrollBy(delta, rows)
		return m, nil
	}
	if msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	box := m.patternBox()
	r, inside := m.dialogRowAt(box, msg.X, msg.Y)
	if !inside {
		return m, nil
	}
	var input *components.TextInput
	switch r - dialogBodyRow {
	case 0:
		p.field, input = fieldFind, &p.find
	case 1:
		p.field, input = fieldReplace, &p.replace
	default:
		return m, nil
	}
	// The border, then the label; text that fits starts there unscrolled
	x0, _, _ := m.dialogFrame(box)
	col := msg.X - x0 - 1 - patternLabelW
	room := max(min(84, m.width)-2-patternLabelW-1, 1)
	if text := []rune(input.Value()); col >= 0 && utils.Width(string(text)) < room {
		at, used := 0, 0
		for at < len(text) && used+utils.Width(string(text[at])) <= col {
			used += utils.Width(string(text[at]))
			at++
		}
		input.SetCursor(at)
	}
	return m, nil
}
