package search

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/tags"
)

// Spotlight searches macOS's Spotlight index, through mdfind. It finds
// names and tags without reading every folder, can search every indexed
// volume, and finds text inside documents a walk can't read, such as
// PDFs. What it reports goes through a walk's rules: hidden files as
// asked, the folders searches skip, the matcher for names, and for text,
// the lines found by reading the files.
type Spotlight struct{}

// ErrSpotlight wraps the reasons Spotlight couldn't search
var ErrSpotlight = errors.New("Spotlight can't search")

// Search asks mdfind, below opts.Root or everywhere
func (Spotlight) Search(ctx context.Context, opts Options, q Query, emit func(Result)) (Report, error) {
	report := Report{Spotlight: true}
	expr, ok := spotlightQuery(q)
	if !ok {
		return report, fmt.Errorf("%w for %q", ErrSpotlight, q.Text)
	}
	args := []string{"-0"} // Names can hold newlines
	if !opts.Everywhere {
		args = append(args, "-onlyin", opts.Root)
	}
	args = append(args, expr)

	// Its own context, so a search that has found enough stops mdfind
	run, stop := context.WithCancel(ctx)
	defer stop()
	cmd := exec.CommandContext(run, "mdfind", args...)
	cmd.WaitDelay = time.Second
	stderr := &tail{}
	cmd.Stderr = stderr
	out, err := cmd.StdoutPipe()
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		return report, fmt.Errorf("%w: %w", ErrSpotlight, err)
	}
	// Reading stops when the search does, even if something mdfind started
	// still holds the pipe open
	context.AfterFunc(run, func() { out.Close() })

	f := newFilter(opts)
	found := 0
	keep := func(r Result) error {
		if opts.Limit > 0 && found == opts.Limit {
			return errLimit
		}
		found++
		emit(r)
		return nil
	}
	reader := bufio.NewReader(out)
	var halt error
	for halt == nil {
		path, err := reader.ReadString(0)
		if path = strings.TrimSuffix(path, "\x00"); path != "" {
			halt = visitSpotlight(run, f, q, path, keep)
		}
		if err != nil {
			break
		}
	}
	if halt != nil {
		stop() // Otherwise it is let finish, so its exit status says how it went
	}
	waitErr := cmd.Wait()

	switch {
	case ctx.Err() != nil:
		return report, ctx.Err()
	case errors.Is(halt, errLimit):
		report.Truncated = true
		return report, nil
	case halt != nil:
		return report, halt
	case waitErr != nil:
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			waitErr = fmt.Errorf("%w: %s", waitErr, msg)
		}
		return report, fmt.Errorf("%w: %w", ErrSpotlight, waitErr)
	}
	return report, nil
}

// visitSpotlight reports what matches q at path, which Spotlight found
func visitSpotlight(ctx context.Context, f filter, q Query, path string, keep func(Result) error) error {
	r, mode, ok := f.accept(path)
	if !ok {
		return nil
	}
	switch {
	case q.Tagged:
		// The index can be behind; the file has the last word
		list, err := tags.Read(r.Path)
		if err != nil {
			return nil
		}
		score, ok := tags.Matches(list, q.Tag)
		if !ok {
			return nil
		}
		r.Score, r.Tags = score, list
		return keep(r)

	case q.Content:
		if !mode.IsRegular() {
			return nil
		}
		// A document Spotlight read the text of, such as a PDF, is a
		// match without a line
		if binary, err := fs.IsBinary(r.Path); err != nil || binary {
			if err != nil {
				return nil
			}
			return keep(r)
		}
		// In text, the lines that match, as a walk finds them. A file
		// Spotlight matched only by ignoring case or by word isn't one.
		fold := q.Text == strings.ToLower(q.Text)
		return grep(ctx, r.Path, []byte(q.Text), fold, func(line int, text string, col int) error {
			hit := r
			hit.Line, hit.Text, hit.Col = line, text, col
			return keep(hit)
		})
	}

	if q.Match != nil {
		score, ok := q.Match(r.Rel, filepath.Base(r.Path))
		if !ok {
			return nil
		}
		r.Score = score
	}
	return keep(r)
}

