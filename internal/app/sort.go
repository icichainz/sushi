package app

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/utils"
)

// sortFields are the orders the sort menu offers, each with the key that
// picks it and the direction it lists files in
var sortFields = []struct{ key, by, desc string }{
	{"n", "name", "A to Z"},
	{"s", "size", "largest first"},
	{"m", "modified", "newest first"},
	{"t", "type", "by extension"},
}

// sortLetters lists the letters of the sort menu, as "n s m t"
func sortLetters() string {
	letters := make([]string, len(sortFields))
	for i, f := range sortFields {
		letters[i] = f.key
	}
	return strings.Join(letters, " ")
}

// resortDelay is how often tabs that were loading when the order changed
// are checked, to sort them once their load is in
const resortDelay = 100 * time.Millisecond

// resortMsg sorts tabs whose loads were in flight when the order changed:
// those loads list files in the old order
type resortMsg struct {
	tabs map[int]int // Tab ID → the load that was in flight
}

// openSortMenu opens the sort menu on the current order
func (m Model) openSortMenu() (tea.Model, tea.Cmd) {
	m.mode = ModeSort
	m.sortCursor = 0
	for i, f := range sortFields {
		if f.by == m.sortBy {
			m.sortCursor = i
		}
	}
	return m, nil
}

// handleSortMode handles keys in the sort menu: a field's letter, or
// moving to it and pressing Enter, sorts by it; the reverse key (S)
// reverses the order. The menu's own keys come first, as it shows them;
// q closes it unless an action there has taken q.
func (m Model) handleSortMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := msg.String()
	switch s {
	case "esc":
		m.mode = ModeNormal
		return m, nil
	case "enter":
		return m.chooseSort(sortFields[m.sortCursor].by)
	}
	for _, f := range sortFields {
		if s == f.key {
			return m.chooseSort(f.by)
		}
	}

	switch {
	case key.Matches(msg, m.keys.Up):
		m.sortCursor = max(m.sortCursor-1, 0)
	case key.Matches(msg, m.keys.Down):
		m.sortCursor = min(m.sortCursor+1, len(sortFields)-1)
	case key.Matches(msg, m.keys.Reverse):
		m.mode = ModeNormal
		return m.reverseSort()
	case s == "q":
		m.mode = ModeNormal
	}
	return m, nil
}

// chooseSort sorts by a field in its usual direction; choosing the current
// field keeps its direction
func (m Model) chooseSort(by string) (tea.Model, tea.Cmd) {
	m.mode = ModeNormal
	return m.setSort(by, m.sortReverse && by == m.sortBy)
}

// reverseSort reverses the current order
func (m Model) reverseSort() (tea.Model, tea.Cmd) {
	return m.setSort(m.sortBy, !m.sortReverse)
}

// setSort changes the order of every tab for the rest of the session. The
// config file is left alone. Lists are sorted where they are rather than
// reloaded, so the change is instant.
func (m Model) setSort(by string, reverse bool) (tea.Model, tea.Cmd) {
	m.sortBy, m.sortReverse = by, reverse

	loading := make(map[int]int)
	for i := range m.tabs {
		tab := &m.tabs[i]
		m.resort(tab)
		if tab.Loading {
			loading[tab.ID] = tab.loadSeq
		}
	}
	cmd := tea.Batch(m.resortLater(loading), m.setStatus("Sorted by "+m.sortLabel()))
	return m, cmd
}

// resort puts a tab's lists in the current order, keeping the cursor on
// the same file
func (m *Model) resort(tab *Tab) {
	current := ""
	if len(tab.Files) > 0 {
		current = tab.Files[tab.Cursor].Path
	}
	fs.SortFiles(tab.Files, m.sortBy, m.sortReverse)
	fs.SortFiles(tab.ParentFiles, m.sortBy, m.sortReverse)
	for i, f := range tab.Files {
		if f.Path == current {
			tab.Cursor = i
			break
		}
	}
	// Search results are positions in the list
	if m.mode == ModeSearch && tab.ID == m.tab().ID {
		m.updateSearchResults()
	}
}

// resortLater checks on loading tabs shortly, to sort them once loaded
func (m *Model) resortLater(tabs map[int]int) tea.Cmd {
	if len(tabs) == 0 {
		return nil
	}
	return tea.Tick(resortDelay, func(time.Time) tea.Msg { return resortMsg{tabs: tabs} })
}

// resortTabs sorts the tabs a resortMsg waits for whose loads are in
func (m *Model) resortTabs(msg resortMsg) tea.Cmd {
	waiting := make(map[int]int)
	for id, seq := range msg.tabs {
		tab := m.tabByID(id)
		// Closed, or a newer load has started, which uses the new order
		if tab == nil || tab.loadSeq != seq {
			continue
		}
		if tab.Loading {
			waiting[id] = seq
			continue
		}
		m.resort(tab)
	}
	return m.resortLater(waiting)
}

// sortBox builds the sort menu
func (m Model) sortBox() []string {
	t := m.theme
	g := currentGlyphs()
	width := min(46, m.width)
	inner := width - 2

	var body []string
	for i, f := range sortFields {
		// The current order is marked first, so narrow menus keep the mark
		mark := " "
		if f.by == m.sortBy {
			mark = g.mark
		}
		body = append(body, m.pickerRow(i == m.sortCursor, inner, mark, f.key, " "+utils.Fit(f.by, 9), f.desc))
	}
	now := "Now " + m.sortLabel() + ", S reverses"
	body = append(body, "", " "+m.fg(t.Muted).Render(utils.Truncate(now, inner-2)))
	return m.dialog("Sort by", t.Accent, body, width)
}
