package app

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/icichainz/sushi/internal/config"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/search"
	"github.com/icichainz/sushi/internal/tags"
	"github.com/icichainz/sushi/internal/ui"
	"github.com/icichainz/sushi/internal/ui/components"
	"github.com/icichainz/sushi/internal/utils"
)

// Finder tags: dots of their colours after names in the list, L to change
// a file's tags, and # (or a query starting with # or tag: in the find
// palette) to find files by tag. tags: false in the config turns them off.

// tagColors are Finder's colours for tags, whatever the theme, so a red
// tag is the red it is in Finder
var tagColors = map[tags.Color]lipgloss.Color{
	tags.Gray:   "#8E8E93",
	tags.Green:  "#34C759",
	tags.Purple: "#AF52DE",
	tags.Blue:   "#007AFF",
	tags.Yellow: "#FFCC00",
	tags.Red:    "#FF3B30",
	tags.Orange: "#FF9500",
}

// minNameW is the least of a name the dots after it may leave
const minNameW = 4

// tagsOn reports whether Finder tags are shown and can be changed
func (m Model) tagsOn() bool {
	return m.config != nil && m.config.Tags
}

// tagKeys leaves the tag keys unbound when the config turns tags off
func tagKeys(k *KeyMap, cfg *config.Config) {
	if !cfg.Tags {
		k.Tag.SetKeys()
		k.FindTag.SetKeys()
	}
}

// hollowMark stands for tags without a colour
func hollowMark() string {
	if ui.GetIconMode() == ui.IconModeASCII {
		return "o"
	}
	return "○"
}

// tagDots draws a dot of each colour among list, in the order the tags
// have them, and a hollow one for any without a colour, after a space.
// They fit in width, leaving minNameW for the name before them; whatever
// doesn't fit is left out. It returns the dots and their width.
func (m Model) tagDots(list []tags.Tag, width int, cursor bool) (string, int) {
	if len(list) == 0 {
		return "", 0
	}
	var colors []tags.Color
	plain := false
	for _, t := range list {
		switch {
		case t.Color == tags.None:
			plain = true
		case !slices.Contains(colors, t.Color):
			colors = append(colors, t.Color)
		}
	}
	n := len(colors)
	if plain {
		n++
	}
	n = min(n, width-minNameW-1)
	if n <= 0 {
		return "", 0
	}

	base := lipgloss.NewStyle()
	if cursor {
		base = base.Background(m.theme.CursorBg)
	}
	var b strings.Builder
	b.WriteString(base.Render(" "))
	for i := range n {
		if i < len(colors) {
			b.WriteString(base.Foreground(tagColors[colors[i]]).Render(currentGlyphs().mark))
		} else {
			b.WriteString(base.Foreground(m.theme.Muted).Render(hollowMark()))
		}
	}
	return b.String(), n + 1
}

// tagPicker is the dialog L opens to tag files as Finder does: Finder's
// seven colours and the tags used nearby, each ticked if the files have
// it, and a field to type a new one. Changes are written as they are made,
// as in Finder, and undone together.
type tagPicker struct {
	paths  []string     // The files being tagged
	before [][]tags.Tag // Their tags when it opened, for undo
	now    [][]tags.Tag // Their tags since
	offer  []tags.Tag   // The rows: Finder's colours, the tags used here, then those added
	cursor int          // Row; len(offer) is the field
	input  components.TextInput
}

// has counts the files that have the tag called name
func (p tagPicker) has(name string) int {
	n := 0
	for _, list := range p.now {
		if tags.Has(list, name) {
			n++
		}
	}
	return n
}

// onField reports whether the cursor is on the field for a new tag
func (p tagPicker) onField() bool {
	return p.cursor >= len(p.offer)
}

