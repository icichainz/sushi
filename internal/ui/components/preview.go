package components

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/ui"
	"github.com/icichainz/sushi/internal/utils"
)

// fileTypeMap is a package-level map for binary file types (avoids recreation on each call)
var fileTypeMap = map[string]string{
	".jpg":  "JPEG Image",
	".jpeg": "JPEG Image",
	".png":  "PNG Image",
	".gif":  "GIF Image",
	".webp": "WebP Image",
	".bmp":  "BMP Image",
	".pdf":  "PDF Document",
	".zip":  "ZIP Archive",
	".tar":  "TAR Archive",
	".gz":   "GZIP Archive",
	".tgz":  "TAR.GZ Archive",
	".mp3":  "MP3 Audio",
	".mp4":  "MP4 Video",
	".exe":  "Executable",
	".dll":  "Dynamic Library",
	".so":   "Shared Object",
}

func init() {
	// Syntax styles that match the default and light themes
	styles.Register(chroma.MustNewStyle("sushi", chroma.StyleEntries{
		chroma.Text:           "#ebe7dc",
		chroma.Comment:        "italic #8f8c84",
		chroma.Keyword:        "#ff9478",
		chroma.LiteralString:  "#b5d46b",
		chroma.LiteralNumber:  "#f2c97d",
		chroma.NameFunction:   "#8ab4f8",
		chroma.NameClass:      "#8ab4f8",
		chroma.NameTag:        "#ff9478",
		chroma.NameAttribute:  "#8ab4f8",
		chroma.NameBuiltin:    "#8ab4f8",
		chroma.GenericHeading: "bold #ff9478",
	}))
	styles.Register(chroma.MustNewStyle("sushi-light", chroma.StyleEntries{
		chroma.Text:           "#1f2024",
		chroma.Comment:        "italic #6b6862",
		chroma.Keyword:        "#b8432a",
		chroma.LiteralString:  "#4d6b12",
		chroma.LiteralNumber:  "#8a5a00",
		chroma.NameFunction:   "#1f5fae",
		chroma.NameClass:      "#1f5fae",
		chroma.NameTag:        "#b8432a",
		chroma.NameAttribute:  "#1f5fae",
		chroma.NameBuiltin:    "#1f5fae",
		chroma.GenericHeading: "bold #b8432a",
	}))
}

// Entry is one item of a previewed directory or archive
type Entry struct {
	Name  string
	IsDir bool
	Size  int64 // Archive entries only
}

// PreviewContent represents the content to preview
type PreviewContent struct {
	Path       string
	FileInfo   fs.FileInfo
	Kind       string   // "Go", "Markdown", "Directory", "PNG Image", ...
	Details    []string // More for the heading, e.g. "1920x1080" or "12 pages"
	LinkTarget string   // Where a symbolic link points
	IsText     bool     // A text file, shown with line numbers
	Lines      []string // Text lines (highlighted), or the message for other kinds
	Total      int      // Lines in the file; more than len(Lines) if it was cut
	Entries    []Entry  // Directory or archive contents
	More       bool     // The directory or archive has more entries than listed
	Archive    bool     // Entries are an archive's contents
	Count      int      // Entries in the archive
	Partial    bool     // The archive wasn't read to the end, so Count is a minimum
	Image      *ImagePreview
	Pending    bool   // Slow work was skipped (PreviewConfig.Quick); load again to finish
	Content    string // Lines as plain text, for searching and tests
	Error      error
}

// PreviewConfig holds preview configuration
type PreviewConfig struct {
	MaxLines        int
	SyntaxHighlight bool
	SyntaxTheme     string
	MaxPreviewSize  int64
	Images          ImageColors // How images are drawn, if at all
	Quick           bool        // Skip slow work (images, archives, PDFs) and mark the preview Pending
}

// DefaultPreviewConfig returns default preview settings
func DefaultPreviewConfig() PreviewConfig {
	return PreviewConfig{
		MaxLines:        2000,
		SyntaxHighlight: true,
		SyntaxTheme:     "sushi",
		MaxPreviewSize:  10 * 1024 * 1024, // 10MB
		Images:          DetectImageColors(),
	}
}

