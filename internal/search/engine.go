package search

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	iofs "io/fs"
	"strings"
	"sync"
	"time"

	"github.com/icichainz/sushi/internal/tags"
)

// Query is what a search looks for
type Query struct {
	Text    string  // As typed
	Content bool    // Look for Text inside files, rather than in names
	Match   Matcher // Ranks names, for a name search
	Tagged  bool    // Look for Finder tags instead: those starting with Tag, or any if it is ""
	Tag     string
}

// Report says how a search went
type Report struct {
	Truncated bool // It stopped at Options.Limit results
	Spotlight bool // Spotlight found the results, rather than a walk
}

// Engine runs searches. Each reports results as it finds them, and stops
// when ctx is cancelled.
type Engine interface {
	Search(ctx context.Context, opts Options, q Query, emit func(Result)) (Report, error)
}

// ErrEverywhere is returned when an engine that can only search below a
// folder is asked to search everywhere
var ErrEverywhere = errors.New("searching everywhere needs Spotlight")

// Walker searches by walking the folder, reading names, tags and files
type Walker struct{}

// Search searches below opts.Root
func (Walker) Search(ctx context.Context, opts Options, q Query, emit func(Result)) (Report, error) {
	if opts.Everywhere {
		return Report{}, ErrEverywhere
	}
	var truncated bool
	var err error
	switch {
	case q.Tagged:
		truncated, err = Tagged(ctx, opts, q.Tag, emit)
	case q.Content:
		truncated, err = Contents(ctx, opts, q.Text, emit)
	default:
		truncated, err = Names(ctx, opts, q.Match, emit)
	}
	return Report{Truncated: truncated}, err
}

const (
	// defaultPatience is how long a text search below a folder waits for
	// Spotlight's documents once its walk is done. Spotlight answers in
	// about a second even for a large home folder; one that hasn't by then
	// is stuck, or busy indexing.
	defaultPatience = 4 * time.Second
	// defaultTimeout is how long a search everywhere may take. Spotlight
	// finishes in seconds; mdfind going on for longer is stuck.
	defaultTimeout = 30 * time.Second
)

// ErrGaveUp is returned when a search everywhere took Spotlight too long
var ErrGaveUp error = spotlightError{"Spotlight took too long, so the search gave up"}

// Auto walks the folder to search below it, and asks Spotlight to search
// everywhere, which only Spotlight can. The walk finds every file, so it
// has the last word below a folder: Spotlight finds nothing in folders it
// doesn't index, such as temporary and hidden ones, excluded volumes and
// files too new to be indexed yet, finds names only by what they contain,
// not fuzzily, and doesn't read the text of source code, YAML or
// Makefiles. A text search below a folder asks Spotlight as well, at the
// same time, for the documents with the text that a walk can't read, such
// as PDFs.
type Auto struct {
	Spotlight Spotlight
	Walker    Walker
	Patience  time.Duration // How long Spotlight may go on once the walk is done; 0 is defaultPatience
	Timeout   time.Duration // How long a search everywhere may take; 0 is defaultTimeout
}

// Search searches below opts.Root by walking it, or everywhere with
// Spotlight
func (a Auto) Search(ctx context.Context, opts Options, q Query, emit func(Result)) (Report, error) {
	switch {
	case opts.Everywhere:
		return a.everywhere(ctx, opts, q, emit)
	case q.Content && !q.Tagged:
		return a.contents(ctx, opts, q, emit)
	}
	return a.Walker.Search(ctx, opts, q, emit)
}

// everywhere asks Spotlight, giving up if it takes too long
func (a Auto) everywhere(ctx context.Context, opts Options, q Query, emit func(Result)) (Report, error) {
	timeout := cmp.Or(a.Timeout, defaultTimeout)
	sctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	report, err := a.Spotlight.Search(sctx, opts, q, emit)
	if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
		return report, fmt.Errorf("%w after %v", ErrGaveUp, timeout)
	}
	return report, err
}

// contents walks the folder for the lines with the text, while Spotlight
// looks for the documents with it. Spotlight only adds documents, which
// the walk skips as binary, so the two never report the same file; what
// Spotlight finds after the walk and some patience, or if it fails, is
// left out.
func (a Auto) contents(ctx context.Context, opts Options, q Query, emit func(Result)) (Report, error) {
	parent := ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Both report through keep, one at a time, up to the limit between them
	var mu sync.Mutex
	found, docs, full := 0, 0, false
	keep := func(r Result, doc bool) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case ctx.Err() != nil:
			return
		case opts.Limit > 0 && found == opts.Limit:
			full = true
			cancel()
			return
		}
		found++
		if doc {
			docs++
		}
		emit(r)
	}

	sctx, stop := context.WithCancel(ctx)
	defer stop()
	spotlightDone := make(chan struct{})
	go func() {
		defer close(spotlightDone)
		a.Spotlight.search(sctx, opts, q, true, func(r Result) { keep(r, true) })
	}()
	report, err := a.Walker.Search(ctx, opts, q, func(r Result) { keep(r, false) })
	if err != nil {
		stop() // The folder can't be read: nor can its documents
	}
	select {
	case <-spotlightDone:
	case <-time.After(cmp.Or(a.Patience, defaultPatience)):
		stop()
		<-spotlightDone
	}

	mu.Lock()
	defer mu.Unlock()
	report.Truncated = report.Truncated || full
	report.Spotlight = docs > 0
	switch {
	case parent.Err() != nil:
		return report, parent.Err()
	case full:
		return report, nil // The walk was stopped at the limit
	}
	return report, err
}

// TagQuery reads a query for Finder tags: "#" or "tag:", then the start of
// a tag's name, or nothing to find everything tagged
func TagQuery(s string) (string, bool) {
	for _, prefix := range []string{"#", "tag:"} {
		if len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix) {
			return strings.TrimSpace(s[len(prefix):]), true
		}
	}
	return "", false
}

// Tagged reports every entry below opts.Root with a Finder tag whose name
// starts with prefix, ignoring case, or with any tag if prefix is empty.
// Tags named prefix exactly rank first. It returns true if it stopped
// early because more than opts.Limit entries matched.
func Tagged(ctx context.Context, opts Options, prefix string, emit func(Result)) (bool, error) {
	found := 0
	err := walk(ctx, opts, func(path, rel string, d iofs.DirEntry) error {
		list, err := tags.Read(path)
		if err != nil {
			return nil
		}
		score, ok := tags.Matches(list, prefix)
		if !ok {
			return nil
		}
		if opts.Limit > 0 && found == opts.Limit {
			return errLimit
		}
		found++
		emit(Result{Path: path, Rel: rel, IsDir: d.IsDir(), Score: score, Tags: list})
		return nil
	})
	return finish(err)
}
