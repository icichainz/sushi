package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/pasteboard"
	"golang.org/x/text/unicode/norm"
)

// Sushi shares its clipboard with the macOS pasteboard, unless pasteboard:
// false is set: copying or cutting also puts the files there, so Cmd+V
// pastes them in Finder (always as a copy, as Finder has no cut for
// files), and pasting takes the files Finder or any other app has put
// there since sushi last did, as a copy. The pasteboard's change count,
// which goes up whenever anything is put on it, tells which is newer.

// generalPasteboard is the pasteboard Cmd+C and Cmd+V use
var generalPasteboard = pasteboard.New(runOsascript, "")

// pbState is what sushi knows of the pasteboard
type pbState struct {
	ours     bool // Whether the pasteboard held sushi's clipboard when its change count was count
	count    int
	writeSeq int // The latest write, so one finishing late is ignored
	readSeq  int // The latest read, likewise
}

// pasteboardWrittenMsg says how putting files on the pasteboard went
type pasteboardWrittenMsg struct {
	seq   int
	count int // The pasteboard's change count after the write
	err   error
}

// pasteboardReadMsg brings what is on the pasteboard, for the paste that
// asked, into the folder shown then
type pasteboardReadMsg struct {
	seq      int
	tabID    int
	dir      string
	contents pasteboard.Contents
	err      error
}

// board returns the pasteboard sushi shares its clipboard with, or nil
func (m Model) board() *pasteboard.Board {
	if !onMac || m.config == nil || !m.config.Pasteboard {
		return nil
	}
	return generalPasteboard
}

// putOnPasteboard puts paths, just put in sushi's clipboard, on the
// pasteboard as well, in the background
func (m *Model) putOnPasteboard(paths []string) tea.Cmd {
	board := m.board()
	if board == nil {
		return nil
	}
	m.pb.writeSeq++
	// Until the write is in, a paste takes sushi's clipboard, the newer
	m.pb.ours = false
	seq := m.pb.writeSeq
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), macTimeout)
		defer cancel()
		count, err := board.Write(ctx, paths)
		return pasteboardWrittenMsg{seq: seq, count: count, err: err}
	}
}

func (msg pasteboardWrittenMsg) apply(m Model) (tea.Model, tea.Cmd) {
	if msg.seq != m.pb.writeSeq {
		return m, nil
	}
	if msg.err != nil {
		// Still in sushi's clipboard, which is what the paste key uses
		cmd := m.setStatus(fmt.Sprintf("Can't share the clipboard with Finder: %v", msg.err))
		return m, cmd
	}
	m.pb.ours, m.pb.count = true, msg.count
	return m, nil
}

// paste pastes into the current folder. With the pasteboard shared, it
// first reads what is there, which takes osascript a moment, and goes on
// when pasteboardReadMsg brings it. While sushi's own write isn't in, or
// after it failed, sushi's clipboard is the newer, so that is pasted at
// once, unless it is empty.
func (m Model) paste() (tea.Model, tea.Cmd) {
	board := m.board()
	if board == nil || !m.pb.ours && len(m.clipboard) > 0 {
		return m.startPaste()
	}
	// What sushi put there is in its clipboard already, so the files are
	// read only if something else has been put there since
	known := -1
	if m.pb.ours {
		known = m.pb.count
	}
	m.pb.readSeq++
	tab := m.tab()
	msg := pasteboardReadMsg{seq: m.pb.readSeq, tabID: tab.ID, dir: tab.CurrentPath}
	read := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), macTimeout)
		defer cancel()
		msg.contents, msg.err = board.Read(ctx, known)
		return msg
	}
	status := m.setStatus("Reading the pasteboard" + currentGlyphs().more)
	return m, tea.Batch(status, read)
}

// apply pastes the files on the pasteboard, if they are newer than sushi's
// clipboard, and sushi's clipboard otherwise, through startPaste, which
// asks before overwriting anything. Only a paste still wanted where it was
// asked for goes ahead.
func (msg pasteboardReadMsg) apply(m Model) (tea.Model, tea.Cmd) {
	if msg.seq != m.pb.readSeq || m.mode != ModeNormal {
		return m, nil
	}
	if m.tab().ID != msg.tabID || m.tab().CurrentPath != msg.dir {
		cmd := m.setStatus("The folder shown changed, so nothing was pasted")
		return m, cmd
	}
	if m.job != nil {
		cmd := m.stillBusy()
		return m, cmd
	}
	if msg.err != nil {
		if len(m.clipboard) == 0 {
			cmd := m.setStatus(fmt.Sprintf("Can't read the pasteboard: %v", msg.err))
			return m, cmd
		}
		return m.startPaste()
	}
	if files := m.fromPasteboard(msg.contents); files != nil {
		// They are sushi's clipboard now, as a copy, so pasting again
		// pastes them again, as in Finder
		m.clipboard, m.clipboardMode = files, "copy"
		m.pb.ours, m.pb.count = true, msg.contents.Count
	}
	return m.startPaste()
}

// fromPasteboard returns the files a paste takes from the pasteboard, or
// nil if it takes sushi's clipboard. They have to be there still, and put
// there by another app since sushi last put its clipboard there; when
// sushi's own write isn't in, or failed, its clipboard is the newer,
// unless it is empty.
func (m Model) fromPasteboard(c pasteboard.Contents) []string {
	var files []string
	for _, f := range c.Files {
		if _, err := os.Lstat(f); err == nil && filepath.IsAbs(f) {
			files = append(files, filepath.Clean(f))
		}
	}
	switch {
	case len(files) == 0, sameFiles(files, m.clipboard):
		return nil
	case m.pb.ours && c.Count == m.pb.count, !m.pb.ours && len(m.clipboard) > 0:
		return nil
	}
	return files
}

// sameFiles reports whether a and b list the same files. AppKit hands
// names back decomposed, é as e and an accent, which macOS file systems
// take for the same name.
func sameFiles(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, p := range a {
		seen[norm.NFC.String(p)]++
	}
	for _, p := range b {
		k := norm.NFC.String(p)
		if seen[k] == 0 {
			return false
		}
		seen[k]--
	}
	return true
}
