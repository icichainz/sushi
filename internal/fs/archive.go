package fs

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
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
// Symlinks are stored as links.
//
// The zip is written under a hidden name beside dst, readable by its owner
// only, and renamed to dst once complete, so a zip under dst's name is
// never partial, even if sushi is killed; if it fails or is cancelled, the
// partial zip is removed. It ends up readable by no more people than the
// files in it: a folder of private files makes a private zip.
func (t *Task) CreateZip(dst string, paths []string) (err error) {
	dst = filepath.Clean(dst)
	if Exists(dst) {
		return fmt.Errorf("%s already exists", filepath.Base(dst))
	}
	f, err := os.CreateTemp(filepath.Dir(dst), partialPrefix+"*.zip")
	if err != nil {
		return err
	}
	tmp := f.Name()
	self, err := f.Stat()
	if err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}

	// What a new file would get, narrowed below by each file put in
	mode := 0666 &^ umask
	zw := zip.NewWriter(f)
	defer func() {
		if cerr := zw.Close(); err == nil {
			err = cerr
		}
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = os.Chmod(tmp, mode)
		}
		if err == nil {
			// Something may have taken the name meanwhile; it is kept
			if err = renameNoReplace(tmp, dst); errors.Is(err, os.ErrExist) {
				err = fmt.Errorf("%s already exists", filepath.Base(dst))
			}
		}
		if err != nil {
			os.Remove(tmp)
		}
	}()

	for _, root := range paths {
		root = filepath.Clean(root)
		parent := filepath.Dir(root)
		err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if cerr := t.ctx.Err(); cerr != nil {
				return cerr
			}
			// Unfinished copies and archives, this one among them when it is
			// inside a folder being compressed
			if path != root && isPartial(d.Name()) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if os.SameFile(info, self) {
				return nil
			}
			if info.Mode()&os.ModeSymlink == 0 {
				// Group and others lose what any file or folder denies them
				mode &= info.Mode().Perm() | 0700
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

// partialPrefix starts the names of what sushi is still writing: files
// being copied, folders being moved across drives and archives being made.
// They are renamed into place once complete, so a name without it is never
// partial.
const partialPrefix = ".sushi-partial-"

// isPartial reports whether name is one sushi gives what it is still writing
func isPartial(name string) bool {
	return strings.HasPrefix(name, partialPrefix)
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
// Nothing is written outside dir. Every write goes through an os.Root
// opened on dir, which refuses paths that lead out of it, however the
// links in it are laid out. Entries named outside it ("../x", "/etc/x")
// are refused, as are entries inside one of the archive's symlinks
// ("link/x"), which would be written wherever the link leads, and symlinks
// that point outside it. Symlinks are made last, once every file is
// written, and then each is followed through the others: one that leads
// outside, or would once what it names is created, is removed. No file is
// replaced: an archive with an entry twice fails. Zip archives are checked
// in full before anything is written; tar archives can only be read in
// order, so extraction stops at the first bad entry, and dir, with what
// was written into it, is removed. If it fails otherwise or is cancelled,
// what was extracted until then stays. Modes come from the archive,
// without special bits and masked with the umask like those of any new
// file.
//
// An error from before dir was created, say because something took its
// name, or from a refused extraction whose dir was removed, matches
// ErrNotCreated: then nothing at dir is the extraction's.
func (t *Task) Extract(path, dir string) error {
	x := newExtractor(t)
	var err error
	switch ArchiveKind(path) {
	case "zip":
		err = t.extractZip(x, path, dir, keepName)
	case "tar":
		err = t.extractTar(x, path, dir, "", keepName)
	case "tar.gz":
		err = t.extractTar(x, path, dir, "gz", keepName)
	default:
		err = fmt.Errorf("%s is not a zip, tar or tar.gz archive", filepath.Base(path))
	}
	if err != nil && !x.made {
		return notCreated{err}
	}
	// A tar archive refused part way has had some of it written. None of
	// it is to be trusted, and the folder is new, so it all goes.
	if r := (refusal{}); err != nil && !x.finished && errors.As(err, &r) {
		if rerr := x.removeRefused(dir); rerr != nil {
			return fmt.Errorf("%w; what was extracted before it is left in %s: %v", err, filepath.Base(dir), rerr)
		}
		return notCreated{err}
	}
	return err
}

// keepName extracts every entry under its own name
func keepName(name string) (string, bool) {
	return name, true
}

// extractZip unpacks the entries of a zip archive that choose picks, under
// the names it gives them
func (t *Task) extractZip(x *extractor, path, dir string, choose func(string) (string, bool)) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", filepath.Base(path), err)
	}
	defer zr.Close()

	for _, f := range zr.File {
		name, ok := choose(f.Name)
		if !ok {
			continue
		}
		mode := f.FileInfo().Mode()
		link := mode&os.ModeSymlink != 0
		if _, err := x.admit(name, link, mode.IsDir()); err != nil {
			return err
		}
		if link {
			target, err := readZipLink(f)
			if err != nil {
				return err
			}
			if err := checkLinkTarget(name, target); err != nil {
				return err
			}
		}
		if !mode.IsDir() {
			t.p.TotalFiles++
			t.p.TotalBytes += int64(f.UncompressedSize64)
		}
	}
	if err := x.open(dir); err != nil {
		return err
	}
	defer x.close()

	for _, f := range zr.File {
		if err := t.ctx.Err(); err != nil {
			return err
		}
		name, ok := choose(f.Name)
		if !ok {
			continue
		}
		info := f.FileInfo()
		mode := info.Mode()
		switch {
		case mode.IsDir():
			err = x.mkdir(name, mode, info.ModTime())
		case mode&os.ModeSymlink != 0:
			var target string
			if target, err = readZipLink(f); err == nil {
				err = x.symlink(name, target)
			}
		case mode.IsRegular():
			var rc io.ReadCloser
			if rc, err = f.Open(); err == nil {
				err = x.file(name, mode, info.ModTime(), rc, true)
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

// extractTar unpacks the entries of a tar archive that choose picks, under
// the names it gives them; compressed is "gz", "bz2" or "" for none.
// Progress goes by how much of the archive file has been read, as the
// number of files isn't known until the end.
func (t *Task) extractTar(x *extractor, path, dir, compressed string, choose func(string) (string, bool)) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", filepath.Base(path), err)
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil {
		t.p.TotalBytes += info.Size()
	}

	var r io.Reader = &countingReader{r: f, t: t}
	switch compressed {
	case "gz":
		zr, err := gzip.NewReader(r)
		if err != nil {
			return fmt.Errorf("cannot read %s: %w", filepath.Base(path), err)
		}
		defer zr.Close()
		r = zr
	case "bz2":
		r = bzip2.NewReader(r)
	}
	if err := x.open(dir); err != nil {
		return err
	}
	defer x.close()

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
		name, ok := choose(hdr.Name)
		if !ok {
			continue
		}
		mode := hdr.FileInfo().Mode()
		switch hdr.Typeflag {
		case tar.TypeDir, tar.TypeReg, tar.TypeSymlink, tar.TypeLink:
			// Checked as they come, as a tar archive is read in order
			if _, err := x.admit(name, hdr.Typeflag == tar.TypeSymlink, hdr.Typeflag == tar.TypeDir); err != nil {
				return err
			}
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			err = x.mkdir(name, mode, hdr.ModTime)
		case tar.TypeReg:
			err = x.file(name, mode, hdr.ModTime, tr, false)
		case tar.TypeSymlink:
			if err = checkLinkTarget(name, hdr.Linkname); err == nil {
				err = x.symlink(name, hdr.Linkname)
			}
		case tar.TypeLink:
			// To an entry extracted with it; a link to anything else would
			// be to whatever is there
			target, picked := choose(hdr.Linkname)
			if !picked {
				return refuse("%s links to %s, which isn't extracted with it", hdr.Name, hdr.Linkname)
			}
			err = x.hardlink(name, target)
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

// extractor writes an archive's entries into a new folder, always through
// an os.Root opened on it
type extractor struct {
	t        *Task
	root     *os.Root
	made     bool        // The folder was created
	folder   os.FileInfo // The folder, to know it again
	finished bool        // Every entry was written; only the links are left
	links    []pendingLink
	dirs     []pendingDir

	// What the archive has held so far, by path below the root, so entries
	// that lead through its symlinks are refused in whatever order they come
	symlinks map[string]bool // Symlink entries
	names    map[string]bool // Every other entry
	parents  map[string]bool // Directories with an entry in or below them
}

func newExtractor(t *Task) *extractor {
	return &extractor{t: t, symlinks: map[string]bool{}, names: map[string]bool{}, parents: map[string]bool{}}
}

// pendingLink is a symlink to create once every file is written
type pendingLink struct {
	name, target string
}

// pendingDir is a directory whose mode and time are set at the end, since
// writing into it would change its time and its mode may forbid writing
type pendingDir struct {
	rel   string
	mode  os.FileMode
	mtime time.Time
}

// open creates dir, which must not exist yet, and opens it as the root that
// every entry is written through
func (x *extractor) open(dir string) error {
	if err := os.Mkdir(dir, 0755); err != nil {
		return err
	}
	x.made = true
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	x.root = root
	x.folder, _ = root.Stat(".")
	return nil
}

// refusal is an entry that extraction refuses, as unsafe or ambiguous
type refusal struct{ error }

func (r refusal) Unwrap() error { return r.error }

// refuse makes an error that says an entry was refused
func refuse(format string, args ...any) error {
	return refusal{fmt.Errorf(format, args...)}
}

// removeRefused removes the folder of an extraction refused part way, if
// it is still the one the extraction made. Nothing but what the
// extraction wrote can be in it, and no symlinks, which are made last.
func (x *extractor) removeRefused(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil || x.folder == nil || !os.SameFile(info, x.folder) {
		return fmt.Errorf("%s is no longer the folder the extraction made", filepath.Base(dir))
	}
	return os.RemoveAll(dir)
}

func (x *extractor) close() {
	if x.root != nil {
		x.root.Close()
	}
}

// local returns where an entry goes, relative to the root, refusing names
// that would put it outside
func local(name string) (string, error) {
	rel := filepath.FromSlash(strings.TrimSuffix(name, "/"))
	if !filepath.IsLocal(rel) {
		return "", refuse("unsafe path in archive: %q", name)
	}
	return filepath.Clean(rel), nil
}

// admit checks an entry against those before it and returns where it goes.
// An entry inside one of the archive's symlinks is refused, whether the
// link comes before it or after, and so is a symlink whose name is taken.
func (x *extractor) admit(name string, link, dir bool) (string, error) {
	rel, err := local(name)
	if err != nil {
		return "", err
	}
	for p := filepath.Dir(rel); p != "."; p = filepath.Dir(p) {
		if x.symlinks[p] {
			return "", refuse("unsafe path in archive: %q is inside the symlink %q", name, filepath.ToSlash(p))
		}
	}
	switch {
	case link && (rel == "." || x.parents[rel]):
		return "", refuse("unsafe path in archive: the archive has entries inside the symlink %q", name)
	case link && (x.symlinks[rel] || x.names[rel]), !link && x.symlinks[rel]:
		return "", refuse("%s is in the archive twice", name)
	case link:
		x.symlinks[rel] = true
	default:
		x.names[rel] = true
		if dir {
			x.parents[rel] = true
		}
	}
	for p := filepath.Dir(rel); p != "." && !x.parents[p]; p = filepath.Dir(p) {
		x.parents[p] = true
	}
	return rel, nil
}

// checkLinkTarget refuses a symlink whose target, read from the link's
// directory, names a place outside the archive's folder. It goes by the
// names alone; finish then follows each link through the others.
func checkLinkTarget(name, target string) error {
	rel := filepath.Join(filepath.Dir(filepath.FromSlash(name)), filepath.FromSlash(target))
	if rooted(target) || !filepath.IsLocal(rel) {
		return refuse("symlink %s points outside the archive", name)
	}
	return nil
}

// rooted reports whether a link target starts from the top of a drive
// rather than from the link's directory
func rooted(target string) bool {
	return filepath.IsAbs(target) || filepath.VolumeName(target) != "" ||
		strings.HasPrefix(target, "/") || strings.HasPrefix(target, string(filepath.Separator))
}

func (x *extractor) mkdir(name string, mode os.FileMode, mtime time.Time) error {
	rel, err := local(name)
	if err != nil {
		return err
	}
	if err := x.root.MkdirAll(rel, 0700); err != nil {
		return err
	}
	// Masked like the modes of new folders, so an archive made where
	// everything is 0777 doesn't open it to everyone; the owner keeps full
	// access, so the folder can be used and removed
	x.dirs = append(x.dirs, pendingDir{rel, mode.Perm()&^umask | 0700, mtime})
	return nil
}

// file writes a regular file. If count is set, its bytes count towards the
// progress. A partly written file is removed.
func (x *extractor) file(name string, mode os.FileMode, mtime time.Time, r io.Reader, count bool) error {
	rel, err := local(name)
	if err != nil {
		return err
	}
	if err := x.root.MkdirAll(filepath.Dir(rel), 0700); err != nil {
		return err
	}
	f, err := x.root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return refuse("%s is in the archive twice", name)
	}
	if err != nil {
		return err
	}
	err = x.t.copyData(f, r, count)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		x.root.Remove(rel)
		return err
	}
	// Special bits like setuid aren't restored from an archive, and the
	// rest is masked as a new file's mode would be
	x.root.Chtimes(rel, time.Time{}, mtime)
	x.root.Chmod(rel, mode.Perm()&^umask)
	x.t.p.Files++
	x.t.update()
	return nil
}

func (x *extractor) symlink(name, target string) error {
	if _, err := local(name); err != nil {
		return err
	}
	x.links = append(x.links, pendingLink{name, target})
	return nil
}

// hardlink links name to an entry already extracted
func (x *extractor) hardlink(name, target string) error {
	rel, err := local(name)
	if err != nil {
		return err
	}
	src, err := local(target)
	if err != nil {
		return err
	}
	if info, err := x.root.Lstat(src); err != nil || !info.Mode().IsRegular() {
		return refuse("%s links to %s, which isn't a file in the archive", name, target)
	}
	if err := x.root.MkdirAll(filepath.Dir(rel), 0700); err != nil {
		return err
	}
	if err := x.root.Link(src, rel); err != nil {
		return err
	}
	x.t.p.Files++
	x.t.update()
	return nil
}

// finish creates the symlinks, removing any that lead outside the root, and
// sets the directories' modes and times
func (x *extractor) finish() error {
	x.finished = true
	var firstErr error
	fail := func(err error) {
		if firstErr == nil {
			firstErr = err
		}
	}

	var made []string
	for _, l := range x.links {
		rel, _ := local(l.name)
		if err := checkLinkTarget(l.name, l.target); err != nil {
			fail(err)
			continue
		}
		if err := x.root.MkdirAll(filepath.Dir(rel), 0700); err != nil {
			fail(err)
			continue
		}
		if err := x.root.Symlink(filepath.FromSlash(l.target), rel); err != nil {
			fail(err)
			continue
		}
		made = append(made, rel)
		x.t.p.Files++
	}

	// Each link was checked on its own, but links can lead through each
	// other ("a" to ".", "b" to "a/.."), so follow each one through the
	// others as they are now. A link to something missing counts by where
	// it would lead once that is created. Removing a link changes where
	// others lead, so check again until none is removed.
	for removed := true; removed; {
		removed = false
		kept := made[:0]
		for _, rel := range made {
			if leadsOutside(x.root, rel) {
				x.root.Remove(rel)
				fail(fmt.Errorf("symlink %s points outside the archive", filepath.ToSlash(rel)))
				removed = true
				continue
			}
			kept = append(kept, rel)
		}
		made = kept
	}

	// Deepest first, so setting a directory's time isn't undone by a change
	// inside it, and a read-only parent doesn't block its children
	sort.Slice(x.dirs, func(i, j int) bool { return len(x.dirs[i].rel) > len(x.dirs[j].rel) })
	for _, d := range x.dirs {
		x.root.Chmod(d.rel, d.mode)
		x.root.Chtimes(d.rel, time.Time{}, d.mtime)
	}
	x.t.update()
	return firstErr
}

// maxLinks is how many symlinks leadsOutside follows before it takes the
// path for a loop, which leads nowhere
const maxLinks = 255

// leadsOutside reports whether following rel from the top of root, through
// the symlinks in root as they are now, ever goes above root. Parts of the
// path that don't exist are taken as they are named, so a link to
// something missing counts by where it would lead once that is created.
func leadsOutside(root *os.Root, rel string) bool {
	var at []string // Where the walk has got to below the root, never a link
	todo := strings.Split(rel, string(filepath.Separator))
	links := 0
	for len(todo) > 0 {
		part := todo[0]
		todo = todo[1:]
		switch part {
		case "", ".":
			continue
		case "..":
			if len(at) == 0 {
				return true
			}
			at = at[:len(at)-1]
			continue
		}
		at = append(at, part)
		here := filepath.Join(at...)
		info, err := root.Lstat(here)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		if links++; links > maxLinks {
			return false
		}
		target, err := root.Readlink(here)
		if err != nil || rooted(target) {
			return true
		}
		at = at[:len(at)-1]
		todo = append(strings.Split(filepath.FromSlash(target), string(filepath.Separator)), todo...)
	}
	return false
}
