package fs

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Browsing inside an archive works from an index of its entries, read once:
// a zip's central directory, or a single pass through a tar. The index
// lists any folder of the archive as FileInfo, as ScanDirectory lists a
// directory, and single entries are read from the archive on demand.
// Nothing is written to disk to browse.
//
// The entries of a folder are listed at paths below the archive's own, as
// if it were a directory: "bundle.zip/src/main.go". Those paths name
// nothing on disk, where the archive is a file, so anything that takes one
// for a real path fails rather than finding something else. An entry named
// outside the archive ("../x", "/etc/x") is listed at the top under its
// full name, at a path holding a NUL, which no file can, and is never
// extracted.

// maxIndexEntries is how many entries an index holds; an archive with more
// is listed in part. The same cap as the preview's; a variable so tests can
// lower it.
var maxIndexEntries = 100_000

// indexTime bounds the pass through a tar: a compressed one is
// decompressed from the start to find each entry. A variable so tests can
// shorten it.
var indexTime = 10 * time.Second

// BrowseKind returns the kind of archive name is by its extension, if it
// is one sushi can browse: "zip" (also .jar), "tar", "tar.gz" or
// "tar.bz2"; otherwise "".
func BrowseKind(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".zip"), strings.HasSuffix(lower, ".jar"):
		return "zip"
	case strings.HasSuffix(lower, ".tar"):
		return "tar"
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return "tar.gz"
	case strings.HasSuffix(lower, ".tar.bz2"), strings.HasSuffix(lower, ".tbz2"):
		return "tar.bz2"
	}
	return ""
}

// ArchiveEntry is one entry of an archive's index
type ArchiveEntry struct {
	Name    string // As stored in the archive
	Size    int64  // Uncompressed
	ModTime time.Time
	Mode    os.FileMode
	Link    string // Where a symlink points (tar only; see ReadLink), or what a hard link links to
	Unsafe  bool   // Named outside the archive: listed, previewed, never extracted

	inner     string // Where it is listed: a clean slash path, or a NUL and a number if Unsafe
	pos       int    // Its place in the archive: the zip file's index, or the tar header's
	hardLink  bool   // A tar hard link, to Link
	synthetic bool   // A folder the archive has no entry of its own for
}

// IsDir reports whether the entry is a folder
func (e ArchiveEntry) IsDir() bool {
	return e.Mode.IsDir()
}

// Inner returns where the entry is listed inside the archive
func (e ArchiveEntry) Inner() string {
	return e.inner
}

// ArchiveIndex is what an archive holds, by folder
type ArchiveIndex struct {
	Path    string // The archive file
	Kind    string // As BrowseKind returns
	Partial bool   // Reading stopped at the limits, so not every entry is listed

	size     int64
	mtime    time.Time
	entries  []ArchiveEntry
	byInner  map[string]int   // Listed path → entry
	children map[string][]int // Folder ("" for the top) → its entries
}

// ReadArchiveIndex reads the index of the archive at path
func ReadArchiveIndex(path string) (ix *ArchiveIndex, err error) {
	defer func() {
		// A malformed archive can make a decoder panic; it is only unreadable
		if r := recover(); r != nil {
			ix, err = nil, fmt.Errorf("unreadable: %v", r)
		}
	}()
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a file", filepath.Base(path))
	}
	ix = &ArchiveIndex{Path: path, Kind: BrowseKind(path), size: info.Size(), mtime: info.ModTime(),
		byInner: map[string]int{}, children: map[string][]int{}}
	switch ix.Kind {
	case "zip":
		err = ix.readZip()
	case "tar", "tar.gz", "tar.bz2":
		ctx, cancel := context.WithTimeout(context.Background(), indexTime)
		defer cancel()
		err = ix.readTar(ctx)
	default:
		err = fmt.Errorf("%s is not an archive sushi can open", filepath.Base(path))
	}
	if err != nil {
		return nil, err
	}
	ix.dateFolders()
	return ix, nil
}

// openZip opens a zip archive. Entries named like "../x" are only a risk
// when extracting, which checks every name itself.
func openZip(path string) (*zip.ReadCloser, error) {
	zr, err := zip.OpenReader(path)
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		return nil, err
	}
	return zr, nil
}

