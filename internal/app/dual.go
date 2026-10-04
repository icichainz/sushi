package app

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/utils"
)

// Dual-pane mode: w splits a tab into two file lists side by side, each
// browsing on its own, with its own folder, cursor, selection, search,
// preview and history. ctrl+h and ctrl+l, or a click, choose the active
// one; > copies and < moves its targets to the other's folder; W swaps
// the two sides, and = shows the active pane's folder in the other too.
//
// A tab's own fields are always its active pane, and split holds the
// other, so m.tab() and everything built on it (the keys, prompts, search,
// the find palette, bookmarks, previews) works on the active pane just as
// on a single one. Switching panes swaps the two: each keeps its ID, so
// loads, previews and git results reach it wherever it is. m.panes()
// lists every pane, for what applies to all: reloads, the watcher,
// sorting, renames.

// minWidthDualPreview is the terminal width from which the preview shows
// beside two lists; on narrower terminals the lists share the width
const minWidthDualPreview = 120

// split is the second pane of a tab in dual-pane mode
type split struct {
	other Tab  // The inactive pane
	right bool // Whether the active pane is the right-hand one
}

// paneTransfer is a copy or move to the other pane that the overwrite
// dialog is asking about
type paneTransfer struct {
	srcs   []string
	mode   string // "copy", or "cut" to move, as for the clipboard
	paneID int    // The pane it goes to, which must still show the folder
}

// panes returns every pane of every tab: the tabs, which are their active
// panes, and the inactive panes of the tabs split in two
func (m *Model) panes() []*Tab {
	out := make([]*Tab, 0, len(m.tabs)+1)
	for i := range m.tabs {
		out = append(out, &m.tabs[i])
	}
	for i := range m.tabs {
		if s := m.tabs[i].split; s != nil {
			out = append(out, &s.other)
		}
	}
	return out
}

// otherPane returns the inactive pane of the active tab, or nil when it
// has only one
func (m *Model) otherPane() *Tab {
	if s := m.tab().split; s != nil {
		return &s.other
	}
	return nil
}

// isInactivePane reports whether tab is the inactive pane of a tab
func (m *Model) isInactivePane(tab *Tab) bool {
	for i := range m.tabs {
		if s := m.tabs[i].split; s != nil && &s.other == tab {
			return true
		}
	}
	return false
}

// splitTab gives tab a second pane showing dir: at once with a copy of the
// tab's own list when it is the same folder, and otherwise once the load
// it returns is in. The tab stays on the side it was on when dual-pane
// mode was left, the left the first time.
func (m *Model) splitTab(tab *Tab, dir string) tea.Cmd {
	other := m.newTab(dir)
	other.PreviewEnabled, other.PreviewWidth = tab.PreviewEnabled, tab.PreviewWidth
	if dir != tab.CurrentPath {
		tab.split = &split{other: other, right: tab.wasRight}
		return m.loadDir(&tab.split.other, dir)
	}
	// Copies, as lists are sorted in place
	other.setFiles(slices.Clone(tab.Files))
	other.ParentFiles = slices.Clone(tab.ParentFiles)
	other.Cursor = tab.Cursor
	// Inside the same archive, so read-only, previewed and left as the
	// tab is; see archive.go
	other.archive = tab.archive
	tab.split = &split{other: other, right: tab.wasRight}
	return nil
}

// swapPanes makes the inactive pane the active one, where it is on screen
func (t *Tab) swapPanes() {
	s := t.split
	active := *t
	active.split = nil
	*t = s.other
	t.split = &split{other: active, right: !s.right}
}

// isPaneKey reports whether msg is handled by handlePaneKey
func (k KeyMap) isPaneKey(msg tea.KeyMsg) bool {
	return key.Matches(msg, k.DualPane, k.SwapPanes, k.LeftPane, k.RightPane, k.CopyToPane, k.MoveToPane,
		k.OtherPaneHere, k.HistoryBack, k.HistoryForward, k.Frequent)
}

// handlePaneKey handles the keys of dual-pane mode and of the folder
// history in normal mode
func (m Model) handlePaneKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := m.keys
	switch {
	case key.Matches(msg, k.DualPane):
		return m.toggleDual()
	case key.Matches(msg, k.SwapPanes):
		return m.swapSides()
	case key.Matches(msg, k.LeftPane):
		return m.focusPane(false)
	case key.Matches(msg, k.RightPane):
		return m.focusPane(true)
	case key.Matches(msg, k.CopyToPane):
		return m.transfer("copy")
	case key.Matches(msg, k.MoveToPane):
		return m.transfer("cut")
	case key.Matches(msg, k.OtherPaneHere):
		return m.otherPaneHere()
	case key.Matches(msg, k.HistoryBack):
		return m.historyStep(-1)
	case key.Matches(msg, k.HistoryForward):
		return m.historyStep(1)
	case key.Matches(msg, k.Frequent):
		return m.openJump()
	}
	return m, nil
}

