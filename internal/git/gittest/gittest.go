// Package gittest sets up Git repositories in temporary directories for
// tests, with git cut off from the user's own configuration
package gittest

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Isolate skips the test if git isn't installed, and keeps the user's
// global and system Git configuration (ignore files, hooks, signing) from
// applying to the git commands the test runs, and the code it tests
func Isolate(t testing.TB) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, who := range []string{"AUTHOR", "COMMITTER"} {
		t.Setenv("GIT_"+who+"_NAME", "sushi")
		t.Setenv("GIT_"+who+"_EMAIL", "sushi@example.com")
	}
}

// Run runs git in dir and returns what it printed, failing the test if it
// fails
func Run(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// Init makes dir a repository with main checked out, whatever the
// version of git would call its first branch
func Init(t testing.TB, dir string) {
	t.Helper()
	Run(t, dir, "init", "-q")
	Run(t, dir, "symbolic-ref", "HEAD", "refs/heads/main")
}
