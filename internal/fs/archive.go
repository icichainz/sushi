package fs

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ArchiveKind returns what kind of archive name is by its extension: "zip",
// "tar" or "tar.gz", or "" if it isn't one sushi can extract
func ArchiveKind(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return "zip"
	case strings.HasSuffix(lower, ".tar"):
		return "tar"
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return "tar.gz"
	}
	return ""
}

// CreateZip compresses paths, and everything in the directories among
// them, into a new zip file at dst, which must not exist. Entries are named
// from each path's parent, so a folder "photos" is stored as "photos/...".
// Symlinks are stored as links. If it fails or is cancelled, dst is removed.
func (t *Task) CreateZip(dst string, paths []string) (err error) {
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%s already exists", filepath.Base(dst))
	}
	if err != nil {
		return err
	}
	self, err := f.Stat()
	if err != nil {
		f.Close()
		os.Remove(dst)
		return err
	}

	zw := zip.NewWriter(f)
	defer func() {
		if cerr := zw.Close(); err == nil {
			err = cerr
		}
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(dst)
		}
	}()

	for _, root := range paths {
		parent := filepath.Dir(root)
		err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if cerr := t.ctx.Err(); cerr != nil {
				return cerr
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			// The archive itself, when it is inside a folder being compressed
			if os.SameFile(info, self) {
				return nil
			}
			rel, err := filepath.Rel(parent, path)
			if err != nil {
				return err
			}
			return t.addToZip(zw, path, filepath.ToSlash(rel), info)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// addToZip writes one file, directory or symlink to the archive as name
func (t *Task) addToZip(zw *zip.Writer, path, name string, info os.FileInfo) error {
	hdr, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	hdr.Name = name

	switch {
	case info.IsDir():
		hdr.Name += "/"
		_, err := zw.CreateHeader(hdr)
		return err

	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			return err
		}
		hdr.Method = zip.Store
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(w, target); err != nil {
			return err
		}

	case info.Mode().IsRegular():
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		hdr.Method = zip.Deflate
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		if err := t.copyData(w, in, true); err != nil {
			return err
		}

	default:
		return fmt.Errorf("%s is not a regular file", info.Name())
	}
	t.p.Files++
	t.update()
	return nil
}

// Extract unpacks the zip, tar or gzipped tar archive at path into dir,
// which it creates and which must not exist yet.
//
// Nothing is written outside dir: entries named outside it ("../x",
// "/etc/x") are refused, as are symlinks that point outside it. Symlinks
// are made last, once every file is written, so nothing is ever written
// through one, and a link that ends up outside dir through other links is
// removed. No file is replaced: an archive with an entry twice fails. Zip
// archives are checked in full before anything is written; tar archives
// can only be read in order, so extraction stops at the first bad entry.
// If it fails or is cancelled, what was extracted until then stays.
func (t *Task) Extract(path, dir string) error {
	switch ArchiveKind(path) {
	case "zip":
		return t.extractZip(path, dir)
	case "tar":
		return t.extractTar(path, dir, false)
	case "tar.gz":
		return t.extractTar(path, dir, true)
	}
	return fmt.Errorf("%s is not a zip, tar or tar.gz archive", filepath.Base(path))
}

// extractZip unpacks a zip archive
func (t *Task) extractZip(path, dir string) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", filepath.Base(path), err)
	}
	defer zr.Close()

	x := &extractor{t: t, root: dir}
	for _, f := range zr.File {
		if _, err := x.local(f.Name); err != nil {
			return err
		}
		if !f.FileInfo().IsDir() {
			t.p.TotalFiles++
			t.p.TotalBytes += int64(f.UncompressedSize64)
		}
	}
	if err := os.Mkdir(dir, 0755); err != nil {
		return err
	}

	for _, f := range zr.File {
		if err := t.ctx.Err(); err != nil {
			return err
		}
		info := f.FileInfo()
		mode := info.Mode()
		switch {
		case mode.IsDir():
			err = x.mkdir(f.Name, mode, info.ModTime())
		case mode&os.ModeSymlink != 0:
			var target string
			if target, err = readZipLink(f); err == nil {
				err = x.symlink(f.Name, target)
			}
		case mode.IsRegular():
			var rc io.ReadCloser
			if rc, err = f.Open(); err == nil {
				err = x.file(f.Name, mode, info.ModTime(), rc, true)
				rc.Close()
			}
		}
		if err != nil {
			return err
		}
	}
	return x.finish()
}

// readZipLink reads the target of a symlink stored in a zip archive
func readZipLink(f *zip.File) (string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	// Link targets are short; anything longer isn't one
	b, err := io.ReadAll(io.LimitReader(rc, 4096))
	return string(b), err
}