// toggleDual splits the active tab in two, or leaves it with the active
// pane alone. The second pane opens where it was when last closed, if that
// folder is still there, and otherwise beside the first.
func (m Model) toggleDual() (tea.Model, tea.Cmd) {
	tab := m.tab()
	if s := tab.split; s != nil {
		if s.other.git.cancel != nil {
			s.other.git.cancel()
		}
		tab.otherDir, tab.wasRight, tab.split = s.other.CurrentPath, s.right, nil
		// The copies of entries of an archive only that pane was in go
		m.sweepArchives()
		cmd := m.setStatus("Dual pane off")
		return m, cmd
	}
	dir := tab.CurrentPath
	if tab.otherDir != "" {
		dir = existingDir(tab.otherDir)
	}
	load := m.splitTab(tab, dir)
	if load == nil {
		// Copied rather than loaded, so its Git status is read now
		load = m.gitAfterLoad(&tab.split.other)
	}
	status := m.setStatus("Dual pane on")
	return m, tea.Batch(load, status)
}

// notSplit says that a pane key needs two panes, and how to get them
func (m Model) notSplit(what string) string {
	if k := shownKey(m.keys.DualPane); k != "" {
		return what + " needs two panes: " + k + " shows them"
	}
	return what + " needs two panes"
}

// focusPane makes the left or the right pane the active one
func (m Model) focusPane(right bool) (tea.Model, tea.Cmd) {
	tab := m.tab()
	if tab.split == nil {
		cmd := m.setStatus(m.notSplit("Switching panes"))
		return m, cmd
	}
	if tab.split.right == right {
		return m, nil
	}
	tab.swapPanes()
	return m, m.refreshPreview(tab)
}

// swapSides puts the left pane on the right and the right on the left;
// the active pane stays the active one
func (m Model) swapSides() (tea.Model, tea.Cmd) {
	tab := m.tab()
	if tab.split == nil {
		cmd := m.setStatus(m.notSplit("Swapping panes"))
		return m, cmd
	}
	tab.split = &split{other: tab.split.other, right: !tab.split.right}
	cmd := m.setStatus("Panes swapped")
	return m, cmd
}

// otherPaneHere shows the active pane's folder in the other pane too, with
// its cursor on the same file
func (m Model) otherPaneHere() (tea.Model, tea.Cmd) {
	tab, other := m.tab(), m.otherPane()
	if other == nil {
		cmd := m.setStatus(m.notSplit("Opening the other pane here"))
		return m, cmd
	}
	if len(tab.Files) > 0 {
		other.focusPath = tab.Files[tab.Cursor].Path
	}
	status := m.setStatus("Both panes in " + displayPath(tab.CurrentPath))
	return m, tea.Batch(m.loadDir(other, tab.CurrentPath), status)
}

// transfer copies, or with mode "cut" moves, the targets to the other
// pane's folder, as a paste there would, through the same checks, the
// overwrite dialog, the progress and undo. The clipboard is left alone.
// From inside an archive, a copy copies the entries out, as c then v
// there would; nothing goes into an archive, or out of one by moving.
func (m Model) transfer(mode string) (tea.Model, tea.Cmd) {
	verb, doing := "copy", "Copying"
	if mode == "cut" {
		verb, doing = "move", "Moving"
	}
	tab, other := m.tab(), m.otherPane()
	why := ""
	switch {
	case other == nil:
		why = m.notSplit(doing + " to the other pane")
	case other.archive != nil:
		why = "Read-only: the other pane is inside an archive"
	case tab.archive != nil && mode == "cut":
		why = readOnly
	case other.leaving():
		// Its folder is the one it is leaving
		why = "Wait for the other pane to open its folder"
	}
	if why != "" {
		cmd := m.setStatus(why)
		return m, cmd
	}
	if m.job != nil {
		cmd := m.stillBusy()
		return m, cmd
	}
	srcs := m.targets()
	if len(srcs) == 0 {
		return m, nil
	}
	dir := other.CurrentPath
	if tab.archive != nil {
		updated, cmd := m.extractTo(tab.archive, srcs, dir, "copy", "in the other pane")
		if m = updated.(Model); m.job != nil {
			clear(m.tab().Selected)
		}
		return m, cmd
	}
	conflicts, err := m.checkPaste(srcs, dir)
	if err != nil {
		cmd := m.setStatus(fmt.Sprintf("Can't %s: %v", verb, err))
		return m, cmd
	}
	if len(conflicts) > 0 {
		m.pending, m.pasteDir = conflicts, dir
		m.sending = &paneTransfer{srcs: srcs, mode: mode, paneID: other.ID}
		m.confirmAction, m.mode = "paste", ModeConfirm
		return m, nil
	}
	clear(m.tab().Selected)
	cmd := m.executeTransfer(srcs, mode, dir)
	return m, cmd
}

