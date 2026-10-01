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
	Root   string // The top of the work tree, reached from the directory by name
	Branch string // The branch checked out, even before its first commit; "" if HEAD is detached
	Commit string // The commit checked out; "" before the first
	Dirty  bool   // Something in the work tree is changed or untracked

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

// WithoutBadges returns the branch and root of s without the badges, for a
// directory nearby in the same work tree while its own status is read
func (s *Status) WithoutBadges() *Status {
	return &Status{Root: s.Root, Branch: s.Branch, Commit: s.Commit, Dirty: s.Dirty}
}

// Read asks git about dir. It returns ErrNotRepo outside a work tree, and
// gives up when ctx is done, as for a repository too large to read in
// time.
func Read(ctx context.Context, dir string) (*Status, error) {
	out, err := run(ctx, dir, "rev-parse", "--is-inside-work-tree", "--show-prefix")
	if err != nil {
		return nil, err
	}
	// The prefix is dir's path below the top of the work tree, ending in
	// a slash, as git names paths: from the top, whatever dir is called
	inside, prefix, _ := strings.Cut(string(out), "\n")
	if inside != "true" {
		return nil, ErrNotRepo
	}
	prefix = strings.TrimSuffix(prefix, "\n")

	out, err = run(ctx, dir, "status", "--porcelain=v2", "--branch", "--untracked-files=all", "--ignored=matching", "-z")
	if err != nil {
		return nil, err
	}
	s := Parse(out, prefix)
	s.Root = dir
	for range strings.Count(prefix, "/") {
		s.Root = filepath.Dir(s.Root)
	}
	return s, nil
}

// run runs git in dir and returns what it printed
func run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	// Otherwise status refreshes the index, writing to the repository
	// that sushi only looks at, and taking a lock git commands run at the
	// same time would trip over
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
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
		// Something in the directory name: it shows as modified, or in
		// conflict, but what is ignored in it doesn't count
		switch code {
		case Ignored:
			return
		case Conflict:
		default:
			code = Modified
		}
	}
	name = norm.NFC.String(name)
	s.badges[name] = stronger(s.badges[name], code)
}
