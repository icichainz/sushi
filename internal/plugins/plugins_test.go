package plugins

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeScript(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverReadsScriptHeaders(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, filepath.Join(dir, "du.sh"), "#!/bin/sh\n# sushi-key: ctrl+g\n# sushi-mode: background\n# sushi-description: Disk usage\ndu -sh \"$@\"\n", 0755)
	writeScript(t, filepath.Join(dir, "plain"), "#!/bin/sh\necho hi\n", 0755)
	writeScript(t, filepath.Join(dir, "forgot-chmod"), "#!/bin/sh\necho hi\n", 0644)
	writeScript(t, filepath.Join(dir, "README.md"), "# Notes\n", 0644)
	writeScript(t, filepath.Join(dir, ".hidden"), "#!/bin/sh\n", 0755)

	found, warnings := Discover(dir)
	if len(found) != 2 {
		t.Fatalf("found %+v, want du and plain", found)
	}
	du := found[0]
	if du.Name != "du" || du.Key != "ctrl+g" || du.Mode != ModeBackground || du.Description != "Disk usage" {
		t.Fatalf("du = %+v", du)
	}
	if found[1].Name != "plain" || found[1].Key != "" {
		t.Fatalf("plain = %+v", found[1])
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "forgot-chmod") {
		t.Fatalf("warnings = %v, want one about the non-executable script", warnings)
	}
}

func TestDiscoverMissingDirIsFine(t *testing.T) {
	found, warnings := Discover(filepath.Join(t.TempDir(), "nope"))
	if found != nil || warnings != nil {
		t.Fatalf("found=%v warnings=%v", found, warnings)
	}
}

func TestLoadValidatesAndMerges(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, filepath.Join(dir, "git"), "#!/bin/sh\n", 0755)
	writeScript(t, filepath.Join(dir, "extra"), "#!/bin/sh\n", 0755)

	loaded, warnings := Load([]Plugin{
		{Name: "git", Command: "git status"},
		{Name: "odd", Command: "true", Mode: "sideways"},
		{Name: "", Command: "true"},
		{Name: "no-command"},
	}, dir)

	var names []string
	for _, p := range loaded {
		names = append(names, p.Name)
	}
	if !slices.Equal(names, []string{"git", "odd", "extra"}) {
		t.Fatalf("loaded %v", names)
	}
	if loaded[0].Script != "" || loaded[0].Mode != ModeWait {
		t.Fatalf("config plugin should win and default to wait: %+v", loaded[0])
	}
	if loaded[1].Mode != ModeWait {
		t.Fatalf("unknown mode not replaced: %+v", loaded[1])
	}
	joined := strings.Join(warnings, "\n")
	for _, want := range []string{"sideways", "#3", "#4", `"git" is defined twice`} {
		if !strings.Contains(joined, want) {
			t.Errorf("warnings missing %q:\n%s", want, joined)
		}
	}
}

func TestCommandPassesSelectionAndEnvironment(t *testing.T) {
	dir := t.TempDir()
	ctx := Context{
		Dir:       dir,
		File:      filepath.Join(dir, "a b.txt"),
		Selection: []string{filepath.Join(dir, "a b.txt"), filepath.Join(dir, "c.txt")},
		CmdFile:   filepath.Join(dir, "cmds"),
	}

	p := Plugin{Name: "t", Command: `printf '%s|' "$#" "$1" "$2" "$SUSHI_DIR" "$SUSHI_FILE" "$SUSHI_CMD_FILE"; pwd`}
	out, err := p.Cmd(ctx).Output()
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{"2", ctx.Selection[0], ctx.Selection[1], dir, ctx.File, ctx.CmdFile}, "|") + "|"
	got := string(out)
	if !strings.HasPrefix(got, want) {
		t.Fatalf("got  %q\nwant %q...", got, want)
	}
	// The plugin runs in the current directory
	realDir, _ := filepath.EvalSymlinks(dir)
	if pwd := strings.TrimSpace(strings.TrimPrefix(got, want)); pwd != dir && pwd != realDir {
		t.Fatalf("pwd = %q, want %q", pwd, dir)
	}

	script := filepath.Join(dir, "script")
	writeScript(t, script, "#!/bin/sh\nprintf '%s|' \"$@\"\nprintf '%s' \"$SUSHI_SELECTION\"\n", 0755)
	out, err = Plugin{Name: "s", Script: script}.Cmd(ctx).Output()
	if err != nil {
		t.Fatal(err)
	}
	if want := ctx.Selection[0] + "|" + ctx.Selection[1] + "|" + strings.Join(ctx.Selection, "\n"); string(out) != want {
		t.Fatalf("script got %q, want %q", out, want)
	}
}

func TestReadInstructions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cmds")
	os.WriteFile(path, []byte("cd /tmp/some dir\r\n\nselect a.txt\nstatus All done!\nfly away\ncd\n"), 0644)

	got, warnings, err := ReadInstructions(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []Instruction{{"cd", "/tmp/some dir"}, {"select", "a.txt"}, {"status", "All done!"}}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings = %v, want the unknown line and the empty cd", warnings)
	}

	if got, warnings, err := ReadInstructions(filepath.Join(t.TempDir(), "missing")); got != nil || warnings != nil || err != nil {
		t.Fatal("a missing file should mean no instructions")
	}
}

// The example plugins shipped in the repository must at least parse
func TestExamplePluginsAreValidShell(t *testing.T) {
	examples, _ := filepath.Glob("../../examples/plugins/*")
	if len(examples) == 0 {
		t.Skip("no examples found")
	}
	for _, path := range examples {
		out, err := exec.Command("sh", "-n", path).CombinedOutput()
		if err != nil {
			t.Errorf("%s: %v\n%s", filepath.Base(path), err, out)
		}
		info, _ := os.Stat(path)
		if info.Mode()&0111 == 0 {
			t.Errorf("%s is not executable", filepath.Base(path))
		}
	}
}

func TestExamplePluginsLoadAndRun(t *testing.T) {
	found, warnings := Discover("../../examples/plugins")
	if len(warnings) > 0 {
		t.Fatalf("warnings: %v", warnings)
	}
	byName := make(map[string]Plugin)
	for _, p := range found {
		byName[p.Name] = p
	}
	for name, key := range map[string]string{"fzf-jump": "ctrl+f", "git-status": "ctrl+g", "disk-usage": "", "copy-path": "Y"} {
		p, ok := byName[name]
		if !ok || p.Key != key || p.Description == "" {
			t.Errorf("%s: %+v", name, p)
		}
	}

	dir := t.TempDir()
	file := filepath.Join(dir, "data.bin")
	os.WriteFile(file, make([]byte, 4096), 0644)
	out, err := byName["disk-usage"].Cmd(Context{Dir: dir, File: file, Selection: []string{file}}).Output()
	if err != nil || !strings.Contains(string(out), "in 1 item(s)") {
		t.Fatalf("disk-usage: %q, %v", out, err)
	}
}