// openTags opens the tag picker on the selection, or the file under the
// cursor
func (m Model) openTags() (tea.Model, tea.Cmd) {
	paths := m.targets()
	if len(paths) == 0 {
		return m, nil
	}
	if !tags.Supported() {
		cmd := m.setStatus("Can't tag files: Finder tags need macOS")
		return m, cmd
	}
	p := tagPicker{paths: paths, input: components.NewTextInput("")}
	for _, path := range paths {
		// From the files, as tags change without the watcher seeing
		list, err := tags.Read(path)
		if err != nil {
			cmd := m.setStatus(fmt.Sprintf("Error: %v", err))
			return m, cmd
		}
		p.before = append(p.before, list)
	}
	p.now = slices.Clone(p.before)

	// Finder's colours, then the other tags used here, by name
	p.offer = tags.Known()
	var used []tags.Tag
	for _, f := range m.tab().Files {
		used = append(used, f.Tags...)
	}
	for _, list := range p.before {
		used = append(used, list...)
	}
	var others []tags.Tag
	for _, t := range used {
		if !tags.Has(p.offer, t.Name) && !tags.Has(others, t.Name) {
			others = append(others, t)
		}
	}
	slices.SortFunc(others, func(a, b tags.Tag) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	p.offer = append(p.offer, others...)

	m.tagger = p
	m.mode = ModeTags
	return m, nil
}

// handleTagMode handles keys in the tag picker. Space and Enter tick a
// row; typing goes to the field, where Enter adds the tag typed.
func (m Model) handleTagMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := &m.tagger
	switch msg.Type {
	case tea.KeyEsc:
		return m.closeTags()
	case tea.KeyUp, tea.KeyCtrlP:
		p.cursor = max(p.cursor-1, 0)
		return m, nil
	case tea.KeyDown, tea.KeyCtrlN:
		p.cursor = min(p.cursor+1, len(p.offer))
		return m, nil
	case tea.KeyTab, tea.KeyShiftTab:
		// Between the list and the field
		if p.onField() {
			p.cursor = 0
		} else {
			p.cursor = len(p.offer)
		}
		return m, nil
	case tea.KeyEnter:
		if !p.onField() {
			cmd := m.toggleTag(p.offer[p.cursor])
			return m, cmd
		}
		name := strings.TrimSpace(p.input.Value())
		if name == "" {
			return m.closeTags()
		}
		return m.addTag(name)
	case tea.KeySpace:
		if !p.onField() {
			cmd := m.toggleTag(p.offer[p.cursor])
			return m, cmd
		}
	}
	// On the list, the keys bound to moving up and down move, as in the
	// Open with list; any other typing goes to the field
	if !p.onField() {
		switch {
		case key.Matches(msg, m.keys.Up):
			p.cursor = max(p.cursor-1, 0)
			return m, nil
		case key.Matches(msg, m.keys.Down):
			p.cursor = min(p.cursor+1, len(p.offer))
			return m, nil
		case msg.Type != tea.KeyRunes:
			return m, nil
		}
	}
	if p.input.Update(msg) {
		p.cursor = len(p.offer)
	}
	return m, nil
}

// addTag ticks the tag called name, a new one unless it is in the list
func (m Model) addTag(name string) (tea.Model, tea.Cmd) {
	p := &m.tagger
	p.input = components.NewTextInput("")
	i := tags.Index(p.offer, name)
	if i < 0 {
		p.offer = append(slices.Clip(p.offer), tags.Tag{Name: name})
		i = len(p.offer) - 1
	}
	p.cursor = len(p.offer) // Still on the field, for another
	if p.has(p.offer[i].Name) == len(p.paths) {
		return m, nil
	}
	cmd := m.toggleTag(p.offer[i])
	return m, cmd
}

