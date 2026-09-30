package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/ui"
	"github.com/icichainz/sushi/internal/ui/components"
	"github.com/icichainz/sushi/internal/utils"
)

const (
	// minWidthParent is the terminal width from which the parent pane shows
	minWidthParent = 100
	// minWidthPreview is the terminal width from which the preview pane shows
	minWidthPreview = 72
	// chromeRows is the rows around the panes: tabs, breadcrumb, status, hints
	chromeRows = 4
)

// glyphs are the drawing characters, with plain ASCII ones for --ascii
type glyphs struct {
	vline, hline, tl, tr, bl, br string
	mark, up, down, dot, more    string
}

func currentGlyphs() glyphs {
	if ui.GetIconMode() == ui.IconModeASCII {
		return glyphs{"|", "-", "+", "+", "+", "+", "*", "^", "v", "-", "..."}
	}
	return glyphs{"│", "─", "╭", "╮", "╰", "╯", "●", "↑", "↓", "·", "…"}
}

// layout is how the terminal is divided between the panes
type layout struct {
	parentW, listW, previewW int // Pane widths including their divider; 0 hides the pane
	bodyH                    int // Rows of each pane, heading included
}

// layout works out the panes for the current terminal size: narrow
// terminals lose the parent pane first, then the preview
func (m Model) layout() layout {
	tab := m.tabs[m.activeTabIdx]
	l := layout{bodyH: max(m.height-chromeRows, 1)}
	rest := m.width
	if m.width >= minWidthParent && filepath.Dir(tab.CurrentPath) != tab.CurrentPath {
		l.parentW = min(max(m.width*18/100, 18), 30)
		rest -= l.parentW
	}
	if tab.PreviewEnabled && m.width >= minWidthPreview {
		l.previewW = rest * tab.PreviewWidth / 100
	}
	l.listW = rest - l.previewW
	return l
}

// listRows returns how many files fit in the file list
func (m Model) listRows() int {
	return max(m.layout().bodyH-1, 1)
}

// previewRows returns the height of the preview pane, heading included
func (m Model) previewRows() int {
	return m.layout().bodyH
}

func (m Model) fg(c lipgloss.Color) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(c)
}

// View renders the application
func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	lines := m.mainLines()
	switch m.mode {
	case ModeConfirm:
		lines = m.withDialog(lines, m.confirmBox())
	case ModeBookmarks:
		lines = m.withDialog(lines, m.bookmarksBox())
	case ModePlugins:
		lines = m.withDialog(lines, m.runBox())
	case ModeFind:
		lines = m.withDialog(lines, m.findBox())
	case ModeHelp:
		lines = m.withHelp(lines)
	}

	// Never exceed the terminal: Bubble Tea drops lines from the top of an
	// oversized frame, which would hide the tabs
	return lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height).Render(strings.Join(lines, "\n"))
}

// renderMainView renders the browser: tabs, breadcrumb, panes, status, hints
func (m Model) renderMainView() string {
	return strings.Join(m.mainLines(), "\n")
}

func (m Model) mainLines() []string {
	l := m.layout()
	lines := make([]string, 0, l.bodyH+chromeRows)
	lines = append(lines, m.renderTabBar(), m.renderHeader())

	var parent, preview []string
	if l.parentW > 0 {
		parent = m.renderParent(l.parentW, l.bodyH)
	}
	list := m.renderFileList(l.listW, l.bodyH, l.parentW > 0)
	if l.previewW > 0 {
		preview = m.renderPreview(l.previewW, l.bodyH)
	}
	for i := 0; i < l.bodyH; i++ {
		row := list[i]
		if parent != nil {
			row = parent[i] + row
		}
		if preview != nil {
			row += preview[i]
		}
		lines = append(lines, row)
	}

	return append(lines, m.renderStatusBar(), m.renderBottomRow())
}

// renderTabBar renders the tabs, which are always shown
func (m Model) renderTabBar() string {
	t := m.theme
	bar := lipgloss.NewStyle().Background(t.TabBarBg)
	active := lipgloss.NewStyle().Background(t.Accent).Foreground(t.TabActiveFg).Bold(true)
	inactive := lipgloss.NewStyle().Background(t.TabInactiveBg).Foreground(t.TabInactiveFg)

	labels := make([]string, len(m.tabs))
	for i, tab := range m.tabs {
		name := filepath.Base(tab.CurrentPath)
		labels[i] = fmt.Sprintf(" %d %s ", i+1, utils.Truncate(name, 15))
	}

	// Skip leading tabs when needed so the active one is always visible
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

	var b strings.Builder
	used := 0
	for i := start; i < len(labels); i++ {
		label := labels[i]
		if i == start {
			label = utils.Clip(label, m.width)
		}
		w := utils.Width(label)
		if used+w > m.width {
			break
		}
		if i == m.activeTabIdx {
			b.WriteString(active.Render(label))
		} else {
			b.WriteString(inactive.Render(label))
		}
		used += w
	}

	right := "P run  b bookmarks "
	if rw := utils.Width(right); used+rw+2 <= m.width {
		b.WriteString(bar.Render(strings.Repeat(" ", m.width-used-rw)))
		b.WriteString(bar.Foreground(t.TabInactiveFg).Render(right))
	} else {
		b.WriteString(bar.Render(strings.Repeat(" ", m.width-used)))
	}
	return b.String()
}

