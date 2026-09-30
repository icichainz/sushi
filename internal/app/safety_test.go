package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/icichainz/sushi/internal/config"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/plugins"
)

// Regression tests for data-safety problems found in review

// symlinkOrSkip makes a symlink, skipping the test where that needs privileges
func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("can't make symlinks here: %v", err)
	}
}

// withPlugin returns a model in dir with one background plugin on Z
func withPlugin(t *testing.T, dir, command string) Model {
	t.Helper()
	skipWithoutSh(t)
	cfg := config.DefaultConfig()
	cfg.Plugins = []plugins.Plugin{{Name: "pick", Key: "Z", Mode: plugins.ModeBackground, Command: command}}
	return newTestModel(t, dir, cfg)
}

func TestPluginSelectedLinkWithSlashDeletesOnlyTheLink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	os.Mkdir(target, 0755)
	precious := filepath.Join(target, "precious.txt")
	writeTestFile(t, precious, "keep")
	link := filepath.Join(dir, "link")
	symlinkOrSkip(t, target, link)

	// "link/" names the target: Lstat follows it, so a hard delete used to
	// empty the target folder
	m := withPlugin(t, dir, `echo "select $SUSHI_DIR/link/" > "$SUSHI_CMD_FILE"; echo "select $SUSHI_DIR/missing.txt" >> "$SUSHI_CMD_FILE"`)
	m, cmd := press(t, m, "Z")
	m = drain(t, m, cmd)
	if len(m.tab().Selected) != 1 || !m.tab().Selected[link] {
		t.Fatalf("selected %v, want only %s", m.tab().Selected, link)
	}
	if !strings.Contains(m.statusMsg, "can't select") || !strings.Contains(m.statusMsg, "missing.txt") {
		t.Fatalf("statusMsg = %q, want the missing path reported", m.statusMsg)
	}

	m, _ = press(t, m, "D")
	m, cmd = press(t, m, "y")
	m = drain(t, m, cmd)
	if fs.Exists(link) {
		t.Fatalf("the link is still there: %q", m.statusMsg)
	}
	if readTestFile(t, precious) != "keep" {
		t.Fatal("deleting the link emptied its target")
	}
}

func TestTargetsAreCleaned(t *testing.T) {
	dir := t.TempDir()
	m := newTestModel(t, dir, nil)
	a := filepath.Join(dir, "a")
	m.tab().Selected[a+string(filepath.Separator)] = true
	m.tab().Selected[a] = true
	if got := m.targets(); len(got) != 1 || got[0] != a {
		t.Fatalf("targets = %q, want just %q", got, a)
	}
}
