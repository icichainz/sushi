package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/icichainz/sushi/internal/git/gittest"
	"github.com/icichainz/sushi/internal/testutil"
)

// porcelain joins records as git status -z prints them
func porcelain(records ...string) []byte {
	return []byte(strings.Join(records, "\x00") + "\x00")
}

// badges returns the badge of each name, with "." for none
func badges(s *Status, names ...string) string {
	var b strings.Builder
	for _, name := range names {
		if c := s.Badge(name); c != 0 {
			b.WriteByte(c)
		} else {
			b.WriteByte('.')
		}
	}
	return b.String()
}

func TestParse(t *testing.T) {
	out := porcelain(
		"# branch.oid 0123456789abcdef0123456789abcdef01234567",
		"# branch.head main",
		"# branch.upstream origin/main",
		"# branch.ab +1 -0",
		"1 .M N... 100644 100644 100644 aaaa bbbb tracked.txt",
		"1 A. N... 000000 100644 100644 0000 cccc added file.txt", // A space in the name
		"2 R. N... 100644 100644 100644 dddd dddd R100 new.txt", "old.txt",
		"2 C. N... 100644 100644 100644 dddd dddd C75 copy.txt", "tracked.txt",
		"u UU N... 100644 100644 100644 100644 e1 e2 e3 both.txt",
		"? untracked.txt",
		"! debug.log",
		"! build/",
		"1 .M N... 100644 100644 100644 ffff ffff sub/inner.txt",
		"! sub/inner.o",
		"? newdir/deep/x.txt",
		"! logs/a.log",
		"u AA N... 000000 100644 100644 100644 0 e2 e3 merge/clash.txt",
		"1 .M SC.. 160000 160000 160000 9999 9999 module",
		"1 D. N... 100644 000000 000000 abab 0000 keep.txt", // git rm --cached: still on disk
		"? keep.txt",
		"1 .D N... 100644 100644 000000 cdcd cdcd gone.txt",
		"1 .T N... 100644 100644 120000 efef efef typed",
		"? mixed/new.txt", // Untracked beside a change: modified
		"1 .M N... 100644 100644 100644 abab abab mixed/old.txt",
	)

	s := Parse(out, "")
	if s.Branch != "main" || !strings.HasPrefix(s.Commit, "0123456") || s.Head() != "main" || !s.Dirty {
		t.Fatalf("branch=%q commit=%q head=%q dirty=%v", s.Branch, s.Commit, s.Head(), s.Dirty)
	}
	names := []string{"tracked.txt", "added file.txt", "new.txt", "old.txt", "copy.txt", "both.txt", "untracked.txt", "debug.log", "build",
		"sub", "newdir", "logs", "merge", "module", "keep.txt", "gone.txt", "typed", "clean.txt", "mixed"}
	// A folder with nothing but untracked files in it is untracked
	if got, want := badges(s, names...), "MAR.AU?!!M?.UMDDM.M"; got != want {
		t.Errorf("badges of %q\n = %s\nwant %s", names, got, want)
	}

	// From a subdirectory, its own entries; the rest makes it dirty
	s = Parse(out, "sub/")
	if got := badges(s, "inner.txt", "inner.o", "tracked.txt"); got != "M!." || !s.Dirty || s.Head() != "main" {
		t.Errorf("in sub: %s, dirty %v", got, s.Dirty)
	}
	if got := badges(Parse(out, "newdir/"), "deep"); got != "?" {
		t.Errorf("in newdir: %s", got)
	}

	// In an ignored directory, or one inside it, everything is ignored
	for _, prefix := range []string{"build/", "build/out/"} {
		if got := badges(Parse(out, prefix), "a.o", "b"); got != "!!" {
			t.Errorf("in %s: %s", prefix, got)
		}
	}
	// A directory whose name starts like an ignored one's isn't in it
	if got := badges(Parse(out, "builder/"), "a.o"); got != "." {
		t.Errorf("in builder/: %s", got)
	}
}

