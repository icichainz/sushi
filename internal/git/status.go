// Package git reads what Git says about a directory of a work tree: which
// of its entries are changed, untracked or ignored, and the branch checked
// out, for the badges of the file list. It runs the git command, so it
// knows about repositories exactly as git does: worktrees, submodules,
// ignore rules and all.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"
)

// The badges, one character each, as the file list shows them
const (
	Modified  = 'M'
	Added     = 'A'
	Deleted   = 'D'
	Renamed   = 'R'
	Untracked = '?'
	Ignored   = '!'
	Conflict  = 'U'
)

// rank orders the badges: where an entry has more than one, as a
// directory holding several changes does, the strongest is shown
var rank = map[byte]int{Ignored: 1, Untracked: 2, Modified: 3, Deleted: 4, Added: 5, Renamed: 6, Conflict: 7}

// stronger returns whichever badge ranks higher
func stronger(a, b byte) byte {
	if rank[b] > rank[a] {
		return b
	}
	return a
}

// ErrNotRepo means the directory isn't in the work tree of a repository,
// or is in its .git directory
var ErrNotRepo = errors.New("not in a git work tree")

// maxOutput is the most git status may print before it is given up on.
// It is far more than the time limit lets a real repository produce.
const maxOutput = 64 << 20

// Status is what git status says about one directory of a work tree
type Status struct {
	Root      string // The top of the work tree, reached from the directory by name
	GitDir    string // The repository's directory for this work tree, where HEAD and the index are
	CommonDir string // The directory the work trees of the repository share, where the branches are
	Branch    string // The branch checked out, even before its first commit; "" if HEAD is detached
	Commit    string // The commit checked out; "" before the first
	Dirty     bool   // Something in the work tree is changed or untracked

	// Restricted means the repository's own configuration names commands
	// that git status would run, so it wasn't run: there are no badges,
	// and Dirty isn't known
	Restricted bool

	inherited byte            // Applies to every entry: the directory is in an ignored one
	badges    map[string]byte // By name (NFC); a directory sums up what is inside it
}

// Head names what is checked out: the branch, or for a detached HEAD the
// commit, as in "(1a2b3c4)"
func (s *Status) Head() string {
	switch {
	case s.Branch != "":
		return s.Branch
	case s.Commit != "":
		return "(" + s.Commit[:min(len(s.Commit), 7)] + ")"
	}
	return ""
}

// Badge returns the badge of the directory's entry name, or 0 if it has
// none: it is unchanged, or s is nil
func (s *Status) Badge(name string) byte {
	if s == nil {
		return 0
	}
	// Git on macOS reports names composed (NFC), whatever the disk holds
	return stronger(s.badges[norm.NFC.String(name)], s.inherited)
}

// WithoutBadges returns what s says about the work tree without the
// badges, for a directory nearby in the same work tree while its own
// status is read
func (s *Status) WithoutBadges() *Status {
	return &Status{Root: s.Root, GitDir: s.GitDir, CommonDir: s.CommonDir, Branch: s.Branch, Commit: s.Commit,
		Dirty: s.Dirty, Restricted: s.Restricted}
}

// Read asks git about dir. It returns ErrNotRepo outside a work tree, and
// gives up when ctx is done, as for a repository too large to read in
// time.
//
// A repository can name commands for git to run, in its own configuration,
// which comes with it when it is downloaded or unpacked: a file system
// monitor, and filters that git status runs on files to compare them.
// Browsing it must not run them. So git status never uses a file system
// monitor or hooks, doesn't look inside the work trees of submodules,
// which have configurations of their own, and isn't run at all in a
// repository whose own configuration sets any of riskyConfig: then the
// status has only the branch and the commit, read without running
// anything, and is Restricted.
func Read(ctx context.Context, dir string) (*Status, error) {
	loc, err := locate(ctx, dir)
	if err != nil {
		return nil, err
	}
	risky, err := configures(ctx, dir)
	if err != nil {
		return nil, err
	}
	var s *Status
	if risky {
		s, err = head(ctx, dir)
	} else {
		var out []byte
		// A submodule shows as changed when its commit is; changes in its
		// work tree would take git status there
		out, err = run(ctx, dir, "status", "--porcelain=v2", "--branch", "--untracked-files=all", "--ignored=matching",
			"--ignore-submodules=dirty", "-z")
		if err == nil {
			s = Parse(out, loc.prefix)
		}
	}
	if err != nil {
		return nil, err
	}
	s.Root, s.GitDir, s.CommonDir = dir, loc.gitDir, loc.commonDir
	for range strings.Count(loc.prefix, "/") {
		s.Root = filepath.Dir(s.Root)
	}
	return s, nil
}

// location is where a directory is in its repository
type location struct {
	prefix    string // Its path below the top of the work tree, ending in a slash, as git names paths; "" at the top
	gitDir    string
	commonDir string
}

