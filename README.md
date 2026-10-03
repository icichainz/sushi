# 🍣 Sushi

A fast and elegant terminal-based file explorer written in Go.

Sushi is made for macOS, the only system it supports: it still builds on Linux, but without its Finder, Quick Look and Spotlight features.

## Features

- 🚀 Fast, asynchronous navigation with Vim-style keybindings
- 🗂️ Three panes (parent folder, files, preview) that adapt to the terminal width, with tabs
- 👁️ Scrollable preview with syntax highlighting, line numbers and file details
- 🖼️ Image, archive and PDF previews: pictures drawn right in the terminal
- 👀 Quick Look, as the space bar in Finder
- 📝 Open files in your editor, their default app, or an app you pick; reveal them in Finder
- 🔍 Fuzzy search within the current directory
- 🔎 Find files by name, by Finder tag, or by text inside them, below the current folder or, with Spotlight, everywhere
- 🏷️ Finder tags: see their colors in the list, and add or remove them
- 🌿 Git status badges and the branch, in repositories
- ✅ Multi-file selection
- 📋 Copy, cut, paste, delete, rename and create, with safeguards against overwriting a file with itself; copy and paste files between sushi and Finder
- 🗑️ Trash and undo (`Ctrl+z`), with progress and cancel (`Ctrl+x`) for long operations, and a notification when they finish
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
| `dist/Sushi-0.3.0.pkg` | Installer: puts Sushi in Applications and the `sushi` command in `/usr/local/bin` |
| `dist/Sushi-0.3.0.dmg` | Disk image: drag Sushi to Applications |

`make app`, `make pkg` and `make dmg` build them one at a time, and `VERSION=1.2.3 make macos` sets the version.

The app runs sushi through your login shell, so plugins and `$EDITOR` work as they do in a terminal. Each window runs its own sushi and is named after the folder it shows. `Cmd+N` opens a new window at your home folder, `Cmd+W` closes one, and quitting sushi with `q` closes its window; the app quits with its last window. `Cmd` `+` and `Cmd` `-` change the text size in every window.

**Opening folders from Finder.** Drop a folder on Sushi's icon in the Dock, or choose Open With > Sushi, and it opens in a new window. From a terminal, `open -a Sushi ~/projects` does the same. A file opens the folder it is in, with the cursor on the file.

Finder also offers **Open in Sushi** for folders: right-click a folder and look under Services (or Quick Actions). If it isn't listed, turn it on in System Settings > Keyboard > Keyboard Shortcuts > Services, under Files and Folders; after installing, it may only appear after logging out and back in.

**Notifications.** When a copy, move, delete or other job that took more than a few seconds (`notify_after` in the config) finishes while you're in another app or another Sushi window, Sushi shows a notification; click it to go back to that window. The first time, macOS asks whether Sushi may send notifications; change that later in System Settings > Notifications > Sushi. With notifications turned off, the Dock icon bounces instead.

