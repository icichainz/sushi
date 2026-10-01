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

	"github.com/icichainz/sushi/internal/tags"
	"github.com/icichainz/sushi/internal/testutil"
)

// fakeSpotlight puts a stand-in for mdfind on PATH. It records its
// arguments, waits if asked, prints paths as mdfind -0 does, and exits
// with the given status, saying something on stderr first.
type fakeSpotlight struct {
	args  string   // File it records its arguments in, one per line
	out   []string // Paths it prints
	sleep string   // How long it waits before printing, for sleep(1)
	exit  int
}

// fakeBin holds the stand-in for mdfind, made once: macOS checks a new
// program the first time it runs, which takes most of a second
var fakeBin string

func TestMain(m *testing.M) {
	code := m.Run()
	if fakeBin != "" {
		os.RemoveAll(fakeBin)
	}
	os.Exit(code)
}

// fakeScript takes what to do from the environment. Its sleep doesn't
// hold the pipes, so killing the script closes them.
const fakeScript = `#!/bin/sh
[ "$1" = warm ] && exit 0
printf '%s\n' "$@" > "$FAKE_MDFIND_DIR/args"
[ -n "$FAKE_MDFIND_SLEEP" ] && sleep "$FAKE_MDFIND_SLEEP" >/dev/null 2>&1
cat "$FAKE_MDFIND_DIR/out"
echo 'mdfind: a complaint' >&2
exit "$FAKE_MDFIND_EXIT"
`

func (f *fakeSpotlight) install(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script")
	}
	if fakeBin == "" {
		bin, err := os.MkdirTemp("", "fake-mdfind")
		if err != nil {
			t.Fatal(err)
		}
		testutil.Script(t, filepath.Join(bin, "mdfind"), fakeScript)
		fakeBin = bin
	}
	dir := t.TempDir()
	f.args = filepath.Join(dir, "args")
	var b strings.Builder
	for _, p := range f.out {
		b.WriteString(p + "\x00")
	}
	if err := os.WriteFile(filepath.Join(dir, "out"), []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_MDFIND_DIR", dir)
	t.Setenv("FAKE_MDFIND_SLEEP", f.sleep)
	t.Setenv("FAKE_MDFIND_EXIT", fmt.Sprint(f.exit))
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+"/usr/bin:/bin")
}