func (ix *ArchiveIndex) readZip() error {
	zr, err := openZip(ix.Path)
	if err != nil {
		return err
	}
	defer zr.Close()
	for i, f := range zr.File {
		if len(ix.entries) >= maxIndexEntries {
			ix.Partial = true
			break
		}
		mode := f.FileInfo().Mode()
		if strings.HasSuffix(f.Name, "/") {
			mode |= os.ModeDir
		}
		size := int64(f.UncompressedSize64)
		if mode.IsDir() {
			size = 0
		}
		ix.add(ArchiveEntry{Name: f.Name, Size: size, ModTime: f.Modified, Mode: mode, pos: i})
	}
	return nil
}

// ctxReader reads from r until ctx is done, and then fails with ctx's
// error. Reading a compressed tar to an entry decompresses everything
// before it, and skipping one large entry is a single call, so ctx is
// checked with every read of the file rather than between entries.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// tarReader opens the tar archive at path for reading from the start,
// decompressing it as its kind says, until ctx is done
func tarReader(ctx context.Context, path, kind string) (*tar.Reader, io.Closer, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	// A plain tar skips what it doesn't read by seeking, so it keeps Seek
	var r io.Reader = ctxSeeker{ctxReader{ctx, f}, f}
	switch kind {
	case "tar.gz":
		gz, err := gzip.NewReader(bufio.NewReader(ctxReader{ctx, f}))
		if err != nil {
			f.Close()
			return nil, nil, err
		}
		r = gz
	case "tar.bz2":
		r = bzip2.NewReader(bufio.NewReader(ctxReader{ctx, f}))
	}
	return tar.NewReader(r), f, nil
}

// ctxSeeker is a ctxReader that can seek
type ctxSeeker struct {
	ctxReader
	s io.Seeker
}

func (c ctxSeeker) Seek(offset int64, whence int) (int64, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.s.Seek(offset, whence)
}

// nextHeader returns the next header of tr. Names a tar reader set to
// refuse insecure paths would refuse are only a risk when extracting.
func nextHeader(tr *tar.Reader) (*tar.Header, error) {
	hdr, err := tr.Next()
	if err != nil && errors.Is(err, tar.ErrInsecurePath) && hdr != nil {
		err = nil
	}
	return hdr, err
}

// readTar indexes a tar until ctx is done: a read too slow, as of a large
// compressed one, lists what it has read by then
func (ix *ArchiveIndex) readTar(ctx context.Context) error {
	tooSlow := func(err error) error {
		if ctx.Err() != nil {
			return fmt.Errorf("too slow to read: no entry found in %v", indexTime)
		}
		return err
	}
	tr, c, err := tarReader(ctx, ix.Path, ix.Kind)
	if err != nil {
		return tooSlow(err)
	}
	defer c.Close()
	for pos := 0; ; pos++ {
		hdr, err := nextHeader(tr)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			if len(ix.entries) == 0 {
				return tooSlow(err)
			}
			// A damaged, cut short or slow archive still lists what came before
			ix.Partial = true
			return nil
		}
		if hdr.Typeflag == tar.TypeXGlobalHeader {
			continue // Settings for the entries, not an entry
		}
		e := ArchiveEntry{Name: hdr.Name, Size: hdr.Size, ModTime: hdr.ModTime, Mode: hdr.FileInfo().Mode(), pos: pos}
		switch hdr.Typeflag {
		case tar.TypeDir:
			e.Mode |= os.ModeDir
			e.Size = 0
		case tar.TypeSymlink:
			e.Link = hdr.Linkname
		case tar.TypeLink:
			e.Link, e.hardLink = hdr.Linkname, true
		}
		ix.add(e)
		if len(ix.entries) >= maxIndexEntries || ctx.Err() != nil {
			ix.Partial = true
			return nil
		}
	}
}

// entryPath cleans an entry's name into the path it is listed at: slash
// separated, without "." parts or a trailing slash. skip is set for the
// archive's top itself, as tar's "./"; ok is false for names that lead
// outside (a ".." part, a leading slash) or can't name a file (a NUL).
func entryPath(name string) (inner string, ok, skip bool) {
	if strings.ContainsRune(name, 0) || strings.HasPrefix(name, "/") {
		return "", false, false
	}
	var parts []string
	for _, p := range strings.Split(name, "/") {
		switch p {
		case "", ".":
			continue
		case "..":
			return "", false, false
		}
		parts = append(parts, p)
	}
	if len(parts) == 0 {
		return "", false, true
	}
	return strings.Join(parts, "/"), true, false
}

