package app

import (
	"fmt"
	"io"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/icichainz/sushi/internal/config"
	"github.com/icichainz/sushi/internal/plugins"
	"github.com/icichainz/sushi/internal/utils"
)

// Keys are remapped under keys: in the config file, by action. Actions are
// named after the KeyMap fields in snake_case (HardDelete is hard_delete),
// so a new field can be remapped, listed and checked with no more work.

// alwaysQuit quits from anywhere, whatever the config binds, so no config
// and no open dialog can leave sushi without a way out
const alwaysQuit = "ctrl+c"

// keyAction is one action of the key map
type keyAction struct {
	name    string       // As written in the config, like "paste_link"
	binding *key.Binding // Points into the key map, so keys can be changed
}

// actions returns the actions of the key map in field order
func (k *KeyMap) actions() []keyAction {
	v := reflect.ValueOf(k).Elem()
	var out []keyAction
	for i := 0; i < v.NumField(); i++ {
		if b, ok := v.Field(i).Addr().Interface().(*key.Binding); ok {
			out = append(out, keyAction{snakeCase(v.Type().Field(i).Name), b})
		}
	}
	return out
}

// snakeCase turns a field name like PasteLink into paste_link
func snakeCase(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

// actionFor returns the name of the action bound to key s, or ""
func (k KeyMap) actionFor(s string) string {
	for _, a := range k.actions() {
		if slices.Contains(a.binding.Keys(), s) {
			return a.name
		}
	}
	return ""
}

// keyOwners maps every key the browser uses to what it does: the name of
// its action, or the bookmark a digit jumps to
func (k KeyMap) keyOwners() map[string]string {
	owners := make(map[string]string)
	for r := '1'; r <= '9'; r++ {
		owners[string(r)] = fmt.Sprintf("bookmark %c", r)
	}
	// Actions come first in the browser, so they win over the digits
	for _, a := range k.actions() {
		for _, s := range a.binding.Keys() {
			owners[s] = a.name
		}
	}
	return owners
}

// namedKeys are the keys Bubble Tea reports by name, like "enter", "ctrl+r"
// or "f5". Key types are small numbers: control codes, and negative
// numbers for the other keys.
var namedKeys = func() map[string]bool {
	names := make(map[string]bool)
	for t := tea.KeyType(-256); t < 256; t++ {
		switch s := (tea.Key{Type: t}).String(); s {
		case "", "runes", " ":
		default:
			names[s] = true
		}
	}
	return names
}()

// parseKey turns a key as written in the config into the form Bubble Tea
// reports it in, as tea.KeyMsg.String does: a character like "k" or "G", or
// a name like "enter", "ctrl+r" or "shift+tab", either after "alt+" if
// need be. The space bar is written "space"; names may be in capitals.
func parseKey(s string) (string, error) {
	alt := ""
	rest, ok := cutPrefixFold(s, "alt+")
	if ok {
		alt = "alt+"
	}
	if r, size := utf8.DecodeRuneInString(rest); size > 0 && size == len(rest) && r != utf8.RuneError {
		// Letters keep their case: shift+g arrives as G
		if unicode.IsPrint(r) {
			return alt + rest, nil
		}
		return "", fmt.Errorf("unknown key %q", s)
	}
	name := strings.ToLower(rest)
	switch {
	case name == "space":
		return alt + " ", nil
	case namedKeys[name]:
		return alt + name, nil
	}
	if base, ok := cutPrefixFold(rest, "shift+"); ok && utf8.RuneCountInString(base) == 1 {
		return "", fmt.Errorf("unknown key %q (shifted letters are capitals, like %q)", s, alt+strings.ToUpper(base))
	}
	return "", fmt.Errorf("unknown key %q", s)
}

// cutPrefixFold is strings.CutPrefix ignoring case, and only cuts if
// something is left
func cutPrefixFold(s, prefix string) (string, bool) {
	if len(s) > len(prefix) && strings.EqualFold(s[:len(prefix)], prefix) {
		return s[len(prefix):], true
	}
	return s, false
}

// keyName is how a key is shown and written in the config: as Bubble Tea
// names it, except for the space bar
func keyName(s string) string {
	switch s {
	case " ":
		return "space"
	case "alt+ ":
		return "alt+space"
	}
	return s
}

// loadKeyMap returns the default key map with the actions under keys: in
// the config bound to the keys given there, and a line for each problem:
// unknown actions, keys that can't be read, and keys that clash.
func loadKeyMap(remap map[string]config.KeyList) (KeyMap, []string) {
	k := DefaultKeyMap()
	actions := k.actions()
	byName := make(map[string]keyAction, len(actions))
	for _, a := range actions {
		byName[a.name] = a
	}

	var problems []string
	chosen := make(map[string]bool) // Actions the config binds
	for _, name := range slices.Sorted(maps.Keys(remap)) {
		a, ok := byName[name]
		if !ok {
			problems = append(problems, unknownAction(name, actions))
			continue
		}
		list := remap[name]
		if list == nil {
			problems = append(problems, fmt.Sprintf("keys: %s has no keys (write %s: [] to unbind it)", name, name))
			continue
		}
		var keys []string
		for _, s := range list {
			parsed, err := parseKey(s)
			if err != nil {
				problems = append(problems, fmt.Sprintf("keys: %s: %v", name, err))
				continue
			}
			if parsed == alwaysQuit && name != "quit" {
				problems = append(problems, fmt.Sprintf("keys: conflict: %q for %s is ignored, as it always quits", alwaysQuit, name))
				continue
			}
			if !slices.Contains(keys, parsed) {
				keys = append(keys, parsed)
			}
		}
		// With none of its keys usable, an action keeps its defaults
		// rather than being left without any
		if len(keys) == 0 && len(list) > 0 {
			continue
		}
		a.binding.SetKeys(keys...)
		chosen[name] = true
	}

	// A key bound to two actions stays with one of them, so what it does
	// is what the key panel says: an action the config binds wins over
	// one left at its defaults, then the first in the key map
	owner := make(map[string]string)
	for _, pass := range []bool{true, false} {
		for _, a := range actions {
			if chosen[a.name] != pass {
				continue
			}
			var kept []string
			for _, s := range a.binding.Keys() {
				if other, taken := owner[s]; taken {
					problems = append(problems, fmt.Sprintf("keys: conflict: %q is bound to both %s and %s; %s keeps it", keyName(s), other, a.name, other))
					continue
				}
				owner[s] = a.name
				kept = append(kept, s)
			}
			if len(kept) < len(a.binding.Keys()) {
				a.binding.SetKeys(kept...)
			}
		}
	}

	// ctrl+c quits whatever the config says (see handleKeyPress), so it is
	// always one of quit's keys: the quit action stops a running job first.
	// Quit still needs a key of its own, one the key panel can show.
	if len(k.Quit.Keys()) == 0 {
		problems = append(problems, fmt.Sprintf("keys: invalid: quit has no key; only %s quits", alwaysQuit))
	}
	if !slices.Contains(k.Quit.Keys(), alwaysQuit) {
		k.Quit.SetKeys(append(slices.Clone(k.Quit.Keys()), alwaysQuit)...)
	}

	// The digits jump to bookmarks, unless an action has taken one
	for r := '1'; r <= '9'; r++ {
		if name, ok := owner[string(r)]; ok {
			problems = append(problems, fmt.Sprintf("keys: conflict: %q for %s hides bookmark %c", string(r), name, r))
		}
	}

	// Dialogs check their own keys first, so an action's key that is one
	// of those does nothing there
	for _, d := range dialogKeys() {
		for _, name := range d.actions {
			for _, s := range byName[name].binding.Keys() {
				if does, ok := d.fixed[s]; ok {
					problems = append(problems, fmt.Sprintf("keys: conflict: %q for %s is ignored in %s, where it %s", keyName(s), name, d.name, does))
				}
			}
		}
	}
	return k, problems
}

// unknownAction describes an action name that isn't one, suggesting the
// right spelling if it is only written differently
func unknownAction(name string, actions []keyAction) string {
	squash := strings.NewReplacer("_", "", "-", "", " ", "")
	for _, a := range actions {
		if strings.EqualFold(squash.Replace(a.name), squash.Replace(name)) {
			return fmt.Sprintf("keys: unknown action %q (did you mean %s?)", name, a.name)
		}
	}
	return fmt.Sprintf("keys: unknown action %q (sushi --list-keys lists them)", name)
}

// dialogKey describes a dialog that follows some actions of the key map
// besides keys of its own, which it checks first
type dialogKey struct {
	name    string
	actions []string          // Actions it follows, like "up" and "down"
	fixed   map[string]string // Its own keys, with what they do there
}

// dialogKeys lists the dialogs that follow the key map. Esc and Enter keep
// their meaning in all of them, and the sort menu's letters are what it
// shows. The sort menu and the Run palette close on q only when no action
// there has taken q, so q is left out, as is the key panel's quit key,
// which belongs to the quit action.
func dialogKeys() []dialogKey {
	sortMenu := map[string]string{"esc": "closes it", "enter": "sorts"}
	for _, f := range sortFields {
		sortMenu[f.key] = "sorts by " + f.by
	}
	return []dialogKey{
		{"the key panel", []string{"up", "down", "help"}, map[string]string{"esc": "closes it"}},
		{"the bookmark list", []string{"up", "down", "delete"}, map[string]string{"esc": "closes it", "enter": "opens the bookmark"}},
		{"the sort menu", []string{"up", "down", "reverse"}, sortMenu},
		{"the Run palette", []string{"up", "down", "shell"}, map[string]string{
			"esc": "closes it", "enter": "runs the plugin", "tab": "switches to the command", "shift+tab": "switches to the command"}},
	}
}

// navKeys are the arrow and paging keys that actions accept besides a
// letter; labels show the letter
var navKeys = map[string]bool{"up": true, "down": true, "left": true, "right": true, "pgup": true, "pgdown": true, "home": true, "end": true}

// shownKey returns the key a binding is labelled with: its first, passing
// over arrow and paging keys, and ctrl+c, which quits everywhere anyway,
// if it has another. It is empty if unbound.
func shownKey(b key.Binding) string {
	keys := b.Keys()
	for _, s := range keys {
		if !navKeys[s] && s != alwaysQuit {
			return keyName(s)
		}
	}
	if len(keys) > 0 {
		return keyName(keys[0])
	}
	return ""
}

// modifiers splits a key into its modifiers and the key itself, as
// "ctrl+" and "u" for ctrl+u. The key itself can be a plus, as in alt++.
func modifiers(s string) (mods, base string) {
	if i := strings.LastIndex(s[:max(len(s)-1, 0)], "+"); i >= 0 {
		return s[:i+1], s[i+1:]
	}
	return "", s
}

// keysLabel labels related actions with a key each, joined by sep, as "j k"
// for down and up. A key with the same modifiers as the key before it is
// shortened, so page up and down read "ctrl+u d". A plain key after a
// modified one would then look shortened too, so such labels spell every
// key out and use commas, as "ctrl+n, k" does. Unbound actions are left out.
func keysLabel(sep string, bindings ...key.Binding) string {
	var keys []string
	for _, b := range bindings {
		if s := shownKey(b); s != "" {
			keys = append(keys, s)
		}
	}

	shorten := true
	for i := 1; i < len(keys); i++ {
		before, _ := modifiers(keys[i-1])
		mods, _ := modifiers(keys[i])
		ambiguous := before != "" && mods == ""
		if ambiguous || sep != " " && strings.Contains(keys[i-1]+keys[i], sep) {
			shorten, sep = false, ", "
		}
	}
	label := slices.Clone(keys)
	for i := 1; shorten && i < len(keys); i++ {
		before, _ := modifiers(keys[i-1])
		if mods, base := modifiers(keys[i]); mods != "" && mods == before {
			label[i] = base
		}
	}
	return strings.Join(label, sep)
}

// keyHint labels related actions with their keys for a hint row or the key
// panel. If only some of them are bound, their own descriptions replace
// desc, which describes them all. It has no key if none is bound.
func keyHint(desc string, bindings ...key.Binding) hint {
	var bound []string
	for _, b := range bindings {
		if shownKey(b) != "" {
			bound = append(bound, b.Help().Desc)
		}
	}
	if len(bound) < len(bindings) {
		desc = strings.Join(bound, ", ")
	}
	return hint{keysLabel(" ", bindings...), desc}
}

// WriteKeys writes every action with the keys cfg binds it to, as a keys:
// section for the config file, and then the keys that can't be changed and
// those of plugins. It returns every problem found with the config, its
// theme, keys and plugins, in the order sushi finds them at startup, where
// the status bar has room for only the first.
func WriteKeys(w io.Writer, cfg *config.Config) ([]string, error) {
	_, problems := loadTheme(cfg)
	problems = append(slices.Clone(cfg.Problems), problems...)
	keys, keyProblems := loadKeyMap(cfg.Keys)
	problems = append(problems, keyProblems...)
	loaded, warnings := plugins.Load(cfg.Plugins, config.PluginDir())
	m := Model{keys: keys, plugins: loaded}
	problems = append(problems, warnings...)
	problems = append(problems, m.bindPluginKeys()...)

	var b strings.Builder
	fmt.Fprintf(&b, "# Every action and its keys, with the keys: section of\n# %s applied.\n", config.GetConfigPath())
	b.WriteString(`# To change an action's keys, copy its line there; actions left out keep
# these. A key is a character (k, G, "?"), space, or a name such as enter,
# esc, tab, backspace, delete, up, pgdown, home, f1, ctrl+r, shift+tab or
# alt+x. An empty list, [], leaves an action without a key.
keys:
`)
	var lines []string
	width := 0
	for _, a := range keys.actions() {
		var names []string
		for _, s := range a.binding.Keys() {
			names = append(names, yamlKey(s))
		}
		line := fmt.Sprintf("  %s: [%s]", a.name, strings.Join(names, ", "))
		lines = append(lines, line)
		width = max(width, utf8.RuneCountInString(line))
	}
	for i, a := range keys.actions() {
		fmt.Fprintf(&b, "%-*s  # %s\n", width, lines[i], a.binding.Help().Desc)
	}

	fmt.Fprintf(&b, "\n# Fixed: %s always quits, 1-9 jump to bookmarks, dialogs keep esc, enter\n"+
		"# and tab, the sort menu its letters (%s), and confirmations y and n.\n", alwaysQuit, sortLetters())
	var pluginLines []string
	for i, p := range m.plugins {
		if j, ok := m.pluginKeys[p.Key]; ok && j == i {
			pluginLines = append(pluginLines, fmt.Sprintf("#   %s  %s", yamlKey(p.Key), utils.Printable(p.Name)))
		}
	}
	if len(pluginLines) > 0 {
		b.WriteString("# Plugin keys, set with each plugin:\n" + strings.Join(pluginLines, "\n") + "\n")
	}

	_, err := io.WriteString(w, b.String())
	return problems, err
}

// yamlKey writes a key as the config does, quoted unless YAML would read
// it back as the same string anyway
func yamlKey(s string) string {
	name := keyName(s)
	for i, r := range name {
		plain := r < utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r) || i > 0 && (r == '+' || r == '_'))
		if !plain {
			return strconv.Quote(name)
		}
	}
	return name
}