// LoadPreview loads the preview content for a file
func LoadPreview(file fs.FileInfo, maxLines int) PreviewContent {
	config := DefaultPreviewConfig()
	config.MaxLines = maxLines
	return LoadPreviewWithConfig(file, config)
}

// LoadPreviewWithConfig loads preview with custom configuration
func LoadPreviewWithConfig(file fs.FileInfo, config PreviewConfig) PreviewContent {
	preview := PreviewContent{
		Path:     file.Path,
		FileInfo: file,
	}
	message := func(kind string, lines ...string) PreviewContent {
		preview.Kind = kind
		preview.Lines = lines
		preview.Content = strings.Join(lines, "\n")
		return preview
	}

	// The heading shows where a link points, for directories too
	if file.IsSymlink {
		if target, err := os.Readlink(file.Path); err == nil {
			preview.LinkTarget = utils.Printable(target)
		}
	}

	// Handle directories
	if file.IsDir {
		entries, more, err := loadDirectoryPreview(file.Path)
		if err != nil {
			preview.Error = err
			return message("Directory", fmt.Sprintf("Error reading directory: %v", err))
		}
		preview.Kind = "Directory"
		preview.Entries = entries
		preview.More = more
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name
		}
		preview.Content = strings.Join(names, "\n")
		return preview
	}

	// Images, archives and PDFs read only what they need, so the size
	// limit for text doesn't apply
	name := strings.ToLower(file.Name)
	switch {
	case imageExts[filepath.Ext(name)]:
		return loadImagePreview(preview, config)
	case archiveFormat(name) != "":
		return loadArchivePreview(preview, config)
	case filepath.Ext(name) == ".pdf":
		return loadPDFPreview(preview, config)
	}

	// Check if file is too large
	if file.Size > config.MaxPreviewSize {
		return message("Large file", "File too large to preview")
	}

	// First, check if file is binary by reading only the first 512 bytes
	isBinaryFile, err := fs.IsBinary(file.Path)
	if err != nil {
		preview.Error = err
		return message("File", fmt.Sprintf("Error reading file: %v", err))
	}

	if isBinaryFile {
		return message(getFileType(strings.ToLower(filepath.Ext(file.Name))),
			"Modified  "+file.ModTime.Format("2006-01-02 15:04:05"),
			"",
			"Cannot preview binary content")
	}

	// It's a text file: read the lines shown, and only count the rest
	lines, total, err := readLines(file.Path, config.MaxLines)
	if err != nil {
		preview.Error = err
		return message("File", fmt.Sprintf("Error reading file: %v", err))
	}
	preview.IsText = true
	preview.Total = total
	preview.Content = strings.Join(lines, "\n")
	preview.Kind = "Text"
	preview.Lines = lines

	// Apply syntax highlighting if enabled; on failure the text stays plain
	if config.SyntaxHighlight && len(lines) > 0 {
		if highlighted, kind, err := highlightLines(file.Path, preview.Content, config.SyntaxTheme); err == nil && len(highlighted) == len(lines) {
			preview.Lines = highlighted
			preview.Kind = kind
		}
	}
	return preview
}

// highlightLines highlights content and returns it line by line, each line
// carrying its own color codes, along with the language name
func highlightLines(path, content, themeName string) ([]string, string, error) {
	// Determine lexer from filename
	lexer := lexers.Match(path)
	if lexer == nil {
		lexer = lexers.Analyse(content)
	}
	kind := "Text"
	if lexer == nil {
		lexer = lexers.Fallback
	} else if name := lexer.Config().Name; name != "" && name != "plaintext" && name != "fallback" {
		kind = name
	}

	// Coalesce to prevent fragmented tokens (fewer escape codes)
	lexer = chroma.Coalesce(lexer)

	style := styles.Get(themeName)
	if style == nil {
		style = styles.Fallback
	}
	formatter := formatters.Get("terminal256")
	if formatter == nil {
		formatter = formatters.Fallback
	}

	iterator, err := lexer.Tokenise(nil, content)
	if err != nil {
		return nil, "", err
	}

	// Format each line on its own, so a multi-line comment or string keeps
	// its color on every line when lines are clipped and scrolled
	tokenLines := chroma.SplitTokensIntoLines(iterator.Tokens())
	lines := make([]string, 0, len(tokenLines))
	for _, tokens := range tokenLines {
		if n := len(tokens); n > 0 {
			tokens[n-1].Value = strings.TrimSuffix(tokens[n-1].Value, "\n")
		}
		var buf bytes.Buffer
		if err := formatter.Format(&buf, style, chroma.Literator(tokens...)); err != nil {
			return nil, "", err
		}
		lines = append(lines, buf.String())
	}
	return lines, kind, nil
}