// sortLabel describes the sort order, e.g. "name ↑"
func (m Model) sortLabel() string {
	g := currentGlyphs()
	// Size and modified list the largest and newest first
	descending := m.config.SortBy == "size" || m.config.SortBy == "modified"
	if m.config.SortReverse {
		descending = !descending
	}
	if descending {
		return m.config.SortBy + " " + g.down
	}
	return m.config.SortBy + " " + g.up
}

// pathSegments splits a path for the breadcrumb, with the home directory as "~"
func pathSegments(path string) []string {
	sep := string(filepath.Separator)
	if home, err := os.UserHomeDir(); err == nil && home != sep {
		if path == home {
			return []string{"~"}
		}
		if rest, ok := strings.CutPrefix(path, home+sep); ok {
			return append([]string{"~"}, strings.Split(rest, sep)...)
		}
	}
	trimmed := strings.Trim(path, sep)
	if trimmed == "" {
		return []string{sep}
	}
	segs := strings.Split(trimmed, sep)
	if strings.HasPrefix(path, sep) {
		segs[0] = sep + segs[0]
	}
	return segs
}

// renderHeader renders the breadcrumb, with the sort order and hidden-file
// setting on the right
func (m Model) renderHeader() string {
	t := m.theme
	g := currentGlyphs()
	tab := m.tabs[m.activeTabIdx]
	inner := max(m.width-2, 0)

	hidden := "off"
	if m.showHidden {
		hidden = "on"
	}
	right := fmt.Sprintf("sort %s %s hidden %s", m.sortLabel(), g.dot, hidden)
	rightW := utils.Width(right)

	// Drop leading directories until the path fits, keeping the current one
	segs := pathSegments(tab.CurrentPath)
	width := func(s []string) int { return utils.Width(strings.Join(s, " / ")) }
	if width(segs)+2+rightW > inner {
		right, rightW = "", 0
	}
	for len(segs) > 1 && width(segs) > inner {
		segs = append([]string{g.more}, segs[2:]...)
		if len(segs) == 1 {
			break
		}
	}

	var b strings.Builder
	used := 0
	for i, seg := range segs {
		last := i == len(segs)-1
		if last {
			seg = utils.TruncateLeft(seg, max(inner-used, 0))
			b.WriteString(m.fg(t.HeaderFg).Bold(true).Render(seg))
		} else {
			b.WriteString(m.fg(t.Muted).Render(seg) + m.fg(t.Faint).Render(" / "))
			used += 3
		}
		used += utils.Width(seg)
	}
	gap := max(inner-used-rightW, 0)
	return utils.Fit(" "+b.String()+strings.Repeat(" ", gap)+m.fg(t.Muted).Render(right), m.width)
}

// divider returns the line drawn on the left edge of a pane
func (m Model) divider() string {
	return m.fg(m.theme.Border).Render(currentGlyphs().vline)
}

// window returns the first visible index of a list of n items showing rows
// at a time, keeping the cursor centered where possible
func window(cursor, n, rows int) int {
	start := max(0, cursor-rows/2)
	if start+rows > n {
		start = max(0, n-rows)
	}
	return start
}

// renderParent renders the parent directory, with the current one marked
func (m Model) renderParent(width, height int) []string {
	t := m.theme
	tab := m.tabs[m.activeTabIdx]
	parentPath := filepath.Dir(tab.CurrentPath)
	name := filepath.Base(parentPath)

	out := make([]string, 0, height)
	out = append(out, utils.Fit(" "+m.fg(t.Faint).Render(utils.Truncate(name, width-2)), width))

	here := 0
	for i, f := range tab.ParentFiles {
		if f.Path == tab.CurrentPath {
			here = i
		}
	}
	rows := height - 1
	start := window(here, len(tab.ParentFiles), rows)
	for i := start; i < start+rows; i++ {
		if i >= len(tab.ParentFiles) {
			out = append(out, strings.Repeat(" ", width))
			continue
		}
		f := tab.ParentFiles[i]
		text := utils.Fit(" "+ui.GetFileIcon(f)+"  "+f.Name, width)
		style := m.fg(t.Faint)
		if f.Path == tab.CurrentPath {
			style = lipgloss.NewStyle().Foreground(t.Text).Background(t.Raised).Bold(true)
		}
		out = append(out, style.Render(text))
	}
	return out
}

