// Package search finds files below a directory by name or by content. It
// reports matches as the walk finds them, so a caller can show results
// while a large tree is still being searched, and stops when its context
// is cancelled.
package search

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	iofs "io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/utils"
)

// DefaultSkip lists directories a search doesn't enter: they are large and
// rarely what is being looked for
var DefaultSkip = []string{".git", "node_modules", "vendor"}

const (
	// maxLine is the longest line a content search reads; the rest of a file
	// with a longer line is skipped
	maxLine = 1024 * 1024
	// maxText is the most of a matching line a result keeps
	maxText = 512
)

// Options says where to search
type Options struct {
	Root       string
	ShowHidden bool     // Include dotfiles and dot-directories
	Skip       []string // Names of directories not to enter; the root is always searched
	Limit      int      // Most results to report; 0 means no limit
}

// Result is a match: an entry for a name search, or a line of a file for a
// content search
type Result struct {
	Path  string // Absolute path
	Rel   string // Path relative to the search root
	IsDir bool
	Line  int    // Line number, from 1; 0 for name matches
	Text  string // The matching line, trimmed and made safe to display
	Col   int    // Where the match starts in Text, in runes
	Score int    // Lower is better; set by the matcher of a name search
}

// Matcher reports whether an entry matches a name search, and how well:
// lower scores are better matches
type Matcher func(rel, name string) (score int, ok bool)

// errLimit stops a walk once it has found more than the limit
var errLimit = errors.New("result limit reached")

// Names reports every entry below opts.Root that match accepts, in the
// order the walk finds them. It returns true if it stopped early because
// more than opts.Limit entries matched.
func Names(ctx context.Context, opts Options, match Matcher, emit func(Result)) (bool, error) {
	found := 0
	err := walk(ctx, opts, func(path, rel string, d iofs.DirEntry) error {
		score, ok := match(rel, d.Name())
		if !ok {
			return nil
		}
		if opts.Limit > 0 && found == opts.Limit {
			return errLimit
		}
		found++
		emit(Result{Path: path, Rel: rel, IsDir: d.IsDir(), Score: score})
		return nil
	})
	return finish(err)
}

// Contents reports every line containing query in the text files below
// opts.Root. Case is ignored unless query has capitals. Binary files are
// skipped, and so is anything that isn't a regular file: reading a pipe
// could block forever. It returns true if it stopped early because more
// than opts.Limit lines matched.
func Contents(ctx context.Context, opts Options, query string, emit func(Result)) (bool, error) {
	if query == "" {
		return false, nil
	}
	fold := query == strings.ToLower(query)
	needle := []byte(query)

	found := 0
	err := walk(ctx, opts, func(path, rel string, d iofs.DirEntry) error {
		if !d.Type().IsRegular() {
			return nil
		}
		return grep(ctx, path, needle, fold, func(line int, text string, col int) error {
			if opts.Limit > 0 && found == opts.Limit {
				return errLimit
			}
			found++
			emit(Result{Path: path, Rel: rel, Line: line, Text: text, Col: col})
			return nil
		})
	})
	return finish(err)
}

// finish turns reaching the limit into a result rather than an error
func finish(err error) (bool, error) {
	if errors.Is(err, errLimit) {
		return true, nil
	}
	return false, err
}

// walk calls visit for every entry below opts.Root that the options allow.
// Symlinks are reported but never followed, so links back up the tree
// can't make it loop. Entries that can't be read are skipped.
func walk(ctx context.Context, opts Options, visit func(path, rel string, d iofs.DirEntry) error) error {
	skip := make(map[string]bool, len(opts.Skip))
	for _, name := range opts.Skip {
		skip[name] = true
	}
	root := opts.Root
	return filepath.WalkDir(root, func(path string, d iofs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if path == root {
			// Only a root that can't be read ends the search early
			return err
		}
		if err != nil {
			return nil
		}

		name := d.Name()
		hidden := !opts.ShowHidden && strings.HasPrefix(name, ".")
		if d.IsDir() && (hidden || skip[name]) {
			return filepath.SkipDir
		}
		if hidden {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = path
		}
		return visit(path, rel, d)
	})
}

// grep calls found for each line of the file at path that contains needle.
// A file that turns out to be binary part way through is abandoned.
func grep(ctx context.Context, path string, needle []byte, fold bool, found func(line int, text string, col int) error) error {
	if binary, err := fs.IsBinary(path); err != nil || binary {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLine)
	for n := 1; scanner.Scan(); n++ {
		// Checked every so often, so a huge file can't hold up a cancel
		if n%1024 == 0 && ctx.Err() != nil {
			return ctx.Err()
		}
		line := scanner.Bytes()
		if bytes.IndexByte(line, 0) >= 0 {
			return nil
		}
		hay := line
		if fold {
			hay = bytes.ToLower(line)
		}
		at := bytes.Index(hay, needle)
		if at < 0 {
			continue
		}
		// Lowercasing keeps the number of runes, so the match is at the
		// same rune in the original line
		text, col := excerpt([]rune(string(line)), utf8.RuneCount(hay[:at]))
		if err := found(n, text, col); err != nil {
			return err
		}
	}
	// A line longer than maxLine ends the file, not the search
	return nil
}

// excerpt returns line ready to display and the match position within it:
// without its indentation, with tabs as spaces and other control
// characters (escape codes that would draw over the interface) replaced as
// utils.Printable does, and cut to a window around the match if very long
func excerpt(line []rune, col int) (string, int) {
	start := 0
	for start < col && unicode.IsSpace(line[start]) {
		start++
	}
	if col-start > maxText/2 {
		start = col - maxText/4
	}
	end := min(len(line), start+maxText)

	text := strings.ReplaceAll(string(line[start:end]), "\t", " ")
	return utils.Printable(text), col - start
}
