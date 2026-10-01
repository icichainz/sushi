package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/opener"
	"github.com/icichainz/sushi/internal/utils"
)

// openWithState is the Open with list: the apps that can open the file
// under the cursor, or the selection's first, to open the targets with
type openWithState struct {
	paths   []string // What opens
	target  string   // The one the apps were looked up for
	apps    []opener.App
	loading bool // Looking the apps up; apps is empty until they come
	cursor  int
	seq     int // The latest lookup, so one for an older list is dropped

	// The apps found for each extension, for the rest of the session
	cache map[string][]opener.App
}

// appsFoundMsg brings the apps that can open a file
type appsFoundMsg struct {
	seq  int
	key  string // What to cache them under; empty to not cache them
	apps []opener.App
	err  error
}

// startOpenWith opens the Open with list for the targets, looking up the
// apps unless those for the file's extension are known
func (m Model) startOpenWith() (tea.Model, tea.Cmd) {
	paths := m.targets()
	if len(paths) == 0 {
		return m, nil
	}
	if !onMac {
		cmd := m.setStatus("Open with needs macOS")
		return m, cmd
	}
	w := &m.openWith
	w.seq++
	w.paths, w.target, w.cursor = paths, lookupTarget(paths), 0
	m.mode = ModeOpenWith
	cacheKey := appsKey(w.target)
	if apps, ok := w.cache[cacheKey]; ok && cacheKey != "" {
		w.apps, w.loading = apps, false
		return m, nil
	}
	w.apps, w.loading = nil, true
	seq, target := w.seq, w.target
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), macTimeout)
		defer cancel()
		apps, err := opener.Apps(ctx, runOsascript, target)
		return appsFoundMsg{seq: seq, key: cacheKey, apps: apps, err: err}
	}
}

// lookupTarget is the item the Open with list finds apps for: the first
// file among paths, or the first item if all are folders
func lookupTarget(paths []string) string {
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return paths[0]
}

// appsKey is what the apps for path are cached under: its extension, which
// Launch Services mostly goes by. Folders and files without one aren't
// cached, as what opens them depends on more than their name.
func appsKey(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if info, err := os.Stat(path); ext == "" || err != nil || info.IsDir() {
		return ""
	}
	return ext
}

func (msg appsFoundMsg) apply(m Model) (tea.Model, tea.Cmd) {
	w := &m.openWith
	if msg.err == nil && msg.key != "" {
		if w.cache == nil {
			w.cache = make(map[string][]opener.App)
		}
		w.cache[msg.key] = msg.apps
	}
	if m.mode != ModeOpenWith || msg.seq != w.seq {
		return m, nil
	}
	if msg.err != nil {
		m.mode = ModeNormal
		cmd := m.setStatus(fmt.Sprintf("Can't find the apps for %s: %v", filepath.Base(w.target), msg.err))
		return m, cmd
	}
	w.apps, w.loading, w.cursor = msg.apps, false, 0
	return m, nil
}

// handleOpenWithMode handles keys in the Open with list: Enter opens with
// the app chosen, Esc closes it, and the up, down, first and last keys
// move, as in the file list
func (m Model) handleOpenWithMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	w := &m.openWith
	switch msg.String() {
	case "esc":
		m.mode = ModeNormal
		return m, nil
	case "enter":
		switch {
		case w.loading:
			return m, nil
		case len(w.apps) == 0:
			m.mode = ModeNormal
			return m, nil
		}
		return m.openWithApp(w.apps[w.cursor])
	}
	last := max(len(w.apps)-1, 0)
	switch {
	case key.Matches(msg, m.keys.Up):
		w.cursor = max(w.cursor-1, 0)
	case key.Matches(msg, m.keys.Down):
		w.cursor = min(w.cursor+1, last)
	case key.Matches(msg, m.keys.Home):
		w.cursor = 0
	case key.Matches(msg, m.keys.End):
		w.cursor = last
	}
	return m, nil
}

// openWithApp opens the targets with app, as dropping them on it would
func (m Model) openWithApp(app opener.App) (tea.Model, tea.Cmd) {
	m.mode = ModeNormal
	paths := m.openWith.paths
	status := m.setStatus(fmt.Sprintf("Opening %s with %s", describe(paths), app.Name))
	run := func() tea.Msg {
		label := "Open with " + app.Name
		// Output is captured so it can't draw over the interface
		if out, err := runOpen(opener.OpenWithArgs(app.Path, paths...)...); err != nil {
			return externalDoneMsg{label: label, err: withOutput(err, out)}
		}
		return externalDoneMsg{label: label}
	}
	return m, tea.Batch(status, run)
}