// columns is how a file row is divided
type columns struct {
	inner, iconW, nameW int
	size, date          bool
}

const (
	sizeW = 9  // "1023.9 KB"
	dateW = 14 // Two spaces and "Jan 02 15:04"
)

// listColumns sizes the columns; narrow lists lose the date, then the size
func listColumns(inner int, files []fs.FileInfo) columns {
	c := columns{inner: inner, iconW: 1, size: inner >= 40, date: inner >= 58}
	for _, f := range files {
		c.iconW = max(c.iconW, utils.Width(ui.GetFileIcon(f)))
	}
	// Margin, marker, space, icon, two spaces, name, [size], [date], margin
	c.nameW = inner - 3 - c.iconW - 2 - 1
	if c.size {
		c.nameW -= sizeW
	}
	if c.date {
		c.nameW -= dateW
	}
	c.nameW = max(c.nameW, 4)
	return c
}

// visibleFiles returns the indexes of the files the list shows: all of
// them, or only the matches while searching
func (m Model) visibleFiles() []int {
	tab := m.tabs[m.activeTabIdx]
	if m.mode == ModeSearch && tab.SearchQuery != "" {
		return tab.SearchResults
	}
	all := make([]int, len(tab.Files))
	for i := range all {
		all[i] = i
	}
	return all
}

// renderFileList renders the heading and rows of the file list
func (m Model) renderFileList(width, height int, divider bool) []string {
	t := m.theme
	g := currentGlyphs()
	tab := m.tabs[m.activeTabIdx]

	edge := ""
	inner := width
	if divider {
		edge = m.divider()
		inner--
	}
	visible := m.visibleFiles()
	files := make([]fs.FileInfo, len(visible))
	for i, idx := range visible {
		files[i] = tab.Files[idx]
	}
	c := listColumns(inner, files)

	// Heading, with an arrow on the sorted column
	arrow := func(col string) string {
		by := m.config.SortBy
		if by == col || (col == "name" && by == "type") {
			return " " + strings.TrimPrefix(m.sortLabel(), by+" ")
		}
		return ""
	}
	nameHead := "Name"
	if m.config.SortBy == "type" {
		nameHead = "Name (by type)"
	}
	head := strings.Repeat(" ", 3+c.iconW+2) + utils.Fit(nameHead+arrow("name"), c.nameW)
	if c.size {
		head += utils.FitRight("Size"+arrow("size"), sizeW)
	}
	if c.date {
		head += utils.FitRight("Modified"+arrow("modified"), dateW)
	}
	out := make([]string, 0, height)
	out = append(out, edge+m.fg(t.Faint).Render(utils.Fit(head, inner)))

	rows := height - 1
	message := ""
	switch {
	case tab.Loading && len(tab.Files) == 0:
		message = "Loading" + g.more
	case len(tab.Files) == 0:
		message = "Empty directory"
	case len(visible) == 0:
		message = "No matches"
	}
	if message != "" {
		out = append(out, edge+m.fg(t.Faint).Render(utils.Fit("   "+message, inner)))
		for len(out) < height {
			out = append(out, edge+strings.Repeat(" ", inner))
		}
		return out
	}

	cursorPos := 0
	for i, idx := range visible {
		if idx == tab.Cursor {
			cursorPos = i
		}
	}
	start := window(cursorPos, len(visible), rows)
	renaming := m.mode == ModeInput && m.prompt.action == promptRename
	for i := start; i < start+rows; i++ {
		if i >= len(visible) {
			out = append(out, edge+strings.Repeat(" ", inner))
			continue
		}
		idx := visible[i]
		file := tab.Files[idx]
		if renaming && idx == tab.Cursor {
			out = append(out, edge+m.renderRenameRow(file, c))
			continue
		}
		var matched map[int]bool
		if m.mode == ModeSearch {
			matched = fuzzyPositions(strings.ToLower(tab.SearchQuery), strings.ToLower(file.Name))
		}
		out = append(out, edge+m.renderFileLine(file, idx == tab.Cursor, matched, c))
	}
	return out
}

