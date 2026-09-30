package fs

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// entry is one item of a test archive
type entry struct {
	name, body string
	mode       os.FileMode // Zero means a 0644 file; ModeDir and ModeSymlink as usual
	hardlink   bool        // tar only: body is the entry linked to
}

func writeZip(t *testing.T, path string, entries []entry) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		mode := e.mode
		if mode == 0 {
			mode = 0644
		}
		hdr := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		hdr.SetMode(mode)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(e.body))
	}
	zw.Close()
	os.WriteFile(path, buf.Bytes(), 0644)
}

func writeTar(t *testing.T, path string, gz bool, entries []entry) {
	t.Helper()
	var buf bytes.Buffer
	var zw *gzip.Writer
	tw := tar.NewWriter(&buf)
	if gz {
		zw = gzip.NewWriter(&buf)
		tw = tar.NewWriter(zw)
	}
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0644, Typeflag: tar.TypeReg, Size: int64(len(e.body)), ModTime: time.Now()}
		switch {
		case e.hardlink:
			hdr.Typeflag, hdr.Linkname, hdr.Size = tar.TypeLink, e.body, 0
		case e.mode&os.ModeSymlink != 0:
			hdr.Typeflag, hdr.Linkname, hdr.Size = tar.TypeSymlink, e.body, 0
		case e.mode.IsDir():
			hdr.Typeflag, hdr.Mode, hdr.Size = tar.TypeDir, 0755, 0
		case e.mode != 0:
			hdr.Mode = int64(e.mode.Perm())
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeReg {
			tw.Write([]byte(e.body))
		}
	}
	tw.Close()
	if zw != nil {
		zw.Close()
	}
	os.WriteFile(path, buf.Bytes(), 0644)
}

// sandbox returns a directory to extract into, with a file beside it that
// escaping entries would hit
func sandbox(t *testing.T) (root, dest string) {
	t.Helper()
	root = t.TempDir()
	writeFile(t, filepath.Join(root, "outside.txt"), "untouched")
	return root, filepath.Join(root, "dest")
}

func assertOutsideUntouched(t *testing.T, root string) {
	t.Helper()
	if readFile(t, filepath.Join(root, "outside.txt")) != "untouched" {
		t.Fatal("a file outside the destination was changed")
	}
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		switch e.Name() {
		case "outside.txt", "dest", "a.zip", "a.tar", "a.tgz":
		default:
			t.Fatalf("%s was created outside the destination", e.Name())
		}
	}
}

func TestZipRoundTrip(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "photos")
	os.MkdirAll(filepath.Join(src, "2024"), 0755)
	writeFile(t, filepath.Join(src, "2024", "beach.jpg"), "sand")
	writeFile(t, filepath.Join(src, "run.sh"), "#!/bin/sh")
	os.Chmod(filepath.Join(src, "run.sh"), 0755)
	old := time.Date(2021, 6, 1, 12, 0, 0, 0, time.UTC)
	os.Chtimes(filepath.Join(src, "run.sh"), old, old)
	os.Symlink("run.sh", filepath.Join(src, "latest"))
	notes := filepath.Join(root, "notes.txt")
	writeFile(t, notes, "hello")

	archive := filepath.Join(root, "out.zip")
	task := background()
	task.Count(src, notes)
	if err := task.CreateZip(archive, []string{src, notes}); err != nil {
		t.Fatal(err)
	}
	if p := task.Progress(); p.Files != p.TotalFiles || p.Files != 4 {
		t.Fatalf("progress after zipping: %+v", p)
	}
	if err := background().CreateZip(archive, []string{notes}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("zipping over an existing archive: %v", err)
	}

	dest := filepath.Join(root, "out")
	if err := background().Extract(archive, dest); err != nil {
		t.Fatal(err)
	}
	if readFile(t, filepath.Join(dest, "photos", "2024", "beach.jpg")) != "sand" || readFile(t, filepath.Join(dest, "notes.txt")) != "hello" {
		t.Fatal("contents differ after the round trip")
	}
	info, _ := os.Stat(filepath.Join(dest, "photos", "run.sh"))
	if info.Mode().Perm() != 0755 || !info.ModTime().Equal(old) {
		t.Fatalf("run.sh: mode %v, modified %v", info.Mode().Perm(), info.ModTime())
	}
	if target, err := os.Readlink(filepath.Join(dest, "photos", "latest")); err != nil || target != "run.sh" {
		t.Fatalf("symlink = %q, %v", target, err)
	}
}

func TestZipSkipsItself(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a"), "a")
	// The archive is written inside the folder being archived
	archive := filepath.Join(dir, "self.zip")
	if err := background().CreateZip(archive, []string{dir}); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, "self.zip") {
			t.Fatal("the archive contains itself")
		}
	}
}