// toggleTag takes t off the files if they all have it, and otherwise
// gives it to those that don't. Each file's tags are read back, so the
// picker shows what they are even if a change fails.
func (m *Model) toggleTag(t tags.Tag) tea.Cmd {
	p := &m.tagger
	add := p.has(t.Name) < len(p.paths)
	var failed error
	changed := make(map[string][]tags.Tag)
	for i, path := range p.paths {
		list := p.now[i]
		if tags.Has(list, t.Name) == add {
			continue
		}
		next := tags.Without(list, t.Name)
		if add {
			next = tags.With(list, t)
		}
		if err := tags.Write(path, next); err != nil && failed == nil {
			failed = err
		}
		if got, err := tags.Read(path); err == nil {
			p.now[i] = got
		}
		changed[path] = p.now[i]
	}
	m.showTags(changed)
	if failed != nil {
		return m.setStatus(fmt.Sprintf("Error: %v", failed))
	}
	return nil
}

// showTags puts new tags in the lists showing the files, without a
// reload: the watcher doesn't see attributes change
func (m *Model) showTags(changed map[string][]tags.Tag) {
	if len(changed) == 0 {
		return
	}
	for _, tab := range m.panes() {
		tab.Files = withTags(tab.Files, changed)
		tab.ParentFiles = withTags(tab.ParentFiles, changed)
	}
}

// withTags returns files with the tags changed, copied if any changed, as
// copies of the model may share the list
func withTags(files []fs.FileInfo, changed map[string][]tags.Tag) []fs.FileInfo {
	copied := false
	for i := range files {
		list, ok := changed[files[i].Path]
		if !ok {
			continue
		}
		if !copied {
			files, copied = slices.Clone(files), true
		}
		files[i].Tags = list
	}
	return files
}

// closeTags closes the picker. What it changed can be undone at once.
func (m Model) closeTags() (tea.Model, tea.Cmd) {
	p := m.tagger
	m.mode = ModeNormal
	m.tagger = tagPicker{}

	var steps []undoStep
	var changed []string
	for i, path := range p.paths {
		if !tags.Equal(p.before[i], p.now[i]) {
			steps = append(steps, undoStep{kind: stepTags, path: path, oldTags: p.before[i], newTags: p.now[i]})
			changed = append(changed, path)
		}
	}
	if len(steps) == 0 {
		return m, nil
	}
	m.pushUndo(&undoEntry{label: "tag " + describe(changed), steps: steps})
	cmd := m.setStatus("Changed the tags of " + describe(changed))
	return m, cmd
}

// undoTags puts back the tags a step changed, unless they have changed
// again since
func undoTags(s undoStep) error {
	now, err := tags.Read(s.path)
	if err != nil {
		return err
	}
	if !tags.Equal(now, s.newTags) {
		return fmt.Errorf("the tags of %s have changed since, so they were left as they are", filepath.Base(s.path))
	}
	return tags.Write(s.path, s.oldTags)
}

// tagRows returns how many tags the picker lists at once, and whether
// there is also room for the rule above the field
func (m Model) tagRows() (int, bool) {
	// Above the status bar, less the dialog's border, title, padding and
	// the field
	room := m.height - 2 - 5 - 1
	n := len(m.tagger.offer)
	if room-1 >= 1 {
		return max(min(room-1, n, 16), 1), true
	}
	return max(min(room, n), 1), false
}

// tagBox builds the tag picker
func (m Model) tagBox() []string {
	t := m.theme
	g := currentGlyphs()
	p := m.tagger
	width := min(52, m.width)
	inner := width - 2
	rows, roomy := m.tagRows()

	var body []string
	start := window(min(p.cursor, len(p.offer)-1), len(p.offer), rows)
	for i := start; i < min(start+rows, len(p.offer)); i++ {
		body = append(body, m.tagRow(p.offer[i], i == p.cursor, inner))
	}
	if roomy {
		body = append(body, m.fg(t.Border).Render(strings.Repeat(g.hline, inner)))
	}

	prompt := " " + m.fg(t.Title).Bold(true).Render("+") + " "
	room := max(inner-utils.Width(prompt)-1, 1)
	field := m.fg(t.Faint).Render(utils.Truncate("type a new tag", room))
	switch {
	case p.onField():
		field = p.input.View(room, m.fg(t.Text), lipgloss.NewStyle().Reverse(true))
	case p.input.Value() != "":
		field = m.fg(t.Muted).Render(utils.Truncate(utils.Printable(p.input.Value()), room))
	}
	body = append(body, prompt+field)
	return m.dialog("Tags of "+utils.Printable(describe(p.paths)), t.Accent, body, width)
}

