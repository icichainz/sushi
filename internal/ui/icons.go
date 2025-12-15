package ui

import (
	"path/filepath"
	"strings"

	"github.com/icichainz/sushi/internal/fs"
)

// IconMode represents the icon display mode
type IconMode int

const (
	IconModeNerd      IconMode = iota // Nerd Font icons (default)
	IconModeBootstrap                 // Bootstrap Icons
	IconModeASCII                     // ASCII fallback
)

// currentIconMode holds the global icon mode setting
var currentIconMode IconMode = IconModeNerd

// SetIconMode sets the global icon display mode
func SetIconMode(mode IconMode) {
	currentIconMode = mode
}

// GetIconMode returns the current icon display mode
func GetIconMode() IconMode {
	return currentIconMode
}

// ASCII fallback icons
var asciiIconMap = map[string]string{
	// Programming languages
	".go": "[GO]", ".py": "[PY]", ".js": "[JS]", ".ts": "[TS]",
	".tsx": "[RX]", ".jsx": "[RX]", ".rs": "[RS]", ".java": "[JV]",
	".c": "[C]", ".cpp": "[C+]", ".cc": "[C+]", ".h": "[H]",
	".hpp": "[H+]", ".rb": "[RB]", ".php": "[PH]", ".swift": "[SW]",
	".kt": "[KT]", ".scala": "[SC]", ".lua": "[LU]", ".pl": "[PL]",
	".r": "[R]", ".ex": "[EX]", ".exs": "[EX]", ".erl": "[ER]",
	".hs": "[HS]", ".clj": "[CL]", ".vim": "[VI]", ".zig": "[ZG]",
	".dart": "[DT]", ".elm": "[EL]",

	// Web
	".html": "[HT]", ".htm": "[HT]", ".css": "[CS]", ".scss": "[SS]",
	".sass": "[SS]", ".less": "[LS]", ".vue": "[VU]", ".svelte": "[SV]",

	// Data/Config
	".json": "[JS]", ".xml": "[XM]", ".yaml": "[YM]", ".yml": "[YM]",
	".toml": "[TM]", ".ini": "[IN]", ".conf": "[CF]", ".config": "[CF]",
	".env": "[EN]",

	// Documents
	".md": "[MD]", ".mdx": "[MD]", ".txt": "[TX]", ".pdf": "[PD]",
	".doc": "[DC]", ".docx": "[DC]", ".xls": "[XL]", ".xlsx": "[XL]",
	".ppt": "[PP]", ".pptx": "[PP]", ".tex": "[TX]", ".rst": "[RS]",

	// Images
	".png": "[IM]", ".jpg": "[IM]", ".jpeg": "[IM]", ".gif": "[IM]",
	".bmp": "[IM]", ".ico": "[IM]", ".webp": "[IM]", ".svg": "[SV]",

	// Audio/Video
	".mp3": "[AU]", ".wav": "[AU]", ".flac": "[AU]", ".ogg": "[AU]",
	".aac": "[AU]", ".mp4": "[VD]", ".mkv": "[VD]", ".avi": "[VD]",
	".mov": "[VD]", ".webm": "[VD]",

	// Archives
	".zip": "[ZP]", ".tar": "[TR]", ".gz": "[GZ]", ".bz2": "[BZ]",
	".xz": "[XZ]", ".7z": "[7Z]", ".rar": "[RA]", ".deb": "[DB]",
	".rpm": "[RP]",

	// Data
	".sql": "[SQ]", ".db": "[DB]", ".csv": "[CV]",

	// Shell/Scripts
	".sh": "[SH]", ".bash": "[SH]", ".zsh": "[SH]", ".fish": "[SH]",
	".ps1": "[PS]", ".bat": "[BT]", ".cmd": "[CM]",

	// Executables/Binary
	".exe": "[EX]", ".dll": "[DL]", ".so": "[SO]", ".o": "[O]",
	".a": "[A]", ".bin": "[BN]",

	// Build/Package
	".lock": "[LK]", ".sum": "[SM]",

	// Git
	".gitignore": "[GI]", ".gitmodules": "[GM]", ".gitattributes": "[GA]",

	// Log files
	".log": "[LG]",
}