func TestCancelledZipIsRemoved(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "big"), bytes.Repeat([]byte("z"), 2*chunkSize), 0644)
	archive := filepath.Join(t.TempDir(), "a.zip")

	task, _ := cancelWhen(func(p Progress) bool { return p.Bytes > 0 })
	if err := task.CreateZip(archive, []string{src}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if Exists(archive) {
		t.Fatal("the partial archive was left behind")
	}
}

func TestExtractTarAndTgz(t *testing.T) {
	entries := []entry{
		{name: "./", mode: os.ModeDir},
		{name: "./src/", mode: os.ModeDir},
		{name: "./src/main.go", body: "package main"},
		{name: "./bin/tool", body: "binary", mode: 0755},
		{name: "./src/link.go", body: "main.go", mode: os.ModeSymlink},
		{name: "./copy.go", body: "./src/main.go", hardlink: true},
	}
	for _, gz := range []bool{false, true} {
		root := t.TempDir()
		archive := filepath.Join(root, "a.tar")
		if gz {
			archive = filepath.Join(root, "a.tgz")
		}
		writeTar(t, archive, gz, entries)

		dest := filepath.Join(root, "dest")
		task := background()
		if err := task.Extract(archive, dest); err != nil {
			t.Fatalf("gz=%v: %v", gz, err)
		}
		if readFile(t, filepath.Join(dest, "src", "link.go")) != "package main" || readFile(t, filepath.Join(dest, "copy.go")) != "package main" {
			t.Fatalf("gz=%v: links not extracted", gz)
		}
		if info, _ := os.Stat(filepath.Join(dest, "bin", "tool")); info.Mode().Perm() != 0755 {
			t.Fatalf("gz=%v: mode %v", gz, info.Mode().Perm())
		}
		if p := task.Progress(); p.Bytes != p.TotalBytes || p.Files != 4 {
			t.Fatalf("gz=%v: progress %+v", gz, p)
		}
	}
}

func TestExtractRefusesUnsafeArchives(t *testing.T) {
	for _, c := range []struct {
		name    string
		entries []entry
		err     string
	}{
		{"parent path", []entry{{name: "../outside.txt", body: "pwned"}}, "unsafe path"},
		{"parent path inside", []entry{{name: "ok/../../outside.txt", body: "pwned"}}, "unsafe path"},
		{"absolute path", []entry{{name: "{root}/absolute.txt", body: "pwned"}}, "unsafe path"},
		{"symlink out", []entry{{name: "link", body: "../outside.txt", mode: os.ModeSymlink}}, "points outside"},
		{"absolute symlink", []entry{{name: "link", body: "/etc", mode: os.ModeSymlink}}, "points outside"},
		{"write through symlink", []entry{
			{name: "dir", body: "..", mode: os.ModeSymlink},
			{name: "dir/outside.txt", body: "pwned"},
		}, "points outside"},
		{"chained symlinks", []entry{
			{name: "here", body: ".", mode: os.ModeSymlink},
			{name: "up", body: "here/..", mode: os.ModeSymlink},
		}, "points outside"},
		{"duplicate", []entry{{name: "a", body: "1"}, {name: "a", body: "2"}}, "twice"},
	} {
		for _, kind := range []string{"zip", "tar"} {
			root, dest := sandbox(t)
			archive := filepath.Join(root, "a."+kind)
			entries := append([]entry(nil), c.entries...)
			for i := range entries {
				entries[i].name = strings.ReplaceAll(entries[i].name, "{root}", filepath.ToSlash(root))
			}
			if kind == "zip" {
				writeZip(t, archive, entries)
			} else {
				writeTar(t, archive, false, entries)
			}

			err := background().Extract(archive, dest)
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%s (%s): err = %v, want %q", c.name, kind, err, c.err)
			}
			assertOutsideUntouched(t, root)
			if kind == "zip" && c.err == "unsafe path" && Exists(dest) {
				t.Errorf("%s (zip): nothing should be written when a name is unsafe", c.name)
			}
			// No link that survived may lead out
			filepath.WalkDir(dest, func(path string, d os.DirEntry, err error) error {
				if err == nil && d.Type()&os.ModeSymlink != 0 {
					resolved, rerr := filepath.EvalSymlinks(path)
					realDest, _ := filepath.EvalSymlinks(dest)
					if rerr == nil && !within(realDest, resolved) {
						t.Errorf("%s (%s): %s still leads to %s", c.name, kind, path, resolved)
					}
				}
				return nil
			})
		}
	}
}