// renderFileLine renders a single file row
func (m Model) renderFileLine(file fs.FileInfo, isCursor bool, matched map[int]bool, c columns) string {
	t := m.theme
	selected := m.tabs[m.activeTabIdx].Selected[file.Path]
	cut := m.clipboardMode == "cut" && m.inClipboard(file.Path)

	// A marker shows selection without relying on color alone
	marker := " "
	if selected {
		marker = currentGlyphs().mark
	}

	markStyle := m.fg(t.Selected)
	iconStyle := m.fg(t.Muted)
	nameStyle := m.fg(t.Text)
	metaStyle := m.fg(t.Muted)
	if file.IsDir {
		iconStyle = m.fg(t.Directory)
		nameStyle = nameStyle.Bold(true)
	}
	if selected {
		nameStyle = nameStyle.Foreground(t.Selected)
	}
	if cut {
		iconStyle = m.fg(t.Faint)
		nameStyle = m.fg(t.Faint).Italic(true)
		metaStyle = m.fg(t.Faint)
	}
	if isCursor {
		base := lipgloss.NewStyle().Foreground(t.CursorFg).Background(t.CursorBg)
		markStyle, iconStyle, metaStyle = base, base, base
		nameStyle = base.Bold(file.IsDir).Italic(cut)
	}

	var b strings.Builder
	b.WriteString(markStyle.Render(" " + marker + " "))
	b.WriteString(iconStyle.Render(utils.Fit(ui.GetFileIcon(file), c.iconW) + "  "))

	// Underline the letters the search matched
	name := utils.Truncate(file.Name, c.nameW)
	shown := []rune(name)
	kept := len(shown)
	if name != file.Name {
		kept -= 3 // The ellipsis
	}
	for i := 0; i < len(shown); {
		hit := i < kept && matched[i]
		j := i
		for j < len(shown) && (j < kept && matched[j]) == hit {
			j++
		}
		style := nameStyle
		if hit {
			style = style.Underline(true).Bold(true)
		}
		b.WriteString(style.Render(string(shown[i:j])))
		i = j
	}
	b.WriteString(nameStyle.Underline(false).Render(strings.Repeat(" ", max(c.nameW-utils.Width(name), 0))))

	meta := ""
	if c.size {
		meta += utils.FitRight(utils.HumanizeSize(file.Size), sizeW)
	}
	if c.date {
		meta += utils.FitRight(file.ModTime.Format("Jan 02 15:04"), dateW)
	}
	b.WriteString(metaStyle.Render(meta + " "))
	return utils.Cells(b.String(), 0, c.inner)
}

// renderRenameRow renders the row being renamed as a text field, with any
// error beside it
func (m Model) renderRenameRow(file fs.FileInfo, c columns) string {
	t := m.theme
	base := lipgloss.NewStyle().Background(t.Raised)
	lead := base.Foreground(t.Accent).Render("   " + utils.Fit(ui.GetFileIcon(file), c.iconW) + "  ")

	room := max(c.inner-utils.Width(lead)-1, 1)
	errText := ""
	if m.prompt.err != "" {
		errText = utils.Truncate(m.prompt.err, room/2)
		room -= utils.Width(errText) + 2
	}
	input := m.prompt.input.View(room, base.Foreground(t.Text), base.Foreground(t.Text).Reverse(true))
	row := lead + input + base.Render(strings.Repeat(" ", max(room-utils.Width(input), 0)))
	if errText != "" {
		row += base.Render("  ") + base.Foreground(t.Danger).Render(errText)
	}
	return utils.Cells(row+base.Render(" "), 0, c.inner)
}

// renderPreview renders the preview pane: the selection if there is one,
// otherwise the file under the cursor
func (m Model) renderPreview(width, height int) []string {
	t := m.theme
	tab := m.tabs[m.activeTabIdx]
	edge := m.divider()
	inner := max(width-1, 1)

	var lines []string
	switch {
	case len(tab.Selected) > 0:
		lines = m.renderSelection(inner, height)
	case len(tab.Files) == 0:
		lines = []string{utils.Fit(" "+m.fg(t.Faint).Render("Nothing to preview"), inner)}
	default:
		lines = components.RenderPreview(tab.Preview, inner, height, tab.PreviewScroll, components.PreviewStyles{
			Title: m.fg(t.Text).Bold(true),
			Text:  m.fg(t.Text),
			Faint: m.fg(t.Faint),
			Dir:   m.fg(t.Directory).Bold(true),
			Error: m.fg(t.Danger),
		})
	}

	out := make([]string, 0, height)
	for i := 0; i < height; i++ {
		if i < len(lines) {
			out = append(out, edge+lines[i])
		} else {
			out = append(out, edge+strings.Repeat(" ", inner))
		}
	}
	return out
}