// ASCII fallback for special filenames
var asciiFilenameMap = map[string]string{
	"Dockerfile":          "[DK]",
	"docker-compose.yml":  "[DK]",
	"docker-compose.yaml": "[DK]",
	"Makefile":            "[MK]",
	"CMakeLists.txt":      "[MK]",
	"go.mod":              "[GO]",
	"go.sum":              "[GO]",
	"Cargo.toml":          "[RS]",
	"Cargo.lock":          "[RS]",
	"package.json":        "[NJ]",
	"package-lock.json":   "[NJ]",
	"yarn.lock":           "[YN]",
	"pnpm-lock.yaml":      "[NJ]",
	"LICENSE":             "[LC]",
	"LICENSE.md":          "[LC]",
	"LICENSE.txt":         "[LC]",
	"README":              "[RM]",
	"README.md":           "[RM]",
	"README.txt":          "[RM]",
	".gitignore":          "[GI]",
	".env":                "[EN]",
	".env.local":          "[EN]",
	".env.example":        "[EN]",
	".editorconfig":       "[ED]",
	".prettierrc":         "[PR]",
	".eslintrc":           "[ES]",
	".eslintrc.js":        "[ES]",
	".eslintrc.json":      "[ES]",
	"tsconfig.json":       "[TS]",
	"vite.config.js":      "[VT]",
	"vite.config.ts":      "[VT]",
	"webpack.config.js":   "[WP]",
	".dockerignore":       "[DK]",
	"Gemfile":             "[RB]",
	"Rakefile":            "[RB]",
	"requirements.txt":    "[PY]",
	"setup.py":            "[PY]",
	"pyproject.toml":      "[PY]",
	".babelrc":            "[BB]",
	"babel.config.js":     "[BB]",
	"nginx.conf":          "[NX]",
	"Vagrantfile":         "[VG]",
}

// ASCII directory and default icons
const (
	asciiDirIcon         = "[D]"
	asciiDirIconOpen     = "[D]"
	asciiDefaultFileIcon = "[F]"
	asciiBinaryIcon      = "[B]"
)