// dualLayout divides the terminal between the two lists of a split tab
// and, from minWidthDualPreview columns, the active pane's preview. The
// parent pane isn't shown. The left list takes the odd column, wherever
// the active one is, so switching panes moves nothing.
func (m Model) dualLayout(tab Tab) layout {
	l := layout{bodyH: max(m.height-chromeRows, 1)}
	if tab.PreviewEnabled && m.width >= minWidthDualPreview {
		// The preview is to each list what preview_width makes it to the
		// one list of a single pane: at 50%, the three take a third each
		l.previewW = m.width * tab.PreviewWidth / (200 - tab.PreviewWidth)
	}
	rest := m.width - l.previewW
	left, right := rest-rest/2, rest/2
	l.listW, l.otherW = left, right
	if tab.split.right {
		l.listW, l.otherW, l.otherFirst = right, left, true
	}
	return l
}

// otherView returns m as it draws the inactive pane of the active tab, and
// finds what lies under the mouse there: that pane in place of the active
// one, in normal mode, as searches and prompts belong to the active pane,
// and with a cursor that doesn't look focused
func (m Model) otherView() Model {
	v := m
	v.tabs = slices.Clone(m.tabs)
	v.tabs[m.activeTabIdx] = m.tabs[m.activeTabIdx].split.other
	v.mode = ModeNormal
	v.inactive = true
	v.theme.CursorBg, v.theme.CursorFg = v.theme.Raised, v.theme.Text
	return v
}

// paneHeading returns the heading of a list in dual-pane mode, or false
// with a single pane. It names the pane's folder, as the breadcrumb names
// only the active one's, and says how many files are selected there; the
// active pane's stands out.
func (m Model) paneHeading(width int) (string, bool) {
	tab := m.tabs[m.activeTabIdx]
	if tab.split == nil && !m.inactive {
		return "", false
	}
	t := m.theme
	style := m.fg(t.Accent).Bold(true)
	if m.inactive {
		style = m.fg(t.Muted)
	}
	// A margin each side, and a space between the folder and the count
	note, room := "", width-2
	if n := len(tab.Selected); n > 0 {
		note = fmt.Sprintf("%d selected", n)
	}
	// The folder matters more than the count
	if room-utils.Width(note)-1 < minPathWidth {
		note = ""
	}
	if note != "" {
		room -= utils.Width(note) + 1
	}
	path := utils.TruncateLeft(displayPath(tab.CurrentPath), max(room, 0))
	gap := max(width-2-utils.Width(path)-utils.Width(note), 0)
	return utils.Fit(" "+style.Render(path)+strings.Repeat(" ", gap)+m.fg(t.Selected).Render(note)+" ", width), true
}

// mouseOther handles the mouse over the inactive list. The wheel moves its
// cursor, leaving it inactive; a click makes it the active pane, then does
// what it does there, so a double-click opens what it is on.
func (m Model) mouseOther(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if delta := wheelDelta(msg, wheelStep); delta != 0 {
		if other := m.otherPane(); len(other.Files) > 0 {
			other.Cursor = max(min(other.Cursor+delta, len(other.Files)-1), 0)
		}
		return m, nil
	}
	// While searching, keys only move between the matches, and so does the
	// mouse
	if m.mode == ModeSearch || msg.Button != tea.MouseButtonLeft && msg.Button != tea.MouseButtonRight {
		return m, nil
	}
	tab := m.tab()
	tab.swapPanes()
	preview := m.refreshPreview(tab)
	// Now the active list, at the same place
	updated, cmd := m.mouseBrowse(msg)
	return updated, tea.Batch(preview, cmd)
}

// dualHints are the hints of the browser in dual-pane mode, with the keys
// that work across the panes
func (m Model) dualHints() []hint {
	k := m.keys
	switchPane := keyHint("switch pane", k.LeftPane, k.RightPane)
	if len(m.tab().Selected) > 0 {
		return []hint{keyHint("toggle", k.Select), keyHint("clear", k.Unselect), keyHint("copy across", k.CopyToPane),
			keyHint("move across", k.MoveToPane), switchPane, keyHint("copy", k.Copy), keyHint("cut", k.Cut),
			keyHint("delete", k.Delete), keyHint("all keys", k.Help)}
	}
	return []hint{keyHint("open", k.Enter), keyHint("select", k.Select), keyHint("copy across", k.CopyToPane),
		keyHint("move across", k.MoveToPane), switchPane, keyHint("other pane here", k.OtherPaneHere),
		keyHint("swap", k.SwapPanes), keyHint("one pane", k.DualPane), keyHint("all keys", k.Help)}
}
