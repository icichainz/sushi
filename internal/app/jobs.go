package app

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/ui"
)

// progressInterval is the least time between progress updates from a
// background operation. Tests raise it, so a quick operation finishes with
// a single message.
var progressInterval = 100 * time.Millisecond

// job is a file operation running in the background, such as a copy or a
// delete, while the interface stays usable. Only one runs at a time, so
// operations can't trip over each other's files.
type job struct {
	id        int
	doing     string // What it is doing, as in "Copying"
	cancel    context.CancelFunc
	updates   chan tea.Msg  // Progress messages, then the jobDoneMsg
	started   chan struct{} // Closed once the work has begun
	finished  chan struct{} // Closed once the work has returned
	progress  fs.Progress
	cancelled bool // ctrl+x was pressed and it is stopping
	quit      bool // Quit once it has stopped
}

// selfApplying is a message that knows how to update the model, so the
// messages from file tools don't each need a case in Update
type selfApplying interface {
	apply(m Model) (tea.Model, tea.Cmd)
}

// jobProgressMsg says how far the running job has got
type jobProgressMsg struct {
	id       int
	progress fs.Progress
}

// jobDoneMsg is sent when a job finishes, however it finished
type jobDoneMsg struct {
	id    int
	op    fileOperationMsg // The outcome, reported like any file operation
	undo  *undoEntry       // How to reverse what was done, if anything was
	moved []fs.RenamePair  // Paths that moved, so references to them follow
	focus string           // Something new to put the cursor on
}

// startJob runs work in the background, passing its progress to the
// status bar. Callers check first that no other job is running.
func (m *Model) startJob(doing string, work func(t *fs.Task) jobDoneMsg) tea.Cmd {
	m.jobSeq++
	ctx, cancel := context.WithCancel(context.Background())
	j := &job{id: m.jobSeq, doing: doing, cancel: cancel, updates: make(chan tea.Msg, 1),
		started: make(chan struct{}), finished: make(chan struct{})}
	m.job = j

	return func() tea.Msg {
		go func() {
			defer cancel()
			close(j.started)
			task := fs.NewTask(ctx, progressInterval, func(p fs.Progress) {
				sendLatest(j.updates, jobProgressMsg{id: j.id, progress: p})
			})
			done := work(task)
			done.id = j.id
			close(j.finished)
			j.updates <- done
		}()
		return <-j.updates
	}
}

// Shutdown stops what sushi may still have running once the program has
// quit, however it quit: it cancels the background operation and waits up
// to wait for it to stop, so that what it was in the middle of (a partial
// copy, an unfinished zip) is cleaned up rather than left behind by the
// process exiting. An operation that never started is not waited for. The
// instruction files of plugins still running are removed. Call it after
// the program has finished; the error says if the operation didn't stop in
// time.
func (m Model) Shutdown(wait time.Duration) error {
	pendingCmdFiles.removeAll()
	j := m.job
	if j == nil {
		return nil
	}
	j.cancel()
	deadline := time.After(wait)
	select {
	case <-j.started:
	case <-time.After(min(wait, 250*time.Millisecond)):
		// Its command never ran, and a cancelled one does nothing if it does
		return nil
	}
	select {
	case <-j.finished:
		return nil
	case <-deadline:
		return fmt.Errorf("%s had not stopped after %v, and may have left an unfinished file behind", strings.ToLower(j.doing), wait)
	}
}

// stillBusy says that something has to wait for the running job. It is
// short, so it fits beside the job's progress in an 80-column status bar.
func (m *Model) stillBusy() tea.Cmd {
	wait := "wait for it to finish"
	if cancel := keysLabel(" ", m.keys.Cancel); cancel != "" {
		wait = "wait, or " + cancel + " to cancel"
	}
	return m.setStatus(fmt.Sprintf("Still %s: %s", strings.ToLower(m.job.doing), wait))
}

// sendLatest sends a progress update without waiting for the interface,
// replacing one it hasn't read yet: only the latest matters
func sendLatest(updates chan tea.Msg, msg tea.Msg) {
	select {
	case updates <- msg:
		return
	default:
	}
	select {
	case <-updates:
	default:
	}
	select {
	case updates <- msg:
	default:
	}
}

// waitForJob waits for the running job's next message
func waitForJob(updates chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return <-updates
	}
}

// changeJob changes a copy of the running job, so copies of the model
// made earlier keep theirs, as with every other field
func (m *Model) changeJob(change func(j *job)) {
	j := *m.job
	change(&j)
	m.job = &j
}

func (msg jobProgressMsg) apply(m Model) (tea.Model, tea.Cmd) {
	if m.job == nil || m.job.id != msg.id {
		return m, nil
	}
	m.changeJob(func(j *job) { j.progress = msg.progress })
	return m, waitForJob(m.job.updates)
}

