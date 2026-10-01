package app

import (
	"context"
	"runtime"
	"time"

	"github.com/icichainz/sushi/internal/jxa"
)

// What sushi does with macOS itself: share its clipboard with Finder
// through the pasteboard (pasteboard.go). It goes through osascript, whose
// failures only make a status message.

// onMac reports whether sushi runs on macOS; elsewhere, the pasteboard is
// left alone. Tests set it.
var onMac = runtime.GOOS == "darwin"

// osascript runs the scripts behind the pasteboard. Tests replace it, so
// none reaches the real one.
var osascript jxa.Runner = jxa.Osascript

// macTimeout is the longest an osascript call may take: the pasteboard
// server could hang, and a paste waits on its read
const macTimeout = 10 * time.Second

// runOsascript calls osascript as it is when the call is made, so a test's
// fake applies to the pasteboard made before it
func runOsascript(ctx context.Context, script string) ([]byte, error) {
	return osascript(ctx, script)
}