func TestParseHeads(t *testing.T) {
	// No commits yet: the branch is named, there is no commit
	s := Parse(porcelain("# branch.oid (initial)", "# branch.head main", "? a.txt"), "")
	if s.Head() != "main" || s.Commit != "" || !s.Dirty || s.Badge("a.txt") != Untracked {
		t.Errorf("initial: head=%q commit=%q dirty=%v", s.Head(), s.Commit, s.Dirty)
	}
	// A detached HEAD is named by its commit
	s = Parse(porcelain("# branch.oid 89abcdef0123456789abcdef0123456789abcdef", "# branch.head (detached)"), "")
	if s.Head() != "(89abcde)" || s.Branch != "" || s.Dirty {
		t.Errorf("detached: head=%q branch=%q dirty=%v", s.Head(), s.Branch, s.Dirty)
	}
	// Ignored files don't make it dirty
	if s := Parse(porcelain("# branch.head main", "! x.log", "! build/"), ""); s.Dirty {
		t.Error("ignored files made the work tree dirty")
	}
	// Nothing at all, and records it doesn't know, are fine
	if s := Parse(porcelain("# stash 2", "x unknown", "1 short", "2 short", "u short", "?", ""), ""); s.Dirty || s.Head() != "" {
		t.Errorf("odd records: %+v", s)
	}
	var none *Status
	if none.Badge("a") != 0 {
		t.Error("a nil status has badges")
	}
}

func TestParseComposesNames(t *testing.T) {
	// Git on macOS reports "é" composed; a name read from disk may not be
	s := Parse(porcelain("? café.txt"), "")
	if s.Badge("café.txt") != Untracked || s.Badge("café.txt") != Untracked {
		t.Error("decomposed name has no badge")
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, dir string) *Status {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := Read(ctx, dir)
	if err != nil {
		t.Fatalf("Read(%s): %v", dir, err)
	}
	return s
}

func TestRead(t *testing.T) {
	gittest.Isolate(t)
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	os.Mkdir(repo, 0755)
	gittest.Init(t, repo)

	// No commits yet
	write(t, filepath.Join(repo, "first.txt"), "1")
	if s := read(t, repo); s.Head() != "main" || s.Commit != "" || s.Badge("first.txt") != Untracked || s.Root != repo {
		t.Fatalf("initial: head=%q commit=%q badge=%c root=%s", s.Head(), s.Commit, s.Badge("first.txt"), s.Root)
	}

	write(t, filepath.Join(repo, ".gitignore"), "*.log\nbuild/\n")
	for _, name := range []string{"tracked.txt", "clean.txt", "old.txt", "keep.txt", "sub/inner.txt", "clean/c.txt"} {
		write(t, filepath.Join(repo, name), name)
	}
	gittest.Run(t, repo, "add", ".")
	gittest.Run(t, repo, "commit", "-qm", "first")

	write(t, filepath.Join(repo, "tracked.txt"), "changed")
	write(t, filepath.Join(repo, "added.txt"), "new")
	gittest.Run(t, repo, "add", "added.txt")
	gittest.Run(t, repo, "mv", "old.txt", "new.txt")
	gittest.Run(t, repo, "rm", "-q", "--cached", "keep.txt")
	write(t, filepath.Join(repo, "untracked.txt"), "")
	write(t, filepath.Join(repo, "debug.log"), "")
	write(t, filepath.Join(repo, "build", "out", "x.bin"), "")
	write(t, filepath.Join(repo, "sub", "inner.txt"), "changed")
	write(t, filepath.Join(repo, "newdir", "x.txt"), "")

	s := read(t, repo)
	names := []string{"tracked.txt", "added.txt", "new.txt", "keep.txt", "untracked.txt", "debug.log", "build", "sub", "newdir", "clean", "clean.txt", ".gitignore"}
	if got, want := badges(s, names...), "MARD?!!M?..."; got != want {
		t.Errorf("badges of %q\n = %s\nwant %s", names, got, want)
	}
	if s.Head() != "main" || !s.Dirty || len(s.Commit) != 40 || s.Root != repo {
		t.Errorf("head=%q dirty=%v commit=%q root=%s", s.Head(), s.Dirty, s.Commit, s.Root)
	}

	// From below the top, by paths from there
	s = read(t, filepath.Join(repo, "sub"))
	if got := badges(s, "inner.txt"); got != "M" || s.Root != repo {
		t.Errorf("in sub: %s, root %s", got, s.Root)
	}
	if got := badges(read(t, filepath.Join(repo, "build", "out")), "x.bin"); got != "!" {
		t.Errorf("in an ignored directory: %s", got)
	}

	// Outside a work tree, and in the .git directory, there is none
	ctx := context.Background()
	for _, dir := range []string{root, filepath.Join(repo, ".git")} {
		if _, err := Read(ctx, dir); !errors.Is(err, ErrNotRepo) {
			t.Errorf("Read(%s) = %v, want ErrNotRepo", dir, err)
		}
	}

	// A detached HEAD
	gittest.Run(t, repo, "stash", "-q", "--include-untracked")
	gittest.Run(t, repo, "checkout", "-q", "--detach")
	head := strings.TrimSpace(gittest.Run(t, repo, "rev-parse", "HEAD"))
	if s := read(t, repo); s.Head() != "("+head[:7]+")" || s.Dirty {
		t.Errorf("detached: head=%q dirty=%v", s.Head(), s.Dirty)
	}
	gittest.Run(t, repo, "checkout", "-q", "main")

	// A linked worktree has its own branch and changes
	wt := filepath.Join(root, "wt")
	gittest.Run(t, repo, "worktree", "add", "-q", "-b", "feature", wt)
	write(t, filepath.Join(wt, "clean.txt"), "changed in the worktree")
	if s := read(t, wt); s.Head() != "feature" || s.Badge("clean.txt") != Modified || s.Root != wt {
		t.Errorf("worktree: head=%q badge=%c root=%s", s.Head(), s.Badge("clean.txt"), s.Root)
	}
	if s := read(t, repo); s.Badge("clean.txt") != 0 {
		t.Error("a change in the worktree shows in the main one")
	}

	// Where each keeps HEAD and the index, and where the branches are
	same := func(a, b string) bool {
		ra, _ := filepath.EvalSymlinks(a)
		rb, _ := filepath.EvalSymlinks(b)
		return ra != "" && ra == rb
	}
	dotGit := filepath.Join(repo, ".git")
	if s := read(t, filepath.Join(repo, "sub")); !same(s.GitDir, dotGit) || !same(s.CommonDir, dotGit) {
		t.Errorf("main: git dir %s, common dir %s", s.GitDir, s.CommonDir)
	}
	if s := read(t, wt); !same(s.GitDir, filepath.Join(dotGit, "worktrees", "wt")) || !same(s.CommonDir, dotGit) {
		t.Errorf("worktree: git dir %s, common dir %s", s.GitDir, s.CommonDir)
	}
}

func TestReadNamesWithNewlines(t *testing.T) {
	gittest.Isolate(t)
	root := filepath.Join(t.TempDir(), "odd\nrepo")
	sub := filepath.Join(root, "two\nlines")
	write(t, filepath.Join(sub, "a.txt"), "a")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Skipf("no newlines in names here: %v", err)
	}
	gittest.Init(t, root)
	s := read(t, sub)
	if s.Root != root || s.Badge("a.txt") != Untracked || !strings.HasSuffix(s.GitDir, "odd\nrepo/.git") {
		t.Fatalf("root=%q badge=%c git dir=%q", s.Root, s.Badge("a.txt"), s.GitDir)
	}
}