// parentOf returns the folder an inner path is in, "" for the top
func parentOf(inner string) string {
	if i := strings.LastIndexByte(inner, '/'); i >= 0 {
		return inner[:i]
	}
	return ""
}

// add puts an entry in the index, with the folders above it the archive
// has no entries for. An entry named twice is listed once, as the later,
// which is what extracting leaves; a name that is a folder for other
// entries stays a folder.
func (ix *ArchiveIndex) add(e ArchiveEntry) {
	inner, ok, skip := entryPath(e.Name)
	if skip {
		return
	}
	if !ok {
		e.Unsafe = true
		e.inner = "\x00" + strconv.Itoa(len(ix.entries))
		ix.put(e)
		return
	}
	e.inner = inner
	for i := 0; i < len(inner); i++ {
		if inner[i] == '/' {
			ix.folder(inner[:i])
		}
	}
	if i, ok := ix.byInner[inner]; ok {
		old := ix.entries[i]
		if old.IsDir() && !e.IsDir() && len(ix.children[inner]) > 0 {
			return
		}
		ix.entries[i] = e
		return
	}
	ix.put(e)
}

// folder makes sure the folder at inner is listed
func (ix *ArchiveIndex) folder(inner string) {
	i, ok := ix.byInner[inner]
	switch {
	case !ok:
		ix.put(ArchiveEntry{Name: inner + "/", Mode: os.ModeDir | 0755, inner: inner, synthetic: true})
	case !ix.entries[i].IsDir():
		// A file of the same name as a folder other entries are in
		ix.entries[i] = ArchiveEntry{Name: inner + "/", Mode: os.ModeDir | 0755, inner: inner, synthetic: true}
	}
}

func (ix *ArchiveIndex) put(e ArchiveEntry) {
	ix.byInner[e.inner] = len(ix.entries)
	parent := ""
	if !e.Unsafe {
		parent = parentOf(e.inner)
	}
	ix.children[parent] = append(ix.children[parent], len(ix.entries))
	ix.entries = append(ix.entries, e)
}

// dateFolders gives the folders the archive has no entries for the time
// of the newest entry in them
func (ix *ArchiveIndex) dateFolders() {
	for _, e := range ix.entries {
		if e.Unsafe || e.synthetic {
			continue
		}
		for p := parentOf(e.inner); p != ""; p = parentOf(p) {
			f := &ix.entries[ix.byInner[p]]
			if f.synthetic && e.ModTime.After(f.ModTime) {
				f.ModTime = e.ModTime
			}
		}
	}
}

// Fresh reports whether the archive is as it was when it was indexed
func (ix *ArchiveIndex) Fresh() bool {
	info, err := os.Stat(ix.Path)
	return err == nil && info.Size() == ix.size && info.ModTime().Equal(ix.mtime)
}

// Stamp identifies the archive as it was indexed: a changed archive has
// another
func (ix *ArchiveIndex) Stamp() string {
	return fmt.Sprintf("%s\x00%d\x00%d", ix.Path, ix.size, ix.mtime.UnixNano())
}

// Inner returns where path, as the index lists it, is inside the archive:
// "" for the archive itself
func (ix *ArchiveIndex) Inner(path string) (string, bool) {
	if path == ix.Path {
		return "", true
	}
	rest, ok := strings.CutPrefix(path, ix.Path+string(filepath.Separator))
	if !ok || rest == "" {
		return "", false
	}
	return filepath.ToSlash(rest), true
}

// PathOf returns the path an inner path is listed at
func (ix *ArchiveIndex) PathOf(inner string) string {
	if inner == "" {
		return ix.Path
	}
	return filepath.Join(ix.Path, filepath.FromSlash(inner))
}

// Entry returns the entry listed at inner
func (ix *ArchiveIndex) Entry(inner string) (ArchiveEntry, bool) {
	i, ok := ix.byInner[inner]
	if !ok {
		return ArchiveEntry{}, false
	}
	return ix.entries[i], true
}

// IsFolder reports whether inner is a folder of the archive, "" being its top
func (ix *ArchiveIndex) IsFolder(inner string) bool {
	if inner == "" {
		return true
	}
	e, ok := ix.Entry(inner)
	return ok && e.IsDir()
}

