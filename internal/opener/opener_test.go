package opener

import (
	"slices"
	"testing"
)

func TestEditorCommandPrefersVisual(t *testing.T) {
	t.Setenv("VISUAL", "code --wait")
	t.Setenv("EDITOR", "nano")
	cmd := EditorCommand("a.txt", "b.txt")
	if want := []string{"code", "--wait", "a.txt", "b.txt"}; !slices.Equal(cmd.Args, want) {
		t.Fatalf("args = %v, want %v", cmd.Args, want)
	}
}

func TestEditorCommandFallsBack(t *testing.T) {
	t.Setenv("VISUAL", " ")
	t.Setenv("EDITOR", "nano")
	if args := EditorCommand("x").Args; !slices.Equal(args, []string{"nano", "x"}) {
		t.Fatalf("args = %v, want EDITOR when VISUAL is blank", args)
	}
	t.Setenv("EDITOR", "")
	if args := EditorCommand("x").Args; len(args) != 2 || args[1] != "x" {
		t.Fatalf("args = %v, want a default editor", args)
	}
}

func TestSystemCommandPassesPathAsOneArgument(t *testing.T) {
	path := "/tmp/report & notes.pdf"
	args := SystemCommand(path).Args
	if args[len(args)-1] != path {
		t.Fatalf("args = %v, path must be passed intact", args)
	}
}
