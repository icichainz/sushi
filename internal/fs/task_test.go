package fs

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// cancelWhen returns a task that reports every change and is cancelled
// as soon as stop says so
func cancelWhen(stop func(Progress) bool) (*Task, *[]Progress) {
	ctx, cancel := context.WithCancel(context.Background())
	var seen []Progress
	t := NewTask(ctx, 0, func(p Progress) {
		seen = append(seen, p)
		if stop(p) {
			cancel()
		}
	})
	return t, &seen
}

// leftovers lists the partial files a copy left in dir
func leftovers(t *testing.T, dir string) []string {
	t.Helper()
	var found []string
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && strings.HasPrefix(d.Name(), ".sushi-") {
			found = append(found, path)
		}
		return nil
	})
	return found
}

func TestCopyKeepsModesAndTimes(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	os.Mkdir(src, 0755)
	file := filepath.Join(src, "script.sh")
	writeFile(t, file, "#!/bin/sh\n")
	old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	os.Chmod(file, 0750)
	os.Chtimes(file, old, old)
	os.Chmod(src, 0750)
	os.Chtimes(src, old, old)

	dst := filepath.Join(root, "dst")
	if err := CopyPath(src, dst); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{dst, filepath.Join(dst, "script.sh")} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0750 {
			t.Errorf("%s: mode %v, want 0750", filepath.Base(p), info.Mode().Perm())
		}
		if !info.ModTime().Equal(old) {
			t.Errorf("%s: modified %v, want %v", filepath.Base(p), info.ModTime(), old)
		}
	}
}

func TestCopyReportsProgress(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	os.MkdirAll(filepath.Join(src, "sub"), 0755)
	writeFile(t, filepath.Join(src, "a"), "12345")
	writeFile(t, filepath.Join(src, "sub", "b"), "123")
	os.Symlink("a", filepath.Join(src, "link"))

	task, seen := cancelWhen(func(Progress) bool { return false })
	if err := task.Count(src); err != nil {
		t.Fatal(err)
	}
	if p := task.Progress(); p.TotalFiles != 3 || p.TotalBytes != 8 || p.Counting {
		t.Fatalf("after counting: %+v, want 3 files and 8 bytes", p)
	}
	if err := task.Copy(src, filepath.Join(root, "dst")); err != nil {
		t.Fatal(err)
	}
	if p := task.Progress(); p.Files != 3 || p.Bytes != 8 || p.Percent() != 100 {
		t.Fatalf("after copying: %+v", p)
	}
	if len(*seen) == 0 {
		t.Fatal("no progress reported")
	}
}

func TestProgressIsThrottled(t *testing.T) {
	src := t.TempDir()
	for _, name := range []string{"a", "b", "c", "d"} {
		writeFile(t, filepath.Join(src, name), "x")
	}
	reports := 0
	task := NewTask(context.Background(), time.Hour, func(Progress) { reports++ })
	task.Count(src)
	task.Copy(src, filepath.Join(t.TempDir(), "dst"))
	if reports != 0 {
		t.Fatalf("%d reports within the interval, want none", reports)
	}
}

func TestPercent(t *testing.T) {
	for _, c := range []struct {
		p    Progress
		want int
	}{
		{Progress{}, 0},
		{Progress{Bytes: 50, TotalBytes: 200, Files: 9, TotalFiles: 10}, 25},
		{Progress{Files: 1, TotalFiles: 4}, 25},
		{Progress{Bytes: 500, TotalBytes: 200}, 100},
	} {
		if got := c.p.Percent(); got != c.want {
			t.Errorf("%+v: %d%%, want %d%%", c.p, got, c.want)
		}
	}
}

func TestCancelledCopyRemovesOnlyThePartialFile(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	os.Mkdir(src, 0755)
	writeFile(t, filepath.Join(src, "1-small"), "done before the cancel")
	big := bytes.Repeat([]byte("x"), 3*chunkSize)
	os.WriteFile(filepath.Join(src, "2-big"), big, 0644)
	writeFile(t, filepath.Join(src, "3-later"), "never reached")

	// Cancel once the big file is partly written
	task, _ := cancelWhen(func(p Progress) bool { return p.Bytes > int64(len("done before the cancel"))+chunkSize })
	task.Count(src)
	dst := filepath.Join(root, "dst")
	err := task.Copy(src, dst)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}

	if got := readFile(t, filepath.Join(dst, "1-small")); got != "done before the cancel" {
		t.Fatalf("completed file = %q", got)
	}
	for _, name := range []string{"2-big", "3-later"} {
		if Exists(filepath.Join(dst, name)) {
			t.Errorf("%s exists after the cancel", name)
		}
	}
	if left := leftovers(t, dst); len(left) != 0 {
		t.Fatalf("partial files left behind: %v", left)
	}
	if p := task.Progress(); p.Files != 1 || p.TotalFiles != 3 {
		t.Fatalf("progress = %+v, want 1 of 3 files", p)
	}
}

