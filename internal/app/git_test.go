package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/icichainz/sushi/internal/config"
	"github.com/icichainz/sushi/internal/git/gittest"
	"github.com/icichainz/sushi/internal/testutil"
	"github.com/icichainz/sushi/internal/ui"
	"github.com/icichainz/sushi/internal/utils"
)

// gitRepo makes a repository on main with a change of every kind: one
// file modified, one added, one renamed, one untracked, ignored files and
// an ignored folder, and folders with changes inside. Some files are
// left alone.
func gitRepo(t *testing.T) string {
	t.Helper()
	gittest.Isolate(t)
	repo := filepath.Join(t.TempDir(), "repo")
	for _, name := range []string{"tracked.txt", "clean.txt", "old.txt", "sub/inner.txt", "sub/same.txt", "tidy/c.txt"} {
		os.MkdirAll(filepath.Dir(filepath.Join(repo, name)), 0755)
		writeTestFile(t, filepath.Join(repo, name), name)
	}
	writeTestFile(t, filepath.Join(repo, ".gitignore"), "*.log\nbuild/\n")
	gittest.Init(t, repo)
	gittest.Run(t, repo, "add", ".")
	gittest.Run(t, repo, "commit", "-qm", "first")

	writeTestFile(t, filepath.Join(repo, "tracked.txt"), "changed")
	writeTestFile(t, filepath.Join(repo, "added.txt"), "new")
	gittest.Run(t, repo, "add", "added.txt")
	gittest.Run(t, repo, "mv", "old.txt", "renamed.txt")
	writeTestFile(t, filepath.Join(repo, "untracked.txt"), "")
	writeTestFile(t, filepath.Join(repo, "debug.log"), "")
	os.MkdirAll(filepath.Join(repo, "build"), 0755)
	writeTestFile(t, filepath.Join(repo, "build", "out.bin"), "")
	writeTestFile(t, filepath.Join(repo, "sub", "inner.txt"), "changed")
	os.MkdirAll(filepath.Join(repo, "newdir"), 0755)
	writeTestFile(t, filepath.Join(repo, "newdir", "x.txt"), "")
	return repo
}

// gitModel opens dir and reads its Git status, as sushi does at startup
func gitModel(t *testing.T, dir string, cfg *config.Config) Model {
	t.Helper()
	m := newTestModel(t, dir, cfg)
	return drain(t, m, m.startGit())
}

// listLine returns the row of the file list that shows name, without colors
func listLine(t *testing.T, m Model, name string) string {
	t.Helper()
	for _, line := range m.renderFileList(80, len(m.tab().Files)+2, false) {
		if plain := ansi.Strip(line); strings.Contains(plain, "  "+utils.Printable(name)+" ") {
			return plain
		}
	}
	t.Fatalf("%s is not in the list", name)
	return ""
}

// badgeOf returns what the badge column shows for name: a badge, or " "
func badgeOf(t *testing.T, m Model, name string) string {
	t.Helper()
	if m.gitStatus() == nil {
		t.Fatalf("no badge column for %s", name)
	}
	return string([]rune(listLine(t, m, name))[3])
}

// nameColumn returns the cell where the list shows name
func nameColumn(t *testing.T, m Model, name string) int {
	t.Helper()
	line := listLine(t, m, name)
	return utils.Width(line[:strings.Index(line, "  "+name)+2])
}

// enter goes into the directory name
func enter(t *testing.T, m Model, name string) Model {
	t.Helper()
	m, cmd := press(t, cursorTo(t, m, name), "enter")
	return drain(t, m, cmd)
}

// header returns the breadcrumb, on a terminal wide enough for all of it
func header(m Model) string {
	return ansi.Strip(resize(m, tea.WindowSizeMsg{Width: 240, Height: 24}).renderHeader())
}