`Cmd+C` and `Cmd+V` copy and paste text in the app, as in a terminal. To copy files, use `c`, `x` and `v`, which share them with Finder (see [Finder Pasteboard](#finder-pasteboard)).

The mouse works in the app as in a terminal (see Mouse below), which means dragging selects files rather than text. Hold `Shift` while dragging to select text, or set `mouse: false` in the config.

The builds are signed ad hoc, which is enough for the Mac that built them. On another Mac, macOS will refuse to open them until they are allowed under System Settings, Privacy & Security. Distributing without that warning needs an Apple Developer ID: build with `SIGN_IDENTITY="Developer ID Application: ..."` and notarize the result.

## Usage

```bash
# Open in current directory
sushi

# Open specific directory
sushi /path/to/directory

# Open a file's folder, with the cursor on the file
sushi ~/Downloads/report.pdf

# Use ASCII icons (no Nerd Font required)
sushi --ascii

# Combine options
sushi --ascii ~/projects
```

Given a file, sushi opens the folder it is in with the cursor on it, and with hidden files shown if it is a dotfile. Given a path that doesn't exist, it opens the nearest folder above it that does, and says so in the status bar.

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
| `--version` | Print the version, as in `sushi 0.3.0` (`sushi dev` when built without the Makefile) |
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

# Finder tags: dots of their colors after names, "L" to change them and
# "#" to find files by tag. Set to false to hide them and unbind both keys.
tags: true

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

# Share the files you copy or cut with Finder through the macOS pasteboard,
# and paste the files Finder and other apps put there
pasteboard: true

# In Git repositories, a column of status badges and the branch in the
# breadcrumb
git: true

# A background operation (copy, move, trash, compress...) that runs longer
# than this tells the terminal, or the Sushi app, when it finishes; see
# Notifications below. A duration such as 5s or 2m; 0 turns this off.
notify_after: 5s

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

Color names for `colors`: `header_fg`, `text`, `muted`, `faint`, `raised`, `directory`, `cursor_fg`, `cursor_bg`, `bar_fg`, `bar_bg`, `tab_bar_bg`, `tab_active_fg`, `tab_inactive_fg`, `tab_inactive_bg`, `accent`, `border`, `title`, `highlight`, `danger`, `selected`, `git_modified`, `git_added`, `git_untracked`. Finder tags keep Finder's colors in every theme.

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
- Typing in the search (`/`), in prompts, in the Find palette and in the tag picker goes into the text. Search moves between matches with `↑` and `↓`, and the Find palette and the tag picker have their own keys (see [Search](#search) and [Finder Tags](#finder-tags)).
- Dialogs close with `Esc` and act with `Enter`; the Run palette switches with `Tab` and `Shift+Tab`, and the sort menu sorts with its letters `n`, `s`, `m` and `t`. Otherwise they follow the actions: the `up` and `down` keys move in the bookmark list, the sort menu, the Run palette and the Open with list, `home` and `end` go to the first and last app in the Open with list, `delete` removes a bookmark, `reverse` reverses the sort, and `shell` switches the Run palette to its command line.
- The key panel closes with `Esc`, the `help` key and the `quit` key it lists, and scrolls with the `up` and `down` keys.

With `tags: false` in the config, `tag` and `find_tag` have no keys, whatever `keys:` says.

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
| `open_with` | `O` | Open with an app picked from those that can |
| `reveal` | `ctrl+o` | Show in Finder |
| `quick_look` | `i` | Show in Quick Look |
| `rename` | `r` | Rename |
| `new_file` | `n` | New file |
| `new_dir` | `N` | New directory |
| `select` | `space` | Select the file and move down |
| `invert` | `"*"` | Invert the selection |
| `unselect` | `u` | Clear the selection |
| `copy` | `c` | Copy to the clipboard, and the pasteboard |
| `cut` | `x` | Cut to the clipboard, and the pasteboard |
| `paste` | `v` | Paste into the current directory, from the pasteboard if it is newer |
| `hard_delete` | `D` | Delete permanently |
| `undo` | `ctrl+z` | Undo the last operation |
| `cancel` | `ctrl+x` | Cancel the operation running in the background |
| `duplicate` | `y` | Duplicate |
| `paste_link` | `V` | Paste as symbolic links |
| `chmod` | `m` | Change permissions |
| `bulk_rename` | `R` | Bulk rename in the editor |
| `archive` | `a` | Compress into a `.zip` |
| `extract` | `X` | Extract archives |
| `tag` | `L` | Change Finder tags |
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
| `find_tag` | `"#"` | Find files by Finder tag below this directory |
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

Images show their details instead of the picture in ASCII icon mode (`--ascii`), in terminals with fewer than 256 colors, and when larger than 50 megapixels. Without poppler (`brew install poppler`, `apt install poppler-utils`), PDFs show their details too. Image previews don't yet follow EXIF orientation, so some photos from phones show on their side; TIFF and animated GIFs aren't supported. For those, and anything else the pane can't show, `i` opens [Quick Look](#quick-look).

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
| `c` | Copy to clipboard, and to the pasteboard for Finder (see [Finder Pasteboard](#finder-pasteboard)) |
| `x` | Cut to clipboard, and to the pasteboard for Finder |
| `v` | Paste into current directory: files copied in Finder or another app since, or else the clipboard |
| `d` | Move to the trash, without asking (`Ctrl+z` brings it back; see [Trash and Undo](#trash-and-undo)) |
| `D` | Delete permanently. Always asks first |
| `r` | Rename |
| `n` | New file (a name ending in `/` makes a directory; `src/main.go` creates `src/` too) |
| `N` | New directory |
| `e` | Edit in `$VISUAL` / `$EDITOR` |
| `o` | Open with the default app |
| `O` | Open with an app picked from a list (see [Open With and Reveal in Finder](#open-with-and-reveal-in-finder)) |
| `Ctrl+o` | Show in Finder |
| `i` | Show in Quick Look; `i` again closes it (see [Quick Look](#quick-look)) |
| `L` | Change Finder tags (see [Finder Tags](#finder-tags)) |

Renaming a folder, or moving it with `x` and `v`, takes the tabs, bookmarks and selections inside it along. Files deleted by another program leave the selection when the list reloads. `v` asks before overwriting anything; if, while it asks, the folder is deleted, the tab moves elsewhere or other names would be overwritten, `y` pastes nothing and says why.

### Undo and Tools

| Key | Action |
| --- | ------ |
| `Ctrl+z` | Undo the last operation; press again to go further back (up to 20) |
| `Ctrl+x` | Cancel the operation running in the background |
| `y` | Duplicate beside the original, as `name copy.ext`, then `name copy 2.ext`; duplicating `name copy.ext` makes `name copy 2.ext` too |
| `V` | Paste symbolic links to the files `v` would paste. Never replaces anything |
| `m` | Change permissions (not recursively): a prompt shows the current mode, such as `644`; type 3 or 4 octal digits. Several items all get the mode typed, and the prompt starts empty when theirs differ. Symlinks are left as they are, as is what they point to |
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
| `#` | Find files and folders by Finder tag, below this one (see [Finder Tags](#finder-tags)) |

`f` lists names that start with the query first, then names that contain it, then fuzzy matches like those of `/`; a query with a `/`, such as `src/main`, matches the path instead. `F` finds the lines containing the query, ignoring case unless the query has capitals. All three open the Find palette, whose heading says what it looks for and where, as in `Find files below this folder`:

| Key | Action |
| --- | ------ |
| Typing | Search; results appear as they are found |
| `↑`/`↓`, `Ctrl+p`/`Ctrl+n`, `PgUp`/`PgDn` | Move through the results |
| `Enter` | Go to the result: its folder, with the cursor on it. For `F`, the preview shows the matching line |
| `Tab` | Switch between name and text search, keeping the query |
| `Ctrl+e` | Switch between searching below this folder and everywhere Spotlight looks, keeping the query |
| `Esc` | Close |

Hidden files are searched when they are shown (`.`). `.git`, `node_modules` and `vendor` folders are skipped, and so are binary files, except documents Spotlight has read the text of. Symbolic links aren't followed, and at most 1000 results are listed. The preview holds only the first 2000 lines of a file, so it can't scroll to a match further down.

#### Spotlight

Below a folder, `f`, `F` and `#` walk it, reading every name and file, so they find everything there, indexed by Spotlight or not: hidden folders when they are shown, folders kept out of Spotlight, files too new to be indexed yet, fuzzy matches, and the text of source code, YAML and Makefiles, which Spotlight doesn't read. Spotlight (`mdfind`) searches alongside: it answers from its index at once, where walking a large folder takes a while, so what it finds shows first, and the walk adds the rest; each result is listed once. For `F`, Spotlight adds only the documents whose text a walk can't read, such as PDFs: they are listed as files, without a line, and the palette's footer says so after where it looked, as in `in ~/projects · Spotlight`. Spotlight is stopped once the walk is done, or for `F`, 4 seconds after; if it is missing or fails, the walk's results are all there is.

`Ctrl+e` searches everywhere Spotlight looks: every volume it indexes, with results outside the folder shown by their full path. Only Spotlight can do that, and it trades some results for speed:

- A name search finds names that contain the query, ignoring case. Fuzzy matches, such as `mgo` for `main.go`, come only from a walk.
- What Spotlight hasn't indexed is left out: hidden and excluded folders, files too new to be indexed yet, and for `F`, text Spotlight doesn't read, such as source code. In the text files it did read, every line that contains the query is listed, as with a walk.
- Without `mdfind`, the palette says `Spotlight isn't available, so only this folder can be searched`. A search Spotlight hasn't finished after 30 seconds gives up, and says so.

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

Tabs also reload by themselves when files change on disk, once the changes pause for 200 ms, or every 2 seconds while they keep coming, keeping the cursor and the selection. If a tab's directory is deleted, the tab moves up to the nearest directory that still exists. Directories that can't be watched, such as on some network drives, reload only with `Ctrl+r`. On macOS, watching a directory holds a file open for each of its entries, and sushi keeps to 2048 in all, so very large directories reload only with `Ctrl+r` too. `watch: false` turns watching off.

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
| Click / double-click in Bookmarks, the Run palette, the Find palette or the Open with list | Pick a row / go there, run it or open with that app |
| Click the command line in the Run palette | Type a shell command |
| Click an order in the sort menu | Sort by it |
| Click a tag / the field in the tag picker | Tick or untick it / type a new tag |
| Wheel while a dialog is open | Move through its rows |
| Click outside a dialog | Close it |
| Wheel / click on the key panel | Scroll it / close it |

While searching with `/`, clicks and the wheel move between the matches, and a double-click keeps the match and opens it. Prompts and confirmations ignore the mouse. Terminals don't pass `Cmd`-clicks on to programs.

While sushi uses the mouse, terminals select text only with a key held: `Shift` in most, `Option` in iTerm2. In macOS Terminal, `Cmd+R` (View > Allow Mouse Reporting) switches the mouse between sushi and the terminal. Set `mouse: false` to leave the mouse to the terminal for good.

## Git

In a Git repository, the file list has a narrow column after the selection marker with each entry's status:

| Badge | Meaning |
| ----- | ------- |
| `M` | Modified. On a folder: something inside is changed |
| `A` | Added to the index, or copied |
| `R` | Renamed |
| `D` | Deleted from the index but still on disk, as after `git rm --cached` |
| `?` | Untracked. On a folder: all that is new inside is untracked, as in a new folder |
| `!` | Ignored. The row is drawn faint, as is everything in an ignored folder |
| `U` | In conflict. On a folder: a conflict inside |

An entry with more than one status shows the strongest, from `U` down: `U`, `R`, `A`, `D`, `M`, `?`, `!`. The badges take the theme's colors `git_modified` (`M`), `git_added` (`A` and `R`), `git_untracked` (`?`) and `danger` (`D` and `U`).

The breadcrumb shows the branch on the right, before the sort order: `⎇ main` (`git:main` with `--ascii`), or the commit for a detached HEAD, as in `⎇ (1a2b3c4)`, with a `*` once anything in the repository is changed or untracked. Where the row is tight, the branch takes the sort order's place, and is shortened, or left out, so that the current folder's name stays whole.

Sushi runs `git status` in the background each time a folder loads, which includes the reloads when files change and `Ctrl+r`. It also watches the repository's `HEAD`, index and branches, so a commit, checkout or `git add` in another terminal updates the badges and the branch at once, without reloading the list. It only reads: with `GIT_OPTIONAL_LOCKS=0`, git doesn't refresh the index, so sushi never writes to the repository or takes a lock that git in another terminal could trip over. A folder that git takes more than 2 seconds to read, or where git fails, is given up on until you move to another folder or press `Ctrl+r`. Sushi ignores the `GIT_` variables that point git at a repository, as `GIT_DIR` and `GIT_WORK_TREE` do in a shell started by a git hook, so every folder shows as what it is.

Browsing a repository never runs a program the repository names. Its own configuration, which comes along when a folder is downloaded, AirDropped or unpacked, can name programs for git to run: a file system monitor (`core.fsmonitor`) and filters (`filter.<name>.clean`, `smudge` and `process`), which `git status` runs on files to compare them. So sushi always runs git with the file system monitor and hooks off, and doesn't look inside the work trees of submodules, which have configurations of their own: a submodule shows `M` once its checked-out commit changes, and the changes inside it show once you go in. In a repository whose own configuration (`.git/config`, the files it includes, or a work tree's `config.worktree`) sets `core.fsmonitor`, a filter, `core.sshCommand` or a credential helper, sushi doesn't run `git status` at all: the breadcrumb shows the branch, without the `*` as the changes aren't known, there are no badges, and the status bar says once `Git badges off for this repository: it configures filters/fsmonitor; see README`. Reading the configuration and the branch runs nothing. The same settings in your own `~/.gitconfig` don't count, so Git LFS installed for your account (`git lfs install`) keeps the badges; installed for one repository (`git lfs install --local`), it turns them off there.

`git: false` in the config turns the badges and the branch off.

## Finder Tags

Finder tags show after the name as a dot for each of their colors, then `○` for tags without a color (`*` and `o` with `--ascii`). They keep Finder's colors in every theme. Dots that don't fit are left out, rather than leave the name fewer than 4 columns. Old-style color labels show as tags of their color too, and are kept in step when tags change. As in Finder, a symbolic link has tags of its own, apart from what it points to. Copies, duplicates and moves to another drive keep the tags. On network volumes (SMB, AFP, NFS) the list shows no dots, as reading the tags would take a round trip to the server for every file; `L` still shows and changes them.

`L` opens the tag picker for the selection, or the file under the cursor. It lists Finder's seven colors, then the other tags used in the folder, each marked `[x]` if every file has it, `[-]` if only some do, and `[ ]` if none do, with a field below for a new tag:

| Key | Action |
| --- | ------ |
| `Space` / `Enter` | Tick or untick the tag. On `[-]`, it gives the tag to the files that don't have it |
| `↑`/`↓`, `Ctrl+p`/`Ctrl+n`, and on the list `k`/`j` (the up and down keys, as remapped) | Move |
| `Tab` | Switch between the list and the field |
| Typing | Type in the field, from anywhere in the list but for the up and down keys |
| `Enter` in the field | Add the tag typed. With the field empty, close |
| `Esc` | Close |

The mouse works too: click a tag to tick it, the field to type in it, or outside to close. Changes are written at once, as in Finder. `Ctrl+z` afterwards undoes everything the picker changed, file by file; a file whose tags have changed again since is left as it is.

`#` opens the Find palette on every tagged file and folder below this one; type the start of a tag's name to narrow it down. In a name search (`f`), a query that starts with `#` or `tag:` looks for tags: `#Red` or `tag:red` finds entries with a tag whose name starts with `Red`, ignoring case, those with a tag of exactly that name first. Results show their tags' dots, as do those of a search by name. To find names that start with `#` or `tag:`, put a backslash first: `\#autosave#` finds `#autosave#.txt`. In a text search (`F`), `#` is just text.

`tags: false` in the config hides the dots, leaves `L` and `#` without keys, and makes `#` in the Find palette plain text.

## Quick Look

`i` shows the file under the cursor, or the selection, in Quick Look, as the space bar does in Finder: in a window in front of the terminal, opened with `qlmanage -p`. Closing the window brings you back to sushi. Back in the terminal, `i` closes the window, or on other files, shows those instead. Quitting sushi closes it too.

## Finder Pasteboard

`c` and `x` also put the files on the macOS pasteboard, so `Cmd+V` in Finder pastes them. Finder has no cut for files, so files cut in sushi paste in Finder as a copy; pasted in sushi with `v`, they still move.

`v` pastes the files Finder or another app has put on the pasteboard while sushi runs, and since sushi last put its own there, as a copy, asking before overwriting anything; the status bar says `Pasting 2 items copied in Finder`. Otherwise it pastes sushi's clipboard. What was on the pasteboard when sushi started, however long ago it was copied, is left alone. Files pasted from the pasteboard become sushi's clipboard, so `v` pastes them again. `V` takes the same files as `v`, and pastes symbolic links to them. Reading the files another app put on the pasteboard can make macOS ask whether to allow it. While sushi reads the pasteboard the status bar says `Reading the pasteboard…`; if a dialog opens meanwhile, the paste is cancelled, and the status bar says so.

Sushi reaches the pasteboard through `osascript`. Where it can't, as over SSH, copying says `Can't share the clipboard with Finder`, and the files stay in sushi's clipboard for `v`. `pasteboard: false` in the config leaves the pasteboard alone.

In the [macOS app](#macos-app), `Cmd+C` and `Cmd+V` remain the terminal's text copy and paste.

## Open With and Reveal in Finder

`O` lists the apps that can open the file under the cursor, or the selection's first file, as Finder's Open With menu does: each with the folder it is in, and the default app first, marked `●` (`*` with `--ascii`). `Enter` or a double-click opens every file with the app chosen, `↑`/`↓` and `g`/`G` move, and `Esc` closes the list. The apps are looked up once for each extension, and remembered until `Ctrl+r`; for folders, files without an extension, and files given an app of their own in Finder's Get Info, they are looked up each time.

`Ctrl+o` shows the file in Finder, selected in a window of its folder. With a selection, all of it is selected.

Sushi reaches macOS for these, and for the pasteboard, through `osascript` and `open`, which get 10 seconds each: one that takes longer, as when Launch Services or the pasteboard server hangs, is stopped, and the status bar says so.

## Trash and Undo

`d` moves files to the trash without asking, since `Ctrl+z` brings them back. `D` deletes permanently and always asks first, even with `confirm_delete: false`. With `delete_to_trash: false`, `d` deletes permanently too, asking first unless `confirm_delete` is off.

| System | Trash |
| ------ | ----- |
| macOS | `~/.Trash`, the Trash in the Dock |
| Linux | The freedesktop.org trash in `$XDG_DATA_HOME/Trash`, or `~/.local/share/Trash`, so desktop file managers can show and restore what sushi trashed |

Nothing in the trash is ever replaced or merged into: a name that is taken gets a number, as in `notes 2.txt`, even when another program trashes something of the same name at the same moment. Files on another drive are copied into the trash and then deleted, which takes longer; only what was copied is deleted, so files added meanwhile stay where they were. The Finder's Put Back doesn't know where files trashed by sushi came from; use `Ctrl+z` instead.

`Ctrl+z` undoes the last of up to 20 operations: trash, rename, move, copy, new file or folder, duplicate, symlink paste, permissions, bulk rename, compress, extract and Finder tags. Undo history lasts until sushi quits.

- Undo never replaces anything. If something now sits where a file would go back, sushi says so and keeps that step, so you can move it out of the way and press `Ctrl+z` again.
- Undoing an operation that created files, such as a copy, removes only what it created, and only if it hasn't changed since. Anything that isn't empty goes to the trash rather than being deleted, unless `delete_to_trash` is off; then a copy whose original is gone or has changed is kept, as it may be the only one left.
- Permanent deletes can't be undone, and neither can files a paste overwrote. Undo stops there: what came before may depend on them, so nothing older can be undone.

## Background Operations

Copy, move, delete, trash, duplicate, compress, extract and undo run in the background, one at a time, with their progress in the status bar:

```text
Copying 3/120 files 45% ████░░░░░░
```

Where the status bar is short of room, the progress drops its bar, as in `Copying 3/120 45%`. You can keep browsing meanwhile. Keys that change files are refused until the operation finishes or you cancel it with `Ctrl+x`, with a message beside the progress (`Still copying: wait, or ctrl+x to cancel`), and so are plugins, the Run palette, opening files in other programs (`e`, `o`, `O`), Quick Look (`i`), showing files in Finder (`Ctrl+o`) and changing Finder tags (`L`). A cancelled copy removes only the file it was in the middle of; the files already copied stay. `q`, `Q` and closing the last tab stop a running operation before quitting; press the key again to quit at once, and sushi still waits a few seconds for the operation to clean up. Files are copied, and zips written, under a hidden `.sushi-partial-` name and renamed once complete, so nothing half-written ever has its real name; if sushi is killed, what it leaves is removed when its folder is listed a day later. Copies keep their permissions, modification times, Finder tags and other extended attributes, as Finder's do; an attribute the destination can't take, as on a drive without them, is left behind.

### Notifications

An operation that runs longer than `notify_after` (5 seconds unless the config sets it; `0` turns this off) tells the terminal when it finishes, whether it succeeded, failed or was cancelled, so you hear of it while you work in another window. The notification says what the status bar says, as in `Copied: 120 items`, `Cancelled: copied 3 of 120 files` or `Moving failed: ...`. What you get depends on where sushi runs:

| Where sushi runs | What a long operation does when it finishes |
| ---------------- | ------------------------------------------- |
| Sushi.app | A macOS notification, when the Sushi window isn't the active one |
| iTerm2, WezTerm | A desktop notification (OSC 9) |
| kitty | A desktop notification (OSC 99) |
| Terminal.app | The bell, which Terminal can turn into a badge or a bounce of its Dock icon: Settings, Profiles, Advanced, Bell |
| tmux, screen, ssh and other terminals | The bell, which tmux passes on to the terminal it runs in |

Sushi tells them apart by `TERM_PROGRAM` (the Sushi app sets it to `Sushi`), and by `KITTY_WINDOW_ID` and `WEZTERM_PANE` where `TERM_PROGRAM` isn't set. Inside tmux or screen it always rings the bell, as they drop notifications unless set up to pass them through. macOS may ask the first time whether the terminal can post notifications, and iTerm2 has its own setting for them under Settings, Profiles, Terminal.

## Requirements

- **macOS 12 (Monterey) or later** - The only system sushi supports. It still builds and runs on Linux, but without Finder tags, Quick Look, Open With, Reveal in Finder, the pasteboard or Spotlight

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

- **git** (optional) - For the [Git](#git) badges and branch. It comes with Xcode's Command Line Tools (`xcode-select --install`), or `brew install git`

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
│   ├── git/         # Reading git status for the badges and the branch
│   ├── jxa/         # Running JavaScript for Automation with osascript
│   ├── notify/      # Telling the terminal, or the Sushi app, that an operation finished
│   ├── opener/      # Editor and default-app commands, Open With and Reveal in Finder
│   ├── pasteboard/  # Files on the macOS pasteboard
│   ├── plugins/     # Plugin loading and running
│   ├── search/      # Finding files by name, tag and content, with Spotlight or a walk
│   ├── shell/       # The sushicd shell functions
│   ├── tags/        # Reading and writing Finder tags
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
- [x] Quick Look
- [x] Git status badges and branch
- [x] Copy and paste files with Finder through the pasteboard
- [x] Open With and Reveal in Finder
- [x] Finder tags: show, change and find them
- [x] Search with Spotlight, below a folder or everywhere
- [ ] Signed and notarized macOS builds, so other Macs open them without a warning
- [x] Open a folder in sushi from Finder, multiple windows, and notifications when a long operation finishes
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
