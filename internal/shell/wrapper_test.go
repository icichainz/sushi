package shell

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/icichainz/sushi/internal/testutil"
)

// needShell returns the path of a shell, skipping the test if it's missing
func needShell(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s is not installed", name)
	}
	return path
}

// writeWrapper saves the wrapper for shell to a file and returns its path
func writeWrapper(t *testing.T, shell string) string {
	t.Helper()
	script, err := Wrapper(shell)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "wrapper."+shell)
	if err := os.WriteFile(path, []byte(script), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestWrappersAreValidShell(t *testing.T) {
	checks := map[string][]string{
		"zsh":  {"-n"},
		"bash": {"-n"},
		"fish": {"--no-execute"},
	}
	for _, name := range Shells {
		t.Run(name, func(t *testing.T) {
			sh := needShell(t, name)
			args := append(checks[name], writeWrapper(t, name))
			if out, err := exec.Command(sh, args...).CombinedOutput(); err != nil {
				t.Fatalf("%s rejects the wrapper: %v\n%s", name, err, out)
			}
		})
	}
}

// fakeSushi puts a stand-in for sushi on PATH that writes $TARGET to the
// file given with --cwd-file and exits with $CODE
func fakeSushi(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	script := `#!/bin/sh
[ "$1" = warm ] && exit 0
while [ $# -gt 0 ]; do
	case "$1" in
	--cwd-file) file="$2"; shift 2 ;;
	*) printf '%s\n' "$1" >> "$ARGS"; shift ;;
	esac
done
if [ -n "$TARGET" ]; then printf '%s' "$TARGET" > "$file"; fi
exit "${CODE:-0}"
`
	testutil.Script(t, filepath.Join(bin, "sushi"), script)
	return bin
}

func TestWrapperChangesDirectory(t *testing.T) {
	for _, name := range Shells {
		t.Run(name, func(t *testing.T) {
			sh := needShell(t, name)
			wrapper := writeWrapper(t, name)
			bin := fakeSushi(t)

			root := t.TempDir()
			// Spaces, a newline in the middle and one at the end, and a
			// leading dash must all survive the round trip
			target := filepath.Join(root, "-my dir\nwith newline\n")
			if err := os.Mkdir(target, 0755); err != nil {
				t.Skipf("can't make a directory with a newline in its name: %v", err)
			}
			start := t.TempDir()

			// Source the wrapper, run it with arguments that need quoting,
			// then print where the shell ended up and the exit status
			var script string
			if name == "fish" {
				script = `source $argv[1]; sushicd "a b" ""; set -l code $status; printf '%s|%s' $code "$PWD"`
			} else {
				script = `. "$1"; sushicd "a b" ""; code=$?; printf '%s|%s' "$code" "$PWD"`
			}

			run := func(target, code string) (string, string) {
				args := filepath.Join(t.TempDir(), "args")
				cmd := exec.Command(sh, "-c", script, "_", wrapper)
				if name == "fish" {
					cmd = exec.Command(sh, "--no-config", "-c", script, wrapper)
				}
				cmd.Dir = start
				cmd.Env = append(os.Environ(),
					"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
					"HOME="+t.TempDir(), "PWD="+start,
					"TARGET="+target, "CODE="+code, "ARGS="+args)
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("%v\n%s", err, out)
				}
				passed, _ := os.ReadFile(args)
				return string(out), string(passed)
			}

			out, args := run(target, "0")
			if want := "0|" + target; out != want {
				t.Fatalf("got %q, want %q", out, want)
			}
			if args != "a b\n\n" {
				t.Fatalf("sushi got arguments %q, want \"a b\" and an empty one", args)
			}

			// Nothing written (Q): stay put, and pass on the exit status
			if out, _ := run("", "3"); !strings.HasPrefix(out, "3|") || !samePath(t, strings.TrimPrefix(out, "3|"), start) {
				t.Fatalf("after Q got %q, want status 3 and still in %s", out, start)
			}
		})
	}
}

// samePath compares directories, allowing for symlinks such as macOS's
// /var -> /private/var
func samePath(t *testing.T, a, b string) bool {
	t.Helper()
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

func TestWrapperChoice(t *testing.T) {
	for _, name := range []string{"zsh", "/bin/zsh", "bash", "/usr/local/bin/fish"} {
		script, err := Wrapper(name)
		if err != nil || !strings.Contains(script, FunctionName) {
			t.Errorf("Wrapper(%q) = %v, want the %s function", name, err, FunctionName)
		}
	}
	for _, name := range []string{"", "tcsh", "sh"} {
		if _, err := Wrapper(name); err == nil || !strings.Contains(err.Error(), "zsh, bash, fish") {
			t.Errorf("Wrapper(%q) err = %v, want the choices listed", name, err)
		}
	}
}