// renderSelection summarises the selected files in the preview pane
func (m Model) renderSelection(width, height int) []string {
	t := m.theme
	g := currentGlyphs()
	tab := m.tabs[m.activeTabIdx]

	var here []fs.FileInfo
	var size int64
	for _, f := range tab.Files {
		if tab.Selected[f.Path] {
			here = append(here, f)
			size += f.Size
		}
	}
	elsewhere := len(tab.Selected) - len(here)

	lines := []string{" " + m.fg(t.Selected).Bold(true).Render(fmt.Sprintf("%d selected", len(tab.Selected)))}
	room := max(height-4, 1)
	for i, f := range here {
		if i == room-1 && len(here) > room {
			lines = append(lines, " "+m.fg(t.Faint).Render(fmt.Sprintf("and %d more", len(here)-i)))
			break
		}
		name := utils.Fit(g.mark+" "+ui.GetFileIcon(f)+"  "+f.Name, max(width-sizeW-3, 4))
		lines = append(lines, " "+m.fg(t.Selected).Render(name)+m.fg(t.Muted).Render(utils.FitRight(utils.HumanizeSize(f.Size), sizeW)))
	}
	total := fmt.Sprintf("%s in %d here", utils.HumanizeSize(size), len(here))
	if elsewhere > 0 {
		total += fmt.Sprintf(", %d in other folders", elsewhere)
	}
	lines = append(lines, "", " "+m.fg(t.Muted).Render(utils.Truncate(total, width-2)))

	for i := range lines {
		lines[i] = utils.Fit(lines[i], width)
	}
	return lines
}

// modeBadge returns the name and color of the current mode
func (m Model) modeBadge() (string, lipgloss.Color) {
	t := m.theme
	switch m.mode {
	case ModeSearch:
		return "SEARCH", t.Accent
	case ModeInput:
		if m.prompt.action == promptRename {
			return "RENAME", t.Accent
		}
		return "NEW", t.Accent
	case ModeConfirm:
		return "CONFIRM", t.Danger
	case ModeBookmarks:
		return "BOOKMARKS", t.Accent
	case ModePlugins:
		return "RUN", t.Accent
	case ModeFind:
		return "FIND", t.Accent
	case ModeHelp:
		return "KEYS", t.Accent
	}
	if len(m.tabs[m.activeTabIdx].Selected) > 0 {
		return "SELECT", t.Selected
	}
	return "NORMAL", t.Accent
}

// isProblem reports whether a status message describes a failure
func isProblem(msg string) bool {
	for _, word := range []string{"Error", "failed", "Can't", "unknown", "invalid"} {
		if strings.Contains(msg, word) {
			return true
		}
	}
	return false
}

// renderStatusBar renders the mode, counts, clipboard and messages
func (m Model) renderStatusBar() string {
	t := m.theme
	tab := m.tabs[m.activeTabIdx]
	bar := lipgloss.NewStyle().Background(t.BarBg).Foreground(t.BarFg)

	mode, color := m.modeBadge()
	badge := lipgloss.NewStyle().Background(color).Foreground(t.CursorFg).Bold(true).Render(" " + mode + " ")

	visible := m.visibleFiles()
	right := ""
	if len(visible) > 0 {
		pos := 0
		for i, idx := range visible {
			if idx == tab.Cursor {
				pos = i
			}
		}
		right = fmt.Sprintf("%d/%d ", pos+1, len(visible))
	}

	type segment struct {
		text  string
		style lipgloss.Style
	}
	var segs []segment
	if m.mode == ModeSearch {
		segs = append(segs, segment{fmt.Sprintf("%d of %d match", len(visible), len(tab.Files)), bar})
	} else {
		segs = append(segs,
			segment{fmt.Sprintf("%d items", len(tab.Files)), bar},
			segment{utils.HumanizeSize(tab.TotalSize), bar.Foreground(t.Muted)})
	}
	if n := len(tab.Selected); n > 0 {
		segs = append(segs, segment{fmt.Sprintf("%d selected", n), bar.Foreground(t.Selected)})
	}
	if n := len(m.clipboard); n > 0 {
		segs = append(segs, segment{fmt.Sprintf("clipboard: %s %d", m.clipboardMode, n), bar.Foreground(t.Muted)})
	}
	if m.statusMsg != "" {
		style := bar.Foreground(t.Highlight)
		if isProblem(m.statusMsg) {
			style = bar.Foreground(t.Danger)
		}
		segs = append(segs, segment{m.statusMsg, style})
	}

	// The message is last, so it is what gets cut when space runs out
	room := m.width - utils.Width(badge) - utils.Width(right)
	var b strings.Builder
	used := 0
	for _, s := range segs {
		text := utils.Truncate("  "+s.text, room-used-1)
		if utils.Width(text) < 5 {
			break
		}
		b.WriteString(s.style.Render(text))
		used += utils.Width(text)
	}
	gap := max(room-used, 0)
	line := badge + b.String() + bar.Render(strings.Repeat(" ", gap)) + bar.Foreground(t.Muted).Render(right)
	return utils.Cells(line, 0, m.width)
}

type hint struct{ key, label string }

// renderHints renders as many key hints as fit on one line
func (m Model) renderHints(hints []hint) string {
	t := m.theme
	var b strings.Builder
	used := 1
	for _, h := range hints {
		w := utils.Width(h.key) + 1 + utils.Width(h.label) + 3
		if used+w > m.width {
			break
		}
		b.WriteString(m.fg(t.Highlight).Bold(true).Render(h.key) + " " + m.fg(t.Muted).Render(h.label) + "   ")
		used += w
	}
	return utils.Fit(" "+b.String(), m.width)
}