// calledWith returns the arguments mdfind was run with
func (f *fakeSpotlight) calledWith(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(f.args)
	if err != nil {
		t.Fatalf("mdfind wasn't run: %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// realRoot returns root with symlinks resolved, as Spotlight reports
// paths: temp dirs on macOS are below /var, a link to /private/var
func realRoot(t *testing.T, root string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

// collect runs a search and returns its results and report
func collect(t *testing.T, e Engine, opts Options, q Query) ([]Result, Report, error) {
	t.Helper()
	var got []Result
	report, err := e.Search(context.Background(), opts, q, func(r Result) { got = append(got, r) })
	return got, report, err
}

// rels lists results as "rel" or "rel:line: text"
func rels(results []Result) []string {
	var out []string
	for _, r := range results {
		s := filepath.ToSlash(r.Rel)
		if r.Line > 0 {
			s += fmt.Sprintf(":%d: %s", r.Line, r.Text)
		}
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}

func TestTagQuery(t *testing.T) {
	for in, want := range map[string]struct {
		tag string
		ok  bool
	}{
		"#Red": {"Red", true}, "#": {"", true}, "tag:Work": {"Work", true}, "TAG: two words ": {"two words", true},
		"# red": {"red", true}, "red": {"", false}, "a#b": {"", false}, "tag": {"", false}, "": {"", false},
	} {
		if tag, ok := TagQuery(in); tag != want.tag || ok != want.ok {
			t.Errorf("TagQuery(%q) = %q, %v; want %q, %v", in, tag, ok, want.tag, want.ok)
		}
	}
}

func TestSpotlightQueries(t *testing.T) {
	for _, c := range []struct {
		q    Query
		want string
		ok   bool
	}{
		{Query{Text: "report"}, `kMDItemFSName == "*report*"c`, true},
		{Query{Text: "src/main"}, `kMDItemFSName == "*main*"c`, true},
		{Query{Text: `a"b\c*d?e`}, `kMDItemFSName == "*a*b*c*d*e*"c`, true},
		{Query{Text: "**"}, "", false},
		{Query{Text: "src/"}, "", false},
		{Query{Text: "needle", Content: true}, `kMDItemTextContent == "*needle*"c`, true},
		{Query{Text: "two words", Content: true}, `kMDItemTextContent == "*two*"c && kMDItemTextContent == "*words*"c`, true},
		{Query{Text: "* ?", Content: true}, "", false},
		{Query{Tagged: true, Tag: "Red"}, `kMDItemUserTags == "Red*"c`, true},
		{Query{Tagged: true}, `kMDItemUserTags == "*"`, true},
	} {
		got, ok := spotlightQuery(c.q)
		if ok != c.ok || ok && got != c.want {
			t.Errorf("%+v: %q, %v; want %q, %v", c.q, got, ok, c.want, c.ok)
		}
	}
}

func TestSpotlightNamesFollowTheWalksRules(t *testing.T) {
	root := tree(t, map[string]string{
		"src/main.go":          "",
		"docs/main.md":         "",
		".hidden/main.go":      "",
		"node_modules/main.js": "",
		"vendor/":              "",
		"tools/vendor":         "", // A file of that name is listed
		"other.txt":            "",
	})
	real := realRoot(t, root)
	f := &fakeSpotlight{out: []string{
		filepath.Join(real, "src", "main.go"),
		filepath.Join(real, "docs", "main.md"),
		filepath.Join(real, ".hidden", "main.go"),
		filepath.Join(real, "node_modules", "main.js"),
		filepath.Join(real, "vendor"),
		filepath.Join(real, "tools", "vendor"),
		filepath.Join(real, "gone.txt"),  // Deleted since it was indexed
		"/somewhere/else/main.go",        // Outside the folder
		"relative/main.go",               // Not a path mdfind prints
		filepath.Join(real, "other.txt"), // The matcher says no
		real,                             // The folder itself
	}}
	f.install(t)

	match := func(rel, name string) (int, bool) {
		return strings.Count(rel, "/"), name != "other.txt"
	}
	got, report, err := collect(t, Spotlight{}, Options{Root: root, Skip: DefaultSkip}, Query{Text: "main", Match: match})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"docs/main.md", "src/main.go", "tools/vendor"}; !slices.Equal(rels(got), want) {
		t.Fatalf("results = %q, want %q", rels(got), want)
	}
	for _, r := range got {
		// Below the folder as it was given, not as Spotlight resolved it
		if !strings.HasPrefix(r.Path, root+string(filepath.Separator)) || r.Score != 1 {
			t.Errorf("result %+v", r)
		}
	}
	if !report.Spotlight || report.Truncated {
		t.Errorf("report = %+v", report)
	}
	if args := f.calledWith(t); !slices.Equal(args, []string{"-0", "-onlyin", root, `kMDItemFSName == "*main*"c`}) {
		t.Errorf("mdfind ran with %q", args)
	}

	// With hidden files shown, the hidden ones count
	got, _, _ = collect(t, Spotlight{}, Options{Root: root, Skip: DefaultSkip, ShowHidden: true}, Query{Text: "main", Match: match})
	if !slices.Contains(rels(got), ".hidden/main.go") {
		t.Errorf("hidden files shown, results = %q", rels(got))
	}
}

func TestSpotlightFindsLinesAndDocuments(t *testing.T) {
	root := tree(t, map[string]string{
		"notes.txt":   "first\n\tthe needle is here\nand a NEEDLE\n",
		"word.txt":    "needles everywhere\n",
		"nothing.txt": "Spotlight matched a word, but no line has it as typed",
		"paper.pdf":   "%PDF-1.4\x00\x01binary",
	})
	real := realRoot(t, root)
	f := &fakeSpotlight{out: []string{
		filepath.Join(real, "notes.txt"), filepath.Join(real, "word.txt"),
		filepath.Join(real, "nothing.txt"), filepath.Join(real, "paper.pdf"),
	}}
	f.install(t)

	got, _, err := collect(t, Spotlight{}, Options{Root: root}, Query{Text: "needle", Content: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"notes.txt:2: the needle is here", "notes.txt:3: and a NEEDLE", "paper.pdf", "word.txt:1: needles everywhere"}
	if !slices.Equal(rels(got), want) {
		t.Fatalf("results = %q, want %q", rels(got), want)
	}
	// Capitals match exactly, as in a walk
	got, _, _ = collect(t, Spotlight{}, Options{Root: root}, Query{Text: "NEEDLE", Content: true})
	if want := []string{"notes.txt:3: and a NEEDLE", "paper.pdf"}; !slices.Equal(rels(got), want) {
		t.Fatalf("results = %q, want %q", rels(got), want)
	}
}

func TestSpotlightStopsAtTheLimit(t *testing.T) {
	files := map[string]string{}
	var out []string
	root := t.TempDir()
	for i := range 50 {
		name := fmt.Sprintf("a%02d.txt", i)
		files[name] = ""
		out = append(out, filepath.Join(realRoot(t, root), name))
	}
	for name := range files {
		os.WriteFile(filepath.Join(root, name), nil, 0644)
	}
	f := &fakeSpotlight{out: out}
	f.install(t)

	got, report, err := collect(t, Spotlight{}, Options{Root: root, Limit: 10}, Query{Text: "a"})
	if err != nil || len(got) != 10 || !report.Truncated {
		t.Fatalf("%d results, report %+v, err %v; want 10 and truncated", len(got), report, err)
	}
}

func TestSpotlightFailureIsReported(t *testing.T) {
	root := t.TempDir()
	f := &fakeSpotlight{exit: 3}
	f.install(t)
	_, _, err := collect(t, Spotlight{}, Options{Root: root}, Query{Text: "a"})
	if !errors.Is(err, ErrSpotlight) || !strings.Contains(err.Error(), "a complaint") {
		t.Fatalf("err = %v, want ErrSpotlight with what mdfind said", err)
	}

	t.Setenv("PATH", t.TempDir()) // No mdfind at all
	if _, _, err := collect(t, Spotlight{}, Options{Root: root}, Query{Text: "a"}); !errors.Is(err, ErrSpotlight) {
		t.Fatalf("without mdfind, err = %v", err)
	}
	if _, _, err := collect(t, Spotlight{}, Options{Root: root}, Query{Text: "*"}); !errors.Is(err, ErrSpotlight) {
		t.Fatalf("for a query of wildcards, err = %v", err)
	}
}

func TestSpotlightCancels(t *testing.T) {
	f := &fakeSpotlight{sleep: "30"}
	f.install(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := Spotlight{}.Search(ctx, Options{Root: t.TempDir()}, Query{Text: "a"}, func(Result) {})
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want cancelled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling didn't stop mdfind")
	}
}

func TestSpotlightEverywhere(t *testing.T) {
	root := tree(t, map[string]string{"here/report.txt": ""})
	elsewhere := tree(t, map[string]string{"report.md": "", ".secret/report.key": ""})
	f := &fakeSpotlight{out: []string{
		filepath.Join(realRoot(t, root), "here", "report.txt"),
		filepath.Join(elsewhere, "report.md"),
		filepath.Join(elsewhere, ".secret", "report.key"),
	}}
	f.install(t)

	got, _, err := collect(t, Spotlight{}, Options{Root: root, Everywhere: true}, Query{Text: "report"})
	if err != nil {
		t.Fatal(err)
	}
	// Outside the folder, results are named by their full path
	want := []string{filepath.ToSlash(filepath.Join(elsewhere, "report.md")), "here/report.txt"}
	if slices.Sort(want); !slices.Equal(rels(got), want) {
		t.Fatalf("results = %q, want %q", rels(got), want)
	}
	if args := f.calledWith(t); !slices.Equal(args, []string{"-0", `kMDItemFSName == "*report*"c`}) {
		t.Fatalf("mdfind ran with %q", args)
	}
}

func TestWalkerCantSearchEverywhere(t *testing.T) {
	if _, _, err := collect(t, Walker{}, Options{Root: t.TempDir(), Everywhere: true}, Query{Text: "a"}); !errors.Is(err, ErrEverywhere) {
		t.Fatalf("err = %v", err)
	}
}

func TestAutoUsesSpotlightsResults(t *testing.T) {
	root := tree(t, map[string]string{"indexed.txt": "", "new.txt": ""})
	f := &fakeSpotlight{out: []string{filepath.Join(realRoot(t, root), "indexed.txt")}}
	f.install(t)

	got, report, err := collect(t, Auto{}, Options{Root: root}, Query{Text: "txt"})
	if err != nil || !report.Spotlight {
		t.Fatalf("report %+v, err %v", report, err)
	}
	// Only what the index has: no walk
	if want := []string{"indexed.txt"}; !slices.Equal(rels(got), want) {
		t.Fatalf("results = %q, want %q", rels(got), want)
	}
}

func TestAutoWalksWhenSpotlightCantHelp(t *testing.T) {
	root := tree(t, map[string]string{"a.txt": "", "b.txt": ""})
	want := []string{"a.txt", "b.txt"}
	cases := map[string]func(t *testing.T){
		"finds nothing": func(t *testing.T) { (&fakeSpotlight{}).install(t) },
		"fails":         func(t *testing.T) { (&fakeSpotlight{exit: 1}).install(t) },
		"is missing":    func(t *testing.T) { t.Setenv("PATH", t.TempDir()) },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			setup(t)
			got, report, err := collect(t, Auto{}, Options{Root: root}, Query{Text: "txt", Match: contains("txt")})
			if err != nil || report.Spotlight || !slices.Equal(rels(got), want) {
				t.Fatalf("results %q, report %+v, err %v; want a walk's %q", rels(got), report, err, want)
			}
		})
	}
}

func TestAutoWalksWhenHiddenFilesAreShown(t *testing.T) {
	root := tree(t, map[string]string{"a.txt": "", ".config/a.yaml": ""})
	f := &fakeSpotlight{out: []string{filepath.Join(realRoot(t, root), "a.txt")}}
	f.install(t)

	got, report, err := collect(t, Auto{}, Options{Root: root, ShowHidden: true}, Query{Text: "a", Match: contains("a")})
	if err != nil || report.Spotlight || !slices.Equal(rels(got), []string{".config/a.yaml", "a.txt"}) {
		t.Fatalf("results %q, report %+v, err %v; want a walk's, hidden files and all", rels(got), report, err)
	}
}

func TestAutoGivesUpOnASlowSpotlight(t *testing.T) {
	root := tree(t, map[string]string{"a.txt": ""})
	// It would answer, but too late, and its answer must not be added
	f := &fakeSpotlight{sleep: "2", out: []string{filepath.Join(realRoot(t, root), "a.txt")}}
	f.install(t)

	start := time.Now()
	got, report, err := collect(t, Auto{Patience: 100 * time.Millisecond}, Options{Root: root}, Query{Text: "a", Match: contains("a")})
	if err != nil || report.Spotlight || !slices.Equal(rels(got), []string{"a.txt"}) {
		t.Fatalf("results %q, report %+v, err %v; want the walk's one", rels(got), report, err)
	}
	if took := time.Since(start); took > 1500*time.Millisecond {
		t.Fatalf("took %v: it waited for Spotlight", took)
	}
}

func TestAutoEverywhereIsSpotlightsAlone(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, _, err := collect(t, Auto{}, Options{Root: t.TempDir(), Everywhere: true}, Query{Text: "a"})
	if !errors.Is(err, ErrSpotlight) {
		t.Fatalf("err = %v, want Spotlight's failure rather than a walk", err)
	}
}

func TestSearchByTag(t *testing.T) {
	if !tags.Supported() {
		t.Skip("no Finder tags here")
	}
	root := tree(t, map[string]string{
		"report.pdf":      "",
		"redo.txt":        "",
		"plain.txt":       "",
		"sub/budget.xlsx": "",
		".hidden/x.txt":   "",
		"sub/":            "",
	})
	tag := func(rel string, list ...tags.Tag) {
		if err := tags.Write(filepath.Join(root, rel), list); err != nil {
			t.Skipf("can't tag files here: %v", err)
		}
	}
	tag("report.pdf", tags.Tag{Name: "Red", Color: tags.Red})
	tag("redo.txt", tags.Tag{Name: "Redo"})
	tag("sub/budget.xlsx", tags.Tag{Name: "Work"}, tags.Tag{Name: "red", Color: tags.Red})
	tag("sub", tags.Tag{Name: "Blue", Color: tags.Blue})
	tag(".hidden/x.txt", tags.Tag{Name: "Red", Color: tags.Red})

	got, _, err := collect(t, Walker{}, Options{Root: root}, Query{Tagged: true, Tag: "red"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"redo.txt", "report.pdf", "sub/budget.xlsx"}; !slices.Equal(rels(got), want) {
		t.Fatalf("#red found %q, want %q", rels(got), want)
	}
	for _, r := range got {
		exact := r.Rel != "redo.txt"
		if exact != (r.Score == 0) || len(r.Tags) == 0 {
			t.Errorf("%s: score %d, tags %q", r.Rel, r.Score, r.Tags)
		}
	}

	got, _, _ = collect(t, Walker{}, Options{Root: root}, Query{Tagged: true})
	if want := []string{"redo.txt", "report.pdf", "sub", "sub/budget.xlsx"}; !slices.Equal(rels(got), want) {
		t.Fatalf("# found %q, want everything tagged: %q", rels(got), want)
	}

	// Spotlight's candidates are checked against the files' own tags
	real := realRoot(t, root)
	f := &fakeSpotlight{out: []string{filepath.Join(real, "report.pdf"), filepath.Join(real, "plain.txt")}}
	f.install(t)
	got, _, _ = collect(t, Spotlight{}, Options{Root: root}, Query{Tagged: true, Tag: "Red"})
	if want := []string{"report.pdf"}; !slices.Equal(rels(got), want) {
		t.Fatalf("Spotlight's #Red = %q, want %q", rels(got), want)
	}
	if args := f.calledWith(t); args[len(args)-1] != `kMDItemUserTags == "Red*"c` {
		t.Fatalf("mdfind ran with %q", args)
	}
}
