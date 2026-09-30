package fs

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSplitExt(t *testing.T) {
	for _, c := range []struct{ name, stem, ext string }{
		{"notes.txt", "notes", ".txt"},
		{"src.tar.gz", "src", ".tar.gz"},
		{"SRC.TAR.GZ", "SRC", ".TAR.GZ"},
		{"a.tgz", "a", ".tgz"},
		{".bashrc", ".bashrc", ""},
		{".tar.gz", ".tar", ".gz"},
		{"Makefile", "Makefile", ""},
		{"trailing.", "trailing.", ""},
		{"v1.2.3", "v1.2", ".3"},
	} {
		if stem, ext := SplitExt(c.name); stem != c.stem || ext != c.ext {
			t.Errorf("SplitExt(%q) = %q, %q; want %q, %q", c.name, stem, ext, c.stem, c.ext)
		}
	}
}

func TestCopyName(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "report.pdf")
	writeFile(t, f, "")
	if got := CopyName(f); got != "report copy.pdf" {
		t.Fatalf("first copy = %q", got)
	}
	writeFile(t, filepath.Join(dir, "report copy.pdf"), "")
	writeFile(t, filepath.Join(dir, "report copy 2.pdf"), "")
	if got := CopyName(f); got != "report copy 3.pdf" {
		t.Fatalf("third copy = %q", got)
	}

	d := filepath.Join(dir, "v1.2")
	os.Mkdir(d, 0755)
	if got := CopyName(d); got != "v1.2 copy" {
		t.Fatalf("directory copy = %q", got)
	}
	env := filepath.Join(dir, ".env")
	writeFile(t, env, "")
	if got := CopyName(env); got != ".env copy" {
		t.Fatalf("dotfile copy = %q", got)
	}
}

func TestStampNoticesChanges(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "d")
	os.Mkdir(dir, 0755)
	f := filepath.Join(dir, "f")
	writeFile(t, f, "abc")

	before, err := TakeStamp(dir)
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := TakeStamp(dir); again != before {
		t.Fatal("stamp changed without a change")
	}
	if before.Trivial() {
		t.Fatal("a folder with a file in it is not trivial")
	}

	later := time.Now().Add(time.Hour)
	os.Chtimes(f, later, later)
	if s, _ := TakeStamp(dir); s == before {
		t.Fatal("a newer file inside went unnoticed")
	}
	writeFile(t, filepath.Join(dir, "g"), "")
	if s, _ := TakeStamp(dir); s.Entries != 3 {
		t.Fatalf("entries = %d, want 3", s.Entries)
	}

	empty := filepath.Join(t.TempDir(), "empty")
	os.Mkdir(empty, 0755)
	link := filepath.Join(t.TempDir(), "link")
	os.Symlink("anywhere", link)
	for _, p := range []string{empty, link} {
		if s, _ := TakeStamp(p); !s.Trivial() {
			t.Errorf("%s: %+v should be trivial", filepath.Base(p), s)
		}
	}
	if s, _ := TakeStamp(link); s.Link != "anywhere" {
		t.Fatalf("link stamp = %+v", s)
	}
	if _, err := TakeStamp(filepath.Join(dir, "missing")); !os.IsNotExist(err) {
		t.Fatalf("missing file: %v", err)
	}
}