func TestGitBadges(t *testing.T) {
	repo := gitRepo(t)
	m := gitModel(t, repo, nil)

	for name, want := range map[string]string{
		"tracked.txt": "M", "added.txt": "A", "renamed.txt": "R", "untracked.txt": "?", "debug.log": "!", "build": "!",
		"sub": "M", "newdir": "M", "tidy": " ", "clean.txt": " ",
	} {
		if got := badgeOf(t, m, name); got != want {
			t.Errorf("%s: badge %q, want %q", name, got, want)
		}
	}
	if h := header(m); !strings.Contains(h, "⎇ main* · sort name") {
		t.Errorf("breadcrumb = %q", h)
	}

	// The column comes before the names, which move along together
	off := gitModel(t, repo, func() *config.Config { c := config.DefaultConfig(); c.Git = false; return c }())
	if off.gitStatus() != nil || strings.Contains(header(off), "main") {
		t.Fatal("git: false still shows the badges")
	}
	for _, name := range []string{"tracked.txt", "build", "clean.txt"} {
		if on, without := nameColumn(t, m, name), nameColumn(t, off, name); on != without+gitBadgeW {
			t.Errorf("%s is at %d, and at %d without the column", name, on, without)
		}
	}
	head := ansi.Strip(m.renderFileList(80, 5, false)[0])
	if strings.Index(head, "Name") != nameColumn(t, m, "tracked.txt") {
		t.Errorf("heading %q isn't over the names", head)
	}

	// Below the top of the work tree, by its own entries
	m = enter(t, m, "sub")
	if got := badgeOf(t, m, "inner.txt") + badgeOf(t, m, "same.txt"); got != "M " {
		t.Errorf("in sub: %q", got)
	}
	if h := header(m); !strings.Contains(h, "⎇ main*") {
		t.Errorf("breadcrumb in sub = %q", h)
	}
	m = enter(t, gitModel(t, repo, nil), "build")
	if got := badgeOf(t, m, "out.bin"); got != "!" {
		t.Errorf("in build: %q", got)
	}

	// Outside the repository there is no column, and no branch
	m, cmd := press(t, m, "h")
	m = drain(t, m, cmd)
	m, cmd = press(t, m, "h")
	if m = drain(t, m, cmd); m.tab().CurrentPath != filepath.Dir(repo) {
		t.Fatalf("in %s", m.tab().CurrentPath)
	}
	if m.gitStatus() != nil || strings.Contains(header(m), "⎇") {
		t.Fatalf("outside the repository: %q", header(m))
	}
	plainCol := 3 + listColumns(80, m.tab().Files).iconW + 2
	if col := nameColumn(t, m, "repo"); col != plainCol {
		t.Errorf("outside the repository the name is at %d, want %d", col, plainCol)
	}
}

func TestGitBadgesFollowChanges(t *testing.T) {
	repo := gitRepo(t)
	m := gitModel(t, repo, nil)
	if got := badgeOf(t, m, "clean.txt"); got != " " {
		t.Fatalf("clean.txt: %q", got)
	}

	// As the watcher reloads after a change
	writeTestFile(t, filepath.Join(repo, "clean.txt"), "changed")
	m = drain(t, m, m.reloadTab(m.tab()))
	if got := badgeOf(t, m, "clean.txt"); got != "M" {
		t.Fatalf("after a change, clean.txt: %q", got)
	}

	// Committing everything leaves the branch clean
	gittest.Run(t, repo, "add", "-A")
	gittest.Run(t, repo, "commit", "-qm", "all")
	m, cmd := ctrl(t, m, tea.KeyCtrlR)
	m = drain(t, m, cmd)
	if got := badgeOf(t, m, "clean.txt") + badgeOf(t, m, "debug.log"); got != " !" {
		t.Errorf("after the commit: %q", got)
	}
	if h := header(m); !strings.Contains(h, "⎇ main ·") {
		t.Errorf("breadcrumb = %q", h)
	}

	// A detached HEAD shows its commit
	gittest.Run(t, repo, "checkout", "-q", "--detach")
	commit := strings.TrimSpace(gittest.Run(t, repo, "rev-parse", "HEAD"))
	m, cmd = ctrl(t, m, tea.KeyCtrlR)
	if m = drain(t, m, cmd); !strings.Contains(header(m), "⎇ ("+commit[:7]+")") {
		t.Errorf("detached: %q", header(m))
	}
}

