package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/notify"
)

// setHostDirectory tells the terminal which directory is shown, so the
// Sushi app can title its window after it. Tests replace it.
var setHostDirectory = notify.SetDirectory

// Update handles all state updates. Messages from work running in the
// background (file watching, recursive search) are handled here and
// everything else by update. Afterwards the watcher is pointed at
// the directories the tabs show, wherever the message took them.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case dirsChangedMsg:
		cmd = m.handleDirsChanged(msg)
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
			cmd = tea.Batch(cmd, m.showJump(msg))
		}
	}
	m.watchTabs()
	// Tell the terminal when the shown directory changes, by any route:
	// navigation, a tab switch, a tab closing, a find jump
	if dir := m.tab().CurrentPath; dir != m.hostDir {
		m.hostDir = dir
		_ = setHostDirectory(dir)
	}
	return m, cmd
}
