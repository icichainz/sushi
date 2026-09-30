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
	copied *[]copied // What a move has copied so far, while it copies
}

// copied is an entry of a move's source that has been copied, as it was
// before the copy: once the whole copy is done, it is deleted if it is
// still as it was
type copied struct {
	path string
	info os.FileInfo
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

// Copy copies src to dst. A file or symlink at dst is replaced only once
// the copy is complete, and a directory at dst is merged into; a symlink
// where a directory is copied is replaced, never followed. Anything that
// turns up at dst while the copy runs is kept, and the copy of that entry
// fails. Symlinks are copied as links, and modes and modification times
// are kept. If the task is cancelled, the file being copied is removed,
// files already copied stay, and the error is the context's.
func (t *Task) Copy(src, dst string) error {
	src, dst = filepath.Clean(src), filepath.Clean(dst)
	if err := CheckTransfer(src, dst); err != nil {
		return err
	}
	return t.copyPath(src, dst)
}

// CopyNew copies src to dst, which must not exist, like Copy but never
// merging into or replacing anything: for a copy beside the original,
// under a name picked as free. If something has taken the name meanwhile,
// nothing is copied and the error matches ErrNotCreated as well as
// os.ErrExist, so the caller knows what is at dst isn't its copy.
func (t *Task) CopyNew(src, dst string) error {
	src, dst = filepath.Clean(src), filepath.Clean(dst)
	if err := CheckTransfer(src, dst); err != nil {
		return notCreated{err}
	}
	info, err := os.Lstat(src)
	if err != nil {
		return notCreated{fmt.Errorf("cannot access source: %w", err)}
	}
	if info.IsDir() {
		// Writable until the contents are in, whatever the source's mode
		if err := os.Mkdir(dst, 0700); err != nil {
			return notCreated{fmt.Errorf("cannot create destination directory: %w", err)}
		}
		return t.copyInto(src, dst, info, true)
	}
	// A file or link is renamed into place only if the name is still free
	err = t.copyEntry(src, dst, info, false)
	if errors.Is(err, os.ErrExist) {
		return notCreated{err}
	}
	return err
}

// copyPath does the recursive copy once CheckTransfer has passed. What is
// at dst now may be replaced; see Copy.
func (t *Task) copyPath(src, dst string) error {
	if err := t.ctx.Err(); err != nil {
		return err
	}
	info, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("cannot access source: %w", err)
	}
	_, dstErr := os.Lstat(dst)
	return t.copyEntry(src, dst, info, dstErr == nil)
}

// copyEntry copies src, which info describes, to dst. Unless replace is
// set, dst must not exist, and nothing that turns up there is replaced.
func (t *Task) copyEntry(src, dst string, info os.FileInfo, replace bool) error {
	var err error
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		err = copySymlink(src, dst, replace)
	case info.IsDir():
		return t.copyDir(src, dst, info)
	case info.Mode().IsRegular():
		err = t.copyFile(src, dst, info, replace)
	default:
		// Opening a named pipe would wait forever for a writer
		return fmt.Errorf("%s is not a regular file", filepath.Base(src))
	}
	if err != nil {
		return err
	}
	t.done(src, info)
	t.p.Files++
	t.update()
	return nil
}

// done records that src, as info describes it from before the copy, has
// been copied, when a move is copying
func (t *Task) done(src string, info os.FileInfo) {
	if t.copied != nil {
		*t.copied = append(*t.copied, copied{src, info})
	}
}

// copyDir copies a directory's contents into dst, creating it if needed. A
// directory at dst is merged into. A symlink at dst is replaced by a new
// directory rather than followed, which would put the copy, and for a move
// the files it then deletes, wherever the link leads.
func (t *Task) copyDir(src, dst string, info os.FileInfo) error {
	existing, err := os.Lstat(dst)
	if err == nil && existing.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(dst); err != nil {
			return fmt.Errorf("cannot replace the symlink %s: %w", filepath.Base(dst), err)
		}
		err = os.ErrNotExist
	}
	switch {
	case err == nil && !existing.IsDir():
		return fmt.Errorf("cannot create destination directory: %s is not a directory", filepath.Base(dst))
	case err == nil:
		return t.copyInto(src, dst, info, false)
	case !errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("cannot access destination: %w", err)
	}
	// Writable until the contents are in, whatever the source's mode
	if err := os.Mkdir(dst, 0700); err != nil {
		return fmt.Errorf("cannot create destination directory: %w", err)
	}
	return t.copyInto(src, dst, info, true)
}

