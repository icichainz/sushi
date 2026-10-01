// Package jxa runs JavaScript for Automation scripts with osascript, which
// reach macOS frameworks such as AppKit through the Objective-C bridge
// without cgo or a compiled helper.
//
// Values go into a script as JavaScript literals rather than as arguments,
// and a script hands its result back with the Prelude's result function,
// as JSON in plain ASCII, which Decode reads.
package jxa

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Runner runs a script and returns what it printed. Osascript is the real
// one; tests use fakes, so they never reach the pasteboard or Finder.
type Runner func(ctx context.Context, script string) ([]byte, error)

// Prelude starts every script: it loads AppKit and defines result, which
// turns a value into JSON with everything outside ASCII escaped, so what
// osascript prints doesn't depend on the locale's encoding
const Prelude = `ObjC.import('AppKit');
function result(v) {
	return JSON.stringify(v).replace(/[\u007f-￿]/g, c => '\\u' + ('000' + c.charCodeAt(0).toString(16)).slice(-4));
}
`

// Timeout is the longest Osascript lets a script run, whatever its
// context allows: the pasteboard server or Launch Services can hang. Tests
// lower it.
var Timeout = 10 * time.Second

// Osascript runs script with osascript, giving it the script on standard
// input, which has no length limit as arguments do. It gives up after
// Timeout.
func Osascript(ctx context.Context, script string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "osascript", "-l", "JavaScript")
	cmd.Stdin = strings.NewReader(script)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	// Something osascript started could keep its output open once it is
	// killed
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("osascript: %w", ctx.Err())
		}
		return nil, fmt.Errorf("osascript: %w%s", err, scriptError(stderr.String()))
	}
	return out, nil
}

// scriptError picks out what went wrong from osascript's error output, as
// in "execution error: Error: no such file (-2700)", for the end of an
// error message
func scriptError(stderr string) string {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if last == "" {
		return ""
	}
	_, msg, ok := strings.Cut(last, "execution error: ")
	if !ok {
		msg = last
	}
	// JXA repeats the kind of error, as in "Error: Error: ...", and ends
	// with a number that means nothing to anyone reading the status bar
	for strings.HasPrefix(msg, "Error: ") {
		msg = strings.TrimPrefix(msg, "Error: ")
	}
	if i := strings.LastIndex(msg, " ("); i > 0 && strings.HasSuffix(msg, ")") {
		if _, err := strconv.Atoi(msg[i+2 : len(msg)-1]); err == nil {
			msg = msg[:i]
		}
	}
	return ": " + msg
}

// Literal writes v as a JavaScript literal. JSON is JavaScript, and Go's
// encoder escapes U+2028 and U+2029, which older engines don't take in a
// string. Strings must be valid UTF-8: JSON would replace the bytes that
// aren't, and a path would then name another file.
func Literal(v any) (string, error) {
	if err := checkUTF8(v); err != nil {
		return "", err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// checkUTF8 fails on a string, or a string in a slice, that isn't UTF-8
func checkUTF8(v any) error {
	var strs []string
	switch v := v.(type) {
	case string:
		strs = []string{v}
	case []string:
		strs = v
	}
	for _, s := range strs {
		if !utf8.ValidString(s) {
			return fmt.Errorf("%q is not valid UTF-8", s)
		}
	}
	return nil
}

// Decode reads what a script returned with result into v
func Decode(out []byte, v any) error {
	out = bytes.TrimSpace(out)
	if len(out) == 0 {
		return errors.New("osascript returned nothing")
	}
	if err := json.Unmarshal(out, v); err != nil {
		return fmt.Errorf("reading what osascript returned: %w", err)
	}
	return nil
}
