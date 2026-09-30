# Plugins

Plugins let you run your own commands and scripts on the file under the cursor or on the selection. A plugin can be a one-line shell command in your config file, or a script dropped into the plugins directory.

Press `P` to open the Run palette and pick a plugin, or give a plugin its own key. Press `!` to open the palette on its command line and run a one-off shell command the same way; `Tab` switches between the two.

## Commands in the config file

Add plugins to `~/.config/sushi/config.yaml`:

```yaml
plugins:
  - name: git-log
    key: ctrl+l
    command: git log --oneline -20
    description: Recent commits

  - name: sizes
    mode: background
    command: du -shc "$@" | tail -n 1

  - name: lazygit
    key: L
    mode: terminal
    command: lazygit
```

| Field | Required | Meaning |
|-------|----------|---------|
| `name` | yes | Shown in the Run palette and in messages |
| `command` | yes | Run with `sh -c`; `"$@"` is the selection |
| `key` | no | Shortcut, e.g. `Z`, `ctrl+l`, `alt+x`, `f5` |
| `mode` | no | `wait` (default), `terminal` or `background` |
| `description` | no | Shown in the Run palette |

## Scripts in the plugins directory

Any executable file in `~/.config/sushi/plugins/` becomes a plugin, named after the file without its extension. Remember to `chmod +x` it; sushi warns at startup about scripts that aren't executable.

A script can set its key, mode and description in comments near the top:

```sh
#!/bin/sh
# sushi-key: ctrl+g
# sushi-mode: wait
# sushi-description: Show git status for the current directory

git -C "$SUSHI_DIR" status
```

If a config plugin and a script have the same name, the config plugin wins.

The [examples/plugins](../examples/plugins) directory has ready-made scripts. Copy or link them into place:

```bash
mkdir -p ~/.config/sushi/plugins
ln -s "$PWD"/examples/plugins/* ~/.config/sushi/plugins/
```

| Example | Key | What it does |
|---------|-----|--------------|
| `fzf-jump` | `ctrl+f` | Fuzzy-find a file below the current directory and jump to it (needs [fzf](https://github.com/junegunn/fzf)) |
| `git-status` | `ctrl+g` | Show `git status` |
| `disk-usage` | | Total size of the selection |
| `copy-path` | `Y` | Copy the selected paths to the system clipboard |

## What a plugin receives

- **Arguments:** the selected paths, or the file under the cursor if nothing is selected. In a config command they are `"$@"`, `"$1"` and so on.
- **Working directory:** the directory sushi is showing.
- **Environment variables:**

| Variable | Value |
|----------|-------|
| `SUSHI_DIR` | The current directory |
| `SUSHI_FILE` | The file under the cursor |
| `SUSHI_SELECTION` | The selected paths (or the file under the cursor), one per line |
| `SUSHI_CMD_FILE` | A file the plugin can write instructions to (see below) |

## Modes

| Mode | Behaviour | Good for |
|------|-----------|----------|
| `wait` | Sushi steps aside, the plugin runs in the terminal, then sushi waits for Enter so you can read the output | Commands that print something: `git status`, `ls -l` |
| `terminal` | Like `wait`, but returns to sushi as soon as the plugin exits | Interactive programs: `fzf`, `lazygit`, `htop` |
| `background` | Runs without leaving sushi; the last line of output is shown in the status bar | Quick actions: copying paths, measuring sizes |

After any plugin finishes, sushi reloads its tabs, so files the plugin created, moved or deleted show up straight away.

## Sending instructions back

A plugin can tell sushi what to do next by writing lines to the file named in `$SUSHI_CMD_FILE`:

| Instruction | Effect |
|-------------|--------|
| `cd PATH` | Go to `PATH`. If it is a file, go to its directory and put the cursor on it |
| `select PATH` | Add `PATH` to the selection. It must name something that exists: paths are checked, and one that doesn't exist is reported in the status bar rather than selected, so a delete never starts on a mistyped path |
| `status TEXT` | Show `TEXT` in the status bar |

Relative paths are relative to the directory the plugin ran in. The paths of `cd` and `select` are cleaned, as `filepath.Clean` does: `a/./b/../c` is `a/c`, and a trailing slash is dropped, so `select link/` selects the symbolic link `link` itself, not the folder it points to. For example, this is the heart of `fzf-jump`:

```sh
target=$(fzf) || exit 0
echo "cd $target" > "$SUSHI_CMD_FILE"
```

## Keys

Keys are written the way sushi receives them: a single character (`Z`, `%`, `#`), or a name such as `ctrl+g`, `alt+x` or `f5`. Plugins can't take over sushi's own keys: those of its actions, as remapped under `keys:` in the config (see [Remapping Keys](../README.md#remapping-keys)), and the digits `1`-`9`, which jump to bookmarks. If a plugin asks for one, or two plugins ask for the same key, sushi says so in the status bar at startup, and `sushi --list-keys` lists it; the plugin stays available from the Run palette, shown there without a key.

## Windows

Config commands run with `cmd /C` and should use the `%SUSHI_...%` environment variables rather than arguments. Script plugins need to be `.exe`, `.bat`, `.cmd` or `.com` files. The example scripts are for macOS and Linux.