func TestReadSubmodule(t *testing.T) {
	gittest.Isolate(t)
	root := t.TempDir()
	lib, repo := filepath.Join(root, "lib"), filepath.Join(root, "repo")
	os.Mkdir(lib, 0755)
	os.Mkdir(repo, 0755)
	gittest.Init(t, lib)
	write(t, filepath.Join(lib, "lib.go"), "package lib")
	gittest.Run(t, lib, "add", ".")
	gittest.Run(t, lib, "commit", "-qm", "lib")
	gittest.Init(t, repo)
	// Newer versions of git refuse local submodules unless told otherwise
	gittest.Run(t, repo, "-c", "protocol.file.allow=always", "submodule", "add", "-q", lib, "lib")
	gittest.Run(t, repo, "commit", "-qm", "with lib")

	if s := read(t, repo); s.Badge("lib") != 0 || s.Dirty {
		t.Fatalf("clean submodule: badge=%c dirty=%v", s.Badge("lib"), s.Dirty)
	}
	// A change inside shows on the file, where the submodule is a
	// repository of its own, but not in its parent, as git status would
	// have to go inside, where the submodule's own configuration applies
	write(t, filepath.Join(repo, "lib", "lib.go"), "package lib // changed")
	if s := read(t, repo); s.Badge("lib") != 0 {
		t.Errorf("changed submodule: badge=%c", s.Badge("lib"))
	}
	if s := read(t, filepath.Join(repo, "lib")); s.Badge("lib.go") != Modified || s.Root != filepath.Join(repo, "lib") {
		t.Errorf("in the submodule: badge=%c root=%s", s.Badge("lib.go"), s.Root)
	}
	// Committed there, the submodule's commit changes, which shows
	gittest.Run(t, filepath.Join(repo, "lib"), "commit", "-qam", "changed")
	if s := read(t, repo); s.Badge("lib") != Modified || !s.Dirty {
		t.Errorf("submodule at another commit: badge=%c dirty=%v", s.Badge("lib"), s.Dirty)
	}
}

