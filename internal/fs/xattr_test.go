package fs

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/icichainz/sushi/internal/tags"
	"golang.org/x/sys/unix"
)

func TestCopiesKeepTagsAndExtendedAttributes(t *testing.T) {
	if !tags.Supported() {
		t.Skip("no Finder tags here")
	}
	root := t.TempDir()
	src := filepath.Join(root, "src")
	os.Mkdir(src, 0755)
	file := filepath.Join(src, "report.txt")
	writeFile(t, file, "report")
	link := filepath.Join(src, "link")
	if err := os.Symlink("report.txt", link); err != nil {
		t.Fatal(err)
	}
	list := []tags.Tag{{Name: "Red", Color: tags.Red}, {Name: "Work"}}
	for _, p := range []string{src, file, link} {
		if err := tags.Write(p, list); err != nil {
			t.Skipf("can't tag files here: %v", err)
		}
	}
	// Any attribute, not only tags, and one larger than a first read takes
	big := bytes.Repeat([]byte("sushi "), 1000)
	if err := unix.Setxattr(file, "com.example.sushi", big, 0); err != nil {
		t.Skipf("can't set attributes here: %v", err)
	}
	// A source that is read-only makes a copy that is, once it has them
	os.Chmod(file, 0444)

	check := func(label, dir string) {
		t.Helper()
		for _, p := range []string{dir, filepath.Join(dir, "report.txt"), filepath.Join(dir, "link")} {
			if got, err := tags.Read(p); err != nil || !tags.Equal(got, list) {
				t.Errorf("%s: %s has tags %q (%v)", label, filepath.Base(p), got, err)
			}
		}
		copied := filepath.Join(dir, "report.txt")
		if got, err := getXattr(copied, "com.example.sushi"); err != nil || !bytes.Equal(got, big) {
			t.Errorf("%s: attribute of %d bytes (%v), want %d", label, len(got), err, len(big))
		}
		if info, err := os.Stat(copied); err != nil || info.Mode().Perm() != 0444 {
			t.Errorf("%s: mode %v (%v)", label, info.Mode(), err)
		}
	}

	if err := CopyPath(src, filepath.Join(root, "copy")); err != nil {
		t.Fatal(err)
	}
	check("copy", filepath.Join(root, "copy"))
	if err := NewTask(context.Background(), 0, nil).CopyNew(src, filepath.Join(root, "src copy")); err != nil {
		t.Fatal(err)
	}
	check("duplicate", filepath.Join(root, "src copy"))
	acrossFilesystems(t)
	if err := NewTask(context.Background(), 0, nil).Move(src, filepath.Join(root, "moved")); err != nil {
		t.Fatal(err)
	}
	check("move to another volume", filepath.Join(root, "moved"))
}

func TestCopyIgnoresAttributesTheDestinationRefuses(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "a.txt"), filepath.Join(root, "b.txt")
	writeFile(t, src, "a")
	writeFile(t, dst, "")
	// The system's own, and a name no volume takes: neither fails the copy
	copyXattrs(src, dst)
	unix.Setxattr(src, "com.apple.rootless", []byte("x"), 0)
	if err := CopyPath(src, filepath.Join(root, "c.txt")); err != nil {
		t.Fatalf("copy failed over an attribute: %v", err)
	}
	if names, err := listXattrs(filepath.Join(root, "missing")); err == nil || names != nil {
		t.Fatalf("listed %q for a missing file", names)
	}
}