// Bootstrap Icons using Unicode escape sequences
// Reference: https://icons.getbootstrap.com/
var bootstrapIconMap = map[string]string{
	// Programming languages
	".go":     "\uf3bf", // bi-file-zip (no specific Go icon, using code)
	".py":     "\uf3b0", // bi-filetype-py
	".js":     "\uf3a3", // bi-filetype-js
	".ts":     "\uf352", // bi-file-code
	".tsx":    "\uf352", // bi-file-code
	".jsx":    "\uf352", // bi-file-code
	".rs":     "\uf352", // bi-file-code
	".java":   "\uf3a1", // bi-filetype-java
	".c":      "\uf352", // bi-file-code
	".cpp":    "\uf352", // bi-file-code
	".cc":     "\uf352", // bi-file-code
	".h":      "\uf352", // bi-file-code
	".hpp":    "\uf352", // bi-file-code
	".rb":     "\uf3ad", // bi-filetype-rb
	".php":    "\uf3ab", // bi-filetype-php
	".swift":  "\uf352", // bi-file-code
	".kt":     "\uf352", // bi-file-code
	".scala":  "\uf352", // bi-file-code
	".lua":    "\uf352", // bi-file-code
	".pl":     "\uf352", // bi-file-code
	".r":      "\uf352", // bi-file-code
	".ex":     "\uf352", // bi-file-code
	".exs":    "\uf352", // bi-file-code
	".erl":    "\uf352", // bi-file-code
	".hs":     "\uf352", // bi-file-code
	".clj":    "\uf352", // bi-file-code
	".vim":    "\uf352", // bi-file-code
	".zig":    "\uf352", // bi-file-code
	".dart":   "\uf352", // bi-file-code
	".elm":    "\uf352", // bi-file-code

	// Web
	".html":   "\uf3a0", // bi-filetype-html
	".htm":    "\uf3a0", // bi-filetype-html
	".css":    "\uf395", // bi-filetype-css
	".scss":   "\uf3ae", // bi-filetype-scss
	".sass":   "\uf3ae", // bi-filetype-scss
	".less":   "\uf352", // bi-file-code
	".vue":    "\uf352", // bi-file-code
	".svelte": "\uf352", // bi-file-code

	// Data/Config
	".json":   "\uf3a4", // bi-filetype-json
	".xml":    "\uf3b8", // bi-filetype-xml
	".yaml":   "\uf3b9", // bi-file-text
	".yml":    "\uf3b9", // bi-file-text
	".toml":   "\uf3b9", // bi-file-text
	".ini":    "\uf3e5", // bi-gear
	".conf":   "\uf3e5", // bi-gear
	".config": "\uf3e5", // bi-gear
	".env":    "\uf4df", // bi-key

	// Documents
	".md":       "\uf481", // bi-markdown
	".mdx":      "\uf481", // bi-markdown
	".txt":      "\uf3b9", // bi-file-text
	".pdf":      "\uf3aa", // bi-filetype-pdf
	".doc":      "\uf397", // bi-filetype-doc
	".docx":     "\uf398", // bi-filetype-docx
	".xls":      "\uf3b5", // bi-filetype-xls
	".xlsx":     "\uf3b6", // bi-filetype-xlsx
	".ppt":      "\uf3ac", // bi-filetype-ppt
	".pptx":     "\uf3ac", // bi-filetype-ppt
	".tex":      "\uf3b9", // bi-file-text
	".rst":      "\uf3b9", // bi-file-text

	// Images
	".png":  "\uf3a8", // bi-filetype-png
	".jpg":  "\uf39b", // bi-file-image
	".jpeg": "\uf39b", // bi-file-image
	".gif":  "\uf39a", // bi-filetype-gif
	".bmp":  "\uf38e", // bi-filetype-bmp
	".ico":  "\uf39b", // bi-file-image
	".webp": "\uf39b", // bi-file-image
	".svg":  "\uf3b2", // bi-filetype-svg

	// Audio/Video
	".mp3":  "\uf3a6", // bi-filetype-mp3
	".wav":  "\uf3b4", // bi-filetype-wav
	".flac": "\uf3a5", // bi-file-music
	".ogg":  "\uf3a5", // bi-file-music
	".aac":  "\uf38c", // bi-filetype-aac
	".mp4":  "\uf3a7", // bi-filetype-mp4
	".mkv":  "\uf3a9", // bi-file-play
	".avi":  "\uf3a9", // bi-file-play
	".mov":  "\uf3a9", // bi-file-play
	".webm": "\uf3a9", // bi-file-play

	// Archives
	".zip": "\uf10d", // bi-archive
	".tar": "\uf10d", // bi-archive
	".gz":  "\uf10d", // bi-archive
	".bz2": "\uf10d", // bi-archive
	".xz":  "\uf10d", // bi-archive
	".7z":  "\uf10d", // bi-archive
	".rar": "\uf10d", // bi-archive
	".deb": "\uf10d", // bi-archive
	".rpm": "\uf10d", // bi-archive

	// Data
	".sql": "\uf8c4", // bi-database
	".db":  "\uf8c4", // bi-database
	".csv": "\uf3b9", // bi-file-text

	// Shell/Scripts
	".sh":   "\uf5c3", // bi-terminal
	".bash": "\uf5c3", // bi-terminal
	".zsh":  "\uf5c3", // bi-terminal
	".fish": "\uf5c3", // bi-terminal
	".ps1":  "\uf5c3", // bi-terminal
	".bat":  "\uf5c3", // bi-terminal
	".cmd":  "\uf5c3", // bi-terminal

	// Executables/Binary
	".exe": "\uf3e5", // bi-gear
	".dll": "\uf3e5", // bi-gear
	".so":  "\uf3e5", // bi-gear
	".o":   "\uf3e5", // bi-gear
	".a":   "\uf3e5", // bi-gear
	".bin": "\uf3c0", // bi-file

	// Build/Package
	".lock": "\uf4dd", // bi-lock
	".sum":  "\uf4dd", // bi-lock

	// Git
	".gitignore":     "\uf69d", // bi-git
	".gitmodules":    "\uf69d", // bi-git
	".gitattributes": "\uf69d", // bi-git

	// Log files
	".log": "\uf3b9", // bi-file-text
}