// renderBottomRow renders the last line: key hints for the current mode,
// or the text being typed
func (m Model) renderBottomRow() string {
	switch m.mode {
	case ModeSearch:
		return m.renderSearchBar()
	case ModeInput:
		if m.prompt.action != promptRename {
			return m.renderPromptBar()
		}
		return m.renderHints([]hint{{"enter", "save"}, {"esc", "cancel"}, {"ctrl+u", "clear"}, {"ctrl+w", "delete word"}})
	case ModeConfirm:
		verb := "delete"
		if m.confirmAction == "paste" {
			verb = "overwrite"
		}
		return m.renderHints([]hint{{"y", verb}, {"n", "keep"}, {"esc", "cancel"}})
	case ModeBookmarks:
		return m.renderHints([]hint{{"1-9", "jump"}, {"enter", "go"}, {"d", "remove"}, {"esc", "close"}})
	case ModePlugins:
		return m.renderHints([]hint{{"enter", "run"}, {"tab", "switch between command and plugins"}, {"esc", "close"}})
	case ModeFind:
		return m.renderHints(m.findHints())
	case ModeHelp:
		if m.maxHelpScroll() > 0 {
			return m.renderHints([]hint{{"esc", "close"}, {"j/k", "scroll"}, {"any other key", "does what it says"}})
		}
		return m.renderHints([]hint{{"esc", "close"}, {"any other key", "does what it says"}})
	}
	if len(m.tabs[m.activeTabIdx].Selected) > 0 {
		return m.renderHints([]hint{{"space", "toggle"}, {"*", "invert"}, {"u", "clear"}, {"c", "copy"}, {"x", "cut"},
			{"d", "delete"}, {"e", "edit"}, {"o", "open"}, {"!", "shell"}, {"?", "all keys"}})
	}
	return m.renderHints([]hint{{"enter", "open"}, {"space", "select"}, {"c", "copy"}, {"x", "cut"}, {"v", "paste"},
		{"r", "rename"}, {"n", "new"}, {"d", "delete"}, {"/", "search"}, {"?", "all keys"}})
}

// renderSearchBar renders the search query being typed
func (m Model) renderSearchBar() string {
	t := m.theme
	g := currentGlyphs()
	tab := m.tabs[m.activeTabIdx]

	right := fmt.Sprintf("%s/%s next match   enter keep   esc cancel ", g.up, g.down)
	room := m.width - 4
	if utils.Width(right)+20 > room {
		right = ""
	}
	room -= utils.Width(right)

	// Keep the end of a long query (where the user is typing) visible
	query := utils.TruncateLeft(tab.SearchQuery, max(room, 1))
	left := " " + m.fg(t.Title).Bold(true).Render("/") + " " + m.fg(t.Text).Render(query) + lipgloss.NewStyle().Reverse(true).Render(" ")
	gap := max(m.width-utils.Width(left)-utils.Width(right), 0)
	return utils.Cells(left+strings.Repeat(" ", gap)+m.fg(t.Faint).Render(right), 0, m.width)
}

// renderPromptBar renders the prompt used to create files and directories
func (m Model) renderPromptBar() string {
	t := m.theme
	label := " " + m.fg(t.Title).Bold(true).Render(m.prompt.label) + " "

	errText := ""
	if m.prompt.err != "" {
		errText = "  " + m.fg(t.Danger).Render(utils.Truncate(m.prompt.err, m.width/2))
	}

	// The input gets whatever room the label and error leave
	room := max(m.width-utils.Width(label)-utils.Width(errText)-1, 1)
	input := m.prompt.input.View(room, m.fg(t.Text), lipgloss.NewStyle().Reverse(true))
	return utils.Cells(label+input+errText, 0, m.width)
}

// dim redraws lines without their colors, in the faint color, so a dialog
// stands out over them
func (m Model) dim(lines []string) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = m.fg(m.theme.Faint).Render(utils.Cells(ansi.Strip(line), 0, m.width))
	}
	return out
}

// place draws box over lines with its top-left corner at column x, row y
func (m Model) place(lines, box []string, x, y int) []string {
	for i, row := range box {
		if y+i < 0 || y+i >= len(lines) {
			continue
		}
		w := utils.Width(row)
		plain := ansi.Strip(lines[y+i])
		faint := m.fg(m.theme.Faint)
		lines[y+i] = faint.Render(utils.Cells(plain, 0, x)) + row + faint.Render(utils.Cells(plain, x+w, m.width))
	}
	return lines
}

