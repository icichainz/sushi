package search

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// tree creates files under a temp dir; names ending in "/" are directories
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(path, 0755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// contains matches names that contain query
func contains(query string) Matcher {
	return func(rel, name string) (int, bool) {
		return 0, strings.Contains(name, query)
	}
}

// names runs a name search and returns the relative paths found, with slashes
func names(t *testing.T, opts Options, match Matcher) ([]string, bool) {
	t.Helper()
	var got []string
	truncated, err := Names(context.Background(), opts, match, func(r Result) {
		got = append(got, filepath.ToSlash(r.Rel))
	})
	if err != nil {
		t.Fatal(err)
	}
	return got, truncated
}

// lines runs a content search and returns "rel:line: text" for each match
func lines(t *testing.T, opts Options, query string) []string {
	t.Helper()
	var got []string
	_, err := Contents(context.Background(), opts, query, func(r Result) {
		got = append(got, fmt.Sprintf("%s:%d: %s", filepath.ToSlash(r.Rel), r.Line, r.Text))
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestNamesSearchesSubfolders(t *testing.T) {
	root := tree(t, map[string]string{
		"main.go":            "",
		"src/app/main.go":    "",
		"src/app/model.go":   "",
		"docs/mainframe.txt": "",
		"src/main/":          "",
	})
	got, truncated := names(t, Options{Root: root}, contains("main"))
	want := []string{"docs/mainframe.txt", "main.go", "src/app/main.go", "src/main"}
	slices.Sort(got)
	if !slices.Equal(got, want) || truncated {
		t.Fatalf("got %v (truncated=%v), want %v", got, truncated, want)
	}
}

func TestSearchSkipsHiddenAndHeavyDirectories(t *testing.T) {
	root := tree(t, map[string]string{
		"keep.txt":                  "needle",
		".env.txt":                  "needle",
		".cache/keep.txt":           "needle",
		".git/keep.txt":             "needle",
		"node_modules/pkg/keep.txt": "needle",
		"vendor/lib/keep.txt":       "needle",
		"src/vendor.txt":            "needle", // Only directories are skipped by name
	})
	opts := Options{Root: root, Skip: DefaultSkip}

	got, _ := names(t, opts, contains("keep"))
	if !slices.Equal(got, []string{"keep.txt"}) {
		t.Fatalf("names with hidden off = %v, want only keep.txt", got)
	}
	if got := lines(t, opts, "needle"); len(got) != 2 {
		t.Fatalf("contents with hidden off = %v, want keep.txt and src/vendor.txt", got)
	}

	// Hidden files are searched when shown, but .git still isn't
	opts.ShowHidden = true
	got, _ = names(t, opts, contains("keep"))
	slices.Sort(got)
	if !slices.Equal(got, []string{".cache/keep.txt", "keep.txt"}) {
		t.Fatalf("names with hidden on = %v", got)
	}
}

func TestSearchStartsInsideASkippedRoot(t *testing.T) {
	root := tree(t, map[string]string{"node_modules/pkg/index.js": "exports"})
	opts := Options{Root: filepath.Join(root, "node_modules"), Skip: DefaultSkip}
	if got, _ := names(t, opts, contains("index")); !slices.Equal(got, []string{"pkg/index.js"}) {
		t.Fatalf("got %v, want the root searched even though its name is skipped", got)
	}
}

func TestSearchDoesNotFollowSymlinkedDirectories(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	root := tree(t, map[string]string{"a/b/target.txt": "needle"})
	// Links back up the tree would loop forever if followed
	if err := os.Symlink(root, filepath.Join(root, "a", "b", "loop")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("..", filepath.Join(root, "a", "up")); err != nil {
		t.Fatal(err)
	}

	// Not names(), which may call t.Fatal: that only works on the test's goroutine
	done := make(chan []string, 1)
	go func() {
		var got []string
		Names(context.Background(), Options{Root: root}, contains(""), func(r Result) {
			got = append(got, filepath.ToSlash(r.Rel))
		})
		done <- got
	}()
	select {
	case got := <-done:
		slices.Sort(got)
		want := []string{"a", "a/b", "a/b/loop", "a/b/target.txt", "a/up"}
		if !slices.Equal(got, want) {
			t.Fatalf("got %v, want the links listed but not entered: %v", got, want)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("search looped through symlinks")
	}
	if got := lines(t, Options{Root: root}, "needle"); len(got) != 1 {
		t.Fatalf("content matches = %v, want target.txt once", got)
	}
}

func TestSearchStopsAtTheLimit(t *testing.T) {
	files := map[string]string{}
	for i := range 30 {
		files[fmt.Sprintf("file%02d.txt", i)] = "needle\nneedle\n"
	}
	root := tree(t, files)

	got, truncated := names(t, Options{Root: root, Limit: 10}, contains("file"))
	if len(got) != 10 || !truncated {
		t.Fatalf("got %d results (truncated=%v), want 10 and truncated", len(got), truncated)
	}
	// Exactly the limit is not truncated
	if got, truncated := names(t, Options{Root: root, Limit: 30}, contains("file")); len(got) != 30 || truncated {
		t.Fatalf("got %d results (truncated=%v), want all 30 and not truncated", len(got), truncated)
	}

	var n int
	truncated, err := Contents(context.Background(), Options{Root: root, Limit: 45}, "needle", func(Result) { n++ })
	if err != nil || n != 45 || !truncated {
		t.Fatalf("contents: %d results, truncated=%v, err=%v; want 45 and truncated", n, truncated, err)
	}
}

func TestSearchStopsWhenCancelled(t *testing.T) {
	files := map[string]string{}
	for i := range 200 {
		files[fmt.Sprintf("d%d/f%d.txt", i%10, i)] = "needle"
	}
	root := tree(t, files)

	for _, content := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		n := 0
		emit := func(Result) {
			n++
			cancel()
		}
		var err error
		if content {
			_, err = Contents(ctx, Options{Root: root}, "needle", emit)
		} else {
			_, err = Names(ctx, Options{Root: root}, contains("f"), emit)
		}
		if !errors.Is(err, context.Canceled) || n != 1 {
			t.Fatalf("content=%v: err=%v after %d results, want it to stop at the first", content, err, n)
		}
	}
}

func TestSearchReportsAMissingRoot(t *testing.T) {
	_, err := Names(context.Background(), Options{Root: filepath.Join(t.TempDir(), "gone")}, contains(""), func(Result) {})
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want not exist", err)
	}
}

func TestContentsFindsLines(t *testing.T) {
	root := tree(t, map[string]string{
		"notes.txt":  "first line\n\tTODO: write tests\nnothing here\ntodo later\n",
		"src/a.go":   "package a\n\n// TODO fix\n",
		"binary.bin": "TODO\x00\x01\x02",
		"late.bin":   strings.Repeat("TODO text\n", 100) + "\x00binary after the first 512 bytes\nTODO\n",
		"empty.txt":  "",
	})

	got := lines(t, Options{Root: root}, "todo")
	slices.Sort(got)
	want := []string{
		"notes.txt:2: TODO: write tests", // Indentation dropped
		"notes.txt:4: todo later",
		"src/a.go:3: // TODO fix",
	}
	// late.bin looks like text at first, so its early lines match until the NUL
	var late int
	for _, g := range got {
		if strings.HasPrefix(g, "late.bin:") {
			late++
		}
	}
	got = slices.DeleteFunc(got, func(s string) bool { return strings.HasPrefix(s, "late.bin:") })
	if !slices.Equal(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if late != 100 {
		t.Fatalf("late.bin gave %d matches, want the 100 before the NUL", late)
	}

	// Capitals make the search case sensitive
	if got := lines(t, Options{Root: root}, "TODO fix"); len(got) != 1 {
		t.Fatalf("smart case: got %q", got)
	}
	if got := lines(t, Options{Root: root}, "Todo"); len(got) != 0 {
		t.Fatalf("Todo should match nothing case-sensitively, got %q", got)
	}
}

func TestContentsPositionsAndCleansText(t *testing.T) {
	long := strings.Repeat("é", 2000) + "NEEDLE" + strings.Repeat("x", 2000)
	root := tree(t, map[string]string{
		"a.txt": "    ÉCOLE needle\x1b[31m red\tand tab\n" + long + "\n",
		"b.txt": "bad \xff\xfe needle\n",
	})

	var results []Result
	_, err := Contents(context.Background(), Options{Root: root}, "needle", func(r Result) { results = append(results, r) })
	if err != nil || len(results) != 3 {
		t.Fatalf("got %d results (%v), want 3", len(results), err)
	}
	for _, r := range results {
		runes := []rune(r.Text)
		if !utf8.ValidString(r.Text) || strings.ContainsAny(r.Text, "\x1b\t") {
			t.Errorf("%s:%d: text %q is not safe to display", r.Rel, r.Line, r.Text)
		}
		if r.Col < 0 || r.Col+6 > len(runes) || !strings.EqualFold(string(runes[r.Col:r.Col+6]), "needle") {
			t.Errorf("%s:%d: col %d does not point at the match in %q", r.Rel, r.Line, r.Col, r.Text)
		}
		if len(runes) > maxText {
			t.Errorf("%s:%d: text is %d runes long", r.Rel, r.Line, len(runes))
		}
	}
}

func TestContentsReadsLongLines(t *testing.T) {
	root := tree(t, map[string]string{
		"min.js": strings.Repeat("a", 200*1024) + "needle\nneedle on line two\n",
		"huge":   strings.Repeat("b", maxLine+10) + "\nneedle after an oversized line\n",
		"z.txt":  "needle\n",
	})
	got := lines(t, Options{Root: root}, "needle")
	if len(got) != 3 {
		t.Fatalf("got %d matches, want min.js twice and z.txt once (huge is abandoned): %q", len(got), got)
	}
}