// reveal shows the targets selected in Finder
func (m Model) reveal() (tea.Model, tea.Cmd) {
	paths := m.targets()
	if len(paths) == 0 {
		return m, nil
	}
	if !onMac {
		cmd := m.setStatus("Showing files in Finder needs macOS")
		return m, cmd
	}
	status := m.setStatus("Showing " + describe(paths) + " in Finder")
	run := func() tea.Msg {
		const label = "Show in Finder"
		if len(paths) == 1 {
			if out, err := runOpen(opener.RevealArgs(paths[0])...); err != nil {
				return externalDoneMsg{label: label, err: withOutput(err, out)}
			}
			return externalDoneMsg{label: label}
		}
		// open -R shows only one; Finder can select them all
		ctx, cancel := context.WithTimeout(context.Background(), macTimeout)
		defer cancel()
		return externalDoneMsg{label: label, err: opener.Reveal(ctx, runOsascript, paths)}
	}
	return m, tea.Batch(status, run)
}

// openWithRows returns how many apps the Open with list shows at once, and
// whether its footer fits too
func (m Model) openWithRows() (int, bool) {
	// Above the status bar, less the dialog's border, title and padding
	room := m.height - 2 - 5
	if room-2 >= 3 {
		return min(room-2, 15), true
	}
	return max(room, 1), false
}

// openWithBox builds the Open with list: each app with the folder it is
// in, as the same app can be in more than one, and the default marked
func (m Model) openWithBox() []string {
	t := m.theme
	g := currentGlyphs()
	w := m.openWith
	width := min(72, m.width)
	inner := width - 2
	rows, roomy := m.openWithRows()

	var body []string
	switch {
	case w.loading:
		body = append(body, " "+m.fg(t.Muted).Render(utils.Truncate("Looking up apps"+g.more, inner-2)))
	case len(w.apps) == 0:
		name := utils.Printable(filepath.Base(w.target))
		body = append(body, " "+m.fg(t.Text).Render(utils.Truncate("No app can open "+name, inner-2)))
	}
	nameW := min(24, max(inner/2-4, 4))
	start := window(w.cursor, len(w.apps), rows)
	for i := start; i < min(start+rows, len(w.apps)); i++ {
		app := w.apps[i]
		mark := " "
		if app.Default {
			mark = g.mark
		}
		where := utils.TruncateLeft(utils.Printable(filepath.Dir(app.Path)), max(inner-nameW-5, 0))
		body = append(body, m.pickerRow(i == w.cursor, inner, mark, utils.Fit(utils.Printable(app.Name), nameW), where))
	}
	if roomy {
		about := "Opens " + utils.Printable(describe(w.paths))
		if len(w.apps) > 0 && w.apps[0].Default {
			about += "; " + g.mark + " is the default app"
		}
		body = append(body, "", " "+m.fg(t.Muted).Render(utils.Truncate(about, inner-2)))
	}
	return m.dialog("Open with", t.Accent, body, width)
}

// openWithHints are the keys of the Open with list
func (m Model) openWithHints() []hint {
	return []hint{{"enter", "open"}, {keysLabel("/", m.keys.Down, m.keys.Up), "move"}, {"esc", "close"}}
}

// mouseOpenWith handles the mouse over the Open with list: the wheel
// moves, a click picks an app and a double-click opens with it, and
// clicking outside closes the list, as Esc does
func (m Model) mouseOpenWith(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	w := &m.openWith
	if delta := wheelDelta(msg, 1); delta != 0 {
		w.cursor = max(min(w.cursor+delta, len(w.apps)-1), 0)
		return m, nil
	}
	if msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	r, inside := m.dialogRowAt(m.openWithBox(), msg.X, msg.Y)
	if !inside {
		m.mode = ModeNormal
		return m, nil
	}
	rows, _ := m.openWithRows()
	row := r - dialogBodyRow
	i := window(w.cursor, len(w.apps), rows) + row
	if w.loading || row < 0 || row >= rows || i >= len(w.apps) {
		return m, nil
	}
	// The list may have scrolled to follow the first click, so what counts
	// is the same row and the app that click picked
	if m.isDouble(areaDialog, msg.Y, w.apps[w.cursor].Path) {
		m.lastClick = click{}
		return m.enter()
	}
	w.cursor = i
	m.remember(areaDialog, msg.Y, w.apps[i].Path)
	return m, nil
}