// loadDirectoryPreview lists up to 200 entries of a directory, directories first
func loadDirectoryPreview(path string) ([]Entry, bool, error) {
	// Open directory for streaming read (avoids loading entire listing for huge dirs)
	dir, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer dir.Close()

	const maxItems = 200
	entries, err := dir.ReadDir(maxItems + 1)
	// EOF is expected when directory has fewer entries than requested - not an error
	if err != nil && err != io.EOF && len(entries) == 0 {
		return nil, false, err
	}

	more := len(entries) > maxItems
	if more {
		entries = entries[:maxItems]
	}

	var dirs, files []Entry
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, Entry{Name: entry.Name(), IsDir: true})
		} else {
			files = append(files, Entry{Name: entry.Name()})
		}
	}
	return append(dirs, files...), more, nil
}

// getFileType returns a human-readable file type
func getFileType(ext string) string {
	if t, ok := fileTypeMap[ext]; ok {
		return t
	}
	return "Binary file"
}

// HasSyntaxTheme reports whether name is a known Chroma style
func HasSyntaxTheme(name string) bool {
	_, ok := styles.Registry[name]
	return ok
}

// PreviewStyles are the styles the preview pane is drawn with
type PreviewStyles struct {
	Title lipgloss.Style // File name in the heading
	Text  lipgloss.Style
	Faint lipgloss.Style // Heading details and line numbers
	Dir   lipgloss.Style // Directories in a directory listing
	Error lipgloss.Style
}

// listing reports whether the body lists entries, of a directory or archive
func (p PreviewContent) listing() bool {
	return (p.Kind == "Directory" || p.Archive) && p.Error == nil
}

// bodyLen returns how many rows the preview's body has in total
func (p PreviewContent) bodyLen() int {
	n := len(p.Lines)
	if p.listing() {
		n = len(p.Entries)
		if n == 0 || p.More {
			n++
		}
	}
	if p.IsText && (p.Total > len(p.Lines) || p.Total == 0) {
		n++
	}
	if p.Image != nil {
		n = 0 // Drawn to fit the pane, so it never scrolls
	}
	return n
}

// MaxScroll returns the furthest the preview can scroll in a pane of the
// given height, heading included
func (p PreviewContent) MaxScroll(height int) int {
	return max(p.bodyLen()-(height-1), 0)
}

