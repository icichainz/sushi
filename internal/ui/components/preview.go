package components


import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

// LoadPreview loads the preview content for a file
func LoadPreview(file fs.FileInfo, maxLines int) PreviewContent {
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

	// Check if file is too large (> 10MB)
	if file.Size > 10*1024*1024 {
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
	lines := strings.Split(string(content), "\n")
	
	// Limit number of lines
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines = append(lines, "", fmt.Sprintf("... (%d more lines)", len(strings.Split(string(content), "\n"))-maxLines))
	}

	preview.Content = strings.Join(lines, "\n")
	return preview
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
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("Name: %s", file.Name))
	lines = append(lines, fmt.Sprintf("Size: %s", utils.HumanizeSize(file.Size)))
	lines = append(lines, fmt.Sprintf("Type: %s", getFileType(ext)))
	lines = append(lines, fmt.Sprintf("Modified: %s", file.ModTime.Format("2006-01-02 15:04:05")))
	lines = append(lines, fmt.Sprintf("Permissions: %s", file.Perms.String()))

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