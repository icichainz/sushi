// Package tags reads and writes Finder tags: the names, most with a colour,
// that macOS keeps on a file in its com.apple.metadata:_kMDItemUserTags
// extended attribute, and that Finder and Spotlight show and search.
package tags

import (
	"slices"
	"strings"
)

// Attr is the extended attribute Finder keeps a file's tags in
const Attr = "com.apple.metadata:_kMDItemUserTags"

// Color is one of the colours Finder gives tags, numbered as Finder
// stores them
type Color int

const (
	None Color = iota
	Gray
	Green
	Purple
	Blue
	Yellow
	Red
	Orange
)

var colorNames = [...]string{"None", "Gray", "Green", "Purple", "Blue", "Yellow", "Red", "Orange"}

// String returns the colour's name, as Finder's standard tag of that
// colour is called
func (c Color) String() string {
	if c < None || int(c) >= len(colorNames) {
		return "None"
	}
	return colorNames[c]
}

// Tag is a Finder tag
type Tag struct {
	Name  string
	Color Color
}

// Known returns Finder's standard tags, one of each colour, in the order
// Finder lists them
func Known() []Tag {
	return []Tag{{"Red", Red}, {"Orange", Orange}, {"Yellow", Yellow}, {"Green", Green}, {"Blue", Blue}, {"Purple", Purple}, {"Gray", Gray}}
}

// parse reads a tag as Finder stores it: its name, then a newline and its
// colour's number, which a tag without a colour may leave out
func parse(s string) Tag {
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		if n := s[i+1:]; len(n) == 1 && n[0] >= '0' && n[0] <= '7' {
			return Tag{Name: s[:i], Color: Color(n[0] - '0')}
		}
	}
	return Tag{Name: s}
}

// String returns the tag as Finder stores it
func (t Tag) String() string {
	// Without a colour the name is enough, unless it ends as if it had one
	if t.Color == None && parse(t.Name) == t {
		return t.Name
	}
	return t.Name + "\n" + string(rune('0'+t.Color))
}

// Decode reads the value of the tags attribute
func Decode(data []byte) ([]Tag, error) {
	strs, err := decodeStrings(data)
	if err != nil {
		return nil, err
	}
	out := make([]Tag, 0, len(strs))
	for _, s := range strs {
		if t := parse(s); t.Name != "" {
			out = append(out, t)
		}
	}
	return out, nil
}

// Encode returns the value of the tags attribute for list
func Encode(list []Tag) []byte {
	strs := make([]string, len(list))
	for i, t := range list {
		strs[i] = t.String()
	}
	return encodeStrings(strs)
}

// Update returns the value of the tags attribute for list, given the value
// it has now, old: the tags list keeps, and the strings Decode leaves out
// (empty names), are written as they were, so text that isn't quite
// Unicode, as other tools may have put there, comes through untouched,
// and only what changed is written afresh. It returns false, and no value,
// if list is what old holds already, so nothing need be written. An old
// value that can't be read is replaced.
func Update(old []byte, list []Tag) ([]byte, bool) {
	var raw []rawString
	if len(old) > 0 {
		raw, _ = decodeRaw(old)
	}
	var shown []Tag
	for _, r := range raw {
		if t := parse(r.s); t.Name != "" {
			shown = append(shown, t)
		}
	}
	if Equal(shown, list) && (len(raw) > 0 || len(old) == 0) {
		return nil, false
	}

	used := make([]bool, len(raw))
	objs := make([][]byte, 0, len(list)+len(raw))
	for _, t := range list {
		obj := stringObject(t.String())
		for i, r := range raw {
			if !used[i] && r.obj != nil && parse(r.s) == t {
				obj, used[i] = r.obj, true
				break
			}
		}
		objs = append(objs, obj)
	}
	// The strings that show no tag stay where they were, as near as can be
	for i, r := range raw {
		if parse(r.s).Name != "" {
			continue
		}
		obj := r.obj
		if obj == nil {
			obj = stringObject(r.s)
		}
		objs = slices.Insert(objs, min(i, len(objs)), obj)
	}
	if len(objs) == 0 {
		return nil, true // Nothing left: no attribute
	}
	return encodeObjects(objs), true
}

// Same reports whether two tag names are the same tag: Finder ignores case
func Same(a, b string) bool {
	return strings.EqualFold(a, b)
}

// Index returns where the tag called name is in list, or -1
func Index(list []Tag, name string) int {
	for i, t := range list {
		if Same(t.Name, name) {
			return i
		}
	}
	return -1
}

// Has reports whether list has the tag called name
func Has(list []Tag, name string) bool {
	return Index(list, name) >= 0
}

// With returns list with t added at the end, unless it is there already
func With(list []Tag, t Tag) []Tag {
	if Has(list, t.Name) {
		return list
	}
	return append(list[:len(list):len(list)], t)
}

// Without returns list without the tag called name
func Without(list []Tag, name string) []Tag {
	out := make([]Tag, 0, len(list))
	for _, t := range list {
		if !Same(t.Name, name) {
			out = append(out, t)
		}
	}
	return out
}

// Equal reports whether two lists hold the same tags in the same order
func Equal(a, b []Tag) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Matches reports whether list has a tag whose name starts with prefix,
// ignoring case, and how well: 0 if one is called exactly that, 1 if one
// only starts with it. An empty prefix matches any tag.
func Matches(list []Tag, prefix string) (int, bool) {
	best, ok := 1, false
	for _, t := range list {
		switch {
		case Same(t.Name, prefix):
			return 0, true
		case hasPrefixFold(t.Name, prefix):
			ok = true
		}
	}
	if ok && prefix == "" {
		best = 0
	}
	return best, ok
}

// hasPrefixFold is strings.HasPrefix ignoring case
func hasPrefixFold(s, prefix string) bool {
	return strings.HasPrefix(strings.ToLower(s), strings.ToLower(prefix))
}
