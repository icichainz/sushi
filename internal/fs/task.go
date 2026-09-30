package fs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

// chunkSize is how much is copied between checks for cancellation
const chunkSize = 1 << 20

// Progress is how far a task has got
type Progress struct {
	Files, TotalFiles int   // Files done and in all; directories aren't counted
	Bytes, TotalBytes int64 // Bytes done and in all
	Counting          bool  // Still adding up what there is to do
}

// Percent returns how much is done, from 0 to 100, going by bytes when
// they were counted and by files otherwise
func (p Progress) Percent() int {
	var pct int64
	switch {
	case p.TotalBytes > 0:
		pct = p.Bytes * 100 / p.TotalBytes
	case p.TotalFiles > 0:
		pct = int64(p.Files) * 100 / int64(p.TotalFiles)
	}
	return int(min(max(pct, 0), 100))
}

// Task runs file operations that can be cancelled and that report their
// progress. It is used by one goroutine at a time.
type Task struct {
	ctx    context.Context
	every  time.Duration
	report func(Progress)
	last   time.Time
	p      Progress
	buf    []byte
}

// NewTask returns a task that stops when ctx is done and passes its
// progress to report, if not nil, at most once per interval
func NewTask(ctx context.Context, every time.Duration, report func(Progress)) *Task {
	return &Task{ctx: ctx, every: every, report: report, last: time.Now()}
}

// background is the task behind the plain functions, which can't be cancelled
func background() *Task {
	return NewTask(context.Background(), 0, nil)
}

// Err returns the context's error once the task has been cancelled
func (t *Task) Err() error {
	return t.ctx.Err()
}

// Progress returns how far the task has got
func (t *Task) Progress() Progress {
	return t.p
}

// update reports progress if an interval has passed since the last report
func (t *Task) update() {
	if t.report == nil {
		return
	}
	if now := time.Now(); now.Sub(t.last) >= t.every {
		t.last = now
		t.report(t.p)
	}
}

// Count adds the files and bytes under paths to the totals, so progress can
// be shown as a share of the whole. Unreadable entries are skipped: the
// operation itself will report them.
func (t *Task) Count(paths ...string) error {
	return t.count(true, paths)
}

// CountFiles adds the files under paths to the totals, but not their size,
// for operations like deleting whose cost doesn't depend on it
func (t *Task) CountFiles(paths ...string) error {
	return t.count(false, paths)
}