// withDialog dims the panes and centers box over them; the status bar and
// hints stay as they are
func (m Model) withDialog(lines, box []string) []string {
	body := len(lines) - 2
	if body < 1 || len(box) == 0 {
		return lines
	}
	out := append(m.dim(lines[:body]), lines[body:]...)
	if len(box) > body {
		box = box[:body]
	}
	x := max((m.width-utils.Width(box[0]))/2, 0)
	y := max((body-len(box))/2, 0)
	return m.place(out, box, x, y)
}

// dialog builds a bordered box: a colored title bar, the body, and a footer
func (m Model) dialog(title string, color lipgloss.Color, body []string, width int) []string {
	g := currentGlyphs()
	width = max(min(width, m.width), 8)
	inner := width - 2
	border := m.fg(color)
	side := border.Render(g.vline)
	row := func(s string) string { return side + utils.Fit(s, inner) + side }

	box := []string{border.Render(g.tl + strings.Repeat(g.hline, inner) + g.tr)}
	titleBar := lipgloss.NewStyle().Background(color).Foreground(m.theme.CursorFg).Bold(true)
	box = append(box, side+titleBar.Render(utils.Fit(" "+title, inner))+side, row(""))
	for _, line := range body {
		box = append(box, row(line))
	}
	box = append(box, row(""), border.Render(g.bl+strings.Repeat(g.hline, inner)+g.br))
	return box
}

// confirmBox builds the delete and overwrite confirmation
func (m Model) confirmBox() []string {
	t := m.theme
	title, message := "Confirm", ""
	switch m.confirmAction {
	case "delete":
		title = "Confirm delete"
		message = m.deleteMessage()
	case "paste":
		title = "Confirm overwrite"
		message = m.pasteMessage()
	}

	var body []string
	for _, line := range strings.Split(message, "\n") {
		body = append(body, " "+m.fg(t.Text).Render(line))
	}
	if m.confirmAction == "delete" {
		body = append(body, "", " "+m.fg(t.Muted).Render("This is permanent. There is no undo."))
	}
	return m.dialog(title, t.Danger, body, 60)
}

// renderConfirmDialog renders the confirmation dialog on its own
func (m Model) renderConfirmDialog() string {
	return strings.Join(m.confirmBox(), "\n")
}

// pickerRow renders one row of a dialog list, highlighted if chosen
func (m Model) pickerRow(chosen bool, width int, cols ...string) string {
	t := m.theme
	text := utils.Fit(" "+strings.Join(cols, " "), width)
	if chosen {
		return lipgloss.NewStyle().Background(t.CursorBg).Foreground(t.CursorFg).Bold(true).Render(text)
	}
	return m.fg(t.Text).Render(text)
}

// bookmarksBox builds the bookmark list
func (m Model) bookmarksBox() []string {
	t := m.theme
	width := min(72, m.width)
	inner := width - 2

	var body []string
	if m.bookmarks.Len() == 0 {
		body = append(body, " "+m.fg(t.Text).Render("No bookmarks yet"), "",
			" "+m.fg(t.Muted).Render("Press B to bookmark the current directory"))
	}
	for i := 0; i < m.bookmarks.Len(); i++ {
		bm := m.bookmarks.Get(i)
		num := " "
		if i < 9 {
			num = fmt.Sprintf("%d", i+1)
		}
		// Keep the end of long paths visible
		path := utils.TruncateLeft(strings.Join(pathSegments(bm.Path), string(filepath.Separator)), max(inner-22, 8))
		body = append(body, m.pickerRow(i == m.bookmarkCursor, inner, num, utils.Fit(bm.Name, 16), path))
	}
	return m.dialog("Bookmarks", t.Accent, body, width)
}

// runBox builds the Run palette: a shell command line above the plugins
func (m Model) runBox() []string {
	t := m.theme
	g := currentGlyphs()
	width := min(84, m.width)
	inner := width - 2

	prompt := " " + m.fg(t.Title).Bold(true).Render("!") + " "
	var body []string
	switch {
	case m.runTyping:
		cursor := lipgloss.NewStyle().Reverse(true)
		body = append(body, prompt+m.runInput.View(max(inner-4, 1), m.fg(t.Text), cursor))
	case m.runInput.Value() != "":
		body = append(body, prompt+m.fg(t.Muted).Render(utils.Truncate(m.runInput.Value(), inner-4)))
	default:
		body = append(body, prompt+m.fg(t.Faint).Render(utils.Truncate("press tab to type a shell command", inner-4)))
	}
	body = append(body, m.fg(t.Border).Render(strings.Repeat(g.hline, inner)))

	if len(m.plugins) == 0 {
		body = append(body, " "+m.fg(t.Text).Render("No plugins yet"), "",
			" "+m.fg(t.Muted).Render("Add commands under plugins: in ~/.config/sushi/config.yaml,"),
			" "+m.fg(t.Muted).Render("or executable scripts to ~/.config/sushi/plugins/"))
	}
	descW := max(inner-10-18-12-4, 0)
	for i, p := range m.plugins {
		body = append(body, m.pickerRow(!m.runTyping && i == m.pluginCursor, inner,
			utils.Fit(p.Key, 10), utils.Fit(p.Name, 18), utils.Fit(p.Description, descW), utils.FitRight(p.Mode, 10)))
	}

	target := describe(m.targets())
	if len(m.targets()) == 0 {
		target = "this directory"
	}
	body = append(body, "", " "+m.fg(t.Muted).Render(utils.Truncate("Runs on "+target, inner-2)))
	return m.dialog("Run", t.Accent, body, width)
}

