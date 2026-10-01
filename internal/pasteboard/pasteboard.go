// Package pasteboard puts files on the macOS pasteboard as file URLs, so
// Cmd+V pastes them in Finder, and reads the files that Finder and other
// apps put there. It goes through AppKit's NSPasteboard with JavaScript
// for Automation (see package jxa).
//
// Finder has no cut for files: what is on the pasteboard is always pasted
// as a copy there.
package pasteboard

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/icichainz/sushi/internal/jxa"
)

// Board is a pasteboard: the general one, which Cmd+C and Cmd+V use, or
// one of its own name
type Board struct {
	run  jxa.Runner
	name string

	// Calls one at a time, so a write can't land between another's
	// clearing and writing, and a read sees writes in the order made
	mu sync.Mutex
}

// New returns the pasteboard called name, or the general one if name is
// empty, reached through run
func New(run jxa.Runner, name string) *Board {
	return &Board{run: run, name: name}
}

// Contents is what a read found on the pasteboard
type Contents struct {
	// Count is the pasteboard's change count, which goes up whenever
	// anything is put on it, by any app
	Count int `json:"count"`
	// Files are the paths of the file URLs on it, in order; none if it
	// holds something else, such as text
	Files []string `json:"files"`
}

// Write replaces what the pasteboard holds with file URLs for paths, which
// must be absolute, and returns its change count afterwards
func (b *Board) Write(ctx context.Context, paths []string) (int, error) {
	script, err := b.writeScript(paths)
	if err != nil {
		return 0, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out, err := b.run(ctx, script)
	if err != nil {
		return 0, err
	}
	var got struct {
		Count *int `json:"count"`
	}
	if err := jxa.Decode(out, &got); err != nil {
		return 0, err
	}
	if got.Count == nil {
		return 0, errors.New("the pasteboard didn't say what it holds")
	}
	return *got.Count, nil
}

// Read returns the pasteboard's change count and the files on it, unless
// the count is known: then what is on it is known too, and it is left
// unread. Pass -1 to read the files whatever the count. Only reading what
// is on the pasteboard can make macOS ask the user whether to allow it;
// the count and the kinds of data on it can be had without.
func (b *Board) Read(ctx context.Context, known int) (Contents, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out, err := b.run(ctx, b.readScript(known))
	if err != nil {
		return Contents{}, err
	}
	var c Contents
	if err := jxa.Decode(out, &c); err != nil {
		return Contents{}, err
	}
	return c, nil
}

// Count returns the pasteboard's change count, without reading what is on
// it, which never makes macOS ask the user anything
func (b *Board) Count(ctx context.Context) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out, err := b.run(ctx, jxa.Prelude+`function run() {
	return result({count: Number(`+b.board()+`.changeCount)});
}
`)
	if err != nil {
		return 0, err
	}
	var got struct {
		Count *int `json:"count"`
	}
	if err := jxa.Decode(out, &got); err != nil {
		return 0, err
	}
	if got.Count == nil {
		return 0, errors.New("the pasteboard didn't say its change count")
	}
	return *got.Count, nil
}

// board is the JavaScript expression for the pasteboard
func (b *Board) board() string {
	if b.name == "" {
		return "$.NSPasteboard.generalPasteboard"
	}
	name, _ := jxa.Literal(b.name) // Names are sushi's own, so valid
	return "$.NSPasteboard.pasteboardWithName(" + name + ")"
}

// writeScript builds the script that puts paths on the pasteboard. An
// NSArray is filled one URL at a time: one made from a JavaScript array
// would turn the URLs into dictionaries, which the pasteboard drops.
// writeObjects reports success even then, so the script reads back how
// many items the pasteboard holds.
func (b *Board) writeScript(paths []string) (string, error) {
	if len(paths) == 0 {
		return "", errors.New("no files to put on the pasteboard")
	}
	for _, p := range paths {
		if !filepath.IsAbs(p) {
			return "", fmt.Errorf("%s is not an absolute path", p)
		}
	}
	list, err := jxa.Literal(paths)
	if err != nil {
		return "", err
	}
	return jxa.Prelude + `function run() {
	const paths = ` + list + `;
	const pb = ` + b.board() + `;
	const urls = $.NSMutableArray.array;
	paths.forEach(p => urls.addObject($.NSURL.fileURLWithPath(p)));
	pb.clearContents;
	if (!pb.writeObjects(urls) || Number(pb.pasteboardItems.count) !== paths.length) {
		throw new Error('the pasteboard did not take the files');
	}
	return result({count: Number(pb.changeCount)});
}
`, nil
}

// readScript builds the script that lists the file URLs on the pasteboard,
// unless its change count is known or it holds no file URL. Finder may
// put file reference URLs there (file:///.file/id=...), which filePathURL
// turns into paths.
func (b *Board) readScript(known int) string {
	return jxa.Prelude + `function run() {
	const pb = ` + b.board() + `;
	const count = Number(pb.changeCount);
	const types = ObjC.deepUnwrap(pb.types) || [];
	if (count === ` + strconv.Itoa(known) + ` || !types.includes('public.file-url')) {
		return result({count: count, files: []});
	}
	const options = $.NSDictionary.dictionaryWithObjectForKey($.NSNumber.numberWithBool(true), $.NSPasteboardURLReadingFileURLsOnlyKey);
	const urls = pb.readObjectsForClassesOptions($.NSArray.arrayWithObject($.NSURL), options);
	const files = [];
	const n = (urls && !urls.isNil()) ? Number(urls.count) : 0;
	for (let i = 0; i < n; i++) {
		const url = urls.objectAtIndex(i).filePathURL;
		if (url && !url.isNil()) {
			files.push(ObjC.unwrap(url.path));
		}
	}
	return result({count: count, files: files});
}
`
}