// Bootstrap Icons special filenames
var bootstrapFilenameMap = map[string]string{
	"Dockerfile":          "\uf1c9", // bi-braces
	"docker-compose.yml":  "\uf1c9", // bi-braces
	"docker-compose.yaml": "\uf1c9", // bi-braces
	"Makefile":            "\uf5c3", // bi-terminal
	"CMakeLists.txt":      "\uf5c3", // bi-terminal
	"go.mod":              "\uf352", // bi-file-code
	"go.sum":              "\uf4dd", // bi-lock
	"Cargo.toml":          "\uf352", // bi-file-code
	"Cargo.lock":          "\uf4dd", // bi-lock
	"package.json":        "\uf3a4", // bi-filetype-json
	"package-lock.json":   "\uf4dd", // bi-lock
	"yarn.lock":           "\uf4dd", // bi-lock
	"pnpm-lock.yaml":      "\uf4dd", // bi-lock
	"LICENSE":             "\uf612", // bi-file-earmark-text
	"LICENSE.md":          "\uf612", // bi-file-earmark-text
	"LICENSE.txt":         "\uf612", // bi-file-earmark-text
	"README":              "\uf44c", // bi-info-circle
	"README.md":           "\uf481", // bi-markdown
	"README.txt":          "\uf44c", // bi-info-circle
	".gitignore":          "\uf69d", // bi-git
	".env":                "\uf4df", // bi-key
	".env.local":          "\uf4df", // bi-key
	".env.example":        "\uf4df", // bi-key
	".editorconfig":       "\uf3e5", // bi-gear
	".prettierrc":         "\uf3e5", // bi-gear
	".eslintrc":           "\uf3e5", // bi-gear
	".eslintrc.js":        "\uf3e5", // bi-gear
	".eslintrc.json":      "\uf3e5", // bi-gear
	"tsconfig.json":       "\uf3a4", // bi-filetype-json
	"vite.config.js":      "\uf3e5", // bi-gear
	"vite.config.ts":      "\uf3e5", // bi-gear
	"webpack.config.js":   "\uf3e5", // bi-gear
	".dockerignore":       "\uf1c9", // bi-braces
	"Gemfile":             "\uf3ad", // bi-filetype-rb
	"Rakefile":            "\uf3ad", // bi-filetype-rb
	"requirements.txt":    "\uf3b0", // bi-filetype-py
	"setup.py":            "\uf3b0", // bi-filetype-py
	"pyproject.toml":      "\uf3b0", // bi-filetype-py
	".babelrc":            "\uf3e5", // bi-gear
	"babel.config.js":     "\uf3e5", // bi-gear
	"nginx.conf":          "\uf3e5", // bi-gear
	"Vagrantfile":         "\uf3ad", // bi-filetype-rb
}

// Bootstrap directory and default icons
const (
	bootstrapDirIcon         = "\uf3d7" // bi-folder
	bootstrapDirIconOpen     = "\uf3d2" // bi-folder2-open
	bootstrapDefaultFileIcon = "\uf3c0" // bi-file
	bootstrapBinaryIcon      = "\uf10d" // bi-archive
)

