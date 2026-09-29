// Package plugins runs external commands and scripts on sushi's current file
// or selection.
//
// A plugin is either a shell command from the config file or an executable
// script in the plugins directory. It receives the selection as arguments,
// plus SUSHI_DIR, SUSHI_FILE and SUSHI_SELECTION in its environment, and can
// send instructions back by writing lines to the file named by
// SUSHI_CMD_FILE (see ReadInstructions).
package plugins

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Run modes
const (
	// ModeWait suspends sushi, runs the plugin in the terminal, then waits
	// for Enter so its output can be read. It is the default.
	ModeWait = "wait"
	// ModeTerminal suspends sushi and hands the terminal to the plugin,
	// returning as soon as it exits; for interactive programs such as fzf
	ModeTerminal = "terminal"
	// ModeBackground runs the plugin without leaving sushi and shows the
	// last line of its output in the status bar
	ModeBackground = "background"
)

// Plugin is an external command sushi can run
type Plugin struct {
	Name        string `yaml:"name"`
	Key         string `yaml:"key"`         // Optional shortcut, e.g. "ctrl+g" or "Z"
	Command     string `yaml:"command"`     // Shell command, for plugins from the config
	Mode        string `yaml:"mode"`        // ModeWait, ModeTerminal or ModeBackground
	Description string `yaml:"description"` // Shown in the plugin menu
	Script      string `yaml:"-"`           // Executable path, for script plugins
}

// Context is what a plugin is told about sushi's state
type Context struct {
	Dir       string   // Current directory; also the plugin's working directory
	File      string   // File under the cursor
	Selection []string // Selected paths, or the file under the cursor
	CmdFile   string   // Where the plugin may write instructions back
}

// Load combines plugins from the config with scripts found in dir and fills
// in defaults. Config plugins win over scripts with the same name. Problems
// come back as warnings, so one bad plugin doesn't stop sushi from starting.
func Load(configured []Plugin, dir string) ([]Plugin, []string) {
	var loaded []Plugin
	var warnings []string
	seen := make(map[string]bool)

	add := func(p Plugin) {
		if seen[p.Name] {
			warnings = append(warnings, fmt.Sprintf("plugin %q is defined twice; keeping the first", p.Name))
			return
		}
		switch p.Mode {
		case ModeWait, ModeTerminal, ModeBackground:
		case "":
			p.Mode = ModeWait
		default:
			warnings = append(warnings, fmt.Sprintf("plugin %s: unknown mode %q, using %q", p.Name, p.Mode, ModeWait))
			p.Mode = ModeWait
		}
		seen[p.Name] = true
		loaded = append(loaded, p)
	}

	for i, p := range configured {
		if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Command) == "" {
			warnings = append(warnings, fmt.Sprintf("plugin #%d in the config needs a name and a command", i+1))
			continue
		}
		p.Script = ""
		add(p)
	}

	scripts, scriptWarnings := Discover(dir)
	warnings = append(warnings, scriptWarnings...)
	for _, p := range scripts {
		add(p)
	}
	return loaded, warnings
}

// Discover lists the executable files in dir as plugins, named after the
// file without its extension. A script can describe itself in comments in
// its first lines:
//
//	# sushi-key: ctrl+g
//	# sushi-mode: background
//	# sushi-description: What it does
func Discover(dir string) ([]Plugin, []string) {
	// Plugins run in the current directory, so script paths must be absolute
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, []string{fmt.Sprintf("plugins directory: %v", err)}
	}

	var found []Plugin
	var warnings []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(dir, name)
		info, err := os.Stat(path) // Follow symlinks to shared scripts
		if err != nil || info.IsDir() {
			continue
		}
		if !isExecutable(info) {
			if hasShebang(path) {
				warnings = append(warnings, fmt.Sprintf("plugin script %s is not executable (chmod +x it)", name))
			}
			continue
		}

		p := Plugin{Name: strings.TrimSuffix(name, filepath.Ext(name)), Script: path}
		readHeader(path, &p)
		found = append(found, p)
	}
	return found, warnings
}

// isExecutable reports whether a file can be run directly
func isExecutable(info os.FileInfo) bool {
	if runtime.GOOS == "windows" {
		switch strings.ToLower(filepath.Ext(info.Name())) {
		case ".exe", ".bat", ".cmd", ".com":
			return true
		}
		return false
	}
	return info.Mode()&0111 != 0
}

// hasShebang reports whether a file starts with "#!", i.e. is a script
func hasShebang(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 2)
	n, _ := io.ReadFull(f, buf)
	return n == 2 && string(buf) == "#!"
}

// readHeader fills in a script's key, mode and description from
// "sushi-key:", "sushi-mode:" and "sushi-description:" in its first lines
func readHeader(path string, p *Plugin) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	// Only look at the start, and never read far into a binary
	scanner := bufio.NewScanner(io.LimitReader(f, 4096))
	for i := 0; i < 20 && scanner.Scan(); i++ {
		line := scanner.Text()
		for field, target := range map[string]*string{
			"sushi-key:":         &p.Key,
			"sushi-mode:":        &p.Mode,
			"sushi-description:": &p.Description,
		} {
			if _, value, ok := strings.Cut(line, field); ok {
				*target = strings.TrimSpace(value)
			}
		}
	}
}

// Cmd builds the process that runs p. Shell commands run with sh -c,
// where "$@" is the selection; scripts get the selection as arguments. On
// Windows, commands run with cmd /C and should use the environment variables.
func (p Plugin) Cmd(ctx Context) *exec.Cmd {
	var cmd *exec.Cmd
	switch {
	case p.Script != "":
		cmd = exec.Command(p.Script, ctx.Selection...)
	case runtime.GOOS == "windows":
		cmd = exec.Command("cmd", "/C", p.Command)
	default:
		// "sushi" becomes $0, so the selection starts at $1
		cmd = exec.Command("sh", append([]string{"-c", p.Command, "sushi"}, ctx.Selection...)...)
	}

	cmd.Dir = ctx.Dir
	cmd.Env = append(os.Environ(),
		"SUSHI_DIR="+ctx.Dir,
		"SUSHI_FILE="+ctx.File,
		"SUSHI_SELECTION="+strings.Join(ctx.Selection, "\n"),
		"SUSHI_CMD_FILE="+ctx.CmdFile,
	)
	return cmd
}

// Instruction is a request a plugin sent back to sushi
type Instruction struct {
	Action string // "cd", "select" or "status"
	Arg    string
}

// ReadInstructions parses the file a plugin wrote to SUSHI_CMD_FILE. Each
// line is one of:
//
//	cd PATH        go to PATH, or to the directory holding it and focus it
//	select PATH    add PATH to the selection
//	status TEXT    show TEXT in the status bar
//
// Unrecognised lines are returned as warnings. A missing file means the
// plugin sent nothing.
func ReadInstructions(path string) ([]Instruction, []string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	var instructions []Instruction
	var warnings []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		action, arg, _ := strings.Cut(line, " ")
		switch action {
		case "cd", "select", "status":
			if arg == "" {
				warnings = append(warnings, fmt.Sprintf("%q needs an argument", action))
				continue
			}
			instructions = append(instructions, Instruction{Action: action, Arg: arg})
		default:
			warnings = append(warnings, fmt.Sprintf("unknown instruction %q", line))
		}
	}
	return instructions, warnings, nil
}
