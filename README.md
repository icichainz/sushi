# 🍣 Sushi

A fast and elegant terminal-based file explorer written in Go.

## Features

- 🚀 Fast and responsive navigation
- ⌨️ Vim-style keybindings
- 🎨 Beautiful interface with colors and icons
- 📁 Directory tree navigation
- 👁️ File preview pane with syntax support
- 📊 Smart preview for text, binary, and directories
- 🔍 File search and filtering (coming soon)
- 📋 File operations: copy, move, delete (coming soon)

## Installation

### From Source

```bash
git clone https://github.com/yourusername/sushi.git
cd sushi
go build -o sushi main.go
./sushi
```

### Quick Install

```bash
go install github.com/yourusername/sushi@latest
```

## Usage

```bash
# Open in current directory
sushi

# Open specific directory
sushi /path/to/directory
```

## Keybindings

| Key | Action |
|-----|--------|
| `↑/k` | Move up |
| `↓/j` | Move down |
| `←/h` | Go to parent directory |
| `→/l` | Enter directory |
| `Enter` | Open file/directory |
| `Backspace` | Go back |
| `p` | Toggle preview pane |
| `q` | Quit |
| `?` | Show help |

## Development

### Prerequisites

- Go 1.21 or higher

### Setup

```bash
# Clone the repository
git clone https://github.com/yourusername/sushi.git
cd sushi

# Install dependencies
go mod download

# Run
go run main.go
```

### Project Structure

```
sushi/
├── internal/
│   ├── app/         # Application logic (Bubbletea)
│   ├── fs/          # File system operations
│   ├── ui/          # UI components and styling
│   ├── config/      # Configuration
│   └── utils/       # Utilities
├── configs/         # Default configurations
└── main.go          # Entry point
```

## Roadmap

- [x] Basic file navigation
- [x] Vim-style keybindings
- [x] File icons and colors
- [x] File preview pane
- [ ] Syntax highlighting in preview
- [ ] File operations (copy, move, delete)
- [ ] Fuzzy search
- [ ] Bookmarks
- [ ] Multiple tabs
- [ ] Configuration file support
- [ ] Plugin system

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

MIT License - see LICENSE file for details

## Acknowledgments

- Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea)
- Styled with [Lip Gloss](https://github.com/charmbracelet/lipgloss)
- Inspired by ranger, nnn, and lf