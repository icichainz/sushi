package fs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCopyFileOntoItselfKeepsContent(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	writeFile(t, f, "important data")

	if err := CopyPath(f, f); !errors.Is(err, ErrSamePath) {
		t.Fatalf("CopyPath(f, f) = %v, want ErrSamePath", err)
	}
	if err := CopyFile(f, f); !errors.Is(err, ErrSamePath) {
		t.Fatalf("CopyFile(f, f) = %v, want ErrSamePath", err)
	}
	if got := readFile(t, f); got != "important data" {
		t.Fatalf("content = %q, file was truncated", got)
	}
}

func TestCopyDirOntoItselfKeepsContent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a")
	os.Mkdir(dir, 0755)
	writeFile(t, filepath.Join(dir, "x.txt"), "x")

	if err := CopyPath(dir, dir); !errors.Is(err, ErrSamePath) {
		t.Fatalf("CopyPath(dir, dir) = %v, want ErrSamePath", err)
	}
	if got := readFile(t, filepath.Join(dir, "x.txt")); got != "x" {
		t.Fatalf("content = %q, file was truncated", got)
	}
}

func TestCopyOntoSymlinkTargetRefused(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	writeFile(t, target, "data")
	link := filepath.Join(dir, "link")
	os.Symlink(target, link)

	// Writing through a link that points back at the source would truncate it
	if err := CopyPath(target, link); !errors.Is(err, ErrSamePath) {
		t.Fatalf("CopyPath(target, link) = %v, want ErrSamePath", err)
	}
	if err := MovePath(link, target); !errors.Is(err, ErrSamePath) {
		t.Fatalf("MovePath(link, target) = %v, want ErrSamePath", err)
	}
	if got := readFile(t, target); got != "data" {
		t.Fatalf("content = %q, target was damaged", got)
	}
}

func TestCopyDirIntoItselfRefused(t *testing.T) {
	src := filepath.Join(t.TempDir(), "a")
	os.Mkdir(src, 0755)

	if err := CopyPath(src, filepath.Join(src, "a")); !errors.Is(err, ErrDestInsideSource) {
		t.Fatalf("CopyPath = %v, want ErrDestInsideSource", err)
	}
	if err := MovePath(src, filepath.Join(src, "sub", "a")); !errors.Is(err, ErrDestInsideSource) {
		t.Fatalf("MovePath = %v, want ErrDestInsideSource", err)
	}
	if Exists(filepath.Join(src, "a")) {
		t.Fatal("nested copy was created")
	}
}

func TestCopyDirIntoSiblingWithSharedPrefix(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a")
	os.Mkdir(src, 0755)
	writeFile(t, filepath.Join(src, "x.txt"), "x")

	// "ab" starts with "a" but is not inside it
	dst := filepath.Join(root, "ab")
	if err := CopyPath(src, dst); err != nil {
		t.Fatalf("CopyPath = %v", err)
	}
	if got := readFile(t, filepath.Join(dst, "x.txt")); got != "x" {
		t.Fatalf("copied content = %q", got)
	}
}

func TestCopyDirRecursive(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	os.MkdirAll(filepath.Join(src, "sub"), 0755)
	writeFile(t, filepath.Join(src, "sub", "f.txt"), "nested")

	dst := filepath.Join(root, "dst")
	if err := CopyPath(src, dst); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dst, "sub", "f.txt")); got != "nested" {
		t.Fatalf("copied content = %q", got)
	}
}

func TestCopySymlinkLoopCopiesLink(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	os.Mkdir(src, 0755)
	// A link back to its own parent would recurse forever if followed
	os.Symlink(src, filepath.Join(src, "loop"))

	dst := filepath.Join(root, "dst")
	if err := CopyPath(src, dst); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(dst, "loop"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink was followed instead of copied")
	}
}

func TestSymlinkIntoTargetDirAllowed(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "dir")
	os.Mkdir(dir, 0755)
	link := filepath.Join(root, "link")
	os.Symlink(dir, link)

	// The link is copied as a link, so placing it inside its target is fine
	if err := CopyPath(link, filepath.Join(dir, "link")); err != nil {
		t.Fatalf("CopyPath = %v", err)
	}
}

func TestMoveOntoItselfRefused(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.txt")
	writeFile(t, f, "data")

	if err := MovePath(f, f); !errors.Is(err, ErrSamePath) {
		t.Fatalf("MovePath(f, f) = %v, want ErrSamePath", err)
	}
	if got := readFile(t, f); got != "data" {
		t.Fatalf("content = %q", got)
	}
}

func TestDeleteDanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "dangling")
	os.Symlink(filepath.Join(dir, "missing"), link)

	if err := DeletePath(link); err != nil {
		t.Fatalf("DeletePath = %v", err)
	}
	if Exists(link) {
		t.Fatal("dangling symlink still exists")
	}
}

// symlink makes a symlink, skipping the test where that needs privileges
func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("can't make symlinks here: %v", err)
	}
}

func TestTrailingSlashNamesTheLinkNotItsTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	os.Mkdir(target, 0755)
	precious := filepath.Join(target, "precious.txt")
	writeFile(t, precious, "keep")
	link := filepath.Join(root, "link")
	slash := link + string(filepath.Separator)
	intact := func(what string) {
		t.Helper()
		if readFile(t, precious) != "keep" || !Exists(target) {
			t.Fatalf("%s touched the link's target", what)
		}
	}

	symlink(t, target, link)
	if err := background().Delete(slash); err != nil || Exists(link) {
		t.Fatalf("Delete: %v", err)
	}
	intact("Delete")

	symlink(t, target, link)
	if err := DeletePath(slash); err != nil || Exists(link) {
		t.Fatalf("DeletePath: %v", err)
	}
	intact("DeletePath")

	symlink(t, target, link)
	moved := filepath.Join(root, "moved")
	if err := MovePath(slash, moved); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(moved); err != nil || info.Mode()&os.ModeSymlink == 0 || Exists(link) {
		t.Fatalf("Move moved more than the link: %v", err)
	}
	intact("Move")

	copied := filepath.Join(root, "copied")
	if err := CopyPath(moved+string(filepath.Separator), copied); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(copied); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("Copy copied the target rather than the link: %v", err)
	}
	intact("Copy")
}

func TestDeleteSymlinkKeepsTarget(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "dir")
	os.Mkdir(dir, 0755)
	writeFile(t, filepath.Join(dir, "keep.txt"), "keep")
	link := filepath.Join(root, "link")
	os.Symlink(dir, link)

	if err := DeletePath(link); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dir, "keep.txt")); got != "keep" {
		t.Fatalf("target content = %q", got)
	}
}