// RenderPreview draws the preview as exactly height lines of exactly width
// cells: a heading, then the body starting at the scroll offset
func RenderPreview(p PreviewContent, width, height, scroll int, st PreviewStyles) []string {
	out := make([]string, 0, height)
	if width <= 0 || height <= 0 {
		return out
	}
	rows := height - 1
	scroll = max(min(scroll, p.MaxScroll(height)), 0)
	total := p.bodyLen()

	// Heading: name and details on the left, position on the right
	var details []string
	if p.LinkTarget != "" {
		arrow := "→"
		if ui.GetIconMode() == ui.IconModeASCII {
			arrow = "->"
		}
		details = append(details, arrow+" "+p.LinkTarget)
	}
	details = append(details, p.Kind)
	details = append(details, p.Details...)
	if !p.FileInfo.IsDir {
		details = append(details, utils.HumanizeSize(p.FileInfo.Size))
	}
	details = append(details, p.FileInfo.Perms.String())
	pos := ""
	switch {
	case p.IsText && p.Total > 0:
		pos = fmt.Sprintf("%d-%d of %d", scroll+1, min(scroll+rows, p.Total), p.Total)
	case p.Kind == "Directory" && len(p.Entries) > 0:
		pos = count(len(p.Entries), p.More, "item", "items")
	case p.Archive && p.Error == nil && p.Count > 0:
		pos = count(p.Count, p.Partial, "entry", "entries")
	}
	inner := width - 2
	posW := utils.Width(pos)
	if posW+12 > inner {
		pos, posW = "", 0
	}
	// Names, link targets and details come from outside, so they may hold
	// newlines or escape codes
	name := utils.Truncate(utils.Printable(p.FileInfo.Name), max(inner-posW-1, 0))
	rest := utils.Truncate(utils.Printable("  "+strings.Join(details, "  ")), max(inner-posW-1-utils.Width(name), 0))
	gap := max(inner-utils.Width(name)-utils.Width(rest)-posW, 0)
	out = append(out, utils.Fit(" "+st.Title.Render(name)+st.Faint.Render(rest)+strings.Repeat(" ", gap)+st.Faint.Render(pos), width))

	// Image rows are exactly inner cells wide already, and long enough
	// that measuring them again would be wasted work
	if p.Image != nil && inner > 0 {
		for _, row := range p.Image.Rows(inner, rows) {
			out = append(out, " "+row+" ")
		}
		return out
	}

	gutter := len(strconv.Itoa(max(p.Total, 1))) + 1
	for i := scroll; i < scroll+rows; i++ {
		var line string
		switch {
		case i >= total:
		case p.Error != nil:
			line = " " + st.Error.Render(utils.Truncate(utils.Printable(p.Lines[i]), inner))
		case p.listing():
			line = " " + renderEntry(p, i, inner, st)
		case p.IsText && i < len(p.Lines):
			num := st.Faint.Render(utils.FitRight(strconv.Itoa(i+1), gutter))
			line = " " + num + "  " + utils.Clip(p.Lines[i], max(inner-gutter-2, 0))
		case p.IsText && p.Total == 0:
			line = " " + st.Faint.Render(utils.Truncate("Empty file", inner))
		case p.IsText:
			line = " " + st.Faint.Render(utils.Truncate(count(p.Total-len(p.Lines), false, "more line", "more lines")+" not shown", inner))
		default:
			line = " " + st.Text.Render(utils.Truncate(utils.Printable(p.Lines[i]), inner))
		}
		out = append(out, utils.Fit(line, width))
	}
	return out
}

// count writes n things, as in "1 item" or "3 items", or "3+ items" when
// there are more than n
func count(n int, more bool, one, many string) string {
	switch {
	case more:
		return fmt.Sprintf("%d+ %s", n, many)
	case n == 1:
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// renderEntry draws row i of a directory or archive preview
func renderEntry(p PreviewContent, i, width int, st PreviewStyles) string {
	if len(p.Entries) == 0 {
		if p.Archive {
			return st.Faint.Render(utils.Truncate("Empty archive", width))
		}
		return st.Faint.Render(utils.Truncate("Empty directory", width))
	}
	if i >= len(p.Entries) {
		if p.Archive && !p.Partial {
			return st.Faint.Render(utils.Truncate(fmt.Sprintf("and %d more", p.Count-len(p.Entries)), width))
		}
		return st.Faint.Render(utils.Truncate("and more", width))
	}
	e := p.Entries[i]
	name := utils.Printable(e.Name)
	if e.IsDir {
		return st.Dir.Render(utils.Truncate(ui.GetDirIcon()+"  "+name, width))
	}
	icon := ui.GetFileIcon(fs.FileInfo{Name: e.Name})
	// Archive entries have their size on the right, when there is room
	if p.Archive && width >= 30 {
		const sizeW = 10
		return st.Text.Render(utils.Fit(icon+"  "+name, width-sizeW)) +
			st.Faint.Render(utils.FitRight(utils.HumanizeSize(e.Size), sizeW))
	}
	return st.Text.Render(utils.Truncate(icon+"  "+name, width))
}
