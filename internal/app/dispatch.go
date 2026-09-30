package app

import tea "github.com/charmbracelet/bubbletea"

// Update handles all state updates. Messages from work running in the
// background (recursive search, re-sorting) are handled here and
// everything else by update.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case resortMsg:
		cmd = m.resortTabs(msg)
	case findDelayMsg:
		cmd = m.handleFindDelay(msg)
	case findResultsMsg:
		cmd = m.handleFindResults(msg)
	default:
		if _, ok := msg.(tea.KeyMsg); ok {
			// A search result's line is shown only if its preview arrives
			// before anything else is done
			m.jump = previewJump{}
		}
		var updated tea.Model
		updated, cmd = m.update(msg)
		m = updated.(Model)
		if msg, ok := msg.(previewLoadedMsg); ok {
			m.showJump(msg)
		}
	}
	return m, cmd
}
