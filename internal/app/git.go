package app

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/git"
	"github.com/icichainz/sushi/internal/ui"
	"github.com/icichainz/sushi/internal/utils"
)

// In a Git repository the file list has a column of badges, one for each
// entry's status, and the breadcrumb names the branch. git status runs in
// the background after every load of a directory, so after the watcher's
// reloads and ctrl+r too, but never twice at once for a tab.

// gitTimeout is how long git may take to describe a directory. One too
// large to read in time is given up on until the tab moves elsewhere,
// rather than keeping git busy after every change. Tests lower it.
var gitTimeout = 2 * time.Second

// gitBadgeW is the width of the badge column: the badge and a space
const gitBadgeW = 2

// gitState is what a tab knows from Git about the directory it shows
type gitState struct {
	dir     string             // The directory this is about
	seq     int                // Identifies the latest run; older results are dropped
	running bool               // A run is in flight
	again   bool               // Loaded again during the run: run once more after it
	off     bool               // Not a repository, or git failed or was too slow: not again until the tab moves, or ctrl+r
	cancel  context.CancelFunc // Stops the run in flight
	status  *git.Status        // What the latest run found; nil outside a repository
}

// gitStatusMsg brings what git said about a tab's directory
type gitStatusMsg struct {
	tabID, seq int
	dir        string
	status     *git.Status
	err        error
}

// gitStartMsg reads the status of the directories the tabs start in,
// which are loaded before the program runs
type gitStartMsg struct{}

// startGit returns the command for Init that sends gitStartMsg, or nil
// with the badges turned off
func (m Model) startGit() tea.Cmd {
	if m.config == nil || !m.config.Git {
		return nil
	}
	return func() tea.Msg { return gitStartMsg{} }
}

func (gitStartMsg) apply(m Model) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	for i := range m.tabs {
		cmds = append(cmds, m.gitAfterLoad(&m.tabs[i]))
	}
	return m, tea.Batch(cmds...)
}

// gitAfterLoad reads the status of the tab's directory once it has loaded.
// A tab that has moved forgets the directory it was in, but keeps the
// branch while it stays in the same work tree, so the badge column doesn't
// come and go with every move.
func (m *Model) gitAfterLoad(tab *Tab) tea.Cmd {
	if m.config == nil || !m.config.Git {
		return nil
	}
	g := &tab.git
	if g.dir != tab.CurrentPath {
		if g.cancel != nil {
			g.cancel()
		}
		var kept *git.Status
		if g.status != nil && within(tab.CurrentPath, g.status.Root) {
			kept = g.status.WithoutBadges()
		}
		*g = gitState{dir: tab.CurrentPath, seq: g.seq, status: kept}
	}
	return m.runGit(tab)
}

// within reports whether path is root or below it
func within(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// runGit starts reading the status of the tab's directory or, with a read
// in flight, asks for another once it is in
func (m *Model) runGit(tab *Tab) tea.Cmd {
	g := &tab.git
	if g.off {
		return nil
	}
	if g.running {
		g.again = true
		return nil
	}
	g.seq++
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	g.running, g.again, g.cancel = true, false, cancel
	msg := gitStatusMsg{tabID: tab.ID, seq: g.seq, dir: g.dir}
	return func() tea.Msg {
		defer cancel()
		msg.status, msg.err = git.Read(ctx, msg.dir)
		return msg
	}
}

func (msg gitStatusMsg) apply(m Model) (tea.Model, tea.Cmd) {
	tab := m.tabByID(msg.tabID)
	// Drop results for closed tabs, and those a move or newer run replaced
	if tab == nil || tab.git.dir != msg.dir || tab.git.seq != msg.seq {
		return m, nil
	}
	g := &tab.git
	g.running, g.cancel = false, nil
	g.status = msg.status
	if msg.err != nil {
		// Not a repository, no git, or too slow: quietly, and once
		g.off = true
		return m, nil
	}
	if g.again {
		return m, m.runGit(tab)
	}
	return m, nil
}

// retryGit lets git run again in every tab, as ctrl+r asks: after git
// init, say, or in a repository that took too long to read
func (m *Model) retryGit() {
	for i := range m.tabs {
		m.tabs[i].git.off = false
	}
}

// stopGit stops the git commands running
func (m Model) stopGit() {
	for _, tab := range m.tabs {
		if tab.git.cancel != nil {
			tab.git.cancel()
		}
	}
}

// gitStatus returns what Git says about the active tab's directory, or nil
// outside a repository
func (m Model) gitStatus() *git.Status {
	tab := &m.tabs[m.activeTabIdx]
	if m.config == nil || !m.config.Git || tab.git.dir != tab.CurrentPath {
		return nil
	}
	return tab.git.status
}

// withGitColumn makes room in c for the badge column, in a repository
func (m Model) withGitColumn(c columns) columns {
	if m.gitStatus() != nil {
		c.gitW = gitBadgeW
		c.nameW = max(c.nameW-gitBadgeW, 4)
	}
	return c
}

// gitBadge returns the badge of a file in the active tab, or 0
func (m Model) gitBadge(file fs.FileInfo) byte {
	return m.gitStatus().Badge(file.Name)
}

// renderBadge renders a file's badge in its column, which is only there in
// a repository
func (m Model) renderBadge(badge byte, c columns, isCursor bool) string {
	if c.gitW == 0 {
		return ""
	}
	t := m.theme
	style := m.fg(t.GitModified)
	switch badge {
	case git.Added, git.Renamed:
		style = m.fg(t.GitAdded)
	case git.Untracked:
		style = m.fg(t.GitUntracked)
	case git.Deleted, git.Conflict:
		style = m.fg(t.Danger)
	case git.Ignored:
		style = m.fg(t.Faint)
	}
	if isCursor {
		style = lipgloss.NewStyle().Foreground(t.CursorFg).Background(t.CursorBg)
	}
	text := ""
	if badge != 0 {
		text = string(rune(badge))
	}
	return style.Render(utils.Fit(text, c.gitW))
}

// branchLabel returns the branch for the breadcrumb, as "⎇ main", with a
// star once something is changed, cut to at most width cells, and its
// width. It is empty outside a repository, and when too little of the
// branch would fit.
func (m Model) branchLabel(width int) (string, int) {
	s := m.gitStatus()
	if s == nil || s.Head() == "" {
		return "", 0
	}
	t := m.theme
	icon := "⎇ "
	if ui.GetIconMode() == ui.IconModeASCII {
		icon = "git:"
	}
	dirty := ""
	if s.Dirty {
		dirty = "*"
	}
	head := utils.Printable(s.Head())
	room := width - utils.Width(icon) - len(dirty)
	if room < min(utils.Width(head), 6) {
		return "", 0
	}
	head = utils.Truncate(head, room)
	label := m.fg(t.Muted).Render(icon) + m.fg(t.Text).Render(head) + m.fg(t.GitModified).Render(dirty)
	return label, utils.Width(icon + head + dirty)
}