// spotlightQuery returns the mdfind query for q, or false if Spotlight
// can't look for it
func spotlightQuery(q Query) (string, bool) {
	switch {
	case q.Tagged:
		if q.Tag == "" {
			return `kMDItemUserTags == "*"`, true
		}
		p, ok := pattern(q.Tag)
		return `kMDItemUserTags == "` + p + `*"c`, ok

	case q.Content:
		// Spotlight indexes words, so each word is looked for on its own,
		// and the lines with all of them as typed are found in the files
		var parts []string
		for _, word := range strings.Fields(q.Text) {
			if p, ok := pattern(word); ok {
				parts = append(parts, `kMDItemTextContent == "*`+p+`*"c`)
			}
		}
		return strings.Join(parts, " && "), len(parts) > 0
	}

	// Spotlight knows names, not paths: it looks for the last part of a
	// path, and the matcher checks the rest
	name := q.Text
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	p, ok := pattern(name)
	return `kMDItemFSName == "*` + p + `*"c`, ok
}

// pattern makes s safe in a quoted Spotlight value: the characters that
// mean something there become wildcards, so Spotlight finds a little more
// than asked, and the checks on what it finds drop the rest. It is false
// if nothing else is left, which would ask for everything.
func pattern(s string) (string, bool) {
	var b strings.Builder
	literal := false
	for _, r := range s {
		switch r {
		case '"', '\\', '*', '?':
			if !strings.HasSuffix(b.String(), "*") {
				b.WriteByte('*')
			}
		default:
			b.WriteRune(r)
			literal = true
		}
	}
	return b.String(), literal
}

// filter applies a walk's rules to what Spotlight finds
type filter struct {
	opts Options
	root string // The root as given
	real string // The root with symlinks resolved, as Spotlight reports paths
	skip map[string]bool
}

func newFilter(opts Options) filter {
	f := filter{opts: opts, root: filepath.Clean(opts.Root), skip: make(map[string]bool, len(opts.Skip))}
	f.real = f.root
	if real, err := filepath.EvalSymlinks(f.root); err == nil {
		f.real = real
	}
	for _, name := range opts.Skip {
		f.skip[name] = true
	}
	return f
}

// accept turns a path Spotlight found into a result, unless a walk
// wouldn't have reported it: it is hidden, in a folder searches skip, or
// gone since it was indexed. Paths below the root are given as below the
// root as it was given, so /tmp stays /tmp rather than /private/tmp.
func (f filter) accept(path string) (Result, os.FileMode, bool) {
	if !filepath.IsAbs(path) {
		return Result{}, 0, false
	}
	path = filepath.Clean(path)
	rel, shown, inside := f.relative(path)
	if !inside && !f.opts.Everywhere || rel == "." {
		return Result{}, 0, false
	}
	info, err := os.Lstat(shown)
	if err != nil {
		return Result{}, 0, false
	}
	parts := strings.Split(strings.TrimPrefix(rel, string(filepath.Separator)), string(filepath.Separator))
	for i, part := range parts {
		if !f.opts.ShowHidden && strings.HasPrefix(part, ".") {
			return Result{}, 0, false
		}
		// A walk doesn't enter these folders, or list them
		if f.skip[part] && (i < len(parts)-1 || info.IsDir()) {
			return Result{}, 0, false
		}
	}
	return Result{Path: shown, Rel: rel, IsDir: info.IsDir()}, info.Mode(), true
}

// relative returns path relative to the root and as below the root as
// given, or path itself, absolute, if it is outside the root
func (f filter) relative(path string) (rel, shown string, inside bool) {
	for _, root := range []string{f.real, f.root} {
		r, err := filepath.Rel(root, path)
		if err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			return r, filepath.Join(f.root, r), true
		}
	}
	return path, path, false
}

// tail keeps the end of what a command writes to stderr, for its error
type tail struct{ b []byte }

func (t *tail) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	if len(t.b) > 2048 {
		t.b = t.b[len(t.b)-2048:]
	}
	return len(p), nil
}

func (t *tail) String() string { return string(t.b) }