// Nerd Font icons using explicit Unicode escape sequences
// Reference: https://www.nerdfonts.com/cheat-sheet
var iconMap = map[string]string{
	// Programming languages
	".go":     "\ue627", // nf-seti-go
	".py":     "\ue73c", // nf-dev-python
	".js":     "\ue74e", // nf-dev-javascript
	".ts":     "\ue628", // nf-seti-typescript
	".tsx":    "\ue7ba", // nf-dev-react
	".jsx":    "\ue7ba", // nf-dev-react
	".rs":     "\ue7a8", // nf-dev-rust
	".java":   "\ue738", // nf-dev-java
	".c":      "\ue61e", // nf-custom-c
	".cpp":    "\ue61d", // nf-custom-cpp
	".cc":     "\ue61d", // nf-custom-cpp
	".h":      "\ue61e", // nf-custom-c
	".hpp":    "\ue61d", // nf-custom-cpp
	".rb":     "\ue739", // nf-dev-ruby
	".php":    "\ue73d", // nf-dev-php
	".swift":  "\ue755", // nf-dev-swift
	".kt":     "\ue634", // nf-seti-kotlin
	".scala":  "\ue737", // nf-dev-scala
	".lua":    "\ue620", // nf-seti-lua
	".pl":     "\ue769", // nf-dev-perl
	".r":      "\ue68a", // nf-seti-r
	".ex":     "\ue62d", // nf-seti-elixir
	".exs":    "\ue62d", // nf-seti-elixir
	".erl":    "\ue7b1", // nf-dev-erlang
	".hs":     "\ue61f", // nf-seti-haskell
	".clj":    "\ue768", // nf-dev-clojure
	".vim":    "\ue62b", // nf-dev-vim
	".zig":    "\ue6a9", // nf-seti-zig
	".dart":   "\ue798", // nf-dev-dart
	".elm":    "\ue62c", // nf-seti-elm

	// Web
	".html":   "\ue736", // nf-dev-html5
	".htm":    "\ue736", // nf-dev-html5
	".css":    "\ue749", // nf-dev-css3
	".scss":   "\ue603", // nf-dev-sass
	".sass":   "\ue603", // nf-dev-sass
	".less":   "\ue758", // nf-dev-less
	".vue":    "\ue6a0", // nf-seti-vue
	".svelte": "\ue697", // nf-seti-svelte
	".angular": "\ue753", // nf-dev-angular

	// Data/Config
	".json":   "\ue60b", // nf-seti-json
	".xml":    "\ue619", // nf-seti-xml
	".yaml":   "\ue6a8", // nf-seti-yaml
	".yml":    "\ue6a8", // nf-seti-yaml
	".toml":   "\ue6b2", // nf-seti-config
	".ini":    "\ue615", // nf-seti-settings
	".conf":   "\ue615", // nf-seti-settings
	".config": "\ue615", // nf-seti-settings
	".env":    "\ue615", // nf-seti-settings

	// Documents
	".md":       "\ue73e", // nf-dev-markdown
	".mdx":      "\ue73e", // nf-dev-markdown
	".txt":      "\uf0f6", // nf-fa-file_text_o
	".pdf":      "\uf1c1", // nf-fa-file_pdf_o
	".doc":      "\uf1c2", // nf-fa-file_word_o
	".docx":     "\uf1c2", // nf-fa-file_word_o
	".xls":      "\uf1c3", // nf-fa-file_excel_o
	".xlsx":     "\uf1c3", // nf-fa-file_excel_o
	".ppt":      "\uf1c4", // nf-fa-file_powerpoint_o
	".pptx":     "\uf1c4", // nf-fa-file_powerpoint_o
	".tex":      "\ue69b", // nf-seti-tex
	".rst":      "\ue6a7", // nf-seti-rst

	// Images
	".png":  "\uf1c5", // nf-fa-file_image_o
	".jpg":  "\uf1c5", // nf-fa-file_image_o
	".jpeg": "\uf1c5", // nf-fa-file_image_o
	".gif":  "\uf1c5", // nf-fa-file_image_o
	".bmp":  "\uf1c5", // nf-fa-file_image_o
	".ico":  "\uf1c5", // nf-fa-file_image_o
	".webp": "\uf1c5", // nf-fa-file_image_o
	".svg":  "\ue698", // nf-seti-svg

	// Audio/Video
	".mp3":  "\uf1c7", // nf-fa-file_audio_o
	".wav":  "\uf1c7", // nf-fa-file_audio_o
	".flac": "\uf1c7", // nf-fa-file_audio_o
	".ogg":  "\uf1c7", // nf-fa-file_audio_o
	".aac":  "\uf1c7", // nf-fa-file_audio_o
	".mp4":  "\uf1c8", // nf-fa-file_video_o
	".mkv":  "\uf1c8", // nf-fa-file_video_o
	".avi":  "\uf1c8", // nf-fa-file_video_o
	".mov":  "\uf1c8", // nf-fa-file_video_o
	".webm": "\uf1c8", // nf-fa-file_video_o

	// Archives
	".zip":   "\uf1c6", // nf-fa-file_archive_o
	".tar":   "\uf1c6", // nf-fa-file_archive_o
	".gz":    "\uf1c6", // nf-fa-file_archive_o
	".bz2":   "\uf1c6", // nf-fa-file_archive_o
	".xz":    "\uf1c6", // nf-fa-file_archive_o
	".7z":    "\uf1c6", // nf-fa-file_archive_o
	".rar":   "\uf1c6", // nf-fa-file_archive_o
	".deb":   "\ue77d", // nf-dev-debian
	".rpm":   "\ue7bb", // nf-dev-redhat

	// Data
	".sql": "\ue706", // nf-dev-database
	".db":  "\ue706", // nf-dev-database
	".csv": "\uf1c3", // nf-fa-file_excel_o

	// Shell/Scripts
	".sh":   "\ue795", // nf-dev-terminal
	".bash": "\ue795", // nf-dev-terminal
	".zsh":  "\ue795", // nf-dev-terminal
	".fish": "\ue795", // nf-dev-terminal
	".ps1":  "\ue70f", // nf-dev-windows (powershell)
	".bat":  "\ue70f", // nf-dev-windows
	".cmd":  "\ue70f", // nf-dev-windows

	// Executables/Binary
	".exe": "\ue70f", // nf-dev-windows
	".dll": "\ue70f", // nf-dev-windows
	".so":  "\uf013", // nf-fa-cog
	".o":   "\uf013", // nf-fa-cog
	".a":   "\uf013", // nf-fa-cog
	".bin": "\uf471", // nf-oct-file_binary

	// Build/Package
	".lock": "\uf023", // nf-fa-lock
	".sum":  "\uf023", // nf-fa-lock

	// Git
	".gitignore":     "\ue702", // nf-dev-git
	".gitmodules":    "\ue702", // nf-dev-git
	".gitattributes": "\ue702", // nf-dev-git

	// Log files
	".log": "\uf18d", // nf-fa-bug
}