func (msg jobDoneMsg) apply(m Model) (tea.Model, tea.Cmd) {
	quit := false
	if m.job != nil && m.job.id == msg.id {
		quit = m.job.quit
		m.job = nil
	}
	if quit {
		return m, tea.Quit
	}

	m.pushUndo(msg.undo)
	if msg.op.operation == "cut" {
		// What a paste moved leaves the clipboard, rather than follow
		// references to it to where it went
		m.clipboard = slices.DeleteFunc(slices.Clone(m.clipboard), func(p string) bool {
			return slices.ContainsFunc(msg.moved, func(r fs.RenamePair) bool { return r.From == p })
		})
	}
	m.retargetAll(msg.moved)
	// Whatever was moved or deleted has left the clipboard
	m.pruneClipboard()
	if msg.focus != "" && filepath.Dir(msg.focus) == m.tab().CurrentPath {
		m.tab().focusPath = msg.focus
	}
	return m.Update(msg.op)
}

// whileBusy handles keys while a job runs: ctrl+x cancels it, every way of
// quitting (q, Q, closing the last tab) stops it first, and keys that
// change files, open the Run palette, or open files in other programs are
// refused until it is done; runPlugin refuses plugins, however they are
// started. Everything else, like moving around, works as usual.
func (m Model) whileBusy(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	k := m.keys
	doing := strings.ToLower(m.job.doing)
	switch {
	case key.Matches(msg, k.Cancel):
		m.job.cancel()
		m.changeJob(func(j *job) { j.cancelled = true })
		return m, nil, true

	case key.Matches(msg, k.Quit, k.QuitNoCd), key.Matches(msg, k.CloseTab) && len(m.tabs) == 1:
		if key.Matches(msg, k.QuitNoCd) {
			m.keepShellDir = true
		}
		// Asked twice: don't wait any longer
		if m.job.quit {
			return m, tea.Quit, true
		}
		m.job.cancel()
		m.changeJob(func(j *job) { j.cancelled, j.quit = true, true })
		cmd := m.setStatus(fmt.Sprintf("Stopping %s before quitting; press %s again to quit now", doing, keyName(msg.String())))
		return m, cmd, true

	case key.Matches(msg, k.Delete, k.HardDelete, k.Paste, k.PasteLink, k.Rename, k.BulkRename,
		k.NewFile, k.NewDir, k.Duplicate, k.Chmod, k.Archive, k.Extract, k.Undo,
		k.Plugins, k.Shell, k.Edit, k.Open, k.QuickLook):
		cmd := m.stillBusy()
		return m, cmd, true
	}
	return m, nil, false
}

// shortStatus describes the running job in fewer cells than status, as in
// "Copying 3/120 45%", for a status bar short of room
func (j *job) shortStatus() string {
	g := currentGlyphs()
	p := j.progress
	switch {
	case j.cancelled:
		return j.doing + ", cancelling" + g.more
	case p.Counting:
		return j.doing + ": counting" + g.more
	case p.TotalFiles > 0:
		return fmt.Sprintf("%s %d/%d %d%%", j.doing, p.Files, p.TotalFiles, p.Percent())
	case p.Files == 0 && p.TotalBytes == 0:
		return j.doing + g.more
	}
	return fmt.Sprintf("%s %d%%", j.doing, p.Percent())
}

// status describes the running job for the status bar, as in
// "Copying 3/120 files 45% ████░░░░░░"
func (j *job) status() string {
	g := currentGlyphs()
	p := j.progress
	text := j.doing
	switch {
	case j.cancelled:
		return text + ", cancelling" + g.more
	case p.Counting:
		return fmt.Sprintf("%s: counting, %s so far", text, plural(p.TotalFiles, "file"))
	case p.TotalFiles > 0:
		text += fmt.Sprintf(" %d/%d files", p.Files, p.TotalFiles)
	case p.Files > 0:
		text += " " + plural(p.Files, "file")
	case p.TotalBytes == 0:
		return text + g.more
	}
	pct := p.Percent()
	return fmt.Sprintf("%s %d%% %s", text, pct, progressBar(pct, 10))
}

// progressBar draws pct percent as a bar of width cells
func progressBar(pct, width int) string {
	full, empty := "█", "░"
	if ui.GetIconMode() == ui.IconModeASCII {
		full, empty = "#", "-"
	}
	n := min(max(pct*width/100, 0), width)
	return strings.Repeat(full, n) + strings.Repeat(empty, width-n)
}

// runBatch runs op on each path like batchResult, but stops once the task
// is cancelled and then says how far it got. It also returns how many
// paths op succeeded on.
func runBatch(t *fs.Task, operation, verb string, paths []string, op func(string) error) (fileOperationMsg, int) {
	done := 0
	msg := batchResult(operation, verb, paths, func(path string) error {
		if err := t.Err(); err != nil {
			return err
		}
		err := op(path)
		if err == nil {
			done++
		}
		return err
	})
	if t.Err() != nil {
		msg = fileOperationMsg{operation: operation, message: "Cancelled: " + howFar(strings.ToLower(verb), t.Progress(), done, len(paths))}
	}
	return msg, done
}

// howFar says how much of an operation got done, as in "copied 3 of 120
// files", by files where they were counted and by items otherwise
func howFar(past string, p fs.Progress, done, total int) string {
	switch {
	case p.TotalFiles > 0:
		return fmt.Sprintf("%s %d of %s", past, p.Files, plural(p.TotalFiles, "file"))
	case p.Files > 0:
		return fmt.Sprintf("%s %s", past, plural(p.Files, "file"))
	}
	return fmt.Sprintf("%s %d of %s", past, done, plural(total, "item"))
}

// plural returns a count and a noun, as in "1 file" or "3 files"
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
