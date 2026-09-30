# 🍣 Sushi

A fast and elegant terminal-based file explorer written in Go.

## Features

- 🚀 Fast, asynchronous navigation with Vim-style keybindings
- 🗂️ Three panes (parent folder, files, preview) that adapt to the terminal width, with tabs
- 👁️ Scrollable preview with syntax highlighting, line numbers and file details
- 🖼️ Image, archive and PDF previews: pictures drawn right in the terminal
- 📝 Open files in your editor or their default app
- 🔍 Fuzzy search within the current directory
- 🔎 Find files by name, or text inside them, in every folder below the current one
- ✅ Multi-file selection
- 📋 Copy, cut, paste, delete, rename and create, with safeguards against overwriting a file with itself
- 🗑️ Trash and undo (`Ctrl+z`), with progress and cancel (`Ctrl+x`) for long operations
- 🧰 Duplicate, symlink paste, permissions, bulk rename in your editor, zip and extract
- 🔄 Lists refresh by themselves when files change on disk
- ↕️ Change the sort order on the fly
- 🖱️ Mouse support: click, double-click, right-click and scroll
- 🐚 Quit into the directory you were browsing
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
| ---- | ---------- |
| `dist/Sushi.app` | The app. Double-click to run it. |
| `dist/Sushi-0.1.0.pkg` | Installer: puts Sushi in Applications and the `sushi` command in `/usr/local/bin` |
| `dist/Sushi-0.1.0.dmg` | Disk image: drag Sushi to Applications |

`make app`, `make pkg` and `make dmg` build them one at a time, and `VERSION=1.2.3 make macos` sets the version.

The app opens in your home folder and runs sushi through your login shell, so plugins and `$EDITOR` work as they do in a terminal. `Cmd` `+` and `Cmd` `-` change the text size. Quitting sushi with `q` closes the app.

