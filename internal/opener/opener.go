// Package opener builds the commands that open files in an editor or with
// the desktop's default application
package opener

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// EditorCommand returns the command that opens paths in the user's editor:
// $VISUAL, then $EDITOR, then vi (notepad on Windows). The variables may
// include arguments, as in "code --wait".
func EditorCommand(paths ...string) *exec.Cmd {
	editor := os.Getenv("VISUAL")
	if strings.TrimSpace(editor) == "" {
		editor = os.Getenv("EDITOR")
	}
	fields := strings.Fields(editor)
	if len(fields) == 0 {
		fields = []string{"vi"}
		if runtime.GOOS == "windows" {
			fields = []string{"notepad"}
		}
	}
	return exec.Command(fields[0], append(fields[1:], paths...)...)
}

// SystemCommand returns the command that opens path with the default
// application, as double-clicking it would
func SystemCommand(path string) *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path)
	case "windows":
		// Avoids "cmd /c start", which would interpret & and ^ in the path
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	default:
		return exec.Command("xdg-open", path)
	}
}