// Special filenames that override extension-based icons
var filenameMap = map[string]string{
	"Dockerfile":          "\ue7b0", // nf-dev-docker
	"docker-compose.yml":  "\ue7b0", // nf-dev-docker
	"docker-compose.yaml": "\ue7b0", // nf-dev-docker
	"Makefile":            "\ue673", // nf-seti-makefile
	"CMakeLists.txt":      "\ue673", // nf-seti-makefile
	"go.mod":              "\ue627", // nf-seti-go
	"go.sum":              "\ue627", // nf-seti-go
	"Cargo.toml":          "\ue7a8", // nf-dev-rust
	"Cargo.lock":          "\ue7a8", // nf-dev-rust
	"package.json":        "\ue71e", // nf-dev-nodejs_small
	"package-lock.json":   "\ue71e", // nf-dev-nodejs_small
	"yarn.lock":           "\ue6a7", // nf-seti-yarn
	"pnpm-lock.yaml":      "\ue71e", // nf-dev-nodejs_small
	"LICENSE":             "\uf0e3", // nf-fa-gavel
	"LICENSE.md":          "\uf0e3", // nf-fa-gavel
	"LICENSE.txt":         "\uf0e3", // nf-fa-gavel
	"README":              "\uf05a", // nf-fa-info_circle
	"README.md":           "\uf05a", // nf-fa-info_circle
	"README.txt":          "\uf05a", // nf-fa-info_circle
	".gitignore":          "\ue702", // nf-dev-git
	".env":                "\uf21b", // nf-fa-key (dotenv)
	".env.local":          "\uf21b", // nf-fa-key
	".env.example":        "\uf21b", // nf-fa-key
	".editorconfig":       "\ue652", // nf-seti-editorconfig
	".prettierrc":         "\ue6b4", // nf-seti-prettier
	".eslintrc":           "\ue60c", // nf-seti-eslint
	".eslintrc.js":        "\ue60c", // nf-seti-eslint
	".eslintrc.json":      "\ue60c", // nf-seti-eslint
	"tsconfig.json":       "\ue628", // nf-seti-typescript
	"vite.config.js":      "\ue6b3", // nf-seti-vite
	"vite.config.ts":      "\ue6b3", // nf-seti-vite
	"webpack.config.js":   "\ue6a4", // nf-seti-webpack
	".dockerignore":       "\ue7b0", // nf-dev-docker
	"Gemfile":             "\ue739", // nf-dev-ruby
	"Rakefile":            "\ue739", // nf-dev-ruby
	"requirements.txt":    "\ue73c", // nf-dev-python
	"setup.py":            "\ue73c", // nf-dev-python
	"pyproject.toml":      "\ue73c", // nf-dev-python
	".babelrc":            "\ue6a1", // nf-seti-babel
	"babel.config.js":     "\ue6a1", // nf-seti-babel
	"nginx.conf":          "\ue776", // nf-dev-nginx
	"Vagrantfile":         "\ue21e", // nf-linux-vagrant
}

