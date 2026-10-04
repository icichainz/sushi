package fs

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// taken returns an occupied function for PlanRenames backed by a set of
// paths, so planning is tested without files
func taken(paths ...string) func(path, src string) bool {
	set := make(map[string]bool)
	for _, p := range paths {
		set[filepath.FromSlash(p)] = true
	}
	return func(path, src string) bool { return set[path] && path != src }
}

func TestPlanRenames(t *testing.T) {
	a, b, c := "/d/a.txt", "/d/b.txt", "/d/c.txt"
	existing := taken(a, b, c, "/d/other.txt")

	for _, tc := range []struct {
		name   string
		paths  []string
		edited string
		want   []RenamePair
		err    string
	}{
		{"one rename", []string{a, b}, "a.txt\nB.md\n", []RenamePair{{b, "/d/B.md"}}, ""},
		{"no trailing newline, CRLF", []string{a, b}, "x.txt\r\nb.txt", []RenamePair{{a, "/d/x.txt"}}, ""},
		{"nothing changed", []string{a, b}, "a.txt\nb.txt\n\n\n", nil, ""},
		{"swap", []string{a, b}, "b.txt\na.txt\n", []RenamePair{{a, b}, {b, a}}, ""},
		{"rotate", []string{a, b, c}, "b.txt\nc.txt\na.txt\n", []RenamePair{{a, b}, {b, c}, {c, a}}, ""},
		{"other directories", []string{a, "/e/a.txt"}, "z\nz\n", []RenamePair{{a, "/d/z"}, {"/e/a.txt", "/e/z"}}, ""},
		{"too few lines", []string{a, b}, "a.txt\n", nil, "expected 2 names"},
		{"too many lines", []string{a}, "a.txt\nb.txt\n", nil, "expected 1 names"},
		{"blank line", []string{a, b}, "\nb.txt\n", nil, "line 1: name is empty"},
		{"separator", []string{a}, "sub/a.txt\n", nil, "line 1: name can't contain a path separator"},
		{"dot dot", []string{a}, "..\n", nil, "line 1"},
		{"duplicate", []string{a, b}, "same\nsame\n", nil, "lines 1 and 2 both say same"},
		{"existing file", []string{a}, "other.txt\n", nil, "line 1: other.txt already exists"},
		{"unchanged file in the way", []string{a, b}, "b.txt\nb.txt\n", nil, "line 1: b.txt already exists"},
		{"folder and its contents", []string{"/d/sub", "/d/sub/x"}, "s\ny\n", nil, "something inside it"},
	} {
		// Written with slashes for readability
		paths := make([]string, len(tc.paths))
		for i, path := range tc.paths {
			paths[i] = filepath.FromSlash(path)
		}
		var want []RenamePair
		for _, pair := range tc.want {
			want = append(want, RenamePair{filepath.FromSlash(pair.From), filepath.FromSlash(pair.To)})
		}
		got, err := PlanRenames(paths, tc.edited, existing)
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("%s: err = %v, want %q", tc.name, err, tc.err)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %v, %v; want %v", tc.name, got, err, want)
		}
	}
}

// contents maps the names in dir to what the files hold
func contents(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	out := make(map[string]string)
	for _, e := range entries {
		out[e.Name()] = readFile(t, filepath.Join(dir, e.Name()))
	}
	return out
}

func TestRenameAllSwapsAndRotates(t *testing.T) {
	dir := t.TempDir()
	p := func(name string) string { return filepath.Join(dir, name) }
	for _, n := range []string{"a", "b", "c"} {
		writeFile(t, p(n), n)
	}

	if err := RenameAll([]RenamePair{{p("a"), p("b")}, {p("b"), p("c")}, {p("c"), p("a")}}); err != nil {
		t.Fatal(err)
	}
	if got := contents(t, dir); !reflect.DeepEqual(got, map[string]string{"b": "a", "c": "b", "a": "c"}) {
		t.Fatalf("after rotating: %v", got)
	}
}

func TestRenameAllRollsBack(t *testing.T) {
	dir := t.TempDir()
	p := func(name string) string { return filepath.Join(dir, name) }
	writeFile(t, p("a"), "a")
	writeFile(t, p("b"), "b")
	// Appeared after planning, so the second rename can't happen
	writeFile(t, p("late"), "late")

	err := RenameAll([]RenamePair{{p("a"), p("x")}, {p("b"), p("late")}})
	if err == nil || !strings.Contains(err.Error(), "late already exists") || !errors.Is(err, os.ErrExist) {
		t.Fatalf("err = %v", err)
	}
	if got := contents(t, dir); !reflect.DeepEqual(got, map[string]string{"a": "a", "b": "b", "late": "late"}) {
		t.Fatalf("after rollback: %v", got)
	}
}

func TestRenameAllMissingSourceRollsBack(t *testing.T) {
	dir := t.TempDir()
	p := func(name string) string { return filepath.Join(dir, name) }
	writeFile(t, p("a"), "a")

	if err := RenameAll([]RenamePair{{p("a"), p("b")}, {p("gone"), p("c")}}); err == nil {
		t.Fatal("renaming a missing file should fail")
	}
	if got := contents(t, dir); !reflect.DeepEqual(got, map[string]string{"a": "a"}) {
		t.Fatalf("after rollback: %v", got)
	}
}

func TestPlanRenamesIntoANameLeftInAnotherCase(t *testing.T) {
	dir := t.TempDir()
	if !ignoresCase(t, dir) {
		t.Skip("the file system tells names apart by case")
	}
	p := func(name string) string { return filepath.Join(dir, name) }
	writeFile(t, p("2.TXT"), "two")
	writeFile(t, p("a.txt"), "a")
	occupied := func(path, src string) bool {
		info, err := os.Lstat(path)
		if err != nil {
			return false
		}
		srcInfo, err := os.Lstat(src)
		return err != nil || !os.SameFile(info, srcInfo)
	}

	// a.txt takes 2.txt, which 2.TXT, renamed away, holds in another case
	pairs, err := PlanRenames([]string{p("2.TXT"), p("a.txt")}, "1.txt\n2.txt\n", occupied)
	if err != nil {
		t.Fatalf("PlanRenames = %v", err)
	}
	if err := RenameAll(pairs); err != nil {
		t.Fatal(err)
	}
	if got := contents(t, dir); !reflect.DeepEqual(got, map[string]string{"1.txt": "two", "2.txt": "a"}) {
		t.Fatalf("after renaming: %v", got)
	}
	// One that stays is still in the way, in whatever case
	if _, err := PlanRenames([]string{p("1.txt")}, "2.TXT\n", occupied); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("into a name taken in another case: %v", err)
	}
}
