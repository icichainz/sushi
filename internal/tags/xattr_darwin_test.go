package tags

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// needTools skips a test that cross-checks with macOS's own tools when
// they aren't there
func needTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"xattr", "plutil"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found", tool)
		}
	}
}

// tempFile creates an empty file in a temp dir
func tempFile(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// plutilStrings reads the tags attribute of path with xattr and plutil,
// as the strings it holds
func plutilStrings(t *testing.T, path string) []string {
	t.Helper()
	out, err := exec.Command("xattr", "-px", Attr, path).Output()
	if err != nil {
		t.Fatalf("xattr -px: %v", err)
	}
	data, err := hex.DecodeString(strings.Join(strings.Fields(string(out)), ""))
	if err != nil {
		t.Fatal(err)
	}
	plist := filepath.Join(t.TempDir(), "tags.plist")
	if err := os.WriteFile(plist, data, 0644); err != nil {
		t.Fatal(err)
	}
	js, err := exec.Command("plutil", "-convert", "json", "-o", "-", plist).Output()
	if err != nil {
		t.Fatalf("plutil can't read what Write wrote: %v", err)
	}
	var strs []string
	if err := json.Unmarshal(js, &strs); err != nil {
		t.Fatalf("%s: %v", js, err)
	}
	return strs
}

func TestWriteIsReadByMacOSTools(t *testing.T) {
	needTools(t)
	path := tempFile(t, "report.pdf")
	list := []Tag{{"Red", Red}, {"Work", None}, {"Projet été", Blue}, {"日本", None}}
	if err := Write(path, list); err != nil {
		t.Fatal(err)
	}
	want := []string{"Red\n6", "Work", "Projet été\n4", "日本"}
	if got := plutilStrings(t, path); !slices.Equal(got, want) {
		t.Fatalf("plutil reads %q, want %q", got, want)
	}
	got, err := Read(path)
	if err != nil || !Equal(got, list) {
		t.Fatalf("Read = %q, %v; want %q", got, err, list)
	}
}

func TestReadsWhatMacOSToolsWrite(t *testing.T) {
	needTools(t)
	dir := t.TempDir()
	xml := filepath.Join(dir, "tags.xml")
	bin := filepath.Join(dir, "tags.bin")
	os.WriteFile(xml, []byte(`<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><array><string>Green
2</string><string>Ünïcödé ✓</string><string>Plain</string></array></plist>`), 0644)
	if out, err := exec.Command("plutil", "-convert", "binary1", "-o", bin, xml).CombinedOutput(); err != nil {
		t.Fatalf("plutil: %v: %s", err, out)
	}
	data, _ := os.ReadFile(bin)

	path := tempFile(t, "notes.txt")
	if out, err := exec.Command("xattr", "-wx", Attr, hex.EncodeToString(data), path).CombinedOutput(); err != nil {
		t.Fatalf("xattr -wx: %v: %s", err, out)
	}
	got, err := Read(path)
	want := []Tag{{"Green", Green}, {"Ünïcödé ✓", None}, {"Plain", None}}
	if err != nil || !Equal(got, want) {
		t.Fatalf("Read = %q, %v; want %q", got, err, want)
	}
}

func TestNoTagsRemovesTheAttribute(t *testing.T) {
	path := tempFile(t, "a.txt")
	if err := Write(path, []Tag{{"Red", Red}}); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := unix.Lgetxattr(path, Attr, nil); err != unix.ENOATTR {
		t.Fatalf("attribute still there (err %v)", err)
	}
	// Removing what isn't there is fine
	if err := Write(path, nil); err != nil {
		t.Fatal(err)
	}
	if got, err := Read(path); err != nil || got != nil {
		t.Fatalf("Read = %q, %v", got, err)
	}
}

func TestReadPassesOverOtherAttributes(t *testing.T) {
	path := tempFile(t, "a.txt")
	// More attribute names than the first buffer holds, then the tags
	for i := range 40 {
		if err := unix.Setxattr(path, fmt.Sprintf("org.example.a-rather-long-attribute-name-%02d", i), []byte("x"), 0); err != nil {
			t.Skipf("can't set attributes here: %v", err)
		}
	}
	if got, err := Read(path); err != nil || got != nil {
		t.Fatalf("untagged: Read = %q, %v", got, err)
	}
	if err := Write(path, []Tag{{"Blue", Blue}}); err != nil {
		t.Fatal(err)
	}
	if got, err := Read(path); err != nil || !Equal(got, []Tag{{"Blue", Blue}}) {
		t.Fatalf("tagged: Read = %q, %v", got, err)
	}
}

func TestSymlinksAreTaggedThemselves(t *testing.T) {
	target := tempFile(t, "target.txt")
	link := filepath.Join(filepath.Dir(target), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := Write(link, []Tag{{"Red", Red}}); err != nil {
		t.Skipf("this volume can't tag symlinks: %v", err)
	}
	if got, _ := Read(target); got != nil {
		t.Fatalf("tagging the link tagged its target: %q", got)
	}
	if got, _ := Read(link); !Equal(got, []Tag{{"Red", Red}}) {
		t.Fatalf("link tags = %q", got)
	}
}

// setLabel gives path Finder information with colour label c, besides
// another flag, as Finder did before tags
func setLabel(t *testing.T, path string, c Color) {
	t.Helper()
	info := make([]byte, finderInfoSize)
	info[8] = 0x40 // Another Finder flag, to be kept
	info[labelByte] = byte(c) << 1
	if err := unix.Lsetxattr(path, finderInfoAttr, info, 0); err != nil {
		t.Skipf("can't set Finder information here: %v", err)
	}
}

func TestColourLabelReadsAsATag(t *testing.T) {
	path := tempFile(t, "old.txt")
	setLabel(t, path, Orange)
	if got, err := Read(path); err != nil || !Equal(got, []Tag{{"Orange", Orange}}) {
		t.Fatalf("label only: Read = %q, %v", got, err)
	}

	// Not twice when a tag has its colour
	Write(path, []Tag{{"Work", None}, {"Done", Orange}})
	if got, _ := Read(path); !Equal(got, []Tag{{"Work", None}, {"Done", Orange}}) {
		t.Fatalf("Read = %q", got)
	}
}

func TestTakingAColourOffClearsItsLabel(t *testing.T) {
	path := tempFile(t, "old.txt")
	setLabel(t, path, Orange)

	// The label follows the colours left
	if err := Write(path, []Tag{{"Red", Red}, {"Work", None}}); err != nil {
		t.Fatal(err)
	}
	info, _ := getAttr(path, finderInfoAttr)
	if label(info) != Red || info[8] != 0x40 {
		t.Fatalf("Finder information = %x, want the red label and the other flag kept", info)
	}
	if got, _ := Read(path); !Equal(got, []Tag{{"Red", Red}, {"Work", None}}) {
		t.Fatalf("Read = %q", got)
	}

	// With no colour left, no label
	Write(path, []Tag{{"Work", None}})
	info, _ = getAttr(path, finderInfoAttr)
	if label(info) != None || info[8] != 0x40 {
		t.Fatalf("Finder information = %x", info)
	}
	if got, _ := Read(path); !Equal(got, []Tag{{"Work", None}}) {
		t.Fatalf("Read = %q, want the label gone", got)
	}

	// Finder information left all zeros goes
	path = tempFile(t, "plain.txt")
	blue := make([]byte, finderInfoSize)
	blue[labelByte] = byte(Blue) << 1
	if err := unix.Lsetxattr(path, finderInfoAttr, blue, 0); err != nil {
		t.Fatal(err)
	}
	if got, _ := Read(path); !Equal(got, []Tag{{"Blue", Blue}}) {
		t.Fatalf("Read = %q", got)
	}
	if err := Write(path, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := unix.Lgetxattr(path, finderInfoAttr, nil); err != unix.ENOATTR {
		t.Fatalf("empty Finder information kept (err %v)", err)
	}
	if got, _ := Read(path); got != nil {
		t.Fatalf("Read = %q", got)
	}
}

func TestReadMissingFile(t *testing.T) {
	if _, err := Read(filepath.Join(t.TempDir(), "gone")); err == nil {
		t.Fatal("no error for a file that isn't there")
	}
	if err := Write(filepath.Join(t.TempDir(), "gone"), []Tag{{"Red", Red}}); err == nil {
		t.Fatal("no error tagging a file that isn't there")
	}
}

func TestReadIsCheapOnUntaggedFiles(t *testing.T) {
	// Not a benchmark: just that untagged files are passed over without
	// asking for the attribute, which is what makes Read cheap
	dir := t.TempDir()
	for i := range 200 {
		os.WriteFile(filepath.Join(dir, fmt.Sprint(i)), nil, 0644)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if tagged, info, err := attrs(filepath.Join(dir, e.Name())); err != nil || tagged || info {
			t.Fatalf("%s: tags=%v info=%v err=%v", e.Name(), tagged, info, err)
		}
	}
}

func BenchmarkReadUntagged(b *testing.B) {
	path := filepath.Join(b.TempDir(), "a")
	os.WriteFile(path, nil, 0644)
	for b.Loop() {
		Read(path)
	}
}
