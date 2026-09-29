# 🍣 Sushi

A fast and elegant terminal-based file explorer written in Go.

## Features

- 🚀 Fast, asynchronous navigation with Vim-style keybindings
- 🗂️ Multiple tabs
- 👁️ Preview pane with syntax highlighting, directory listings and binary file details
- 🔍 Fuzzy search within the current directory
- 📋 Copy, cut, paste and delete, with safeguards against overwriting a file with itself
- 🔖 Bookmarks with quick-jump keys
- 🎨 Nerd Font, Bootstrap Icons or plain ASCII icons
- ⚙️ YAML configuration for hidden files, sorting, preview layout and more

## Installation

### From Source

```bash
git clone https://github.com/icichainz/sushi.git
cd sushi
go build -o sushi main.go
./sushi
```

### Quick Install

```bash
go install github.com/icichainz/sushi@latest
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
| `--install-font` | Download and install a Nerd Font (JetBrainsMono by default) |
| `--font NAME` | With `--install-font`, choose which font to install |
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

# List dotfiles at startup (toggle at runtime with ".")
show_hidden: false

# Show the preview pane by default (toggle at runtime with "p")
preview_enabled: true

# Share of the screen width given to the preview pane, in percent (1-80)
preview_width: 50

# Ask before deleting files
confirm_delete: true

# Sort by "name", "size" (largest first), "modified" (newest first),
# or "type" (file extension). Directories are always listed first.
sort_by: name

# Reverse the sort order
sort_reverse: false

# Reserved for future use; has no effect yet
theme: default
```

Command line flags (like `--ascii`) override config file settings.

## Keybindings

### Navigation

| Key | Action |
|-----|--------|
| `↑`/`k` | Move up |
| `↓`/`j` | Move down |
| `PgUp`/`Ctrl+u` | Page up |
| `PgDn`/`Ctrl+d` | Page down |
| `g`/`Home` | Go to first file |
| `G`/`End` | Go to last file |
| `←`/`h`, `Backspace` | Go to parent directory |
| `→`/`l`, `Enter` | Enter directory |

### Files

| Key | Action |
|-----|--------|
| `c` | Copy to clipboard |
| `x` | Cut to clipboard |
| `v` | Paste into current directory |
| `d` | Delete (asks first unless `confirm_delete: false`) |

### View, Search and Bookmarks

| Key | Action |
|-----|--------|
| `/` | Fuzzy search (`↑`/`↓` between matches, `Enter` to keep, `Esc` to cancel) |
| `p` | Toggle preview pane |
| `.` | Toggle hidden files |
| `b` | Open bookmarks (`j`/`k` to move, `Enter` to go, `d` to delete, `Esc` to close) |
| `B` | Bookmark current directory |
| `1`-`9` | Jump to bookmark |

### Tabs

| Key | Action |
|-----|--------|
| `t` | New tab in current directory |
| `T` | New tab in home directory |
| `Tab` / `Shift+Tab` | Next / previous tab |
| `Ctrl+w` | Close tab (quits on the last one) |

### General

| Key | Action |
|-----|--------|
| `?` | Show help |
| `q`/`Ctrl+c` | Quit |

## Requirements

- **Nerd Font** (default mode) - For proper icon display, you need a Nerd Font. Install automatically with:

  ```bash
  sushi --install-font                 # JetBrainsMono
  sushi --install-font --font FiraCode # or any font from --list-fonts
  ```

  Then configure your terminal to use the installed Nerd Font.

  Or manually download from [Nerd Fonts](https://www.nerdfonts.com/):
  - [JetBrainsMono](https://github.com/ryanoasis/nerd-fonts/releases/download/v3.3.0/JetBrainsMono.zip)
  - [FiraCode](https://github.com/ryanoasis/nerd-fonts/releases/download/v3.3.0/FiraCode.zip)
  - [Hack](https://github.com/ryanoasis/nerd-fonts/releases/download/v3.3.0/Hack.zip)

- **Bootstrap Icons** (optional, with `--bootstrap` flag) - Install from [Bootstrap Icons](https://icons.getbootstrap.com/)

- **No font required** - Use `--ascii` flag for ASCII text icons that work in any terminal

## Development

### Prerequisites

- Go 1.25 or higher
- A Nerd Font installed and configured in your terminal

### Setup

```bash
# Clone the repository
git clone https://github.com/icichainz/sushi.git
cd sushi

# Install dependencies
go mod download

# Run
go run main.go

# Format, vet and test
make check
```

### Project Structure

```text
sushi/
├── internal/
│   ├── app/         # Application logic (Bubble Tea model, update, view)
│   ├── config/      # Configuration and bookmarks
│   ├── fonts/       # Nerd Font installer
│   ├── fs/          # File system scanning and operations
│   ├── ui/          # Icons, styles and the preview component
│   └── utils/       # Formatting helpers
└── main.go          # Entry point and command line flags
```

## Roadmap

- [x] Basic file navigation
- [x] Vim-style keybindings
- [x] File icons and colors
- [x] File preview pane
- [x] Syntax highlighting in preview
- [x] File operations (copy, move, delete)
- [x] Fuzzy search (current directory)
- [x] Bookmarks
- [x] Multiple tabs
- [x] Configuration file support
- [ ] Open files in an editor or the system default app
- [ ] Rename, and create files and directories
- [ ] Multi-file selection
- [ ] Color themes
- [ ] Plugin system

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

MIT License - see LICENSE file for details

## Acknowledgments

- Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea)
- Styled with [Lip Gloss](https://github.com/charmbracelet/lipgloss)
- Inspired by ranger, nnn, and lf