func TestExtractNeedsANewFolder(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "a.zip")
	writeZip(t, archive, []entry{{name: "f", body: "x"}})
	dest := filepath.Join(root, "dest")
	os.Mkdir(dest, 0755)
	writeFile(t, filepath.Join(dest, "f"), "mine")

	if err := background().Extract(archive, dest); err == nil {
		t.Fatal("extracting into an existing folder should fail")
	}
	if readFile(t, filepath.Join(dest, "f")) != "mine" {
		t.Fatal("an existing file was replaced")
	}
	if err := background().Extract(filepath.Join(root, "notes.txt"), filepath.Join(root, "x")); err == nil || !strings.Contains(err.Error(), "not a zip") {
		t.Fatalf("unsupported archive: %v", err)
	}
}

func TestCancelledExtractKeepsFinishedFiles(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "a.zip")
	writeZip(t, archive, []entry{
		{name: "1-small", body: "done"},
		{name: "2-big", body: strings.Repeat("b", 2*chunkSize)},
	})

	task, _ := cancelWhen(func(p Progress) bool { return p.Bytes > int64(len("done")) })
	dest := filepath.Join(root, "dest")
	if err := task.Extract(archive, dest); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if readFile(t, filepath.Join(dest, "1-small")) != "done" || Exists(filepath.Join(dest, "2-big")) {
		t.Fatal("want the finished file kept and the partial one removed")
	}
	if p := task.Progress(); p.Files != 1 || p.TotalFiles != 2 {
		t.Fatalf("progress = %+v", p)
	}
}

func TestArchiveKind(t *testing.T) {
	for name, want := range map[string]string{"a.zip": "zip", "A.ZIP": "zip", "a.tar": "tar", "a.tar.gz": "tar.gz", "a.tgz": "tar.gz", "a.gz": "", "a.rar": "", "zip": ""} {
		if got := ArchiveKind(name); got != want {
			t.Errorf("ArchiveKind(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestExtractMasksModesWithTheUmask(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows files have no Unix permissions")
	}
	// Zips made on Windows store everything as 0777 and 0666
	real := umask
	umask = 0o027
	t.Cleanup(func() { umask = real })

	entries := []entry{
		{name: "open/", mode: os.ModeDir | 0777},
		{name: "open/notes.txt", body: "n", mode: 0666},
		{name: "run.sh", body: "#!/bin/sh", mode: 0777},
	}
	for _, kind := range []string{"zip", "tar"} {
		root := t.TempDir()
		archive := filepath.Join(root, "a."+kind)
		writeArchive(t, archive, kind, entries)
		dest := filepath.Join(root, "dest")
		if err := background().Extract(archive, dest); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		want := map[string]os.FileMode{"open/notes.txt": 0640, "run.sh": 0750}
		if kind == "zip" {
			want["open"] = 0750 // The tar helper always writes folders as 0755
		}
		for name, mode := range want {
			info, err := os.Stat(filepath.Join(dest, filepath.FromSlash(name)))
			if err != nil || info.Mode().Perm() != mode {
				t.Errorf("%s: %s is %v, want %v (%v)", kind, name, info.Mode().Perm(), mode, err)
			}
		}
	}
}

// writeArchive writes entries as a zip or an uncompressed tar
func writeArchive(t *testing.T, path, kind string, entries []entry) {
	t.Helper()
	if kind == "zip" {
		writeZip(t, path, entries)
	} else {
		writeTar(t, path, false, entries)
	}
}

// escapesFrom lists what an extraction into dest put in home outside dest,
// which should be nothing but the folders leading to dest, and the links
// inside dest that lead out of it, dangling or not
func escapesFrom(t *testing.T, home, dest string) []string {
	t.Helper()
	var bad []string
	realDest, _ := filepath.EvalSymlinks(dest)
	filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !within(dest, path) && !within(path, dest) {
			bad = append(bad, path)
		}
		if d.Type()&os.ModeSymlink == 0 || !within(dest, path) {
			return nil
		}
		target, _ := os.Readlink(path)
		dir, _ := filepath.EvalSymlinks(filepath.Dir(path))
		rel, _ := filepath.Rel(realDest, filepath.Join(dir, target))
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			rel, _ = filepath.Rel(realDest, resolved)
		}
		if !filepath.IsLocal(rel) && rel != "." {
			bad = append(bad, path+" -> "+target)
		}
		return nil
	})
	return bad
}

