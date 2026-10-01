// Package testutil holds helpers shared by the tests of several packages
package testutil

import (
	"os"
	"os/exec"
	"testing"
)

// Script writes an executable script to path and runs it once with the
// argument warm, which it must answer by exiting at once, as with
//
//	[ "$1" = warm ] && exit 0
//
// macOS checks a new program the first time it runs, which takes most of
// a second when idle and seconds while the whole suite runs: done here, it
// doesn't count against the time limits of the code the script stands in
// for.
func Script(t testing.TB, path, script string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(path, "warm").CombinedOutput(); err != nil {
		t.Fatalf("running %s: %v\n%s", path, err, out)
	}
}

// Warm is the line a script given to Script starts with, after #!/bin/sh
const Warm = "[ \"$1\" = warm ] && exit 0\n"
