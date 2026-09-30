package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
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
		cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+home, "XDG_DATA_HOME="+home, "AppData="+home)
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
		// --help lists it
		help, _ := sushi(bin, "--help").CombinedOutput()
		if !strings.Contains(string(help), "-version") {
			t.Errorf("--help doesn't mention --version:\n%s", help)
		}
	}
}
