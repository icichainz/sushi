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
