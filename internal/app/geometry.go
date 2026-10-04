package app

import (
	"fmt"
	"path/filepath"

	"github.com/icichainz/sushi/internal/utils"
)

// Where things are on screen. The renderer and the mouse handler both work
// from these, so a click always lands on what was drawn under it.

// Screen rows above the panes. The panes follow, each starting with a
// heading row; the status bar and the hints come last.
const (
	tabBarRow     = 0
	breadcrumbRow = 1
	paneTop       = 2
)

// dialogBodyRow is the first row of a dialog's body: dialog draws a
// border, a title bar and a blank row above it
const dialogBodyRow = 3

// area is a part of the screen the mouse can point at
type area int

const (
	areaNone area = iota
	areaTabs
	areaBreadcrumb
	areaParent
	areaList
	areaPreview
	areaStatus // The status bar and the hints below it
	areaDialog
	areaOther // The inactive list of a dual-pane tab; see dual.go
)

// spot is what lies under a screen cell
type spot struct {
	area  area
	row   int // Row of the pane below its heading; -1 is the heading
	index int // The tab, file (into Files) or parent entry there; -1 if none
}

// paneAt returns the pane drawn at column x. mainLines puts the parent,
// the file list and the preview side by side, in that order, and in
// dual-pane mode the inactive list on whichever side of the active one
// it is.
func (l layout) paneAt(x int) area {
	if x < 0 {
		return areaNone
	}
	type col struct {
		area  area
		width int
	}
	cols := []col{{areaParent, l.parentW}, {areaList, l.listW}, {areaOther, l.otherW}, {areaPreview, l.previewW}}
	if l.otherFirst {
		cols[1], cols[2] = cols[2], cols[1]
	}
	for _, c := range cols {
		if x < c.width {
			return c.area
		}
		x -= c.width
	}
	return areaNone
}

// tabSpan is where a tab's label is drawn in the tab bar
type tabSpan struct {
	index int    // Into m.tabs
	label string // Cut to fit if need be
	x     int    // First column
	width int
}

// tabSpans lays out the tab bar. Leading tabs are skipped when needed so
// the active one is always visible, and tabs that don't fit are left out.
func (m Model) tabSpans() []tabSpan {
	labels := make([]string, len(m.tabs))
	for i, tab := range m.tabs {
		name := utils.Printable(filepath.Base(tab.CurrentPath))
		labels[i] = fmt.Sprintf(" %d %s ", i+1, utils.Truncate(name, 15))
	}

	start := 0
	for start < m.activeTabIdx {
		w := 0
		for _, label := range labels[start : m.activeTabIdx+1] {
			w += utils.Width(label)
		}
		if w <= m.width {
			break
		}
		start++
	}

	var spans []tabSpan
	x := 0
	for i := start; i < len(labels); i++ {
		label := labels[i]
		if i == start {
			label = utils.Clip(label, m.width)
		}
		w := utils.Width(label)
		if x+w > m.width {
			break
		}
		spans = append(spans, tabSpan{index: i, label: label, x: x, width: w})
		x += w
	}
	return spans
}

// listStart returns the position in visible of the first file the list
// shows in rows rows, keeping the cursor centered where possible
func (m Model) listStart(visible []int, rows int) int {
	tab := m.tabs[m.activeTabIdx]
	pos := 0
	for i, idx := range visible {
		if idx == tab.Cursor {
			pos = i
		}
	}
	return window(pos, len(visible), rows)
}

// parentStart returns the first entry the parent pane shows in rows rows,
// keeping the current directory centered where possible
func (m Model) parentStart(rows int) int {
	tab := m.tabs[m.activeTabIdx]
	here := 0
	for i, f := range tab.ParentFiles {
		if f.Path == tab.CurrentPath {
			here = i
		}
	}
	return window(here, len(tab.ParentFiles), rows)
}

// spotAt works out what is drawn at column x of screen row y
func (m Model) spotAt(x, y int) spot {
	s := spot{row: -1, index: -1}
	if x < 0 || x >= m.width || y < 0 || y >= m.height {
		return s
	}
	l := m.layout()
	switch {
	case y == tabBarRow:
		s.area = areaTabs
		for _, span := range m.tabSpans() {
			if x >= span.x && x < span.x+span.width {
				s.index = span.index
			}
		}
		return s
	case y == breadcrumbRow:
		s.area = areaBreadcrumb
		return s
	case y >= paneTop+l.bodyH:
		s.area = areaStatus
		return s
	}

	s.area = l.paneAt(x)
	s.row = y - paneTop - 1
	rows := l.bodyH - 1 // Below the heading
	if s.row < 0 || s.row >= rows {
		return s
	}
	tab := m.tabs[m.activeTabIdx]
	switch s.area {
	case areaParent:
		if i := m.parentStart(rows) + s.row; i < len(tab.ParentFiles) {
			s.index = i
		}
	case areaList:
		visible := m.visibleFiles()
		if i := m.listStart(visible, rows) + s.row; i < len(visible) {
			s.index = visible[i]
		}
	case areaOther:
		// Found as it is drawn
		o := m.otherView()
		visible := o.visibleFiles()
		if i := o.listStart(visible, rows) + s.row; i < len(visible) {
			s.index = visible[i]
		}
	}
	return s
}

// dialogFrame returns where withDialog draws box: its top-left corner, and
// the rows of it that fit above the status bar
func (m Model) dialogFrame(box []string) (x, y int, shown []string) {
	body := m.layout().bodyH + paneTop
	shown = box
	if len(shown) > body {
		shown = shown[:body]
	}
	if len(shown) == 0 {
		return 0, 0, nil
	}
	x = max((m.width-utils.Width(shown[0]))/2, 0)
	y = max((body-len(shown))/2, 0)
	return x, y, shown
}

// dialogRowAt returns the row of box drawn at screen cell (x, y), or false
// when the cell is outside the box
func (m Model) dialogRowAt(box []string, x, y int) (int, bool) {
	x0, y0, shown := m.dialogFrame(box)
	if len(shown) == 0 {
		return 0, false
	}
	if x < x0 || x >= x0+utils.Width(shown[0]) || y < y0 || y >= y0+len(shown) {
		return 0, false
	}
	return y - y0, true
}