// Walk calls visit for each entry below the folder at inner, folders
// before what is in them, until visit returns false
func (ix *ArchiveIndex) Walk(inner string, visit func(e ArchiveEntry) bool) {
	var walk func(dir string) bool
	walk = func(dir string) bool {
		for _, i := range ix.children[dir] {
			e := ix.entries[i]
			if !visit(e) {
				return false
			}
			if e.IsDir() && !e.Unsafe && !walk(e.inner) {
				return false
			}
		}
		return true
	}
	walk(inner)
}

// FileInfo describes an entry as ScanDirectory describes a file
func (ix *ArchiveIndex) FileInfo(e ArchiveEntry) FileInfo {
	name := path.Base(e.inner)
	if e.Unsafe {
		name = e.Name
	}
	return FileInfo{
		Name:      name,
		Path:      ix.PathOf(e.inner),
		Size:      e.Size,
		ModTime:   e.ModTime,
		IsDir:     e.IsDir(),
		IsSymlink: e.Mode&os.ModeSymlink != 0,
		Perms:     e.Mode,
	}
}

// ScanArchive lists the folder at inner of the archive ix indexes, as
// ScanDirectory lists a directory. Entries named outside the archive are
// listed at the top, hidden files or not, so they can't go unnoticed.
func ScanArchive(ix *ArchiveIndex, inner string, opts ScanOptions) ([]FileInfo, error) {
	if !ix.IsFolder(inner) {
		return nil, fmt.Errorf("%s is not a folder in %s", inner, filepath.Base(ix.Path))
	}
	kids := ix.children[inner]
	files := make([]FileInfo, 0, len(kids))
	for _, i := range kids {
		e := ix.entries[i]
		f := ix.FileInfo(e)
		if !opts.ShowHidden && !e.Unsafe && strings.HasPrefix(f.Name, ".") {
			continue
		}
		files = append(files, f)
	}
	SortFiles(files, opts.SortBy, opts.SortReverse)
	return files, nil
}

// errChanged says the archive no longer holds an entry where it did
var errChanged = errors.New("the archive has changed since it was read; refresh to read it again")

// readCloser closes more than the reader it reads from
type readCloser struct {
	io.Reader
	close func() error
}

func (r readCloser) Close() error { return r.close() }

// OpenEntry returns the contents of a file entry; a hard link's are those
// of the entry it links to. A tar is read from the start to the entry,
// which stops when ctx is done.
func (ix *ArchiveIndex) OpenEntry(ctx context.Context, e ArchiveEntry) (io.ReadCloser, error) {
	if e.hardLink {
		target, ok, _ := entryPath(e.Link)
		linked, found := ix.Entry(target)
		if !ok || !found || linked.hardLink {
			return nil, fmt.Errorf("%s links to %s, which isn't in the archive", e.Name, e.Link)
		}
		e = linked
	}
	if !e.Mode.IsRegular() {
		return nil, fmt.Errorf("%s is not a file", e.Name)
	}
	if ix.Kind == "zip" {
		zr, err := openZip(ix.Path)
		if err != nil {
			return nil, err
		}
		if e.pos >= len(zr.File) || zr.File[e.pos].Name != e.Name {
			zr.Close()
			return nil, errChanged
		}
		rc, err := zr.File[e.pos].Open()
		if err != nil {
			zr.Close()
			return nil, err
		}
		return readCloser{ctxReader{ctx, rc}, func() error { rc.Close(); return zr.Close() }}, nil
	}

	tr, c, err := tarReader(ctx, ix.Path, ix.Kind)
	if err != nil {
		return nil, err
	}
	for pos := 0; ; pos++ {
		if err := ctx.Err(); err != nil {
			c.Close()
			return nil, err
		}
		hdr, err := nextHeader(tr)
		if err != nil {
			c.Close()
			if errors.Is(err, io.EOF) {
				return nil, errChanged
			}
			return nil, err
		}
		if pos < e.pos {
			continue
		}
		if hdr.Name != e.Name {
			c.Close()
			return nil, errChanged
		}
		return readCloser{tr, c.Close}, nil
	}
}

