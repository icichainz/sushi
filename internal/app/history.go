package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Each pane remembers the folders it has shown, as a browser does: [ goes
// back to the one before and ] forward again. Every way of changing folder
// counts (h, l, Enter, a click, a bookmark, a find jump, a plugin's cd, z),
// as each is noted once its load is in; reloading the same folder doesn't.

// historyLimit is how many folders each pane remembers either way
const historyLimit = 100

// navHistory is where a pane has been
type navHistory struct {
	back    []string // Folders left, the latest last
	forward []string // Folders gone back from, the latest last
	landing string   // Where [ or ] is going: arriving there is no new step
}

// noteVisit records that tab has loaded a folder other than from, the one
// it showed before: in its history, as a new step unless [ or ] took it
// there, and among the frequent folders. It returns the command that saves
// those, if one is due.
func (m *Model) noteVisit(tab *Tab, from string) tea.Cmd {
	if from == tab.CurrentPath {
		return nil
	}
	h := &tab.nav
	if h.landing != tab.CurrentPath {
		h.back = pushDir(h.back, from)
		h.forward = nil
	}
	h.landing = ""
	return m.frecent.visit(tab.CurrentPath)
}

// pushDir adds dir to the end of list, unless it is there already, and
// keeps the latest historyLimit
func pushDir(list []string, dir string) []string {
	if dir == "" || len(list) > 0 && list[len(list)-1] == dir {
		return list
	}
	// Clipped, so copies of the model never share where it goes
	list = append(slices.Clip(list), dir)
	if len(list) > historyLimit {
		list = list[len(list)-historyLimit:]
	}
	return list
}

// historyStep goes back (-1) or forward (1) in the active pane's history,
// passing over folders that are gone
func (m Model) historyStep(dir int) (tea.Model, tea.Cmd) {
	tab := m.tab()
	h := &tab.nav
	from, to, word := &h.back, &h.forward, "Back"
	if dir > 0 {
		from, to, word = &h.forward, &h.back, "Forward"
	}
	// Where the pane is going, if a step is still loading, else where it is
	here := tab.CurrentPath
	if h.landing != "" && tab.Loading {
		here = h.landing
	}
	for len(*from) > 0 {
		n := len(*from)
		target := (*from)[n-1]
		*from = slices.Clip((*from)[:n-1])
		if target == here {
			continue
		}
		if info, err := os.Stat(target); err != nil || !info.IsDir() {
			continue
		}
		*to = pushDir(*to, here)
		h.landing = target
		status := m.setStatus(word + " to " + displayPath(target))
		return m, tea.Batch(m.loadDir(tab, target), status)
	}
	cmd := m.setStatus("Nothing to go " + strings.ToLower(word) + " to")
	return m, cmd
}

// retarget moves the folders of the history along with a rename; move
// says where a path went
func (h *navHistory) retarget(move func(string) string) {
	for _, list := range []*[]string{&h.back, &h.forward} {
		moved := make([]string, len(*list))
		for i, p := range *list {
			moved[i] = move(p)
		}
		*list = moved
	}
	h.landing = move(h.landing)
}

// displayPath is a path as sushi shows it: home as ~, and printable
func displayPath(path string) string {
	return strings.Join(pathSegments(path), string(filepath.Separator))
}
