package fs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/icichainz/sushi/internal/tags"
)

func TestTagsAreReadOnLocalVolumesOnly(t *testing.T) {
	if !tags.Supported() {
		t.Skip("no Finder tags here")
	}
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		writeFile(t, filepath.Join(dir, name), "")
		if err := tags.Write(filepath.Join(dir, name), []tags.Tag{{Name: "Red", Color: tags.Red}}); err != nil {
			t.Skipf("can't tag files here: %v", err)
		}
	}
	os.Mkdir(filepath.Join(dir, "sub"), 0755)

	// A temporary folder is on a local volume, and its tags are read
	if !isLocal(dir) {
		t.Fatalf("%s isn't on a local volume", dir)
	}
	files, err := ScanDirectory(dir, ScanOptions{Tags: true})
	if err != nil || len(findFile(t, files, "a.txt").Tags) != 1 {
		t.Fatalf("tags %q, %v", findFile(t, files, "a.txt").Tags, err)
	}

	// On a network volume they aren't, and the volume is asked about once
	asked := 0
	defer func(f func(string) bool) { volumeIsLocal = f }(volumeIsLocal)
	volumeIsLocal = func(string) bool { asked++; return false }
	files, err = ScanDirectory(dir, ScanOptions{Tags: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Tags != nil {
			t.Errorf("%s: tags %q read on a network volume", f.Name, f.Tags)
		}
	}
	if asked != 1 {
		t.Errorf("asked about the volume %d times", asked)
	}
	// Nor at all with tags off
	asked = 0
	if ScanDirectory(dir, ScanOptions{}); asked != 0 {
		t.Errorf("asked about the volume %d times with tags off", asked)
	}
}