func TestGitBranchLabel(t *testing.T) {
	gittest.Isolate(t)
	repo := filepath.Join(t.TempDir(), "fresh")
	os.Mkdir(repo, 0755)
	gittest.Init(t, repo)
	writeTestFile(t, filepath.Join(repo, "first.txt"), "")

	// No commits yet
	m := gitModel(t, repo, nil)
	if h := header(m); !strings.Contains(h, "⎇ main* · sort") || badgeOf(t, m, "first.txt") != "?" {
		t.Fatalf("new repository: %q", h)
	}

	defer ui.SetIconMode(ui.GetIconMode())
	ui.SetIconMode(ui.IconModeASCII)
	if h := header(m); !strings.Contains(h, "git:main* - sort") {
		t.Errorf("ascii: %q", h)
	}
	ui.SetIconMode(ui.IconModeNerd)

	// A long branch beside a long path: the path makes room for the
	// branch, which is cut short when even that isn't enough
	branch := "feature/" + strings.Repeat("long-branch-name-", 4)
	gittest.Run(t, repo, "checkout", "-q", "-b", branch)
	deep := filepath.Join(repo, strings.Repeat("very-long-directory-name-", 4))
	os.Mkdir(deep, 0755)
	writeTestFile(t, filepath.Join(deep, "a.txt"), "")
	m = gitModel(t, deep, nil)
	for _, size := range []tea.WindowSizeMsg{{Width: 200, Height: 30}, {Width: 120, Height: 24}, {Width: 80, Height: 24}, {Width: 60, Height: 15}, {Width: 30, Height: 10}} {
		m = resize(m, size)
		label := fmt.Sprintf("%dx%d", size.Width, size.Height)
		lines := assertFills(t, label, m)
		h := lines[1]
		switch {
		case size.Width >= 120 && !strings.Contains(h, "⎇ "+branch+"*"):
			t.Errorf("%s: the branch isn't whole: %q", label, h)
		case size.Width == 60 && (!strings.Contains(h, "⎇ feature/") || !strings.Contains(h, "...*")):
			t.Errorf("%s: the branch isn't cut short: %q", label, h)
		case size.Width < 120 && strings.Contains(h, "sort"):
			t.Errorf("%s: the sort order shows beside a path cut short: %q", label, h)
		}

		// Every screen still fits, in the repository
		for _, keys := range []string{"r", " ", "?", "/a"} {
			screen := detach(m)
			for _, r := range keys {
				screen, _ = press(t, screen, string(r))
			}
			assertFills(t, label+" after "+keys, screen)
		}
	}
}

// fakeGit puts a stand-in for git alone on PATH that writes its arguments
// to the returned file, then sleeps for sleep seconds if that isn't 0, or
// says it isn't in a repository
func fakeGit(t *testing.T, sleep int) (calls string) {
	t.Helper()
	sleepCmd, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("uses a shell script")
	}
	bin := t.TempDir()
	calls = filepath.Join(bin, "calls")
	script := fmt.Sprintf("#!/bin/sh\n%secho \"$*\" >> %s\n", testutil.Warm, calls)
	if sleep > 0 {
		script += fmt.Sprintf("exec %s %d\n", sleepCmd, sleep)
	} else {
		script += "echo 'fatal: not a git repository (or any of the parent directories): .git' >&2\nexit 128\n"
	}
	testutil.Script(t, filepath.Join(bin, "git"), script)
	t.Setenv("PATH", bin)
	return calls
}

// runs counts the runs of git status the active tab has started
func runs(m Model) int {
	return m.tab().git.seq
}

