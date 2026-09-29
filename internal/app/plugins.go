package app

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/plugins"
)

// pluginDoneMsg is sent when a plugin finishes
type pluginDoneMsg struct {
	plugin  plugins.Plugin
	dir     string // Directory it ran in, for resolving relative paths
	cmdFile string
	output  []byte // Captured output, for background plugins
	err     error
}

// bindPluginKeys maps plugin shortcuts to plugins. Built-in keys can't be
// taken over, so conflicts are reported instead.
func (m *Model) bindPluginKeys() []string {
	used := m.keys.usedKeys()
	m.pluginKeys = make(map[string]int)

	var warnings []string
	for i, p := range m.plugins {
		switch {
		case p.Key == "":
		case used[p.Key]:
			warnings = append(warnings, fmt.Sprintf("plugin %s: key %q is used by sushi", p.Name, p.Key))
		default:
			if other, taken := m.pluginKeys[p.Key]; taken {
				warnings = append(warnings, fmt.Sprintf("plugin %s: key %q is already used by %s", p.Name, p.Key, m.plugins[other].Name))
				continue
			}
			m.pluginKeys[p.Key] = i
		}
	}
	return warnings
}

// usedKeys returns every key bound to a built-in action in normal mode
func (k KeyMap) usedKeys() map[string]bool {
	used := make(map[string]bool)
	v := reflect.ValueOf(k)
	for i := 0; i < v.NumField(); i++ {
		if b, ok := v.Field(i).Interface().(key.Binding); ok {
			for _, s := range b.Keys() {
				used[s] = true
			}
		}
	}
	// Digits jump to bookmarks
	for r := '1'; r <= '9'; r++ {
		used[string(r)] = true
	}
	return used
}

// runPlugin runs p on the selection, or the file under the cursor
func (m Model) runPlugin(p plugins.Plugin) (tea.Model, tea.Cmd) {
	cmdFile, err := os.CreateTemp("", "sushi-cmd-*")
	if err != nil {
		cmd := m.setStatus(fmt.Sprintf("Can't run %s: %v", p.Name, err))
		return m, cmd
	}
	cmdFile.Close()

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

		path := in.Arg
		if !filepath.IsAbs(path) {
			path = filepath.Join(msg.dir, path)
		}
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

// handlePluginMode handles key presses in the plugin menu
func (m Model) handlePluginMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Up):
		m.pluginCursor = max(m.pluginCursor-1, 0)
	case key.Matches(msg, m.keys.Down):
		m.pluginCursor = min(m.pluginCursor+1, max(len(m.plugins)-1, 0))
	case msg.Type == tea.KeyEnter:
		m.mode = ModeNormal
		if m.pluginCursor < len(m.plugins) {
			return m.runPlugin(m.plugins[m.pluginCursor])
		}
	case msg.Type == tea.KeyEsc, msg.String() == "q":
		m.mode = ModeNormal
	}
	return m, nil
}