// ReadLink returns where a symlink entry points
func (ix *ArchiveIndex) ReadLink(e ArchiveEntry) (string, error) {
	if e.Mode&os.ModeSymlink == 0 {
		return "", fmt.Errorf("%s is not a symlink", e.Name)
	}
	if ix.Kind != "zip" {
		return e.Link, nil
	}
	zr, err := openZip(ix.Path)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	if e.pos >= len(zr.File) || zr.File[e.pos].Name != e.Name {
		return "", errChanged
	}
	return readZipLink(zr.File[e.pos])
}

// ExtractEntries copies entries of the archive ix indexes into dest, an
// existing folder: each of inners, a file, symlink or folder with all that
// is in it, goes into dest under its own name. It returns the paths it
// made there.
//
// It is as safe as Extract, and goes through the same checks: everything
// is first extracted into a new hidden folder in dest, through an os.Root
// opened on it, and only once all of it is there does each item move into
// place, never over anything. Entries named outside the archive are
// refused, as are symlinks leading outside what is copied, and so is the
// whole copy then: nothing is left of it. A name already taken in dest is
// refused before anything is written. A hard link is copied only with the
// entry it links to. If the copy fails or is cancelled, nothing is made;
// the error then matches ErrNotCreated.
func (t *Task) ExtractEntries(ix *ArchiveIndex, inners []string, dest string) ([]string, error) {
	type pick struct{ inner, name string }
	picks := make([]pick, 0, len(inners))
	for _, in := range inners {
		e, ok := ix.Entry(in)
		switch {
		case !ok:
			return nil, notCreated{fmt.Errorf("%s is not in %s", in, filepath.Base(ix.Path))}
		case e.Unsafe:
			return nil, notCreated{refuse("unsafe path in archive: %q", e.Name)}
		}
		picks = append(picks, pick{in, path.Base(in)})
	}
	for i, a := range picks {
		for _, b := range picks[i+1:] {
			switch {
			case strings.HasPrefix(b.inner, a.inner+"/"), strings.HasPrefix(a.inner, b.inner+"/"):
				return nil, notCreated{fmt.Errorf("can't copy %s and something inside it together", min(a.name, b.name))}
			case strings.EqualFold(a.name, b.name):
				return nil, notCreated{fmt.Errorf("%s and %s have the same name", a.inner, b.inner)}
			}
		}
		if Exists(filepath.Join(dest, a.name)) {
			return nil, notCreated{fmt.Errorf("%s already exists in %s", a.name, filepath.Base(dest))}
		}
	}

	// Each entry under a pick, renamed to start from the pick's own name
	choose := func(name string) (string, bool) {
		inner, ok, _ := entryPath(name)
		if !ok {
			return "", false
		}
		for _, p := range picks {
			if inner == p.inner {
				return p.name, true
			}
			if rest, ok := strings.CutPrefix(inner, p.inner+"/"); ok {
				return p.name + "/" + rest, true
			}
		}
		return "", false
	}

	tmp := tempName(dest, partialPrefix)
	x := newExtractor(t)
	var err error
	switch ix.Kind {
	case "zip":
		err = t.extractZip(x, ix.Path, tmp, choose)
	case "tar":
		err = t.extractTar(x, ix.Path, tmp, "", choose)
	case "tar.gz":
		err = t.extractTar(x, ix.Path, tmp, "gz", choose)
	case "tar.bz2":
		err = t.extractTar(x, ix.Path, tmp, "bz2", choose)
	default:
		err = fmt.Errorf("%s is not an archive sushi can open", filepath.Base(ix.Path))
	}
	if err == nil {
		err = t.Err()
	}
	if err != nil {
		// Nothing in it is wanted, and it is the copy's own
		if x.made {
			x.removeRefused(tmp)
		}
		return nil, notCreated{err}
	}

	var made []string
	for _, p := range picks {
		from, to := filepath.Join(tmp, p.name), filepath.Join(dest, p.name)
		var rerr error
		if !Exists(from) {
			rerr = fmt.Errorf("%s was not found in %s", p.inner, filepath.Base(ix.Path))
		} else if rerr = renameNoReplace(from, to); errors.Is(rerr, os.ErrExist) {
			rerr = inTheWay(to)
		}
		if rerr != nil {
			if err == nil {
				err = rerr
			}
			continue
		}
		made = append(made, to)
	}
	x.removeRefused(tmp)
	if err != nil && len(made) == 0 {
		err = notCreated{err}
	}
	return made, err
}
