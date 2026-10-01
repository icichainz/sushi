// Package opener builds the commands that open files in an editor or with
// the desktop's default application, and on macOS lists the other apps
// that can open a file and shows files in Finder (see apps.go)
package opener

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// EditorCommand returns the command that opens paths in the user's editor:
// $VISUAL, then $EDITOR, then vi. The variables may
// include arguments, as in "code --wait".
func EditorCommand(paths ...string) *exec.Cmd {
	editor := os.Getenv("VISUAL")
	if strings.TrimSpace(editor) == "" {
		editor = os.Getenv("EDITOR")
	}
	fields := strings.Fields(editor)
	if len(fields) == 0 {
		fields = []string{"vi"}
	}
	return exec.Command(fields[0], append(fields[1:], paths...)...)
}

// SystemCommand returns the command that opens path with the default
// application, as double-clicking it would
func SystemCommand(path string) *exec.Cmd {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", path)
	}
	return exec.Command("xdg-open", path)
}
