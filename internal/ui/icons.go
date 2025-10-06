package ui


import (
	"path/filepath"
	"strings"

	"github.com/icichainz/sushi/internal/fs"
)

// GetFileIcon returns an icon for a file based on its type
func GetFileIcon(file fs.FileInfo) string {
	if file.IsDir {
		return "📁"
	}

	ext := strings.ToLower(filepath.Ext(file.Name))
	
	// Map of extensions to icons
	iconMap := map[string]string{
		// Programming languages
		".go":   "🐹",
		".py":   "🐍",
		".js":   "📜",
		".ts":   "📘",
		".rs":   "🦀",
		".java": "☕",
		".c":    "©️",
		".cpp":  "©️",
		".h":    "📋",
		
		// Web
		".html": "🌐",
		".css":  "🎨",
		".scss": "🎨",
		".json": "📊",
		".xml":  "📰",
		
		// Documents
		".md":   "📝",
		".txt":  "📄",
		".pdf":  "📕",
		".doc":  "📘",
		".docx": "📘",
		
		// Images
		".png":  "🖼️",
		".jpg":  "🖼️",
		".jpeg": "🖼️",
		".gif":  "🎞️",
		".svg":  "🎨",
		
		// Archives
		".zip":  "📦",
		".tar":  "📦",
		".gz":   "📦",
		".rar":  "📦",
		
		// Config
		".yaml": "⚙️",
		".yml":  "⚙️",
		".toml": "⚙️",
		".ini":  "⚙️",
		
		// Data
		".sql":  "🗄️",
		".db":   "🗄️",
		".csv":  "📊",
		
		// Executables
		".exe":  "⚙️",
		".sh":   "🔧",
		".bat":  "🔧",
	}

	if icon, ok := iconMap[ext]; ok {
		return icon
	}

	// Default icon
	return "📄"
}