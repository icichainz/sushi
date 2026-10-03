package main

import (
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// build compiles sushi into a temporary directory with the given linker
// flags, as the Makefile does
func build(t *testing.T, ldflags string) string {
	t.Helper()
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not on the PATH")
	}
	bin := filepath.Join(t.TempDir(), "sushi")
	cmd := exec.Command(gobin, "build", "-ldflags", ldflags, "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func TestVersionFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("builds sushi")
	}
	// Run with a home of its own, so nothing of the user's is read
	home := t.TempDir()
	sushi := func(bin string, args ...string) *exec.Cmd {
		cmd := exec.Command(bin, args...)
		cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+home, "XDG_DATA_HOME="+home)
		return cmd
	}

	// The version the build sets, or dev
	for ldflags, want := range map[string]string{"-X main.version=1.2.3": "sushi 1.2.3\n", "": "sushi dev\n"} {
		bin := build(t, ldflags)
		out, err := sushi(bin, "--version").Output()
		if err != nil || string(out) != want {
			t.Errorf("with %q: --version printed %q (%v), want %q", ldflags, out, err, want)
		}
		if ldflags != "" {
			continue
		}
		// --help lists it, the notifications, and that a file can be given
		help, _ := sushi(bin, "--help").CombinedOutput()
		for _, want := range []string{"-version", "notify_after", "[directory or file]"} {
			if !strings.Contains(string(help), want) {
				t.Errorf("--help doesn't mention %s:\n%s", want, help)
			}
		}
	}
}

// idle is a Bubble Tea model that does nothing until it is told to quit
type idle struct{}

func (idle) Init() tea.Cmd                       { return nil }
func (idle) Update(tea.Msg) (tea.Model, tea.Cmd) { return idle{}, nil }
func (idle) View() string                        { return "" }

func TestHangupQuitsTheProgram(t *testing.T) {
	if signal.Ignored(syscall.SIGHUP) {
		t.Skip("SIGHUP is ignored, as under nohup")
	}
	p := tea.NewProgram(idle{}, tea.WithInput(nil), tea.WithOutput(io.Discard))
	stop := quitOnHangup(p)
	defer stop()
	done := make(chan error, 1)
	go func() {
		_, err := p.Run()
		done <- err
	}()

	// Quit as for SIGTERM, with no error: the model is cleaned up after
	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("a hangup ended the program with %v", err)
		}
	case <-time.After(5 * time.Second):
		p.Kill()
		t.Fatal("a hangup didn't quit the program")
	}

	// Still caught once the program has quit, as sushi cleans up then
	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
}
