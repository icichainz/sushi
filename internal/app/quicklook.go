package app

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"

	tea "github.com/charmbracelet/bubbletea"
)

// Quick Look shows files as the space bar does in Finder, in the window of
// qlmanage -p. Sushi starts it and carries on, with the window in front;
// closing the window gives the terminal the focus back. Sushi closes the
// window when asked to show something else, and when it quits.

// quickLookWindow is a Quick Look window sushi opened
type quickLookWindow struct {
	paths  []string
	proc   *os.Process
	exited chan struct{} // Closed once qlmanage has exited, as when its window is closed
}

// quickLook shows paths in Quick Look. Pressed again while the window
// shows them, it closes the window; on other files, it shows those instead.
func (m Model) quickLook(paths []string) (tea.Model, tea.Cmd) {
	if len(paths) == 0 {
		return m, nil
	}
	if w := m.quickLookWin; w.isOpen() {
		w.close()
		m.quickLookWin = nil
		if slices.Equal(w.paths, paths) {
			cmd := m.setStatus("Quick Look closed")
			return m, cmd
		}
	}
	w, err := openQuickLook(paths)
	if err != nil {
		cmd := m.setStatus(fmt.Sprintf("Quick Look failed: %v", err))
		return m, cmd
	}
	m.quickLookWin = w
	cmd := m.setStatus("Quick Look: " + describe(paths))
	return m, cmd
}

// openQuickLook starts qlmanage on paths, without waiting for it
func openQuickLook(paths []string) (*quickLookWindow, error) {
	// Its input and output are left as /dev/null: it reports each file it
	// shows and more, which would draw over the interface
	cmd := exec.Command("qlmanage", append([]string{"-p"}, paths...)...)
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, errors.New("qlmanage not found; Quick Look needs macOS")
		}
		return nil, err
	}
	w := &quickLookWindow{paths: paths, proc: cmd.Process, exited: make(chan struct{})}
	go func() {
		cmd.Wait()
		close(w.exited)
	}()
	return w, nil
}

// isOpen reports whether the window is still open
func (w *quickLookWindow) isOpen() bool {
	if w == nil {
		return false
	}
	select {
	case <-w.exited:
		return false
	default:
		return true
	}
}

// close closes the window, if it is still open
func (w *quickLookWindow) close() {
	if w.isOpen() {
		w.proc.Kill()
	}
}
