package tags

import (
	"bytes"
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// finderInfoAttr holds the classic Finder information, whose flags still
// carry a colour label from before there were tags. macOS shows that
// label as a tag of its colour, so it is read with the tags and kept in
// step when they change: otherwise a colour taken off would stay.
const finderInfoAttr = "com.apple.FinderInfo"

const (
	finderInfoSize = 32
	labelByte      = 9    // Low byte of the Finder flags
	labelMask      = 0x0E // Bits 1 to 3: the label's colour number
)

// Supported reports whether files here can have Finder tags
func Supported() bool { return true }

// Read returns the Finder tags of path, which isn't followed if it is a
// symlink: Finder tags a link itself. A file without tags, or on a volume
// without extended attributes, has none.
func Read(path string) ([]Tag, error) {
	// Most files have no tags. Listing a file's attributes costs about as
	// much as a stat, but asking for one it doesn't have costs several
	// times more, which adds up over a large directory.
	hasTags, hasInfo, err := attrs(path)
	if err != nil || !hasTags && !hasInfo {
		return nil, err
	}
	var list []Tag
	if hasTags {
		data, err := getAttr(path, Attr)
		if err != nil && !errors.Is(err, unix.ENOATTR) { // Gone just now: no tags
			return nil, fmt.Errorf("can't read the tags of %s: %w", path, err)
		}
		if err == nil {
			if list, err = Decode(data); err != nil {
				return nil, fmt.Errorf("can't read the tags of %s: %w", path, err)
			}
		}
	}
	// A label is a tag of its colour, unless one is there already
	if hasInfo {
		if info, err := getAttr(path, finderInfoAttr); err == nil && len(info) == finderInfoSize {
			c := label(info)
			if c != None && !hasColor(list, c) && !Has(list, c.String()) {
				list = append(list, Tag{Name: c.String(), Color: c})
			}
		}
	}
	return list, nil
}

// Write sets the Finder tags of path, which isn't followed if it is a
// symlink; with none, the attribute is removed, as Finder does
func Write(path string, list []Tag) error {
	var err error
	if len(list) == 0 {
		err = unix.Lremovexattr(path, Attr)
		if errors.Is(err, unix.ENOATTR) {
			err = nil
		}
	} else {
		err = unix.Lsetxattr(path, Attr, Encode(list), 0)
	}
	if err == nil {
		err = syncLabel(path, list)
	}
	if err != nil {
		return fmt.Errorf("can't tag %s: %w", path, err)
	}
	return nil
}

// syncLabel changes the colour label of path to one of list's colours,
// the last, or to none, if its colour is no longer among them. A label
// that is, or a file without one, is left as it is.
func syncLabel(path string, list []Tag) error {
	info, err := getAttr(path, finderInfoAttr)
	if errors.Is(err, unix.ENOATTR) || err == nil && len(info) != finderInfoSize {
		return nil
	}
	if err != nil {
		return err
	}
	c := label(info)
	if c == None || hasColor(list, c) {
		return nil
	}
	want := None
	for _, t := range list {
		if t.Color != None {
			want = t.Color
		}
	}
	info[labelByte] = info[labelByte]&^labelMask | byte(want)<<1
	// Finder information of nothing but zeros is the same as none
	if bytes.Equal(info, make([]byte, finderInfoSize)) {
		err = unix.Lremovexattr(path, finderInfoAttr)
		if errors.Is(err, unix.ENOATTR) {
			err = nil
		}
		return err
	}
	return unix.Lsetxattr(path, finderInfoAttr, info, 0)
}

// label returns the colour label in Finder information
func label(info []byte) Color {
	return Color((info[labelByte] & labelMask) >> 1)
}

// hasColor reports whether a tag in list has colour c
func hasColor(list []Tag, c Color) bool {
	for _, t := range list {
		if t.Color == c {
			return true
		}
	}
	return false
}

// attrs reports whether path has tags and Finder information
func attrs(path string) (tags, info bool, err error) {
	buf := make([]byte, 512)
	for {
		n, err := unix.Llistxattr(path, buf)
		switch {
		case errors.Is(err, unix.ERANGE):
			// More names than fit: ask how much room they need
			size, err := unix.Llistxattr(path, nil)
			if err != nil {
				return false, false, fmt.Errorf("can't read the tags of %s: %w", path, err)
			}
			buf = make([]byte, size+64)
			continue
		case errors.Is(err, unix.ENOTSUP):
			return false, false, nil // A volume without extended attributes
		case err != nil:
			return false, false, fmt.Errorf("can't read the tags of %s: %w", path, err)
		}
		for _, name := range bytes.Split(buf[:n], []byte{0}) {
			switch string(name) {
			case Attr:
				tags = true
			case finderInfoAttr:
				info = true
			}
		}
		return tags, info, nil
	}
}

// getAttr reads attribute name of path
func getAttr(path, name string) ([]byte, error) {
	buf := make([]byte, 256)
	for {
		n, err := unix.Lgetxattr(path, name, buf)
		if errors.Is(err, unix.ERANGE) {
			size, err := unix.Lgetxattr(path, name, nil)
			if err != nil {
				return nil, err
			}
			buf = make([]byte, size+64)
			continue
		}
		if err != nil {
			return nil, err
		}
		return buf[:n], nil
	}
}