// extractTar unpacks a tar archive, gzipped if gz is set. Progress goes by
// how much of the archive file has been read, as the number of files isn't
// known until the end.
func (t *Task) extractTar(path, dir string, gz bool) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", filepath.Base(path), err)
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil {
		t.p.TotalBytes += info.Size()
	}

	var r io.Reader = &countingReader{r: f, t: t}
	if gz {
		zr, err := gzip.NewReader(r)
		if err != nil {
			return fmt.Errorf("cannot read %s: %w", filepath.Base(path), err)
		}
		defer zr.Close()
		r = zr
	}
	if err := os.Mkdir(dir, 0755); err != nil {
		return err
	}

	x := &extractor{t: t, root: dir}
	tr := tar.NewReader(r)
	for {
		if err := t.ctx.Err(); err != nil {
			return err
		}
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("cannot read %s: %w", filepath.Base(path), err)
		}
		mode := hdr.FileInfo().Mode()
		switch hdr.Typeflag {
		case tar.TypeDir:
			err = x.mkdir(hdr.Name, mode, hdr.ModTime)
		case tar.TypeReg:
			err = x.file(hdr.Name, mode, hdr.ModTime, tr, false)
		case tar.TypeSymlink:
			err = x.symlink(hdr.Name, hdr.Linkname)
		case tar.TypeLink:
			err = x.hardlink(hdr.Name, hdr.Linkname)
		default:
			// Devices, pipes and other special entries aren't extracted
		}
		if err != nil {
			return err
		}
	}
	return x.finish()
}

// countingReader counts what is read from an archive towards the progress
type countingReader struct {
	r io.Reader
	t *Task
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.t.p.Bytes += int64(n)
	c.t.update()
	return n, err
}

// extractor writes an archive's entries under root
type extractor struct {
	t     *Task
	root  string
	links []pendingLink
	dirs  []pendingDir
}

// pendingLink is a symlink to create once every file is written
type pendingLink struct {
	name, target string
}

// pendingDir is a directory whose mode and time are set at the end, since
// writing into it would change its time and its mode may forbid writing
type pendingDir struct {
	path  string
	mode  os.FileMode
	mtime time.Time
}

// local returns where an entry goes, refusing names that would put it
// outside the root
func (x *extractor) local(name string) (string, error) {
	rel := filepath.FromSlash(strings.TrimSuffix(name, "/"))
	if !filepath.IsLocal(rel) {
		return "", fmt.Errorf("unsafe path in archive: %q", name)
	}
	return filepath.Join(x.root, rel), nil
}

func (x *extractor) mkdir(name string, mode os.FileMode, mtime time.Time) error {
	path, err := x.local(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	// The owner keeps full access, so the folder can be used and removed
	x.dirs = append(x.dirs, pendingDir{path, mode.Perm() | 0700, mtime})
	return nil
}

// file writes a regular file. If count is set, its bytes count towards the
// progress. A partly written file is removed.
func (x *extractor) file(name string, mode os.FileMode, mtime time.Time, r io.Reader, count bool) error {
	path, err := x.local(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%s is in the archive twice", name)
	}
	if err != nil {
		return err
	}
	err = x.t.copyData(f, r, count)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		return err
	}
	// Special bits like setuid aren't restored from an archive
	os.Chtimes(path, time.Time{}, mtime)
	os.Chmod(path, mode.Perm())
	x.t.p.Files++
	x.t.update()
	return nil
}

func (x *extractor) symlink(name, target string) error {
	if _, err := x.local(name); err != nil {
		return err
	}
	x.links = append(x.links, pendingLink{name, target})
	return nil
}

// hardlink links name to an entry already extracted
func (x *extractor) hardlink(name, target string) error {
	path, err := x.local(name)
	if err != nil {
		return err
	}
	src, err := x.local(target)
	if err != nil {
		return err
	}
	if info, err := os.Lstat(src); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("%s links to %s, which isn't a file in the archive", name, target)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := os.Link(src, path); err != nil {
		return err
	}
	x.t.p.Files++
	x.t.update()
	return nil
}

// finish creates the symlinks, removing any that lead outside the root, and
// sets the directories' modes and times
func (x *extractor) finish() error {
	var firstErr error
	fail := func(err error) {
		if firstErr == nil {
			firstErr = err
		}
	}

	var made []string
	for _, l := range x.links {
		path, _ := x.local(l.name)
		// Relative to the link's directory, the target must stay inside
		rel := filepath.Join(filepath.Dir(filepath.FromSlash(l.name)), filepath.FromSlash(l.target))
		if filepath.IsAbs(l.target) || !filepath.IsLocal(rel) {
			fail(fmt.Errorf("symlink %s points outside the archive", l.name))
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			fail(err)
			continue
		}
		if err := os.Symlink(l.target, path); err != nil {
			fail(err)
			continue
		}
		made = append(made, path)
		x.t.p.Files++
	}

	// Each link was checked on its own, but links can lead through each
	// other ("a" to ".", "b" to "a/.."), so check where each one ends up.
	// Links that lead nowhere lead nowhere outside either.
	root, err := filepath.EvalSymlinks(x.root)
	if err != nil {
		root = x.root
	}
	for _, path := range made {
		if dest, err := filepath.EvalSymlinks(path); err == nil && !within(root, dest) {
			os.Remove(path)
			rel, _ := filepath.Rel(x.root, path)
			fail(fmt.Errorf("symlink %s points outside the archive", filepath.ToSlash(rel)))
		}
	}

	// Deepest first, so setting a directory's time isn't undone by a change
	// inside it, and a read-only parent doesn't block its children
	sort.Slice(x.dirs, func(i, j int) bool { return len(x.dirs[i].path) > len(x.dirs[j].path) })
	for _, d := range x.dirs {
		os.Chmod(d.path, d.mode)
		os.Chtimes(d.path, time.Time{}, d.mtime)
	}
	x.t.update()
	return firstErr
}