// readDir lists a directory; tests replace it to fail as a bad disk would
var readDir = os.ReadDir

// copyInto copies the contents of the directory src into the directory
// dst. If created is set, dst is new, and gets src's mode and time.
func (t *Task) copyInto(src, dst string, info os.FileInfo, created bool) error {
	// ReadDir can fail part way and still return the entries it read: they
	// are copied, but the copy is incomplete and must fail, or a move would
	// go on to delete what was never read
	entries, readErr := readDir(src)
	var err error
	for _, entry := range entries {
		// Another copy's unfinished file: it is no one's to copy
		if isPartial(entry.Name()) {
			continue
		}
		if err = t.copyPath(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
			break
		}
	}
	if err == nil && readErr != nil {
		err = fmt.Errorf("cannot read source directory: %w", readErr)
	}
	if err == nil {
		// After its contents, so a move deletes them before it
		t.done(src, info)
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
// under dst's name or damages the file it would replace. Unless replace is
// set, it is renamed into place only if nothing has taken the name.
func (t *Task) copyFile(src, dst string, info os.FileInfo, replace bool) error {
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
	// On disk before it takes the place of a file, so a crash can't leave
	// an empty file where the old one was
	if err == nil && replace {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		// Times before the mode, which may make the file read-only
		os.Chtimes(tmp.Name(), time.Time{}, info.ModTime())
		os.Chmod(tmp.Name(), info.Mode().Perm())
		err = place(tmp.Name(), dst, replace)
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

// place renames tmp, a finished copy, to dst. It replaces what is at dst
// only if replace is set; otherwise something that has turned up at dst is
// kept, and the error says it is in the way.
func place(tmp, dst string, replace bool) error {
	if replace {
		return os.Rename(tmp, dst)
	}
	err := renameNoReplace(tmp, dst)
	if errors.Is(err, os.ErrExist) {
		return inTheWay(dst)
	}
	return err
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

// copySymlink recreates the symlink at src as dst. It is made under a
// hidden name and renamed into place, so whatever it replaces is there
// until it is; unless replace is set, it replaces nothing.
func copySymlink(src, dst string, replace bool) error {
	target, err := os.Readlink(src)
	if err != nil {
		return fmt.Errorf("cannot read symlink: %w", err)
	}
	tmp := tempName(filepath.Dir(dst), partialPrefix)
	if err := os.Symlink(target, tmp); err != nil {
		return fmt.Errorf("cannot create symlink: %w", err)
	}
	if err := place(tmp, dst, replace); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("cannot create symlink: %w", err)
	}
	return nil
}

// renameForMove is Move's first try, a rename that replaces what is at dst
// only if replace is set. Tests replace it to fail as renames do across
// filesystems.
var renameForMove = func(src, dst string, replace bool) error {
	if replace {
		return os.Rename(src, dst)
	}
	return renameNoReplace(src, dst)
}

// Move moves src to dst. On one filesystem this is a rename. Across
// filesystems, or onto an existing directory, src is copied and then
// deleted; if the copy fails in any way or is cancelled, what it created
// is removed and src is left as it was. Only what was copied is deleted,
// and only if it hasn't changed since, so files added to src during the
// copy stay. Deleting can't be cancelled, so a move never stops with the
// files only half in either place.
//
// What is at dst when the move starts is what the caller chose to
// replace: a file, a symlink, which is never followed, or an empty
// directory is replaced, and a directory with something in it is merged
// into. Anything that turns up at dst after that is kept, and the move
// fails.
func (t *Task) Move(src, dst string) (err error) {
	src, dst = filepath.Clean(src), filepath.Clean(dst)
	if err := CheckTransfer(src, dst); err != nil {
		return err
	}
	srcInfo, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("cannot access source: %w", err)
	}
	existing, dstErr := os.Lstat(dst)
	if dstErr != nil && !errors.Is(dstErr, os.ErrNotExist) {
		return fmt.Errorf("cannot access destination: %w", dstErr)
	}
	replace := dstErr == nil
	if replace && srcInfo.IsDir() && existing.Mode()&os.ModeSymlink != 0 {
		// A directory can't be renamed over a link, and moving it into the
		// link would put it wherever the link leads, so the link makes way,
		// and is put back if the move fails
		target, lerr := os.Readlink(dst)
		if lerr == nil {
			lerr = os.Remove(dst)
		}
		if lerr != nil {
			return fmt.Errorf("cannot replace the symlink %s: %w", filepath.Base(dst), lerr)
		}
		defer func() {
			if err != nil {
				os.Symlink(target, dst)
			}
		}()
		replace = false
	}

	err = renameForMove(src, dst, replace)
	if err == nil {
		return nil
	}
	merge := replace && srcInfo.IsDir() && existing.IsDir()
	if !merge && !isCrossDevice(err) {
		if !replace && errors.Is(err, os.ErrExist) {
			return inTheWay(dst)
		}
		return err
	}

	t.Count(src)
	var list []copied
	t.copied = &list
	if merge || !srcInfo.IsDir() {
		err = t.copyEntry(src, dst, srcInfo, replace)
	} else {
		err = t.copyAside(src, dst, srcInfo)
	}
	t.copied = nil
	if err != nil {
		if cerr := t.ctx.Err(); cerr != nil {
			return cerr
		}
		return fmt.Errorf("move failed during copy: %w", err)
	}
	if err := deleteCopied(list); err != nil {
		return fmt.Errorf("move failed during cleanup: %w", err)
	}
	return nil
}

// deleteCopied deletes the entries a move copied from its source, and only
// those: files added to the source while the copy ran, and entries changed
// since they were copied, are left where they are, with the folders
// holding them, and the error says so
func deleteCopied(list []copied) error {
	var left []string
	var firstErr error
	for _, c := range list {
		now, err := os.Lstat(c.path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err == nil && !c.info.IsDir() && !sameVersion(now, c.info) {
			left = append(left, c.path)
			continue
		}
		if err == nil {
			err = os.Remove(c.path)
		}
		switch {
		case err == nil:
		case c.info.IsDir() && errors.Is(err, os.ErrExist):
			// Not empty: something in it was added or left
			left = append(left, c.path)
		case firstErr == nil:
			firstErr = err
		}
	}
	if firstErr != nil {
		return firstErr
	}
	switch len(left) {
	case 0:
		return nil
	case 1:
		return fmt.Errorf("%s changed during the move, so it was left where it was", filepath.Base(left[0]))
	}
	return fmt.Errorf("%d items, %s among them, changed during the move, so they were left where they were", len(left), filepath.Base(left[0]))
}

// sameVersion reports whether now is the entry was describes, unchanged:
// the same file, mode, size and time and, where the system keeps it, the
// same status change time
func sameVersion(now, was os.FileInfo) bool {
	_, _, nowChanged := fileIDs(now)
	_, _, wasChanged := fileIDs(was)
	return os.SameFile(now, was) && now.Mode() == was.Mode() && nowChanged == wasChanged &&
		(!was.Mode().IsRegular() || now.Size() == was.Size() && now.ModTime().Equal(was.ModTime()))
}

// copyAside copies the directory src into a new hidden directory beside
// dst and renames that to dst once it is complete, so the copy never
// merges into, or replaces, something that turns up at dst meanwhile, and
// a copy cut short is never left under dst's name. If it fails, the
// hidden directory is removed.
func (t *Task) copyAside(src, dst string, info os.FileInfo) error {
	tmp, err := t.copyToTemp(src, filepath.Dir(dst), info)
	if err != nil {
		return err
	}
	if err := place(tmp, dst, false); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	return nil
}

// copyToTemp copies src, which info describes, to a new hidden name in
// dir and returns it. If the copy fails, nothing is left.
func (t *Task) copyToTemp(src, dir string, info os.FileInfo) (string, error) {
	if !info.IsDir() {
		tmp := tempName(dir, partialPrefix)
		return tmp, t.copyEntry(src, tmp, info, false)
	}
	tmp, err := os.MkdirTemp(dir, partialPrefix+"*")
	if err != nil {
		return "", fmt.Errorf("cannot create destination directory: %w", err)
	}
	if err := t.copyInto(src, tmp, info, true); err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	return tmp, nil
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
