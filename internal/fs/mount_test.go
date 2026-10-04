package fs

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// otherVolume makes the folders at paths look like the roots of volumes
// mounted there: they, and everything below them, are on another device
func otherVolume(t *testing.T, paths ...string) {
	t.Helper()
	real := deviceOf
	deviceOf = func(path string, info os.FileInfo) uint64 {
		for _, p := range paths {
			if within(p, path) {
				return real(path, info) + 1000
			}
		}
		return real(path, info)
	}
	t.Cleanup(func() { deviceOf = real })
}

// volumeWith makes a folder, vol, holding a file, in a folder of its own
func volumeWith(t *testing.T) (root, vol, file string) {
	t.Helper()
	root = t.TempDir()
	vol = filepath.Join(root, "USB")
	os.MkdirAll(filepath.Join(vol, "photos"), 0755)
	file = filepath.Join(vol, "photos", "beach.jpg")
	writeFile(t, file, "sand")
	return root, vol, file
}

// assertMountRefused fails unless err refuses a mounted volume and the
// volume's file is still there
func assertMountRefused(t *testing.T, what string, err error, file string) {
	t.Helper()
	if !errors.Is(err, ErrMounted) || !strings.Contains(err.Error(), "is a mounted volume") {
		t.Fatalf("%s: err = %v, want a mounted volume refused", what, err)
	}
	if readFile(t, file) != "sand" {
		t.Fatalf("%s: the volume's file is gone", what)
	}
}

func TestMountedVolumesAreNeverEmptied(t *testing.T) {
	// The rename fails as it does for a volume, so trashing and moving
	// would go on to copy and delete
	acrossFilesystems(t)
	_, vol, file := volumeWith(t)
	otherVolume(t, vol)

	tr := testTrash(t)
	_, err := tr.Put(background(), vol)
	assertMountRefused(t, "Put", err, file)
	if got, _ := os.ReadDir(tr.Files); len(got) != 0 {
		t.Fatalf("the trash holds %d items: the volume was copied into it", len(got))
	}
	if got, _ := os.ReadDir(tr.Info); len(got) != 0 {
		t.Fatalf("the trash holds %d info files", len(got))
	}

	err = background().Move(vol, filepath.Join(t.TempDir(), "USB"))
	assertMountRefused(t, "Move", err, file)
	assertMountRefused(t, "Delete", background().Delete(vol), file)
	assertMountRefused(t, "DeletePath", DeletePath(vol), file)
	assertMountRefused(t, "Delete with a trailing slash", background().Delete(vol+"/"), file)

	// The root is a volume, whatever the device numbers say
	if err := checkNotMount("/", mustLstat(t, "/")); !errors.Is(err, ErrMounted) {
		t.Errorf("/ wasn't refused: %v", err)
	}
	// A link to a volume is only a link
	link := filepath.Join(t.TempDir(), "link")
	symlink(t, vol, link)
	tr2 := testTrash(t)
	if _, err := tr2.Put(background(), link); err != nil {
		t.Fatalf("trashing a link to a volume: %v", err)
	}
	if readFile(t, file) != "sand" {
		t.Fatal("trashing the link emptied the volume")
	}
}

func mustLstat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func TestDeleteStopsAtAVolumeInside(t *testing.T) {
	root, vol, file := volumeWith(t)
	otherVolume(t, vol)
	writeFile(t, filepath.Join(root, "notes.txt"), "notes")

	err := background().Delete(root)
	if !errors.Is(err, ErrMounted) || !strings.Contains(err.Error(), "USB inside it is a mounted volume") {
		t.Fatalf("Delete = %v, want it to stop at the volume", err)
	}
	if readFile(t, file) != "sand" {
		t.Fatal("deleting the folder emptied the volume inside it")
	}
}

func TestMoveLeavesAVolumeInside(t *testing.T) {
	// Across filesystems a move copies and then deletes what it copied: a
	// volume inside is copied, but not emptied
	acrossFilesystems(t)
	root, vol, file := volumeWith(t)
	otherVolume(t, vol)
	writeFile(t, filepath.Join(root, "notes.txt"), "notes")
	dst := filepath.Join(t.TempDir(), "moved")

	err := background().Move(root, dst)
	if err == nil || !strings.Contains(err.Error(), "USB is a mounted volume, so it was copied but left where it was") {
		t.Fatalf("Move = %v", err)
	}
	if readFile(t, file) != "sand" {
		t.Fatal("the move emptied the volume inside the folder")
	}
	if readFile(t, filepath.Join(dst, "notes.txt")) != "notes" || Exists(filepath.Join(root, "notes.txt")) {
		t.Fatal("the rest of the folder wasn't moved")
	}
}

// attachImage mounts a tiny disk image at a folder of the test's and
// returns that folder, skipping the test where that can't be done
func attachImage(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("disk images are macOS's")
	}
	if testing.Short() {
		t.Skip("mounts a disk image")
	}
	dir := t.TempDir()
	img := filepath.Join(dir, "vol.dmg")
	if out, err := exec.Command("hdiutil", "create", "-size", "1m", "-fs", "HFS+", "-layout", "NONE",
		"-volname", "SushiTest", img).CombinedOutput(); err != nil {
		t.Skipf("can't make a disk image: %v\n%s", err, out)
	}
	mnt := filepath.Join(dir, "box", "mnt")
	os.MkdirAll(mnt, 0755)
	if out, err := exec.Command("hdiutil", "attach", "-nobrowse", "-noautoopen", "-mountpoint", mnt, img).CombinedOutput(); err != nil {
		t.Skipf("can't mount a disk image: %v\n%s", err, out)
	}
	// Before the folder goes, which it can't while mounted
	t.Cleanup(func() {
		if out, err := exec.Command("hdiutil", "detach", "-force", mnt).CombinedOutput(); err != nil {
			t.Errorf("detaching %s: %v\n%s", mnt, err, out)
		}
	})
	return mnt
}

func TestRealMountedVolumeIsRefused(t *testing.T) {
	mnt := attachImage(t)
	file := filepath.Join(mnt, "keep.txt")
	writeFile(t, file, "sand")

	// The system says a volume is mounted there, and the devices differ
	if resolved, err := filepath.EvalSymlinks(mnt); err != nil || !mountedOn(resolved) {
		t.Errorf("mountedOn(%s) = false, %v", resolved, err)
	}
	tr := testTrash(t)
	_, err := tr.Put(background(), mnt)
	assertMountRefused(t, "Put", err, file)
	err = background().Move(mnt, filepath.Join(t.TempDir(), "moved"))
	assertMountRefused(t, "Move", err, file)
	assertMountRefused(t, "Delete", background().Delete(mnt), file)
	if err := background().Delete(filepath.Dir(mnt)); !errors.Is(err, ErrMounted) {
		t.Fatalf("deleting the folder holding the volume: %v", err)
	}
	if readFile(t, file) != "sand" {
		t.Fatal("deleting the folder holding the volume emptied it")
	}
}