func TestSlowGitIsGivenUp(t *testing.T) {
	fakeGit(t, 30)
	defer func(d time.Duration) { gitTimeout = d }(gitTimeout)
	gitTimeout = 200 * time.Millisecond
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "sub"), 0755)
	writeTestFile(t, filepath.Join(dir, "a.txt"), "")

	start := time.Now()
	m := gitModel(t, dir, nil)
	if time.Since(start) > 3*time.Second {
		t.Fatalf("waited %v for git", time.Since(start))
	}
	if !m.tab().git.off || m.gitStatus() != nil || runs(m) != 1 {
		t.Fatalf("off=%v status=%v runs=%d", m.tab().git.off, m.gitStatus(), runs(m))
	}
	if col := nameColumn(t, m, "a.txt"); m.statusMsg != "" || col != 3+listColumns(80, m.tab().Files).iconW+2 {
		t.Fatalf("statusMsg %q, name at %d: giving up should be quiet, and leave no column", m.statusMsg, col)
	}

	// The watcher's reloads don't try again
	for range 2 {
		m = drain(t, m, m.reloadTab(m.tab()))
	}
	if runs(m) != 1 {
		t.Fatalf("reloads ran git %d times", runs(m))
	}

	// Moving elsewhere does, as does ctrl+r
	m = enter(t, m, "sub")
	if runs(m) != 2 || !m.tab().git.off {
		t.Fatalf("after moving, git ran %d times", runs(m))
	}
	m, cmd := ctrl(t, m, tea.KeyCtrlR)
	if m = drain(t, m, cmd); runs(m) != 3 || !m.tab().git.off {
		t.Fatalf("after ctrl+r, git ran %d times", runs(m))
	}
}

func TestGitRunsOnceOutsideRepositories(t *testing.T) {
	calls := fakeGit(t, 0)
	// New scripts can be slow to start the first time on macOS
	defer func(d time.Duration) { gitTimeout = d }(gitTimeout)
	gitTimeout = 10 * time.Second
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "sub"), 0755)

	m := gitModel(t, dir, nil)
	for range 3 {
		m = drain(t, m, m.reloadTab(m.tab()))
	}
	if runs(m) != 1 || m.gitStatus() != nil || !m.tab().git.off {
		t.Fatalf("git ran %d times in a directory outside a repository", runs(m))
	}
	if b, _ := os.ReadFile(calls); string(b) != "-C "+dir+" rev-parse --is-inside-work-tree --show-prefix\n" {
		t.Errorf("git ran as %q", b)
	}
	m = enter(t, m, "sub")
	if runs(m) != 2 {
		t.Fatalf("after moving, git ran %d times", runs(m))
	}

	// Never with git: false
	cfg := config.DefaultConfig()
	cfg.Git = false
	os.Remove(calls)
	m = gitModel(t, dir, cfg)
	m = enter(t, m, "sub")
	m, cmd := ctrl(t, m, tea.KeyCtrlR)
	if m = drain(t, m, cmd); runs(m) != 0 {
		t.Fatalf("git ran %d times with git: false", runs(m))
	}
	if _, err := os.Stat(calls); err == nil {
		t.Fatal("git ran with git: false")
	}
}

func TestGitRunsOneAtATime(t *testing.T) {
	repo := gitRepo(t)
	m := newTestModel(t, repo, nil)
	tab := m.tab()

	// Loaded again during a run: one more run once it is in, not two at once
	first := m.gitAfterLoad(tab)
	if first == nil || m.gitAfterLoad(tab) != nil || m.gitAfterLoad(tab) != nil || !tab.git.again {
		t.Fatal("a second run started while the first was running")
	}
	updated, again := m.Update(first())
	m = updated.(Model)
	if again == nil || !m.tab().git.running {
		t.Fatal("no run after the one in flight")
	}
	if m = drain(t, m, again); m.tab().git.running || badgeOf(t, m, "tracked.txt") != "M" {
		t.Fatalf("running=%v", m.tab().git.running)
	}

	// A result for a directory the tab has left is dropped
	stale := m.gitAfterLoad(m.tab())
	m = enter(t, m, "sub")
	updated, _ = m.Update(stale())
	if m = updated.(Model); badgeOf(t, m, "inner.txt") != "M" || m.tab().git.dir != filepath.Join(repo, "sub") {
		t.Fatalf("the result for %s replaced the one for sub", repo)
	}

	// Quitting stops a run in flight
	inFlight := m.gitAfterLoad(m.tab())
	m.Close()
	if msg := inFlight().(gitStatusMsg); !errors.Is(msg.err, context.Canceled) {
		t.Fatalf("the run went on after quitting: %v", msg.err)
	}
}