func TestCancelledOverwriteKeepsTheOldFile(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "new")
	os.WriteFile(src, bytes.Repeat([]byte("n"), 2*chunkSize), 0644)
	dst := filepath.Join(root, "old")
	writeFile(t, dst, "precious")

	task, _ := cancelWhen(func(p Progress) bool { return p.Bytes > 0 })
	if err := task.Copy(src, dst); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if got := readFile(t, dst); got != "precious" {
		t.Fatalf("destination = %q, want it untouched by the cancelled copy", got[:min(len(got), 20)])
	}
	if left := leftovers(t, root); len(left) != 0 {
		t.Fatalf("partial files left behind: %v", left)
	}
}

func TestCancelBeforeStartDoesNothing(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a")
	writeFile(t, src, "x")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	task := NewTask(ctx, 0, nil)

	if err := task.Copy(src, filepath.Join(root, "b")); !errors.Is(err, context.Canceled) || Exists(filepath.Join(root, "b")) {
		t.Fatalf("copy: err=%v, b exists=%v", err, Exists(filepath.Join(root, "b")))
	}
	if err := task.Delete(src); !errors.Is(err, context.Canceled) || !Exists(src) {
		t.Fatalf("delete: err=%v, a exists=%v", err, Exists(src))
	}
}

func TestCopyRefusesSpecialFiles(t *testing.T) {
	// A socket stands in for pipes and devices, which would block or fail.
	// Socket paths have a short length limit, hence the relative one.
	t.Chdir(t.TempDir())
	l, err := net.Listen("unix", "sock")
	if err != nil {
		t.Skip("can't create a socket here:", err)
	}
	defer l.Close()
	err = CopyPath("sock", "copy")
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("err = %v", err)
	}
	if Exists("copy") {
		t.Fatal("a copy was created")
	}
}

func TestDeleteReportsAndCancels(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "dir")
	os.MkdirAll(filepath.Join(dir, "sub"), 0755)
	for _, p := range []string{"a", "b", filepath.Join("sub", "c")} {
		writeFile(t, filepath.Join(dir, p), "x")
	}

	task, _ := cancelWhen(func(p Progress) bool { return p.Files == 1 })
	task.CountFiles(dir)
	if p := task.Progress(); p.TotalFiles != 3 || p.TotalBytes != 0 {
		t.Fatalf("counted %+v, want 3 files and no bytes", p)
	}
	if err := task.Delete(dir); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if Exists(filepath.Join(dir, "a")) || !Exists(filepath.Join(dir, "b")) {
		t.Fatal("delete should stop after the first file")
	}

	task = background()
	if err := task.Delete(dir); err != nil || Exists(dir) {
		t.Fatalf("second delete: err=%v, exists=%v", err, Exists(dir))
	}
	if task.Progress().Files != 2 {
		t.Fatalf("deleted %d files, want 2", task.Progress().Files)
	}
}

func TestMoveOntoDirectoryMerges(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src", "d")
	os.MkdirAll(src, 0755)
	writeFile(t, filepath.Join(src, "new"), "n")
	dst := filepath.Join(root, "d")
	os.Mkdir(dst, 0755)
	writeFile(t, filepath.Join(dst, "old"), "o")

	if err := MovePath(src, dst); err != nil {
		t.Fatal(err)
	}
	if !Exists(filepath.Join(dst, "old")) || !Exists(filepath.Join(dst, "new")) || Exists(src) {
		t.Fatal("move onto a directory should merge into it and remove the source")
	}
}

func TestRestoreRefusesToReplace(t *testing.T) {
	root := t.TempDir()
	from, to := filepath.Join(root, "renamed"), filepath.Join(root, "original")
	writeFile(t, from, "mine")
	writeFile(t, to, "someone else's")

	err := background().Restore(from, to)
	if err == nil || err.Error() != "original already exists" || !errors.Is(err, os.ErrExist) {
		t.Fatalf("err = %v, want one that matches os.ErrExist", err)
	}
	if readFile(t, to) != "someone else's" || readFile(t, from) != "mine" {
		t.Fatal("restore touched the files")
	}

	os.Remove(to)
	nested := filepath.Join(root, "gone", "parent", "original")
	if err := background().Restore(from, nested); err != nil || readFile(t, nested) != "mine" {
		t.Fatalf("restore into a missing folder: %v", err)
	}
	if err := background().Restore(from, to); err == nil || !strings.Contains(err.Error(), "no longer") {
		t.Fatalf("restoring something missing: %v", err)
	}
}

func TestRestoreChangesCaseOnly(t *testing.T) {
	dir := t.TempDir()
	upper := filepath.Join(dir, "README")
	writeFile(t, upper, "x")
	lower := filepath.Join(dir, "readme")
	// Works whether or not the filesystem ignores case
	if err := background().Restore(upper, lower); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "readme" {
		t.Fatalf("entries = %v", entries)
	}
}
