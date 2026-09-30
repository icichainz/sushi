package app

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/plugins"
	"github.com/icichainz/sushi/internal/ui/components"
)

// pluginDoneMsg is sent when a plugin finishes
type pluginDoneMsg struct {
	plugin  plugins.Plugin
	dir     string // Directory it ran in, for resolving relative paths
	cmdFile string
	output  []byte // Captured output, for background plugins
	err     error
}

// cmdFiles tracks the SUSHI_CMD_FILE of each plugin that hasn't finished,
// so that one still running in the background when sushi quits doesn't
// leave its file in the temporary directory. It is shared by every copy
// of the model, as the files are.
type cmdFiles struct {
	mu    sync.Mutex
	files map[string]bool
}

var pendingCmdFiles = &cmdFiles{files: map[string]bool{}}

func (c *cmdFiles) add(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.files[path] = true
}

func (c *cmdFiles) done(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.files, path)
}

// removeAll removes the files of plugins that haven't finished
func (c *cmdFiles) removeAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for path := range c.files {
		os.Remove(path)
		delete(c.files, path)
	}
}

// bindPluginKeys maps plugin shortcuts to plugins. Built-in keys can't be
// taken over, so conflicts are reported instead, and the plugin is left
// without a key, so the Run palette doesn't show one that does something
// else.
func (m *Model) bindPluginKeys() []string {
	// The keys as remapped in the config, so a plugin can have a key an
	// action has given up
	used := m.keys.keyOwners()
	m.pluginKeys = make(map[string]int)

	var warnings []string
	for i, p := range m.plugins {
		switch {
		case p.Key == "":
			continue
		case used[p.Key] != "":
			warnings = append(warnings, fmt.Sprintf("plugin %s: key %q is used by sushi for %s", p.Name, p.Key, used[p.Key]))
		default:
			other, taken := m.pluginKeys[p.Key]
			if !taken {
				m.pluginKeys[p.Key] = i
				continue
			}
			warnings = append(warnings, fmt.Sprintf("plugin %s: key %q is already used by %s", p.Name, p.Key, m.plugins[other].Name))
		}
		m.plugins[i].Key = ""
	}
	return warnings
}

// runPlugin runs p on the selection, or the file under the cursor. Not
// while a job runs: the plugin could change the files it is working on.
func (m Model) runPlugin(p plugins.Plugin) (tea.Model, tea.Cmd) {
	if m.job != nil {
		cmd := m.stillBusy()
		return m, cmd
	}
	cmdFile, err := os.CreateTemp("", "sushi-cmd-*")
	if err != nil {
		cmd := m.setStatus(fmt.Sprintf("Can't run %s: %v", p.Name, err))
		return m, cmd
	}
	cmdFile.Close()
	pendingCmdFiles.add(cmdFile.Name())

	tab := m.tab()
	ctx := plugins.Context{Dir: tab.CurrentPath, Selection: m.targets(), CmdFile: cmdFile.Name()}
	if len(tab.Files) > 0 {
		ctx.File = tab.Files[tab.Cursor].Path
	}
	proc := p.Cmd(ctx)
	done := func(err error, output []byte) tea.Msg {
		return pluginDoneMsg{plugin: p, dir: ctx.Dir, cmdFile: ctx.CmdFile, output: output, err: err}
	}

	switch p.Mode {
	case plugins.ModeTerminal:
		cmd := tea.ExecProcess(proc, func(err error) tea.Msg { return done(err, nil) })
		return m, cmd
	case plugins.ModeBackground:
		status := m.setStatus(fmt.Sprintf("Running %s…", p.Name))
		run := func() tea.Msg {
			// Captured, so it can't draw over the interface
			out, err := proc.CombinedOutput()
			return done(err, out)
		}
		return m, tea.Batch(status, run)
	default:
		cmd := tea.Exec(&waitCommand{Cmd: proc}, func(err error) tea.Msg { return done(err, nil) })
		return m, cmd
	}
}