// Directory and default icons
const (
	DirIcon         = "\uf07b" // nf-fa-folder
	DirIconOpen     = "\uf07c" // nf-fa-folder_open
	DefaultFileIcon = "\uf15b" // nf-fa-file
)

// GetFileIcon returns an icon for a file based on its type
func GetFileIcon(file fs.FileInfo) string {
	switch currentIconMode {
	case IconModeASCII:
		return getASCIIIcon(file)
	case IconModeBootstrap:
		return getBootstrapIcon(file)
	default:
		return getNerdIcon(file)
	}
}

// getNerdIcon returns a Nerd Font icon for the file
func getNerdIcon(file fs.FileInfo) string {
	if file.IsDir {
		return DirIcon
	}

	// Check special filenames first
	if icon, ok := filenameMap[file.Name]; ok {
		return icon
	}

	// Check by extension
	ext := strings.ToLower(filepath.Ext(file.Name))
	if icon, ok := iconMap[ext]; ok {
		return icon
	}

	// Default file icon
	return DefaultFileIcon
}

// getBootstrapIcon returns a Bootstrap Icon for the file
func getBootstrapIcon(file fs.FileInfo) string {
	if file.IsDir {
		return bootstrapDirIcon
	}

	// Check special filenames first
	if icon, ok := bootstrapFilenameMap[file.Name]; ok {
		return icon
	}

	// Check by extension
	ext := strings.ToLower(filepath.Ext(file.Name))
	if icon, ok := bootstrapIconMap[ext]; ok {
		return icon
	}

	// Default file icon
	return bootstrapDefaultFileIcon
}

// getASCIIIcon returns an ASCII fallback icon for the file
func getASCIIIcon(file fs.FileInfo) string {
	if file.IsDir {
		return asciiDirIcon
	}

	// Check special filenames first
	if icon, ok := asciiFilenameMap[file.Name]; ok {
		return icon
	}

	// Check by extension
	ext := strings.ToLower(filepath.Ext(file.Name))
	if icon, ok := asciiIconMap[ext]; ok {
		return icon
	}

	// Default file icon
	return asciiDefaultFileIcon
}

// GetDirIcon returns the directory icon based on current mode
func GetDirIcon() string {
	switch currentIconMode {
	case IconModeASCII:
		return asciiDirIcon
	case IconModeBootstrap:
		return bootstrapDirIcon
	default:
		return DirIcon
	}
}

// GetDirIconOpen returns the open directory icon based on current mode
func GetDirIconOpen() string {
	switch currentIconMode {
	case IconModeASCII:
		return asciiDirIconOpen
	case IconModeBootstrap:
		return bootstrapDirIconOpen
	default:
		return DirIconOpen
	}
}

// GetDefaultFileIcon returns the default file icon based on current mode
func GetDefaultFileIcon() string {
	switch currentIconMode {
	case IconModeASCII:
		return asciiDefaultFileIcon
	case IconModeBootstrap:
		return bootstrapDefaultFileIcon
	default:
		return DefaultFileIcon
	}
}

// GetBinaryIcon returns the binary file icon based on current mode
func GetBinaryIcon() string {
	switch currentIconMode {
	case IconModeASCII:
		return asciiBinaryIcon
	case IconModeBootstrap:
		return bootstrapBinaryIcon
	default:
		return "\uf1c6" // nf-fa-file_archive_o
	}
}