// locate asks git whether dir is in a work tree, and where it and the
// repository are. A name can hold a newline, which makes two lines of it:
// the prefix comes last, so it is the rest whatever it holds, and a path to
// the repository with one, which puts the lines before it out, is caught
// by checking them, and each is asked for on its own.
func locate(ctx context.Context, dir string) (location, error) {
	out, err := run(ctx, dir, "rev-parse", "--is-inside-work-tree", "--absolute-git-dir", "--git-common-dir", "--show-prefix")
	if err != nil {
		return location{}, err
	}
	inside, rest, _ := strings.Cut(string(out), "\n")
	if inside != "true" {
		return location{}, ErrNotRepo
	}
	if lines := strings.SplitN(rest, "\n", 3); len(lines) == 3 {
		loc := location{gitDir: lines[0], commonDir: absolute(dir, lines[1]), prefix: strings.TrimSuffix(lines[2], "\n")}
		if loc.valid(dir) {
			return loc, nil
		}
	}
	var loc location
	for _, ask := range []struct {
		arg string
		to  *string
	}{{"--absolute-git-dir", &loc.gitDir}, {"--git-common-dir", &loc.commonDir}, {"--show-prefix", &loc.prefix}} {
		out, err := run(ctx, dir, "rev-parse", ask.arg)
		if err != nil {
			return location{}, err
		}
		*ask.to = strings.TrimSuffix(string(out), "\n")
	}
	loc.commonDir = absolute(dir, loc.commonDir)
	return loc, nil
}

// valid reports whether what locate read for dir makes sense: the
// repository's directories are there, and the prefix ends dir's path
func (loc location) valid(dir string) bool {
	if !isDir(loc.gitDir) || !isDir(loc.commonDir) {
		return false
	}
	return loc.prefix == "" || strings.HasSuffix(filepath.ToSlash(dir)+"/", "/"+loc.prefix)
}

// absolute returns path, which git gives relative to dir, as an absolute
// path
func absolute(dir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dir, path)
}

// isDir reports whether path is absolute, and a directory
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir() && filepath.IsAbs(path)
}

// riskyConfig matches the settings that have git run a command of the
// repository's choosing: a file system monitor and filters, which git
// status runs, and for good measure the commands that reach other
// repositories, which it doesn't
const riskyConfig = `^(core\.fsmonitor|filter\..*\.(clean|smudge|process|required)|core\.sshcommand|credential(\..*)?\.helper)$`

// configures reports whether the repository's own configuration, rather
// than the user's, sets any of riskyConfig. Reading configuration runs
// nothing.
func configures(ctx context.Context, dir string) (bool, error) {
	out, err := run(ctx, dir, "config", "--show-scope", "--includes", "--get-regexp", riskyConfig)
	if notFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// "local\tfilter.x.clean command", a line each; what the repository's
	// configuration includes has its scope too
	for _, line := range strings.Split(string(out), "\n") {
		if scope, _, _ := strings.Cut(line, "\t"); scope == "local" || scope == "worktree" {
			return true, nil
		}
	}
	return false, nil
}

// head reads the branch and the commit checked out in dir, from the
// references, which runs nothing, for a Restricted status
func head(ctx context.Context, dir string) (*Status, error) {
	s := &Status{Restricted: true, badges: make(map[string]byte)}
	// Neither is an error: a detached HEAD has no branch, and a branch
	// without commits no commit
	out, err := run(ctx, dir, "symbolic-ref", "-q", "--short", "HEAD")
	if err != nil && !notFound(err) {
		return nil, err
	}
	s.Branch = strings.TrimSuffix(string(out), "\n")
	out, err = run(ctx, dir, "rev-parse", "-q", "--verify", "HEAD^{commit}")
	if err != nil && !notFound(err) {
		return nil, err
	}
	s.Commit = strings.TrimSuffix(string(out), "\n")
	return s, nil
}

// notFound reports whether git failed only to find what it was asked
// for, which it says by exiting with 1
func notFound(err error) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == 1
}

// safety goes before every git command, as a last line of defence: no
// file system monitor, which a repository could name, and no hooks, which
// no command run here should run anyway
var safety = []string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null"}

// keptVars are the variables for git that are passed on from sushi's
// environment: where the user's own configuration is, and where looking
// for a repository stops
var keptVars = map[string]bool{"GIT_CONFIG_GLOBAL": true, "GIT_CONFIG_SYSTEM": true, "GIT_CONFIG_NOSYSTEM": true, "GIT_CEILING_DIRECTORIES": true}

// environ returns the environment git runs in: sushi's, without the GIT_
// variables naming a repository, work tree, index or configuration, which
// git sets for its hooks and the commands it runs. Inherited, they would
// make every directory look like the same repository.
func environ() []string {
	var env []string
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "GIT_") && !keptVars[name] {
			continue
		}
		env = append(env, kv)
	}
	// Otherwise status refreshes the index, writing to the repository
	// that sushi only looks at, and taking a lock git commands run at the
	// same time would trip over
	return append(env, "GIT_OPTIONAL_LOCKS=0")
}

