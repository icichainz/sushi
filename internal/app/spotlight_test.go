package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/search"
)

func init() {
	// The palette's searches walk in tests, whatever Spotlight has indexed
	findEngine = search.Walker{}
}

// fakeEngine answers every search with the same results, saying Spotlight
// found them, and remembers where it was asked to look
type fakeEngine struct {
	asked   *[]search.Options
	results []search.Result
}

func (e fakeEngine) Search(ctx context.Context, opts search.Options, q search.Query, emit func(search.Result)) (search.Report, error) {
	*e.asked = append(*e.asked, opts)
	for _, r := range e.results {
		emit(r)
	}
	return search.Report{Spotlight: true}, nil
}

// useEngine makes the palette search with e for the rest of the test
func useEngine(t *testing.T, e search.Engine) {
	t.Helper()
	findEngine = e
	t.Cleanup(func() { findEngine = search.Walker{} })
}

func TestFindScopeTogglesWithCtrlE(t *testing.T) {
	root := makeTree(t, map[string]string{"match.txt": ""})

	// A walk can't search everywhere, and says so
	m := find(t, newTestModel(t, root, nil), "f", "match")
	if m.find.everywhere || !strings.Contains(strings.Join(plain(m.View()), "\n"), "Find files below this folder") {
		t.Fatalf("not below this folder to start with:\n%s", strings.Join(plain(m.View()), "\n"))
	}
	m, cmd := ctrl(t, m, tea.KeyCtrlE)
	m = drain(t, m, cmd)
	view := strings.Join(plain(m.View()), "\n")
	if !m.find.everywhere || !strings.Contains(view, "Find files everywhere") || !strings.Contains(view, "searching everywhere needs Spotlight") {
		t.Fatalf("after ctrl+e:\n%s", view)
	}

	// Spotlight can. What it finds outside the folder shows in full, with
	// the home folder as ~.
	var asked []search.Options
	elsewhere := filepath.Join(os.Getenv("HOME"), "elsewhere", "match.txt")
	useEngine(t, fakeEngine{&asked, []search.Result{{Path: elsewhere, Rel: elsewhere}}})
	m, cmd = ctrl(t, m, tea.KeyCtrlE) // Back below this folder
	m = drain(t, m, cmd)
	m, cmd = ctrl(t, m, tea.KeyCtrlE)
	m = drain(t, m, cmd)
	if len(asked) != 2 || asked[0].Everywhere || !asked[1].Everywhere {
		t.Fatalf("searches asked for %+v", asked)
	}
	view = strings.Join(plain(m.View()), "\n")
	for _, want := range []string{"Find files everywhere", "~/elsewhere/match.txt", "everywhere · Spotlight", "ctrl+e search this folder"} {
		if !strings.Contains(view, want) {
			t.Errorf("palette is missing %q:\n%s", want, view)
		}
	}

	// Enter goes there
	m, cmd = press(t, m, "enter")
	os.MkdirAll(filepath.Dir(elsewhere), 0755)
	writeTestFile(t, elsewhere, "")
	m = drain(t, m, cmd)
	if m.tab().CurrentPath != filepath.Dir(elsewhere) || cursorName(m) != "match.txt" {
		t.Fatalf("after enter: in %s on %s", m.tab().CurrentPath, cursorName(m))
	}
}

func TestFindShowsDocumentsSpotlightFound(t *testing.T) {
	root := makeTree(t, map[string]string{"paper.pdf": "%PDF\x00", "notes.txt": "a\nb\nthe needle\n"})
	var asked []search.Options
	useEngine(t, fakeEngine{&asked, []search.Result{
		{Path: filepath.Join(root, "paper.pdf"), Rel: "paper.pdf"},
		{Path: filepath.Join(root, "notes.txt"), Rel: "notes.txt", Line: 3, Text: "the needle", Col: 4},
	}})

	m := find(t, newTestModel(t, root, nil), "F", "needle")
	view := strings.Join(plain(m.View()), "\n")
	for _, want := range []string{"Find in files below this folder", "paper.pdf", "notes.txt:3: the needle", "2 matches", "Spotlight"} {
		if !strings.Contains(view, want) {
			t.Errorf("palette is missing %q:\n%s", want, view)
		}
	}
	assertFills(t, "documents", m)

	// A document goes to the file, with no line to show
	m, cmd := press(t, m, "enter")
	m = drain(t, m, cmd)
	if cursorName(m) != "paper.pdf" || m.jump != (previewJump{}) {
		t.Fatalf("after enter: on %s, jump %+v", cursorName(m), m.jump)
	}
}