The mouse works in the app as in a terminal (see Mouse below), which means dragging selects files rather than text. Hold `Shift` while dragging to select text, or set `mouse: false` in the config.

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
| ------ | ----------- |
| `--ascii` | Use ASCII text icons (works everywhere, no font required) |
| `--bootstrap` | Use Bootstrap Icons font |
| `--install-font` | Download and install a Nerd Font (JetBrainsMono by default) |
| `--font NAME` | With `--install-font`, choose which font to install |
| `--list-fonts` | List available Nerd Fonts to install |
| `--init-config` | Create default configuration file |
| `--list-keys` | Print every action and its keys as a `keys:` section for the config file, then the keys that can't be changed and the plugins' keys. Problems with the config go to stderr, and the exit status is 1 if there are any (see [Remapping Keys](#remapping-keys)) |
| `--cwd-file FILE` | When you quit with `q`, write the directory shown to `FILE`, for the `sushicd` shell function (`Q` quits without writing) |
| `--print-shell-wrapper [SHELL]` | Print the `sushicd` shell function for `zsh`, `bash` or `fish` (by default, the shell in `$SHELL`) |
| `--version` | Print the version, as in `sushi 0.1.0` (`sushi dev` when built without the Makefile) |
| `--help`, `-h` | Show help message |

### Changing Directory on Quit

A program can't change the directory of the shell that started it, so sushi comes with a shell function, `sushicd`, that runs sushi and then changes to the directory sushi was showing when you quit with `q`. `Q` quits without changing directory.

For zsh, add this line to `~/.zshrc` (for bash, to `~/.bashrc`, with `bash` in place of `zsh`):

```bash
eval "$(sushi --print-shell-wrapper zsh)"
```

For fish, add this line to `~/.config/fish/config.fish`:

```fish
sushi --print-shell-wrapper fish | source
```

Then run `sushicd` instead of `sushi`, with the same arguments. For a shorter name, add an alias such as `alias s=sushicd`.

`Ctrl+c`, and closing the last tab with `Ctrl+w`, quit like `q` and change directory too. The function runs `sushi --cwd-file TMPFILE` and reads the directory sushi writes there when it quits, so other integrations can do the same.

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

# Clicks and the scroll wheel. While sushi uses the mouse, terminals select
# text only with a key held (Shift in most, Option in iTerm2); set this to
# false to select text without it.
mouse: true

# Ask before deleting permanently (d with delete_to_trash off; D always asks)
confirm_delete: true

# d moves files to the trash, which ctrl+z undoes; D always deletes
# permanently. Set to false to make d delete permanently too.
delete_to_trash: true

# How Enter opens files: "auto" (text files in $EDITOR, others in their
# default app), "editor", or "system"
opener: auto

# Sort by "name", "size" (largest first), "modified" (newest first),
# or "type" (file extension). Directories are always listed first.
# This is the order sushi starts with; "s" and "S" change it at runtime.
sort_by: name

# Reverse the sort order
sort_reverse: false

# Reload file lists when files change on disk. Turn off to refresh only
# with ctrl+r, for example on slow network drives.
watch: true

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

# Keys for actions, replacing their defaults; see Remapping Keys below
keys:
  hidden: H
```

Color names for `colors`: `header_fg`, `text`, `muted`, `faint`, `raised`, `directory`, `cursor_fg`, `cursor_bg`, `bar_fg`, `bar_bg`, `tab_bar_bg`, `tab_active_fg`, `tab_inactive_fg`, `tab_inactive_bg`, `accent`, `border`, `title`, `highlight`, `danger`, `selected`.

Problems with the config are reported in the status bar when sushi starts, and the rest of the file still applies. They are a config file that can't be read, settings sushi doesn't know (`shw_hidden: true` is reported by its line, with the setting you probably meant), values of the wrong kind, which keep their defaults, invalid values, which are replaced (`icon_mode: unknown value "emoji", using nerd`), unknown themes and colors, and problems with keys and plugins. The status bar shows the first problem for 10 seconds, with how many more there are; `sushi --list-keys` lists them all.

Command line flags (like `--ascii`) override config file settings.

### Remapping Keys

Every action's keys can be changed under `keys:` in the config file:

```yaml
keys:
  up: [k, up]
  down: [j, down]
  delete: [d, delete]
  quit: [q, ctrl+c]
  hidden: H        # one key needs no list
  refresh: []      # leaves refresh without a key
```

- Each action takes a list of keys, or a single key on its own. The list replaces all of the action's default keys, so include the arrow keys if you still want them. Actions left out keep their defaults.
- The first key listed is the one the key panel and the hint rows show; arrow and paging keys are passed over when the action has another.
- `[]` leaves an action without a key. An action written with nothing after it (`refresh:`) is reported and keeps its defaults, and so does one whose keys can't be read at all.

Keys are written as Bubble Tea reports them:

| Key | Written |
| --- | ------- |
| A character | `k`, `G`, `"?"`, `"*"`. Capitals are shifted letters: write `G`, not `shift+g`. Quote punctuation, which YAML may otherwise read as something else |
| The space bar | `space` |
| Named keys | `enter`, `esc`, `tab`, `shift+tab`, `backspace`, `delete`, `insert`, `up`, `down`, `left`, `right`, `home`, `end`, `pgup`, `pgdown`, `f1` to `f20` |
| Control keys | `ctrl+a` to `ctrl+z`. Terminals send `ctrl+i` as `tab` and `ctrl+m` as `enter`, so those two can't be told apart; write `tab` or `enter` |
| Modified keys | The arrows, `home` and `end` after `ctrl+`, `shift+` or `ctrl+shift+`, as in `ctrl+up`, `shift+left` or `ctrl+shift+end`, and `ctrl+pgup` and `ctrl+pgdown` |
| Alt | `alt+` before any of these, as in `alt+x`, `alt+G` or `alt+enter` |

Key names can be written in capitals (`Enter`, `F5`); characters can't, as `g` and `G` are different keys.

When sushi starts, it reports problems with the keys in the status bar: unknown actions (with the right name when one is only written differently, as `hard-delete` for `hard_delete`), key names it can't read, an action with no keys, and conflicts:

- A key bound to two actions stays with one of them, which the message names: an action whose keys the config sets wins over one left at its defaults, and between two the config sets, the one listed first in the table below.
- An action that takes a digit hides the bookmark that digit jumps to.
- A key that a dialog uses for itself, such as `esc`, or a letter of the sort menu, does what the dialog says there, so the action's key is ignored in that dialog.
- `ctrl+c` always quits, so another action can't have it, and `quit` needs a key of its own besides it.

Plugin keys can't take a key that an action or a bookmark digit uses: a plugin that asks for one is reported, and left in the Run palette without a key.

`sushi --list-keys` prints every action with its keys as they are now, as a `keys:` section to copy lines from, followed by the keys that can't be changed and the plugins' keys. It lists every problem with the config on stderr, and exits with status 1 if there are any.

These keys don't change:

- `ctrl+c` quits from anywhere, stopping a running operation first, as `q` does.
- `1`-`9` jump to bookmarks, unless an action has taken the digit.
- Confirmations take `y` or `Enter` to go ahead, and `n`, `Esc` or `q` to cancel.
- Typing in the search (`/`), in prompts and in the Find palette goes into the text. Search moves between matches with `↑` and `↓`, and the Find palette has its own keys (see [Search](#search)).
- Dialogs close with `Esc` and act with `Enter`; the Run palette switches with `Tab` and `Shift+Tab`, and the sort menu sorts with its letters `n`, `s`, `m` and `t`. Otherwise they follow the actions: the `up` and `down` keys move in the bookmark list, the sort menu and the Run palette, `delete` removes a bookmark, `reverse` reverses the sort, and `shell` switches the Run palette to its command line.
- The key panel closes with `Esc`, the `help` key and the `quit` key it lists, and scrolls with the `up` and `down` keys.

The actions, with their default keys as the config writes them:

| Action | Default keys | What it does |
| ------ | ------------ | ------------ |
| `up` | `up`, `k` | Move up |
| `down` | `down`, `j` | Move down |
| `left` | `left`, `h` | Go to the parent directory |
| `right` | `right`, `l` | Enter a directory, or open a file |
| `enter` | `enter` | Enter a directory, or open a file |
| `back` | `backspace` | Go to the parent directory |
| `page_up` | `pgup`, `ctrl+u` | Page up |
| `page_down` | `pgdown`, `ctrl+d` | Page down |
| `home` | `home`, `g` | Go to the first file |
| `end` | `end`, `G` | Go to the last file |
| `delete` | `d` | Move to the trash |
| `edit` | `e` | Edit in `$VISUAL` / `$EDITOR` |
| `open` | `o` | Open with the default app |
| `rename` | `r` | Rename |
| `new_file` | `n` | New file |
| `new_dir` | `N` | New directory |
| `select` | `space` | Select the file and move down |
| `invert` | `"*"` | Invert the selection |
| `unselect` | `u` | Clear the selection |
| `copy` | `c` | Copy to the clipboard |
| `cut` | `x` | Cut to the clipboard |
| `paste` | `v` | Paste into the current directory |
| `hard_delete` | `D` | Delete permanently |
| `undo` | `ctrl+z` | Undo the last operation |
| `cancel` | `ctrl+x` | Cancel the operation running in the background |
| `duplicate` | `y` | Duplicate |
| `paste_link` | `V` | Paste as symbolic links |
| `chmod` | `m` | Change permissions |
| `bulk_rename` | `R` | Bulk rename in the editor |
| `archive` | `a` | Compress into a `.zip` |
| `extract` | `X` | Extract archives |
| `search` | `"/"` | Fuzzy search in the current directory |
| `bookmark` | `b` | Open the bookmarks |
| `add_bookmark` | `B` | Bookmark the current directory |
| `quit` | `q`, `ctrl+c` | Quit |
| `help` | `"?"` | Show the key panel |
| `preview_up` | `K` | Scroll the preview up |
| `preview_down` | `J` | Scroll the preview down |
| `plugins` | `P` | Open the Run palette on the plugins |
| `shell` | `"!"` | Open the Run palette to type a shell command |
| `preview` | `p` | Toggle the preview pane |
| `hidden` | `"."` | Toggle hidden files |
| `new_tab` | `t` | New tab in the current directory |
| `new_tab_home` | `T` | New tab in the home directory |
| `next_tab` | `tab` | Next tab |
| `prev_tab` | `shift+tab` | Previous tab |
| `close_tab` | `ctrl+w` | Close the tab (quits on the last one) |
| `refresh` | `ctrl+r` | Reload every tab |
| `sort` | `s` | Open the sort menu |
| `reverse` | `S` | Reverse the sort order |
| `find` | `f` | Find files by name below this directory |
| `grep` | `F` | Find text in the files below this directory |
| `quit_no_cd` | `Q` | Quit without changing the shell's directory |

## Layout

| Terminal width | Panes shown |
| -------------- | ----------- |
| 100 columns or more | Parent folder, files, preview |
| 72 to 99 columns | Files, preview |
| Under 72 columns | Files only |

Narrow file lists drop the date column first, then the size; sorted by one of those, the name's heading then says so, as in `Name (modified ↓)`. The status bar shows the current mode (`NORMAL`, `SELECT`, `SEARCH`, `FIND`, `SORT`, `CHMOD`, `ARCHIVE`, ...), then the latest message and the progress of any operation running in the background, then the number of items, their size, the selection and the clipboard, as far as they fit. The last line lists the keys that apply to the mode.

## Previews

The preview pane shows the file under the cursor. `J` and `K`, or the mouse wheel, scroll it.

| File | Preview |
| ---- | ------- |
| Text and code | Syntax highlighting and line numbers, for the first 2000 lines. Files over 10 MB show their details only |
| Folders | The first 200 entries, folders first and then by name, as the file list sorts them by name |
| Images (PNG, JPEG, GIF, WebP, BMP) | Drawn to fit the pane in 24-bit or 256 colors, with the format and size in pixels in the heading. GIFs show their first frame |
| Archives (`.zip`, `.jar`, `.tar`, `.tar.gz`/`.tgz`, `.tar.bz2`/`.tbz2`) | The entries with their sizes (the first 500), with the number of entries and the unpacked size in the heading. Nothing is extracted |
| PDF | The text of the first 5 pages, and the page count, from poppler's `pdftotext` and `pdfinfo` |
| Symbolic links | The preview of what the link points to, with its target in the heading. A link without an extension is previewed by its target's name, so `latest` pointing to `photo.png` shows the picture |
| Other files | Type, size, permissions and modification date |

Images show their details instead of the picture in ASCII icon mode (`--ascii`), in terminals with fewer than 256 colors, and when larger than 50 megapixels. Without poppler (`brew install poppler`, `apt install poppler-utils`), PDFs show their details too. Image previews don't yet follow EXIF orientation, so some photos from phones show on their side; TIFF and animated GIFs aren't supported.

## Keybindings

These are the default keys. `keys:` in the config changes them (see [Remapping Keys](#remapping-keys)), and the key panel (`?`) and hint rows show them as they are.

### Navigation

| Key | Action |
| --- | ------ |
| `↑`/`k` | Move up |
| `↓`/`j` | Move down |
| `PgUp`/`Ctrl+u` | Page up |
| `PgDn`/`Ctrl+d` | Page down |
| `g`/`Home` | Go to first file |
| `G`/`End` | Go to last file |
| `←`/`h`, `Backspace` | Go to parent directory |
| `→`/`l`, `Enter` | Enter a directory, or open a file (see `opener`) |

### Files

Keys that act on files use the selection when there is one, and the file under the cursor otherwise.

| Key | Action |
| --- | ------ |
| `Space` | Select file and move down |
| `*` | Invert selection |
| `u` | Clear selection |
| `c` | Copy to clipboard |
| `x` | Cut to clipboard |
| `v` | Paste into current directory |
| `d` | Move to the trash, without asking (`Ctrl+z` brings it back; see [Trash and Undo](#trash-and-undo)) |
| `D` | Delete permanently. Always asks first |
| `r` | Rename |
| `n` | New file (a name ending in `/` makes a directory; `src/main.go` creates `src/` too) |
| `N` | New directory |
| `e` | Edit in `$VISUAL` / `$EDITOR` |
| `o` | Open with the default app |

Renaming a folder, or moving it with `x` and `v`, takes the tabs, bookmarks and selections inside it along. Files deleted by another program leave the selection when the list reloads. `v` asks before overwriting anything; if, while it asks, the folder is deleted, the tab moves elsewhere or other names would be overwritten, `y` pastes nothing and says why.

### Undo and Tools

| Key | Action |
| --- | ------ |
| `Ctrl+z` | Undo the last operation; press again to go further back (up to 20) |
| `Ctrl+x` | Cancel the operation running in the background |
| `y` | Duplicate beside the original, as `name copy.ext`, then `name copy 2.ext`; duplicating `name copy.ext` makes `name copy 2.ext` too |
| `V` | Paste the clipboard as symbolic links to its files. Never replaces anything |
| `m` | Change permissions (not recursively): a prompt shows the current mode, such as `644`; type 3 or 4 octal digits. Several items all get the mode typed, and the prompt starts empty when theirs differ. Symlinks are left as they are, as is what they point to. Not available on Windows |
| `R` | Bulk rename the selection in `$VISUAL` / `$EDITOR`. Without a selection, the same as `r` |
| `a` | Compress into a new `.zip`, asking for its name |
| `X` | Extract `.zip`, `.tar`, `.tar.gz` and `.tgz` archives, each into a new folder named after it |

Undo, duplicate, compress and extract run in the background, like copying; see [Background Operations](#background-operations).

`R` opens the selected names in your editor, one per line. Change the names, save and close the editor: sushi checks every new name before renaming anything, so a mistake renames nothing, and names can be swapped or rotated. With an editor that opens a window, make it wait for the file to close, as in `EDITOR="code --wait"`.

`a` refuses a name that is already taken. `X` extracts `photos.zip` into a new folder `photos`, or `photos 2` if that is taken, never overwrites anything, and refuses entries and links that would reach outside that folder, directly or through other links in the archive. An archive refused part way, as a `.tar` can only be once some of it is written, leaves nothing behind: the new folder is removed.

### Search

| Key | Action |
| --- | ------ |
| `/` | Fuzzy search: the list narrows to the matches (`↑`/`↓` between them, `Enter` to keep, `Esc` to cancel) |
| `f` | Find files and folders by name, in every folder below this one |
| `F` | Find text inside the files below this one |

`f` matches names fuzzily, like `/`, listing names that start with the query first; a query with a `/`, such as `src/main`, matches the path instead. `F` finds the lines containing the query, ignoring case unless the query has capitals. Both open the Find palette:

| Key | Action |
| --- | ------ |
| Typing | Search; results appear as they are found |
| `↑`/`↓`, `Ctrl+p`/`Ctrl+n`, `PgUp`/`PgDn` | Move through the results |
| `Enter` | Go to the result: its folder, with the cursor on it. For `F`, the preview shows the matching line |
| `Tab` | Switch between name and text search, keeping the query |
| `Esc` | Close |

Hidden files are searched when they are shown (`.`). `.git`, `node_modules` and `vendor` folders and binary files are skipped, symbolic links aren't followed, and at most 1000 results are listed. The preview holds only the first 2000 lines of a file, so it can't scroll to a match further down.

### View and Bookmarks

| Key | Action |
| --- | ------ |
| `p` | Toggle preview pane |
| `J` / `K` | Scroll the preview down / up |
| `.` | Toggle hidden files |
| `s` | Sort menu: by name, size, modified or type (press `n`, `s`, `m` or `t`, or `j`/`k` and `Enter`) |
| `S` | Reverse the sort order |
| `Ctrl+r` | Reload every tab |
| `b` | Open bookmarks (`j`/`k` to move, `Enter` to go, `d` to delete, `Esc` to close) |
| `B` | Bookmark current directory |
| `1`-`9` | Jump to bookmark |

The sort order applies to every tab for the rest of the session; `sort_by` and `sort_reverse` in the config set the order sushi starts with. A newly chosen order starts in its usual direction: size largest first, modified newest first.

Tabs also reload by themselves when files change on disk, once the changes pause for 200 ms, or every 2 seconds while they keep coming, keeping the cursor and the selection. If a tab's directory is deleted, the tab moves up to the nearest directory that still exists. Directories that can't be watched, such as on some network drives, reload only with `Ctrl+r`. On macOS and the BSDs, watching a directory holds a file open for each of its entries, and sushi keeps to 2048 in all, so very large directories there reload only with `Ctrl+r` too. `watch: false` turns watching off.

### Plugins

| Key | Action |
| --- | ------ |
| `P` | Open the Run palette on the plugin list |
| `!` | Open the Run palette to type a shell command |

In the palette, `Tab` switches between the command line and the plugin list.

Plugins can also have their own keys. See [docs/plugins.md](docs/plugins.md).

### Tabs

| Key | Action |
| --- | ------ |
| `t` | New tab in current directory |
| `T` | New tab in home directory |
| `Tab` / `Shift+Tab` | Next / previous tab |
| `Ctrl+w` | Close tab (quits on the last one) |

### General

| Key | Action |
| --- | ------ |
| `?` | Show the key panel. `Esc`, `?` and `q` only close it (`Ctrl+c` still quits), and `j`/`k` scroll it when it doesn't fit; any other key closes it and does its job |
| `q`/`Ctrl+c` | Quit. With [`sushicd`](#changing-directory-on-quit), the shell changes to the current directory |
| `Q` | Quit without changing the shell's directory |

## Mouse

| Action | Effect |
| ------ | ------ |
| Click a file | Move the cursor to it |
| Double-click a file | Open it, as `Enter` does |
| Right-click or `Ctrl`+click a file | Select or deselect it |
| Click in the parent pane | Go to that folder; for a file, go to its folder with the cursor on it |
| Click the parent pane's heading | Go up |
| Click a tab | Switch to it |
| Wheel over the file list / preview | Move the cursor / scroll the preview, 3 rows at a time |
| Click / double-click in Bookmarks or the Run palette | Pick a row / go there or run it |
| Click the command line in the Run palette | Type a shell command |
| Click outside Bookmarks or the Run palette | Close it |
| Wheel / click on the key panel | Scroll it / close it |

While searching with `/`, clicks and the wheel move between the matches, and a double-click keeps the match and opens it. Prompts, confirmations, the sort menu and the Find palette ignore the mouse. Terminals don't pass `Cmd`-clicks on to programs.

While sushi uses the mouse, terminals select text only with a key held: `Shift` in most, `Option` in iTerm2. In macOS Terminal, `Cmd+R` (View > Allow Mouse Reporting) switches the mouse between sushi and the terminal. Set `mouse: false` to leave the mouse to the terminal for good.

## Trash and Undo

`d` moves files to the trash without asking, since `Ctrl+z` brings them back. `D` deletes permanently and always asks first, even with `confirm_delete: false`. With `delete_to_trash: false`, `d` deletes permanently too, asking first unless `confirm_delete` is off.

| System | Trash |
| ------ | ----- |
| macOS | `~/.Trash`, the Trash in the Dock |
| Linux and BSD | The freedesktop.org trash in `$XDG_DATA_HOME/Trash`, or `~/.local/share/Trash`, so desktop file managers can show and restore what sushi trashed |
| Windows | Sushi's own trash in `%AppData%\sushi\Trash`. The Recycle Bin doesn't show it: restore with `Ctrl+z`, or by moving files out of its `files` folder |

Nothing in the trash is ever replaced or merged into: a name that is taken gets a number, as in `notes 2.txt`, even when another program trashes something of the same name at the same moment. Files on another drive are copied into the trash and then deleted, which takes longer; only what was copied is deleted, so files added meanwhile stay where they were. The Finder's Put Back doesn't know where files trashed by sushi came from; use `Ctrl+z` instead.

`Ctrl+z` undoes the last of up to 20 operations: trash, rename, move, copy, new file or folder, duplicate, symlink paste, permissions, bulk rename, compress and extract. Undo history lasts until sushi quits.

- Undo never replaces anything. If something now sits where a file would go back, sushi says so and keeps that step, so you can move it out of the way and press `Ctrl+z` again.
- Undoing an operation that created files, such as a copy, removes only what it created, and only if it hasn't changed since. Anything that isn't empty goes to the trash rather than being deleted, unless `delete_to_trash` is off; then a copy whose original is gone or has changed is kept, as it may be the only one left.
- Permanent deletes can't be undone, and neither can files a paste overwrote. Undo stops there: what came before may depend on them, so nothing older can be undone.

## Background Operations

Copy, move, delete, trash, duplicate, compress, extract and undo run in the background, one at a time, with their progress in the status bar:

```text
Copying 3/120 files 45% ████░░░░░░
```

Where the status bar is short of room, the progress drops its bar, as in `Copying 3/120 45%`. You can keep browsing meanwhile. Keys that change files are refused until the operation finishes or you cancel it with `Ctrl+x`, with a message beside the progress (`Still copying: wait, or ctrl+x to cancel`), and so are plugins, the Run palette and opening files in other programs. A cancelled copy removes only the file it was in the middle of; the files already copied stay. `q`, `Q` and closing the last tab stop a running operation before quitting; press the key again to quit at once, and sushi still waits a few seconds for the operation to clean up. Files are copied, and zips written, under a hidden `.sushi-partial-` name and renamed once complete, so nothing half-written ever has its real name; if sushi is killed, what it leaves is removed when its folder is listed a day later. Copies keep their permissions and modification times.

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

- **poppler** (optional) - `pdftotext` and `pdfinfo` enable PDF text previews: `brew install poppler` on macOS, `apt install poppler-utils` on Debian and Ubuntu

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
│   ├── fs/          # File system scanning and operations, trash and archives
│   ├── opener/      # Editor and default-app commands
│   ├── plugins/     # Plugin loading and running
│   ├── search/      # Finding files by name and content below a directory
│   ├── shell/       # The sushicd shell functions
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
- [x] macOS app
- [x] Trash and undo
- [x] Background operations with progress and cancel
- [x] Duplicate, symlink paste, permissions, bulk rename, zip and extract
- [x] Image, archive and PDF previews
- [x] Mouse support
- [x] Recursive search by name and content
- [x] Automatic refresh when files change on disk
- [x] Sort menu
- [x] Change the shell's directory on quit
- [x] Customizable keybindings
- [ ] Test on Windows, and use the Recycle Bin there rather than sushi's own trash
- [ ] Signed and notarized macOS builds, so other Macs open them without a warning
- [ ] Follow EXIF orientation in image previews, and preview TIFF and animated GIFs
- [ ] Per-volume trashes (`.Trashes` on macOS, `.Trash-$uid` on Linux), so trashing on another drive doesn't copy
- [ ] Extract more formats, such as `.tar.bz2`, `.tar.xz` and `.7z`

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

MIT License - see LICENSE file for details

## Acknowledgments

- Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea)
- Styled with [Lip Gloss](https://github.com/charmbracelet/lipgloss)
- Inspired by ranger, nnn, and lf
