# 🍣 Sushi

A fast and elegant terminal-based file explorer written in Go.

## Features

- 🚀 Fast, asynchronous navigation with Vim-style keybindings
- 🗂️ Three panes (parent folder, files, preview) that adapt to the terminal width, with tabs
- 👁️ Scrollable preview with syntax highlighting, line numbers and file details
- 📝 Open files in your editor or their default app
- 🔍 Fuzzy search within the current directory
- ✅ Multi-file selection
- 📋 Copy, cut, paste, delete, rename and create, with safeguards against overwriting a file with itself
- 🔖 Bookmarks with quick-jump keys
- 🧩 Plugins: run your own commands and scripts on the selection
- 🎨 Color themes, plus Nerd Font, Bootstrap Icons or plain ASCII icons
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

### macOS App

On a Mac, sushi is also available as an app with its own window and Dock icon, so it can be opened without a terminal. The app bundles the JetBrainsMono Nerd Font, so file icons work without installing a font.

Building it needs Xcode as well as Go:

```bash
make macos    # builds all three into dist/
```

| File | What it is |
|------|------------|
| `dist/Sushi.app` | The app. Double-click to run it. |
| `dist/Sushi-0.1.0.pkg` | Installer: puts Sushi in Applications and the `sushi` command in `/usr/local/bin` |
| `dist/Sushi-0.1.0.dmg` | Disk image: drag Sushi to Applications |

`make app`, `make pkg` and `make dmg` build them one at a time, and `VERSION=1.2.3 make macos` sets the version.

The app opens in your home folder and runs sushi through your login shell, so plugins and `$EDITOR` work as they do in a terminal. `Cmd` `+` and `Cmd` `-` change the text size. Quitting sushi with `q` closes the app.

The builds are signed ad hoc, which is enough for the Mac that built them. On another Mac, macOS will refuse to open them until they are allowed under System Settings, Privacy & Security. Distributing without that warning needs an Apple Developer ID: build with `SIGN_IDENTITY="Developer ID Application: ..."` and notarize the result.

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

# How Enter opens files: "auto" (text files in $EDITOR, others in their
# default app), "editor", or "system"
opener: auto

# Sort by "name", "size" (largest first), "modified" (newest first),
# or "type" (file extension). Directories are always listed first.
sort_by: name

# Reverse the sort order
sort_reverse: false

# Color theme: "default", "light", "dark" (Dracula-inspired) or "classic"
# (the colors sushi used before its redesign)
theme: default

# Override individual theme colors with ANSI numbers or hex values
colors:
  directory: "33"
  accent: "#7d56f4"

# Syntax highlighting style for the preview ("sushi", "sushi-light", or any
# Chroma style such as "monokai", "dracula", "github", "nord"); empty
# follows the theme
syntax_theme: ""

# Commands to run on the selection; see docs/plugins.md
plugins:
  - name: git-log
    key: ctrl+l
    command: git log --oneline -20
```

Color names for `colors`: `header_fg`, `text`, `muted`, `faint`, `raised`, `directory`, `cursor_fg`, `cursor_bg`, `bar_fg`, `bar_bg`, `tab_bar_bg`, `tab_active_fg`, `tab_inactive_fg`, `tab_inactive_bg`, `accent`, `border`, `title`, `highlight`, `danger`, `selected`. Unknown names and invalid values are reported in the status bar when sushi starts.

Command line flags (like `--ascii`) override config file settings.

## Layout

| Terminal width | Panes shown |
|----------------|-------------|
| 100 columns or more | Parent folder, files, preview |
| 72 to 99 columns | Files, preview |
| Under 72 columns | Files only |

Narrow file lists drop the date column first, then the size. The status bar shows the current mode (`NORMAL`, `SELECT`, `SEARCH`, ...), and the last line lists the keys that apply to it.

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
| `→`/`l`, `Enter` | Enter a directory, or open a file (see `opener`) |

### Files

Copy, cut, delete, edit and open act on the selection when there is one, and on the file under the cursor otherwise.

| Key | Action |
|-----|--------|
| `Space` | Select file and move down |
| `*` | Invert selection |
| `u` | Clear selection |
| `c` | Copy to clipboard |
| `x` | Cut to clipboard |
| `v` | Paste into current directory |
| `d` | Delete (asks first unless `confirm_delete: false`) |
| `r` | Rename |
| `n` | New file (a name ending in `/` makes a directory; `src/main.go` creates `src/` too) |
| `N` | New directory |
| `e` | Edit in `$VISUAL` / `$EDITOR` |
| `o` | Open with the default app |

### View, Search and Bookmarks

| Key | Action |
|-----|--------|
| `/` | Fuzzy search: the list narrows to the matches (`↑`/`↓` between them, `Enter` to keep, `Esc` to cancel) |
| `p` | Toggle preview pane |
| `J` / `K` | Scroll the preview down / up |
| `.` | Toggle hidden files |
| `b` | Open bookmarks (`j`/`k` to move, `Enter` to go, `d` to delete, `Esc` to close) |
| `B` | Bookmark current directory |
| `1`-`9` | Jump to bookmark |

### Plugins

| Key | Action |
|-----|--------|
| `P` | Open the Run palette on the plugin list |
| `!` | Open the Run palette to type a shell command |

In the palette, `Tab` switches between the command line and the plugin list.

Plugins can also have their own keys. See [docs/plugins.md](docs/plugins.md).

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
| `?` | Show the key panel. `Esc` closes it; any other key closes it and does its job (`j`/`k` scroll it first on small terminals) |
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
│   ├── opener/      # Editor and default-app commands
│   ├── plugins/     # Plugin loading and running
│   ├── ui/          # Icons, themes, styles and UI components
│   └── utils/       # Formatting helpers
├── macos/           # The macOS app: Swift window, icon and build script
├── docs/            # Plugin guide
├── examples/plugins # Example plugin scripts
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
- [x] Open files in an editor or the system default app
- [x] Rename, and create files and directories
- [x] Multi-file selection
- [x] Color themes
- [x] Plugin system

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

MIT License - see LICENSE file for details

## Acknowledgments

- Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea)
- Styled with [Lip Gloss](https://github.com/charmbracelet/lipgloss)
- Inspired by ranger, nnn, and lf
