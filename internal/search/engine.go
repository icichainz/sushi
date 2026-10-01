package search

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Query is what a search looks for
type Query struct {
	Text    string  // As typed
	Content bool    // Look for Text inside files, rather than in names
	Match   Matcher // Ranks names, for a name search
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
	if q.Content {
		truncated, err = Contents(ctx, opts, q.Text, emit)
	} else {
		truncated, err = Names(ctx, opts, q.Match, emit)
	}
	return Report{Truncated: truncated}, err
}

// defaultPatience is how long Auto waits for Spotlight's first result
// before walking the folder instead. Spotlight answers in about a second
// even for a large home folder; one that has said nothing for this long
// is stuck, or busy indexing.
const defaultPatience = 4 * time.Second

// Auto asks Spotlight, and walks the folder itself where Spotlight can't
// help: when mdfind is missing or fails, or finds nothing in time.
// Spotlight finds nothing in folders it doesn't index, such as temporary
// and hidden ones, excluded volumes and files too new to be indexed yet,
// so a walk also makes sure that "no matches" means none. Searching
// everywhere is Spotlight's alone.
type Auto struct {
	Spotlight Spotlight
	Walker    Walker
	Patience  time.Duration // How long Spotlight may take to find something; 0 is defaultPatience
}

// Search searches with Spotlight, then if need be by walking
func (a Auto) Search(ctx context.Context, opts Options, q Query, emit func(Result)) (Report, error) {
	switch {
	case opts.Everywhere:
		return a.Spotlight.Search(ctx, opts, q, emit)
	case opts.ShowHidden:
		// Spotlight doesn't index hidden folders, so it would leave out
		// what showing hidden files asks for, without a word
		return a.Walker.Search(ctx, opts, q, emit)
	}
	patience := a.Patience
	if patience == 0 {
		patience = defaultPatience
	}

	// Spotlight's results stop counting once it has been given up on, so
	// none can arrive after the walk has started
	var mu sync.Mutex
	found, gaveUp := 0, false
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	timer := time.AfterFunc(patience, func() {
		mu.Lock()
		defer mu.Unlock()
		if found == 0 {
			gaveUp = true
			cancel()
		}
	})
	report, err := a.Spotlight.Search(sctx, opts, q, func(r Result) {
		mu.Lock()
		if gaveUp {
			mu.Unlock()
			return
		}
		found++
		mu.Unlock()
		emit(r)
	})
	timer.Stop()

	if ctx.Err() != nil {
		return report, ctx.Err()
	}
	mu.Lock()
	n := found
	mu.Unlock()
	if n > 0 {
		// Spotlight may have failed part way; what it found stands
		return report, err
	}
	return a.Walker.Search(ctx, opts, q, emit)
}
