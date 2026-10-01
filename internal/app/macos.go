package app

import (
	"context"
	"os/exec"
	"runtime"
	"time"

	"github.com/icichainz/sushi/internal/jxa"
)

// What sushi does with macOS itself: share its clipboard with Finder
// through the pasteboard (pasteboard.go), open files with an app picked
// from the ones that can (openwith.go), and show them in Finder. Each goes
// through osascript or open(1), whose failures only make a status message.

// onMac reports whether sushi runs on macOS; elsewhere, the pasteboard is
// left alone and Open With and Finder say they need it. Tests set it.
var onMac = runtime.GOOS == "darwin"

// osascript runs the scripts behind the pasteboard, Open With and showing
// several files in Finder; runOpen runs open(1) with args and returns what
// it printed. Tests replace both, so none reaches the real ones.
var (
	osascript jxa.Runner = jxa.Osascript
	runOpen              = func(args ...string) ([]byte, error) {
		return exec.Command("open", args...).CombinedOutput()
	}
)

// macTimeout is the longest an osascript call may take: the pasteboard
// server or Launch Services could hang, and a paste waits on its read
const macTimeout = 10 * time.Second

// runOsascript calls osascript as it is when the call is made, so a test's
// fake applies to the pasteboard made before it
func runOsascript(ctx context.Context, script string) ([]byte, error) {
	return osascript(ctx, script)
}