// tagRow draws a row of the picker: whether the files have the tag (all,
// some or none), its colour and its name
func (m Model) tagRow(tag tags.Tag, chosen bool, width int) string {
	t := m.theme
	p := m.tagger
	box := "[ ]"
	switch n := p.has(tag.Name); {
	case n == len(p.paths):
		box = "[x]"
	case n > 0:
		box = "[-]"
	}

	base := lipgloss.NewStyle()
	text := m.fg(t.Text)
	if chosen {
		base = base.Background(t.CursorBg)
		text = base.Foreground(t.CursorFg).Bold(true)
	}
	dot, color := currentGlyphs().mark, tagColors[tag.Color]
	if tag.Color == tags.None {
		dot, color = hollowMark(), t.Muted
	}
	name := utils.Truncate(utils.Printable(tag.Name), max(width-8, 1))
	row := text.Render(" "+box+" ") + base.Foreground(color).Render(dot) + text.Render(" "+name)
	return utils.Cells(row+text.Render(strings.Repeat(" ", max(width-utils.Width(row), 0))), 0, width)
}

// tagHints are the keys shown while the picker is open
func (m Model) tagHints() []hint {
	g := currentGlyphs()
	move := hint{g.up + "/" + g.down, "move"}
	if m.tagger.onField() {
		enter := "add the tag"
		if strings.TrimSpace(m.tagger.input.Value()) == "" {
			enter = "close"
		}
		return []hint{{"enter", enter}, move, {"tab", "to the list"}, {"esc", "close"}}
	}
	// On the list, the keys bound to moving move too
	if keys := keysLabel("/", m.keys.Down, m.keys.Up); keys != "" {
		move.key = keys
	}
	return []hint{{"space", "tick"}, move, {"tab", "type a new tag"}, {"esc", "close"}}
}

// mouseTags handles the mouse over the tag picker: the wheel moves, a
// click ticks a tag or goes to the field, and clicking outside closes it
func (m Model) mouseTags(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	p := &m.tagger
	if delta := wheelDelta(msg, 1); delta != 0 {
		p.cursor = max(min(p.cursor+delta, len(p.offer)), 0)
		return m, nil
	}
	if msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	r, inside := m.dialogRowAt(m.tagBox(), msg.X, msg.Y)
	if !inside {
		return m.closeTags()
	}
	// The body is the list, a rule if there is room, then the field
	rows, roomy := m.tagRows()
	start := window(min(p.cursor, len(p.offer)-1), len(p.offer), rows)
	shown := min(rows, len(p.offer)-start)
	field := shown
	if roomy {
		field++
	}
	switch row := r - dialogBodyRow; {
	case row >= 0 && row < shown:
		p.cursor = start + row
		cmd := m.toggleTag(p.offer[p.cursor])
		return m, cmd
	case row == field:
		p.cursor = len(p.offer)
	}
	return m, nil
}

// tagQuery returns the tag a query in the search palette looks for: a
// search by name starting with # or tag: is a search by tag, when tags
// are on
func (m Model) tagQuery() (string, bool) {
	if m.find.content || !m.tagsOn() {
		return "", false
	}
	return search.TagQuery(m.find.input.Value())
}

// openFindTag opens the search palette on the tagged files below this
// folder, ready to narrow down by typing a tag's name
func (m Model) openFindTag() (tea.Model, tea.Cmd) {
	updated, _ := m.openFind(false)
	m = updated.(Model)
	m.find.input = components.NewTextInput("#")
	cmd := m.startFind()
	return m, cmd
}
