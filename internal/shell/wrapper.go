// Package shell provides the shell functions that let sushi change the
// shell's directory when it quits. A program can't change the directory of
// the shell that started it, so the function runs sushi with --cwd-file,
// then cds to the directory sushi wrote there.
package shell

import (
	"fmt"
	"path/filepath"
	"strings"
)

// FunctionName is the name of the shell function the wrappers define
const FunctionName = "sushicd"

// Shells lists the shells there is a wrapper for
var Shells = []string{"zsh", "bash", "fish"}

// posixWrapper works in both bash and zsh. With an empty delimiter, read -d
// reads up to a NUL, which never comes, so it reads the whole file: unlike
// $(cat), that keeps trailing newlines, so names can contain any character.
const posixWrapper = `# sushicd runs sushi, then changes to the directory it was showing when you
# quit with q. Q quits without changing directory.
sushicd() {
	local tmp dir code
	tmp="$(mktemp -t sushi-cwd.XXXXXX)" || return
	command sushi --cwd-file "$tmp" "$@"
	code=$?
	IFS= read -r -d '' dir < "$tmp" || true
	rm -f -- "$tmp"
	if [ -n "$dir" ] && [ -d "$dir" ] && [ "$dir" != "$PWD" ]; then
		builtin cd -- "$dir" || return
	fi
	return "$code"
}
`

const fishWrapper = `# sushicd runs sushi, then changes to the directory it was showing when you
# quit with q. Q quits without changing directory.
function sushicd --description 'Run sushi, then cd to the directory it was showing'
    set -l tmp (mktemp -t sushi-cwd.XXXXXX)
    or return 1
    command sushi --cwd-file $tmp $argv
    set -l code $status
    read -l -z dir < $tmp
    rm -f -- $tmp
    if test -n "$dir"; and test -d "$dir"; and test "$dir" != "$PWD"
        builtin cd -- $dir
        or return 1
    end
    return $code
end
`

// Wrapper returns the shell function for the named shell, which may be
// given as a path such as /bin/zsh
func Wrapper(name string) (string, error) {
	switch filepath.Base(name) {
	case "zsh", "bash":
		return posixWrapper, nil
	case "fish":
		return fishWrapper, nil
	}
	if name == "" {
		return "", fmt.Errorf("no shell given: choose %s", strings.Join(Shells, ", "))
	}
	return "", fmt.Errorf("no wrapper for %q: choose %s", name, strings.Join(Shells, ", "))
}
