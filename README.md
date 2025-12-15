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

# Use ASCII icons (no Nerd Font required)
sushi --ascii

# Combine options
sushi --ascii ~/projects
```

### Command Line Options

| Option | Description |
|--------|-------------|
| `--ascii` | Use ASCII text icons (works everywhere, no font required) |
| `--bootstrap` | Use Bootstrap Icons font |
| `--install-font` | Download and install JetBrainsMono Nerd Font |
| `--list-fonts` | List available Nerd Fonts to install |
| `--init-config` | Create default configuration file |
| `--help`, `-h` | Show help message |

### Icon Modes

Sushi supports three icon modes:

1. **Nerd Font** (default) - Rich icons for 100+ file types. Requires a [Nerd Font](https://www.nerdfonts.com/) installed.
2. **Bootstrap Icons** (`--bootstrap`) - Clean, modern icons. Requires [Bootstrap Icons](https://icons.getbootstrap.com/) font installed.
3. **ASCII** (`--ascii`) - Text-based icons like `[GO]`, `[PY]`, `[D]`. Works in any terminal without special fonts.

## Configuration

Sushi supports a YAML configuration file at `~/.config/sushi/config.yaml`.

### Creating the Config File

```bash
sushi --init-config
```

### Configuration Options

```yaml
# Icon display mode: "nerd", "bootstrap", or "ascii"
icon_mode: nerd

# Show hidden files by default
show_hidden: false

# Enable preview pane by default
preview_enabled: true

# Preview pane width percentage (1-80)
preview_width: 50

# Require confirmation before deleting files
confirm_delete: true

# Sort files by: "name", "size", "modified", or "type"
sort_by: name

# Reverse sort order
sort_reverse: false

# Theme (for future use): "default", "dark", "light"
theme: default
```

Command line flags (like `--ascii`) override config file settings.

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

## Requirements

- **Nerd Font** (default mode) - For proper icon display, you need a Nerd Font. Install automatically with:
  ```bash
  sushi --install-font
  ```
  Then configure your terminal to use "JetBrainsMono Nerd Font".

  Or manually download from [Nerd Fonts](https://www.nerdfonts.com/):
  - [JetBrainsMono](https://github.com/ryanoasis/nerd-fonts/releases/download/v3.3.0/JetBrainsMono.zip)
  - [FiraCode](https://github.com/ryanoasis/nerd-fonts/releases/download/v3.3.0/FiraCode.zip)
  - [Hack](https://github.com/ryanoasis/nerd-fonts/releases/download/v3.3.0/Hack.zip)

- **Bootstrap Icons** (optional, with `--bootstrap` flag) - Install from [Bootstrap Icons](https://icons.getbootstrap.com/)

- **No font required** - Use `--ascii` flag for ASCII text icons that work in any terminal

## Development

### Prerequisites

- Go 1.21 or higher
- A Nerd Font installed and configured in your terminal

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
- [x] Syntax highlighting in preview
- [x] File operations (copy, move, delete)
- [x] Fuzzy search
- [x] Bookmarks
- [x] Multiple tabs
- [x] Configuration file support
- [ ] Plugin system

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

MIT License - see LICENSE file for details

## Acknowledgments

- Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea)
- Styled with [Lip Gloss](https://github.com/charmbracelet/lipgloss)
- Inspired by ranger, nnn, and lf