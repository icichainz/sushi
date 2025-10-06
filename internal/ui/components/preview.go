package components

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/icichainz/sushi/internal/fs"
	"github.com/icichainz/sushi/internal/utils"
)

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
	MaxLines          int
	SyntaxHighlight   bool
	SyntaxTheme       string
	MaxPreviewSize    int64
}

// DefaultPreviewConfig returns default preview settings
func DefaultPreviewConfig() PreviewConfig {
	return PreviewConfig{
		MaxLines:        100,
		SyntaxHighlight: true,
		SyntaxTheme:     "monokai", // Options: monokai, dracula, github, nord, etc.
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

	// Try to read as text
	content, err := os.ReadFile(file.Path)
	if err != nil {
		preview.Error = err
		preview.Content = fmt.Sprintf("Error reading file: %v", err)
		return preview
	}

	// Check if content is binary
	if isBinary(content) {
		preview.IsText = false
		preview.Content = formatBinaryPreview(file)
		return preview
	}

	// It's a text file
	preview.IsText = true
	
	// Apply syntax highlighting if enabled
	if config.SyntaxHighlight {
		highlighted, err := highlightCode(file.Path, string(content), config.SyntaxTheme)
		if err == nil {
			content = []byte(highlighted)
		}
		// If highlighting fails, fall back to plain text
	}

	lines := strings.Split(string(content), "\n")
	
	// Limit number of lines
	if len(lines) > config.MaxLines {
		totalLines := len(lines)
		lines = lines[:config.MaxLines]
		lines = append(lines, "", fmt.Sprintf("... (%d more lines)", totalLines-config.MaxLines))
	}

	preview.Content = strings.Join(lines, "\n")
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

	// Coalesce to prevent fragmented tokens
	//lexer = lexers.Coalesce(lexer)

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
	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Sprintf("Error reading directory: %v", err)
	}

	if len(entries) == 0 {
		return "Empty directory"
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("📁 Directory contents (%d items)", len(entries)))
	lines = append(lines, "")

	// Count directories and files
	var dirs, files int
	for _, entry := range entries {
		if entry.IsDir() {
			dirs++
		} else {
			files++
		}
	}

	lines = append(lines, fmt.Sprintf("📊 %d directories, %d files", dirs, files))
	lines = append(lines, strings.Repeat("─", 40))
	lines = append(lines, "")

	// Show up to 50 items
	maxItems := 50
	for i, entry := range entries {
		if i >= maxItems {
			lines = append(lines, fmt.Sprintf("... and %d more items", len(entries)-maxItems))
			break
		}

		icon := "📄"
		if entry.IsDir() {
			icon = "📁"
		}
		lines = append(lines, fmt.Sprintf("  %s %s", icon, entry.Name()))
	}

	return strings.Join(lines, "\n")
}

// formatBinaryPreview creates info display for binary files
func formatBinaryPreview(file fs.FileInfo) string {
	ext := strings.ToLower(filepath.Ext(file.Name))
	
	var lines []string
	lines = append(lines, "📦 Binary File")
	lines = append(lines, strings.Repeat("─", 40))
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("📝 Name: %s", file.Name))
	lines = append(lines, fmt.Sprintf("📏 Size: %s", utils.HumanizeSize(file.Size)))
	lines = append(lines, fmt.Sprintf("🏷️  Type: %s", getFileType(ext)))
	lines = append(lines, fmt.Sprintf("📅 Modified: %s", file.ModTime.Format("2006-01-02 15:04:05")))
	lines = append(lines, fmt.Sprintf("🔒 Permissions: %s", file.Perms.String()))
	lines = append(lines, "")
	lines = append(lines, "Cannot preview binary content")

	return strings.Join(lines, "\n")
}

// isBinary checks if content is binary
func isBinary(content []byte) bool {
	// Check first 512 bytes for null bytes
	checkSize := 512
	if len(content) < checkSize {
		checkSize = len(content)
	}

	for i := 0; i < checkSize; i++ {
		if content[i] == 0 {
			return true
		}
	}
	return false
}

// getFileType returns a human-readable file type
func getFileType(ext string) string {
	types := map[string]string{
		// Images
		".jpg":  "JPEG Image",
		".jpeg": "JPEG Image",
		".png":  "PNG Image",
		".gif":  "GIF Image",
		".bmp":  "Bitmap Image",
		".svg":  "SVG Image",
		".webp": "WebP Image",
		
		// Documents
		".pdf":  "PDF Document",
		".doc":  "Word Document",
		".docx": "Word Document",
		".xls":  "Excel Spreadsheet",
		".xlsx": "Excel Spreadsheet",
		".ppt":  "PowerPoint",
		".pptx": "PowerPoint",
		
		// Archives
		".zip":  "ZIP Archive",
		".tar":  "TAR Archive",
		".gz":   "GZIP Archive",
		".bz2":  "BZIP2 Archive",
		".rar":  "RAR Archive",
		".7z":   "7-Zip Archive",
		
		// Media
		".mp3":  "MP3 Audio",
		".mp4":  "MP4 Video",
		".avi":  "AVI Video",
		".mkv":  "Matroska Video",
		".flac": "FLAC Audio",
		".wav":  "WAV Audio",
		
		// Executables
		".exe":  "Windows Executable",
		".dll":  "Dynamic Link Library",
		".so":   "Shared Object Library",
		".dylib": "Dynamic Library",
		".app":  "macOS Application",
		
		// Data
		".db":   "Database File",
		".sqlite": "SQLite Database",
	}

	if t, ok := types[ext]; ok {
		return t
	}
	return "Binary file"
}

// RenderPreview renders the preview pane with styling
func RenderPreview(preview PreviewContent, width, height int, styles lipgloss.Style) string {
	if preview.Error != nil {
		errorStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Padding(1)
		return errorStyle.Render(preview.Content)
	}

	// Create the preview content with padding
	contentStyle := styles.
		Width(width).
		Height(height).
		Padding(1)

	return contentStyle.Render(preview.Content)
}