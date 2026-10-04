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
	dir := tab.CurrentPath
	if tab.archive != nil {
		// A folder inside an archive is no folder z could go to: the one
		// holding the archive counts instead, once, as the pane arrives
		// from elsewhere (going in from it counted it already)
		dir = tab.realDir()
		if from == dir || tab.archive.holds(from) {
			return nil
		}
	}
	return m.frecent.visit(dir)
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
// passing over folders that are gone. With nowhere to go, the history is
// left as it was: a folder passed over may come back, as a drive does.
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
	list := *from
	for i := len(list) - 1; i >= 0; i-- {
		target := list[i]
		if target == here || !canReturnTo(target) {
			continue
		}
		// What was passed over on the way goes
		*from = slices.Clip(list[:i])
		*to = pushDir(*to, here)
		h.landing = target
		status := m.setStatus(word + " to " + displayPath(target))
		return m, tea.Batch(m.loadDir(tab, target), status)
	}
	cmd := m.setStatus("Nothing to go " + strings.ToLower(word) + " to")
	return m, cmd
}

// canReturnTo reports whether the history can go back to path: a folder,
// or an archive or a folder inside one (see archive.go)
func canReturnTo(path string) bool {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return true
	}
	return archiveAt(path) != ""
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