func TestExtractCannotEscapeThroughChainedLinks(t *testing.T) {
	// Extracted into ~/Downloads/evil, this archive used to create
	// ~/.ssh/authorized_keys: "a" is the folder itself, so "a/b" put ".."
	// at "b", "b/c" then led to the home folder, and the last entry was
	// made through it
	entries := []entry{
		{name: "payload", body: "ssh-ed25519 AAAA attacker"},
		{name: "a", body: ".", mode: os.ModeSymlink},
		{name: "a/b", body: "..", mode: os.ModeSymlink},
		{name: "b/c", body: "..", mode: os.ModeSymlink},
		{name: "b/c/.ssh/authorized_keys", body: "../Downloads/evil/payload", mode: os.ModeSymlink},
	}
	for _, kind := range []string{"zip", "tar"} {
		home := t.TempDir()
		downloads := filepath.Join(home, "Downloads")
		os.Mkdir(downloads, 0755)
		archive := filepath.Join(t.TempDir(), "evil."+kind)
		writeArchive(t, archive, kind, entries)

		dest := filepath.Join(downloads, "evil")
		err := background().Extract(archive, dest)
		if err == nil || !strings.Contains(err.Error(), "unsafe path") {
			t.Errorf("%s: err = %v, want the entry through a symlink refused", kind, err)
		}
		if Exists(filepath.Join(home, ".ssh")) {
			t.Errorf("%s: ~/.ssh was created", kind)
		}
		if bad := escapesFrom(t, home, dest); len(bad) != 0 {
			t.Errorf("%s: outside the destination: %v", kind, bad)
		}
	}
}

func TestExtractRemovesDanglingLinksThatLeadOut(t *testing.T) {
	// "z" names "a/../nowhere", which is "nowhere" by name but, with "a"
	// the folder itself, "../nowhere" on disk: missing for now, and outside
	entries := []entry{
		{name: "a", body: ".", mode: os.ModeSymlink},
		{name: "z", body: "a/../nowhere", mode: os.ModeSymlink},
	}
	for _, kind := range []string{"zip", "tar"} {
		home := t.TempDir()
		archive := filepath.Join(t.TempDir(), "a."+kind)
		writeArchive(t, archive, kind, entries)
		dest := filepath.Join(home, "dest")
		err := background().Extract(archive, dest)
		if err == nil || !strings.Contains(err.Error(), "symlink z points outside") {
			t.Errorf("%s: err = %v", kind, err)
		}
		if Exists(filepath.Join(dest, "z")) || !Exists(filepath.Join(dest, "a")) {
			t.Errorf("%s: want z removed and a kept", kind)
		}
		if bad := escapesFrom(t, home, dest); len(bad) != 0 {
			t.Errorf("%s: outside the destination: %v", kind, bad)
		}
	}
}

func TestExtractRefusesEntriesInsideLinks(t *testing.T) {
	for _, entries := range [][]entry{
		// The link first, then an entry through it
		{{name: "sub/", mode: os.ModeDir}, {name: "a", body: "sub", mode: os.ModeSymlink}, {name: "a/f", body: "x"}},
		// The entry first, then a link where its folder is
		{{name: "sub/", mode: os.ModeDir}, {name: "a/f", body: "x"}, {name: "a", body: "sub", mode: os.ModeSymlink}},
	} {
		for _, kind := range []string{"zip", "tar"} {
			root := t.TempDir()
			archive := filepath.Join(root, "a."+kind)
			writeArchive(t, archive, kind, entries)
			err := background().Extract(archive, filepath.Join(root, "dest"))
			if err == nil || !strings.Contains(err.Error(), "inside the symlink") {
				t.Errorf("%s, %s second: err = %v", kind, entries[1].name, err)
			}
		}
	}
}

func TestExtractKeepsLinksThatStayInside(t *testing.T) {
	entries := []entry{
		{name: "usr/lib/libfoo.so.1", body: "elf"},
		{name: "usr/lib/libfoo.so", body: "libfoo.so.1", mode: os.ModeSymlink},
		{name: "lib", body: "usr/lib", mode: os.ModeSymlink},
		{name: "bin/foo", body: "../lib/libfoo.so", mode: os.ModeSymlink},
		{name: "later", body: "usr/not-yet", mode: os.ModeSymlink},
	}
	for _, kind := range []string{"zip", "tar"} {
		root := t.TempDir()
		archive := filepath.Join(root, "a."+kind)
		writeArchive(t, archive, kind, entries)
		dest := filepath.Join(root, "dest")
		if err := background().Extract(archive, dest); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if readFile(t, filepath.Join(dest, "bin", "foo")) != "elf" {
			t.Fatalf("%s: the chain of links was not kept", kind)
		}
		if target, err := os.Readlink(filepath.Join(dest, "later")); err != nil || target != "usr/not-yet" {
			t.Fatalf("%s: a dangling link that stays inside was removed: %q, %v", kind, target, err)
		}
	}
}
