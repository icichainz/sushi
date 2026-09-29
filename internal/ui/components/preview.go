package components

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	".pdf":  "PDF Document",
	".zip":  "ZIP Archive",
	".tar":  "TAR Archive",
	".gz":   "GZIP Archive",
	".mp3":  "MP3 Audio",
	".mp4":  "MP4 Video",
	".exe":  "Executable",
	".dll":  "Dynamic Library",
	".so":   "Shared Object",
}

// PreviewContent represents the content to preview
type PreviewContent struct {
	Path     string
	Content  string
	FileInfo fs.FileInfo
	IsText   bool
	Error    error
}

// PreviewConfig holds preview configuration
type PreviewConfig struct {
	MaxLines        int
	SyntaxHighlight bool
	SyntaxTheme     string
	MaxPreviewSize  int64
}

// DefaultPreviewConfig returns default preview settings
func DefaultPreviewConfig() PreviewConfig {
	return PreviewConfig{
		MaxLines:        100,
		SyntaxHighlight: true,
		SyntaxTheme:     "monokai",        // Options: monokai, dracula, github, nord, etc.
		MaxPreviewSize:  10 * 1024 * 1024, // 10MB
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

	// Handle directories
	if file.IsDir {
		preview.Content = loadDirectoryPreview(file.Path)
		preview.IsText = true
		return preview
	}

	// Check if file is too large
	if file.Size > config.MaxPreviewSize {
		preview.Content = fmt.Sprintf("File too large to preview\nSize: %s", utils.HumanizeSize(file.Size))
		preview.IsText = false
		return preview
	}

	// First, check if file is binary by reading only the first 512 bytes
	isBinaryFile, err := fs.IsBinary(file.Path)
	if err != nil {
		preview.Error = err
		preview.Content = fmt.Sprintf("Error reading file: %v", err)
		return preview
	}

	if isBinaryFile {
		preview.IsText = false
		preview.Content = formatBinaryPreview(file)
		return preview
	}

	// It's a text file, read full content
	content, err := os.ReadFile(file.Path)
	if err != nil {
		preview.Error = err
		preview.Content = fmt.Sprintf("Error reading file: %v", err)
		return preview
	}

	preview.IsText = true

	lines := strings.Split(string(content), "\n")

	// Limit number of lines
	totalLines := len(lines)
	if len(lines) > config.MaxLines {
		lines = lines[:config.MaxLines]
	}

	text := strings.Join(lines, "\n")

	// Apply syntax highlighting if enabled
	if config.SyntaxHighlight {
		highlighted, err := highlightCode(file.Path, text, config.SyntaxTheme)
		if err == nil {
			text = highlighted
		}
		// If highlighting fails, fall back to plain text
	}

	if totalLines > config.MaxLines {
		text += fmt.Sprintf("\n\n... (%d more lines)", totalLines-config.MaxLines)
	}

	preview.Content = text
	return preview
}

// highlightCode applies syntax highlighting to code
func highlightCode(filepath string, content string, themeName string) (string, error) {
	// Determine lexer from filename
	lexer := lexers.Match(filepath)
	if lexer == nil {
		lexer = lexers.Analyse(content)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}

	// Coalesce to prevent fragmented tokens (fewer escape codes)
	lexer = chroma.Coalesce(lexer)

	// Get style
	style := styles.Get(themeName)
	if style == nil {
		style = styles.Fallback
	}

	// Use terminal256 formatter for better color support
	formatter := formatters.Get("terminal256")
	if formatter == nil {
		formatter = formatters.Fallback
	}

	// Tokenize and format
	iterator, err := lexer.Tokenise(nil, content)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	err = formatter.Format(&buf, style, iterator)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

// loadDirectoryPreview creates a preview for directories
func loadDirectoryPreview(path string) string {
	// Open directory for streaming read (avoids loading entire listing for huge dirs)
	dir, err := os.Open(path)
	if err != nil {
		return fmt.Sprintf("Error reading directory: %v", err)
	}
	defer dir.Close()

	// Read only what we need (max 51 entries to check for overflow)
	const maxItems = 50
	entries, err := dir.ReadDir(maxItems + 1)
	// EOF is expected when directory has fewer entries than requested - not an error
	if err != nil && err != io.EOF && len(entries) == 0 {
		return fmt.Sprintf("Error reading directory: %v", err)
	}

	if len(entries) == 0 {
		return "Empty directory"
	}

	hasMore := len(entries) > maxItems
	if hasMore {
		entries = entries[:maxItems]
	}

	// Pre-allocate lines slice: header + blank + entries + potential "more" line
	lines := make([]string, 0, len(entries)+3)

	dirIcon := ui.GetDirIcon()
	fileIcon := ui.GetDefaultFileIcon()

	if hasMore {
		lines = append(lines, fmt.Sprintf("%s Directory contents (50+ items)", dirIcon))
	} else {
		lines = append(lines, fmt.Sprintf("%s Directory contents (%d items)", dirIcon, len(entries)))
	}
	lines = append(lines, "")

	for _, entry := range entries {
		icon := fileIcon
		if entry.IsDir() {
			icon = dirIcon
		}
		lines = append(lines, fmt.Sprintf("  %s %s", icon, entry.Name()))
	}

	if hasMore {
		lines = append(lines, "... and more items")
	}

	return strings.Join(lines, "\n")
}

// formatBinaryPreview creates info display for binary files
func formatBinaryPreview(file fs.FileInfo) string {
	lines := []string{
		fmt.Sprintf("%s Binary File", ui.GetBinaryIcon()),
		"",
		fmt.Sprintf("Type:        %s", getFileType(strings.ToLower(filepath.Ext(file.Name)))),
		fmt.Sprintf("Size:        %s", utils.HumanizeSize(file.Size)),
		fmt.Sprintf("Modified:    %s", file.ModTime.Format("2006-01-02 15:04:05")),
		fmt.Sprintf("Permissions: %s", file.Perms),
		"",
		"Cannot preview binary content",
	}
	return strings.Join(lines, "\n")
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

// RenderPreview renders the preview into a block of exactly width x height cells
func RenderPreview(preview PreviewContent, width, height int, style, errorStyle lipgloss.Style) string {
	if preview.Error != nil {
		style = errorStyle
	}

	// Clip before padding: lipgloss would otherwise wrap long lines, pushing
	// the pane (and the rest of the screen) past the terminal height
	const padding = 1
	content := lipgloss.NewStyle().
		MaxWidth(max(width-2*padding, 1)).
		MaxHeight(max(height-2*padding, 1)).
		Render(preview.Content)

	return style.
		Width(width).
		Height(height).
		MaxHeight(height).
		Padding(padding).
		Render(content)
}