// handlePluginDone reports how a plugin went and carries out any
// instructions it sent back
func (m Model) handlePluginDone(msg pluginDoneMsg) (tea.Model, tea.Cmd) {
	instructions, warnings, readErr := plugins.ReadInstructions(msg.cmdFile)
	os.Remove(msg.cmdFile)
	pendingCmdFiles.done(msg.cmdFile)

	var status string
	switch {
	case msg.err != nil:
		status = fmt.Sprintf("%s failed: %v", msg.plugin.Name, withOutput(msg.err, msg.output))
	case msg.plugin.Mode == plugins.ModeBackground:
		status = lastLine(msg.output)
		if status == "" {
			status = msg.plugin.Name + " done"
		}
	}
	if readErr != nil {
		warnings = append(warnings, readErr.Error())
	}

	// The plugin may have changed files. A cd below supersedes this reload
	// for the active tab.
	cmds := []tea.Cmd{m.reloadAll()}
	tab := m.tab()
	for _, in := range instructions {
		if in.Action == "status" {
			status = in.Arg
			continue
		}

		// Cleaned, since "link/" would name what a symlink points to: deleting
		// it would empty the target rather than remove the link
		path := in.Arg
		if !filepath.IsAbs(path) {
			path = filepath.Join(msg.dir, path)
		}
		path = filepath.Clean(path)
		switch in.Action {
		case "cd":
			info, err := os.Stat(path)
			if err != nil {
				warnings = append(warnings, err.Error())
				continue
			}
			// A file means: go to its directory and put the cursor on it
			if !info.IsDir() {
				tab.focusPath = path
				path = filepath.Dir(path)
			}
			cmds = append(cmds, m.loadDir(tab, path))
		case "select":
			// Only what exists, so a delete never starts on a mistyped path
			if _, err := os.Lstat(path); err != nil {
				warnings = append(warnings, "can't select "+in.Arg+": "+errText(err))
				continue
			}
			tab.Selected[path] = true
		}
	}

	if len(warnings) > 0 {
		status = strings.TrimPrefix(status+"; "+msg.plugin.Name+": "+strings.Join(warnings, "; "), "; ")
	}
	if status != "" {
		cmds = append(cmds, m.setStatus(status))
	}
	cmd := tea.Batch(cmds...)
	return m, cmd
}

// errText returns what went wrong, without the operation and path that an
// *os.PathError repeats
func errText(err error) string {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

// lastLine returns the last non-empty line of output
func lastLine(output []byte) string {
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// waitCommand runs a command in the terminal, then waits for Enter so its
// output can be read before sushi redraws the screen
type waitCommand struct {
	*exec.Cmd
}

func (w *waitCommand) SetStdin(r io.Reader) {
	if w.Stdin == nil {
		w.Stdin = r
	}
}

func (w *waitCommand) SetStdout(out io.Writer) {
	if w.Stdout == nil {
		w.Stdout = out
	}
}

func (w *waitCommand) SetStderr(out io.Writer) {
	if w.Stderr == nil {
		w.Stderr = out
	}
}

func (w *waitCommand) Run() error {
	err := w.Cmd.Run()
	result := "done"
	if err != nil {
		result = err.Error()
	}
	fmt.Fprintf(w.Stdout, "\n[%s] Press Enter to return to sushi", result)
	bufio.NewReader(w.Stdin).ReadString('\n')
	return err
}

// openRun opens the Run palette, ready to type a shell command or to pick
// a plugin
func (m Model) openRun(typing bool) (tea.Model, tea.Cmd) {
	m.mode = ModePlugins
	m.pluginCursor = 0
	m.runInput = components.NewTextInput("")
	m.runTyping = typing
	return m, nil
}

// handlePluginMode handles key presses in the Run palette. Tab switches
// between typing a shell command and the plugin list.
func (m Model) handlePluginMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.mode = ModeNormal
		return m, nil
	case tea.KeyTab, tea.KeyShiftTab:
		m.runTyping = !m.runTyping
		return m, nil
	case tea.KeyEnter:
		command := strings.TrimSpace(m.runInput.Value())
		switch {
		case m.runTyping && command == "":
			return m, nil
		case m.runTyping:
			// A shell command is an unnamed plugin: it gets the same
			// arguments and environment, and waits so its output can be read
			m.mode = ModeNormal
			return m.runPlugin(plugins.Plugin{Name: "shell", Command: command, Mode: plugins.ModeWait})
		case m.pluginCursor < len(m.plugins):
			m.mode = ModeNormal
			return m.runPlugin(m.plugins[m.pluginCursor])
		}
		return m, nil
	}

	if m.runTyping {
		m.runInput.Update(msg)
		return m, nil
	}
	switch {
	case key.Matches(msg, m.keys.Up):
		m.pluginCursor = max(m.pluginCursor-1, 0)
	case key.Matches(msg, m.keys.Down):
		m.pluginCursor = min(m.pluginCursor+1, max(len(m.plugins)-1, 0))
	case key.Matches(msg, m.keys.Shell):
		m.runTyping = true
	case msg.String() == "q":
		m.mode = ModeNormal
	}
	return m, nil
}