// run runs git in dir and returns what it printed
func run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append(append([]string{"-C", dir}, safety...), args...)...)
	cmd.Env = environ()
	stdout := &limitedBuffer{limit: maxOutput, full: cancel}
	stderr := &limitedBuffer{limit: 4096}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	// Something git started could keep its output open after it is killed
	cmd.WaitDelay = 500 * time.Millisecond

	err := cmd.Run()
	switch {
	case stdout.over:
		return nil, fmt.Errorf("git %s: printed more than %d MB", args[0], maxOutput>>20)
	case ctx.Err() != nil:
		return nil, fmt.Errorf("git %s: %w", args[0], ctx.Err())
	case err == nil:
		return stdout.Bytes(), nil
	}
	// The first line says what went wrong; the rest says what to do
	msg, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n")
	if strings.Contains(msg, "not a git repository") {
		return nil, ErrNotRepo
	}
	if msg != "" {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, msg)
	}
	return nil, fmt.Errorf("git %s: %w", args[0], err)
}

// limitedBuffer keeps what is written to it up to limit bytes. Past that
// it calls full, so the writer can be stopped, and fails; without full it
// drops the rest.
type limitedBuffer struct {
	bytes.Buffer
	limit int
	over  bool
	full  func()
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) <= b.limit {
		return b.Buffer.Write(p)
	}
	if b.full == nil {
		b.Buffer.Write(p[:max(b.limit-b.Len(), 0)])
		return len(p), nil
	}
	b.over = true
	b.full()
	return 0, errors.New("too much output")
}

// Parse reads the output of git status --porcelain=v2 --branch -z, run
// anywhere in the work tree, for the directory at prefix: its path below
// the top of the work tree ending in a slash, or "" for the top itself.
// Git names every path from the top. Changes outside the directory only
// make the work tree dirty.
func Parse(out []byte, prefix string) *Status {
	s := &Status{badges: make(map[string]byte)}
	records := strings.Split(string(out), "\x00")
	for i := 0; i < len(records); i++ {
		rec := records[i]
		if rec == "" {
			continue
		}
		var code byte
		var path string
		switch rec[0] {
		case '#':
			s.header(rec)
			continue
		case '1': // 1 XY sub mH mI mW hH hI path
			f := strings.SplitN(rec, " ", 9)
			if len(f) < 9 || len(f[1]) != 2 {
				continue
			}
			code, path = ordinary(f[1]), f[8]
		case '2': // 2 XY sub mH mI mW hH hI Xscore path, then the path it had
			f := strings.SplitN(rec, " ", 10)
			if len(f) < 10 {
				continue
			}
			code, path = Renamed, f[9]
			if f[8] != "" && f[8][0] == 'C' {
				code = Added // A copy: the original is still there
			}
			i++ // The old path is no longer in the work tree under that name
		case 'u': // u XY sub m1 m2 m3 mW h1 h2 h3 path
			f := strings.SplitN(rec, " ", 11)
			if len(f) < 11 {
				continue
			}
			code, path = Conflict, f[10]
		case '?', '!':
			if len(rec) < 3 {
				continue
			}
			code, path = rec[0], rec[2:]
		default:
			continue
		}
		if code != Ignored {
			s.Dirty = true
		}
		s.add(code, path, prefix)
	}
	return s
}

// header reads a "# branch." line
func (s *Status) header(rec string) {
	switch name, value, _ := strings.Cut(strings.TrimPrefix(rec, "# "), " "); name {
	case "branch.oid":
		if value != "(initial)" {
			s.Commit = value
		}
	case "branch.head":
		if value != "(detached)" {
			s.Branch = value
		}
	}
}

// ordinary returns the badge of a changed entry from its XY status: X for
// the index, Y for the work tree
func ordinary(xy string) byte {
	switch {
	case xy[0] == 'A':
		return Added
	case xy[0] == 'D' || xy[1] == 'D':
		return Deleted
	}
	// M, T for a type change, and submodules with changes inside
	return Modified
}

// add records path's badge on the entry of the directory at prefix that it
// is, or is inside of
func (s *Status) add(code byte, path, prefix string) {
	rest, ok := strings.CutPrefix(path, prefix)
	if !ok || rest == "" {
		// An ignored directory listed as a whole that holds the directory,
		// or is it: everything here is ignored too
		if strings.HasSuffix(path, "/") && strings.HasPrefix(prefix, path) {
			s.inherited = stronger(s.inherited, code)
		}
		return
	}
	// Directories are listed with a slash when ignored as a whole
	name, inside, _ := strings.Cut(rest, "/")
	if inside != "" {
		// Something in the directory name: it shows as modified, in
		// conflict, or untracked if all that is new or changed in it is,
		// as in a new folder; what is ignored in it doesn't count
		switch code {
		case Ignored:
			return
		case Conflict, Untracked:
		default:
			code = Modified
		}
	}
	name = norm.NFC.String(name)
	s.badges[name] = stronger(s.badges[name], code)
}