// helpGroups lists the keyboard shortcuts shown in the key panel
var helpGroups = []struct {
	title string
	keys  []hint
}{
	{"Move", []hint{{"j k", "down, up"}, {"h l", "parent, open"}, {"g G", "first, last"}, {"ctrl+u d", "page up, down"}, {"J K", "scroll preview"}}},
	{"Files", []hint{{"enter", "open"}, {"e o", "edit, default app"}, {"r", "rename"}, {"n N", "new file, folder"}, {"d", "delete"}}},
	{"Select", []hint{{"space", "toggle"}, {"*", "invert"}, {"u", "clear"}, {"c x v", "copy, cut, paste"}}},
	{"View", []hint{{"/", "search"}, {"p", "preview"}, {".", "hidden files"}, {"?", "this panel"}}},
	{"Find", []hint{{"f", "find by name"}, {"F", "find in files"}}},
	{"Tabs", []hint{{"t T", "new here, home"}, {"tab", "next"}, {"shift+tab", "previous"}, {"ctrl+w", "close"}}},
	{"Go", []hint{{"b B", "bookmarks, add"}, {"1-9", "jump to bookmark"}, {"P", "plugins"}, {"!", "shell command"}, {"q", "quit"}}},
}

const (
	helpKeyW = 10
	helpColW = 30
)

// helpLines lays the key groups out in as many columns as fit the width
func (m Model) helpLines() []string {
	t := m.theme
	cols := max(min(m.width/helpColW, len(helpGroups)), 1)
	colW := m.width / cols

	var lines []string
	for start := 0; start < len(helpGroups); start += cols {
		row := helpGroups[start:min(start+cols, len(helpGroups))]
		height := 0
		for _, group := range row {
			height = max(height, len(group.keys)+1)
		}
		if start > 0 {
			// Blank, not empty: the panel covers what is behind it
			lines = append(lines, strings.Repeat(" ", m.width))
		}
		for i := 0; i < height; i++ {
			var b strings.Builder
			for _, group := range row {
				cell := ""
				switch {
				case i == 0:
					cell = " " + m.fg(t.Faint).Render(group.title)
				case i <= len(group.keys):
					k := group.keys[i-1]
					cell = " " + m.fg(t.Highlight).Bold(true).Render(utils.Fit(k.key, helpKeyW)) +
						m.fg(t.Text).Render(utils.Truncate(k.label, colW-helpKeyW-2))
				}
				b.WriteString(utils.Fit(cell, colW))
			}
			lines = append(lines, utils.Fit(b.String(), m.width))
		}
	}
	return lines
}

// helpRows returns how many rows of the key panel fit above the status bar
func (m Model) helpRows() int {
	return max(m.height-chromeRows, 1)
}

// maxHelpScroll returns how far the key panel can scroll
func (m Model) maxHelpScroll() int {
	return max(len(m.helpLines())-m.helpRows(), 0)
}

// renderHelpView renders the key panel on its own
func (m Model) renderHelpView() string {
	g := currentGlyphs()
	all := m.helpLines()
	rows := min(len(all), m.helpRows())
	offset := min(m.helpScroll, len(all)-rows)

	rule := strings.Repeat(g.hline, m.width)
	if len(all) > rows {
		label := fmt.Sprintf(" j/k to scroll, %d-%d of %d ", offset+1, offset+rows, len(all))
		rule = utils.Cells(strings.Repeat(g.hline, 2)+label+rule, 0, m.width)
	}
	lines := []string{m.fg(m.theme.Accent).Render(rule)}
	return strings.Join(append(lines, all[offset:offset+rows]...), "\n")
}

// withHelp docks the key panel at the bottom, over the dimmed panes
func (m Model) withHelp(lines []string) []string {
	body := len(lines) - 2
	if body < 1 {
		return lines
	}
	panel := strings.Split(m.renderHelpView(), "\n")
	if len(panel) > body {
		panel = panel[:body]
	}
	out := append(m.dim(lines[:body]), lines[body:]...)
	return m.place(out, panel, 0, body-len(panel))
}