// hookScript writes a script that records each run in the file marker,
// and passes what it is given through, as a filter that changes nothing
func hookScript(t *testing.T, dir, marker string) string {
	t.Helper()
	path := filepath.Join(dir, "hook")
	testutil.Script(t, path, "#!/bin/sh\n"+testutil.Warm+"echo \"$*\" >> '"+marker+"'\ncat\n")
	return path
}

// racy changes a committed file without changing its size, so git can only
// tell by reading it, through the filters the attributes give it
func racy(t *testing.T, path string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b[0] ^= 1
	write(t, path, string(b))
	later := time.Now().Add(time.Minute)
	os.Chtimes(path, later, later)
}

func TestReadRunsNothingTheRepositoryConfigures(t *testing.T) {
	for _, c := range []struct {
		name   string
		config func(repo, hook string)
		proven bool // Plain git status runs the hook, which proves the setup
	}{
		{"fsmonitor", func(repo, hook string) { gittest.Run(t, repo, "config", "core.fsmonitor", hook) }, true},
		{"clean filter", func(repo, hook string) { gittest.Run(t, repo, "config", "filter.evil.clean", hook) }, true},
		{"process filter", func(repo, hook string) { gittest.Run(t, repo, "config", "filter.evil.process", hook) }, false},
		{"included", func(repo, hook string) {
			write(t, filepath.Join(repo, "extra.cfg"), "[filter \"evil\"]\n\tclean = "+hook+"\n")
			gittest.Run(t, repo, "config", "include.path", "../extra.cfg")
		}, true},
		{"worktree configuration", func(repo, hook string) {
			gittest.Run(t, repo, "config", "extensions.worktreeConfig", "true")
			gittest.Run(t, repo, "config", "--worktree", "core.fsmonitor", hook)
		}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			gittest.Isolate(t)
			root := t.TempDir()
			marker := filepath.Join(root, "ran")
			hook := hookScript(t, root, marker)
			repo := filepath.Join(root, "repo")
			write(t, filepath.Join(repo, "a.txt"), "aaaa")
			write(t, filepath.Join(repo, "sub", "b.txt"), "b")
			write(t, filepath.Join(repo, ".gitattributes"), "* filter=evil\n")
			gittest.Init(t, repo)
			gittest.Run(t, repo, "add", ".")
			gittest.Run(t, repo, "commit", "-qm", "first")
			c.config(repo, hook)
			racy(t, filepath.Join(repo, "a.txt"))

			ran := func() {
				t.Helper()
				if b, err := os.ReadFile(marker); err == nil {
					t.Fatalf("reading the status ran the repository's command: %q", b)
				}
			}
			s := read(t, repo)
			ran()
			if !s.Restricted || s.Head() != "main" || len(s.Commit) != 40 || s.Badge("a.txt") != 0 || s.Dirty || s.Root != repo {
				t.Fatalf("restricted=%v head=%q commit=%q badge=%c dirty=%v root=%s", s.Restricted, s.Head(), s.Commit, s.Badge("a.txt"), s.Dirty, s.Root)
			}
			// Nor below the top, or with HEAD detached, as written by hand,
			// since git checkout would run the command itself
			if err := os.WriteFile(filepath.Join(repo, ".git", "HEAD"), []byte(s.Commit+"\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if s := read(t, filepath.Join(repo, "sub")); !s.Restricted || s.Head() != "("+s.Commit[:7]+")" || s.Root != repo {
				t.Fatalf("detached, below the top: restricted=%v head=%q root=%s", s.Restricted, s.Head(), s.Root)
			}
			ran()
			if c.proven {
				gittest.Run(t, repo, "status")
				if _, err := os.Stat(marker); err != nil {
					t.Fatal("git status itself doesn't run the command, so the test proves nothing")
				}
			}
		})
	}
}