func (t *Task) count(bytes bool, paths []string) error {
	t.p.Counting = true
	defer func() { t.p.Counting = false }()
	for _, root := range paths {
		err := filepath.WalkDir(filepath.Clean(root), func(path string, d os.DirEntry, err error) error {
			if cerr := t.ctx.Err(); cerr != nil {
				return cerr
			}
			if err != nil || d.IsDir() {
				return nil
			}
			t.p.TotalFiles++
			if bytes && d.Type().IsRegular() {
				if info, err := d.Info(); err == nil {
					t.p.TotalBytes += info.Size()
				}
			}
			t.update()
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Copy copies src to dst. A file at dst is replaced only once the copy is
// complete, and a directory at dst is merged into. Symlinks are copied as
// links, and modes and modification times are kept. If the task is
// cancelled, the file being copied is removed, files already copied stay,
// and the error is the context's.
func (t *Task) Copy(src, dst string) error {
	src, dst = filepath.Clean(src), filepath.Clean(dst)
	if err := CheckTransfer(src, dst); err != nil {
		return err
	}
	return t.copyPath(src, dst)
}

// copyPath does the recursive copy once CheckTransfer has passed
func (t *Task) copyPath(src, dst string) error {
	if err := t.ctx.Err(); err != nil {
		return err
	}
	info, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("cannot access source: %w", err)
	}

	switch {
	case info.Mode()&os.ModeSymlink != 0:
		err = copySymlink(src, dst)
	case info.IsDir():
		return t.copyDir(src, dst, info)
	case info.Mode().IsRegular():
		err = t.copyFile(src, dst, info)
	default:
		// Opening a named pipe would wait forever for a writer
		return fmt.Errorf("%s is not a regular file", filepath.Base(src))
	}
	if err != nil {
		return err
	}
	t.p.Files++
	t.update()
	return nil
}

// copyDir copies a directory's contents into dst, creating it if needed
func (t *Task) copyDir(src, dst string, info os.FileInfo) error {
	created := false
	if existing, err := os.Stat(dst); err == nil {
		if !existing.IsDir() {
			return fmt.Errorf("cannot create destination directory: %s is not a directory", filepath.Base(dst))
		}
	} else {
		// Writable until the contents are in, whatever the source's mode
		if err := os.Mkdir(dst, 0700); err != nil {
			return fmt.Errorf("cannot create destination directory: %w", err)
		}
		created = true
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		err = fmt.Errorf("cannot read source directory: %w", err)
	}
	for _, entry := range entries {
		if err = t.copyPath(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
			break
		}
	}

	// Keeping the mode and time is best effort: some filesystems have neither.
	// The mode is set even after a failure, so a partial copy isn't left more
	// open than the original; the time last, as copying in changes it.
	if created {
		os.Chmod(dst, info.Mode().Perm())
		if err == nil {
			os.Chtimes(dst, time.Time{}, info.ModTime())
		}
	}
	return err
}

// copyFile copies a regular file, writing it beside dst and renaming it
// into place, so a failed or cancelled copy never leaves a partial file
// under dst's name or damages the file it would replace
func (t *Task) copyFile(src, dst string, info os.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("cannot open source: %w", err)
	}
	defer in.Close()

	if dstInfo, err := os.Stat(dst); err == nil && os.SameFile(info, dstInfo) {
		return ErrSamePath
	}

	tmp, err := os.CreateTemp(filepath.Dir(dst), partialPrefix+"*")
	if err != nil {
		return fmt.Errorf("cannot create destination: %w", err)
	}
	err = t.copyData(tmp, in, true)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		// Times before the mode, which may make the file read-only
		os.Chtimes(tmp.Name(), time.Time{}, info.ModTime())
		os.Chmod(tmp.Name(), info.Mode().Perm())
		err = os.Rename(tmp.Name(), dst)
	}
	if err != nil {
		os.Remove(tmp.Name())
		if cerr := t.ctx.Err(); cerr != nil {
			return cerr
		}
		return fmt.Errorf("copy failed: %w", err)
	}
	return nil
}

// copyData copies r to w a chunk at a time, stopping if the task is
// cancelled. If count is set, the bytes count towards the progress.
func (t *Task) copyData(w io.Writer, r io.Reader, count bool) error {
	if t.buf == nil {
		t.buf = make([]byte, chunkSize)
	}
	for {
		if err := t.ctx.Err(); err != nil {
			return err
		}
		n, rerr := r.Read(t.buf)
		if n > 0 {
			if _, err := w.Write(t.buf[:n]); err != nil {
				return err
			}
			if count {
				t.p.Bytes += int64(n)
				t.update()
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}

// copySymlink recreates the symlink at src as dst, replacing any existing file
func copySymlink(src, dst string) error {
	target, err := os.Readlink(src)
	if err != nil {
		return fmt.Errorf("cannot read symlink: %w", err)
	}

	if info, err := os.Lstat(dst); err == nil && !info.IsDir() {
		if err := os.Remove(dst); err != nil {
			return fmt.Errorf("cannot replace destination: %w", err)
		}
	}

	if err := os.Symlink(target, dst); err != nil {
		return fmt.Errorf("cannot create symlink: %w", err)
	}
	return nil
}

// Move moves src to dst. On one filesystem this is a rename. Across
// filesystems, or onto an existing directory, src is copied and then
// deleted; if the copy fails or is cancelled, what it created is removed
// and src is left as it was. Deleting src can't be cancelled, so a move
// never stops with the files only half in either place.
func (t *Task) Move(src, dst string) error {
	src, dst = filepath.Clean(src), filepath.Clean(dst)
	if err := CheckTransfer(src, dst); err != nil {
		return err
	}

	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	existing, statErr := os.Stat(dst)
	merge := statErr == nil && existing.IsDir()
	if !merge && !isCrossDevice(err) {
		return err
	}

	t.Count(src)
	if err := t.copyPath(src, dst); err != nil {
		if statErr != nil {
			os.RemoveAll(dst)
		}
		if cerr := t.ctx.Err(); cerr != nil {
			return cerr
		}
		return fmt.Errorf("move failed during copy: %w", err)
	}
	if err := DeletePath(src); err != nil {
		return fmt.Errorf("move failed during cleanup: %w", err)
	}
	return nil
}

// isCrossDevice reports whether a rename failed because the paths are on
// different filesystems
func isCrossDevice(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	if runtime.GOOS == "windows" {
		return errno == 17 // ERROR_NOT_SAME_DEVICE
	}
	return errno == syscall.EXDEV
}

// Delete deletes path, and everything in it if it is a directory, a file
// at a time so it can be cancelled. Symlinks are removed without touching
// their target.
func (t *Task) Delete(path string) error {
	if err := t.ctx.Err(); err != nil {
		return err
	}
	// Lstat follows a link named with a trailing slash, as in "link/", which
	// would delete what is in its target
	path = filepath.Clean(path)
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("cannot access %s: %w", path, err)
	}

	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return fmt.Errorf("cannot read %s: %w", path, err)
		}
		for _, entry := range entries {
			if err := t.Delete(filepath.Join(path, entry.Name())); err != nil {
				return err
			}
		}
		return os.Remove(path)
	}

	if err := os.Remove(path); err != nil {
		return err
	}
	t.p.Files++
	t.update()
	return nil
}

// existsError says something is in the way of putting a file back. It
// matches os.ErrExist, so callers can tell it from failures that moving
// the obstacle won't fix.
type existsError struct {
	name string
}

func (e existsError) Error() string        { return e.name + " already exists" }
func (e existsError) Is(target error) bool { return target == os.ErrExist }

// inTheWay returns the error for something at path being in the way
func inTheWay(path string) error {
	return existsError{filepath.Base(path)}
}

// Restore moves from back to to, where it came from, refusing to replace
// anything that is at to now. Missing parent directories are recreated.
func (t *Task) Restore(from, to string) error {
	from, to = filepath.Clean(from), filepath.Clean(to)
	src, err := os.Lstat(from)
	if err != nil {
		return fmt.Errorf("%s is no longer at %s", filepath.Base(to), from)
	}
	if dst, err := os.Lstat(to); err == nil {
		// A case-only rename finds itself on case-insensitive filesystems
		if !os.SameFile(src, dst) {
			return inTheWay(to)
		}
		return os.Rename(from, to)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(to), 0755); err != nil {
		return err
	}
	return t.Move(from, to)
}