func TestReadKeepsTheUsersOwnConfiguration(t *testing.T) {
	gittest.Isolate(t)
	root := t.TempDir()
	marker := filepath.Join(root, "ran")
	hook := hookScript(t, root, marker)
	repo := filepath.Join(root, "repo")
	write(t, filepath.Join(repo, "a.txt"), "aaaa")
	gittest.Init(t, repo)
	gittest.Run(t, repo, "add", ".")
	gittest.Run(t, repo, "commit", "-qm", "first")
	racy(t, filepath.Join(repo, "a.txt"))

	// A monitor the user set up for every repository: status runs, without
	// it
	gittest.Run(t, repo, "config", "--global", "core.fsmonitor", hook)
	s := read(t, repo)
	if s.Restricted || s.Badge("a.txt") != Modified {
		t.Fatalf("restricted=%v badge=%c", s.Restricted, s.Badge("a.txt"))
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the file system monitor ran")
	}
}

func TestReadDoesntRunSubmodulesFilters(t *testing.T) {
	gittest.Isolate(t)
	root := t.TempDir()
	marker := filepath.Join(root, "ran")
	hook := hookScript(t, root, marker)
	lib, repo := filepath.Join(root, "lib"), filepath.Join(root, "repo")
	write(t, filepath.Join(lib, "lib.go"), "package lib")
	write(t, filepath.Join(lib, ".gitattributes"), "* filter=evil\n")
	gittest.Init(t, lib)
	gittest.Run(t, lib, "add", ".")
	gittest.Run(t, lib, "commit", "-qm", "lib")
	os.Mkdir(repo, 0755)
	gittest.Init(t, repo)
	gittest.Run(t, repo, "-c", "protocol.file.allow=always", "submodule", "add", "-q", lib, "lib")
	gittest.Run(t, repo, "commit", "-qm", "with lib")

	// The submodule's own configuration, which the parent's doesn't show
	gittest.Run(t, filepath.Join(repo, "lib"), "config", "filter.evil.clean", hook)
	racy(t, filepath.Join(repo, "lib", "lib.go"))
	if s := read(t, repo); s.Restricted {
		t.Fatal("the parent is restricted by its submodule's configuration")
	}
	if s := read(t, filepath.Join(repo, "lib")); !s.Restricted {
		t.Fatal("the submodule isn't restricted by its own configuration")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("reading the status ran the submodule's filter")
	}
}

func TestReadIgnoresInheritedGitVariables(t *testing.T) {
	gittest.Isolate(t)
	root := t.TempDir()
	repo, other := filepath.Join(root, "repo"), filepath.Join(root, "other")
	write(t, filepath.Join(repo, "a.txt"), "a")
	write(t, filepath.Join(other, "b.txt"), "b")
	gittest.Init(t, repo)

	// As in a hook, or a shell git started: every folder would be repo
	t.Setenv("GIT_DIR", filepath.Join(repo, ".git"))
	t.Setenv("GIT_WORK_TREE", repo)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(repo, ".git", "index"))
	t.Setenv("GIT_COMMON_DIR", filepath.Join(repo, ".git"))
	t.Setenv("GIT_OBJECT_DIRECTORY", filepath.Join(repo, ".git", "objects"))
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.worktree")
	t.Setenv("GIT_CONFIG_VALUE_0", repo)
	if _, err := Read(context.Background(), other); !errors.Is(err, ErrNotRepo) {
		t.Fatalf("Read(other) = %v, want ErrNotRepo", err)
	}
	if s := read(t, repo); s.Badge("a.txt") != Untracked || s.Root != repo {
		t.Fatalf("in the repository: badge=%c root=%s", s.Badge("a.txt"), s.Root)
	}
}

func TestReadGivesUpInTime(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("uses a shell script")
	}
	// A git that takes far too long, as on a huge repository
	bin := t.TempDir()
	testutil.Script(t, filepath.Join(bin, "git"), "#!/bin/sh\n"+testutil.Warm+"exec "+sleep+" 30\n")
	t.Setenv("PATH", bin)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := Read(ctx, t.TempDir()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the deadline", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("gave up after %v", d)
	}

	// Without git at all
	t.Setenv("PATH", t.TempDir())
	if _, err := Read(context.Background(), t.TempDir()); err == nil {
		t.Fatal("no error without git")
	}
}

func TestLimitedBuffer(t *testing.T) {
	stopped := false
	b := limitedBuffer{limit: 4, full: func() { stopped = true }}
	if _, err := b.Write([]byte("12345")); err == nil || !b.over || !stopped {
		t.Fatal("a write past the limit succeeded")
	}
	quiet := limitedBuffer{limit: 4}
	if n, err := quiet.Write([]byte("123456")); n != 6 || err != nil || quiet.String() != "1234" {
		t.Fatalf("wrote %d (%v), kept %q", n, err, quiet.String())
	}
}
