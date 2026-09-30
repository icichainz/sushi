package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// compoundExts are extensions of two parts, kept together by SplitExt
var compoundExts = []string{".tar.gz", ".tar.bz2", ".tar.xz", ".tar.zst"}

// SplitExt splits a file name into its stem and extension. Compound
// extensions like ".tar.gz" stay together, and a leading dot, as in
// ".bashrc", doesn't start one.
func SplitExt(name string) (stem, ext string) {
	lower := strings.ToLower(name)
	for _, compound := range compoundExts {
		if strings.HasSuffix(lower, compound) && strings.Trim(name[:len(name)-len(compound)], ".") != "" {
			return name[:len(name)-len(compound)], name[len(name)-len(compound):]
		}
	}
	ext = filepath.Ext(name)
	stem = strings.TrimSuffix(name, ext)
	if strings.Trim(stem, ".") == "" || ext == "." {
		return name, ""
	}
	return stem, ext
}

// FreeName returns the first name not taken in dir out of stem+ext, then
// "stem 2"+ext, "stem 3"+ext and so on
func FreeName(dir, stem, ext string) string {
	for n := 1; ; n++ {
		name := stem + ext
		if n > 1 {
			name = fmt.Sprintf("%s %d%s", stem, n, ext)
		}
		if !Exists(filepath.Join(dir, name)) {
			return name
		}
	}
}

// CopyName returns a free name for a copy of path beside it: "notes
// copy.txt", then "notes copy 2.txt" and so on. A copy of a copy is
// numbered too, so copying "notes copy.txt" gives "notes copy 2.txt",
// not "notes copy copy.txt". Directories keep any dots in their name, as
// in "v1.2 copy".
func CopyName(path string) string {
	name := filepath.Base(path)
	stem, ext := SplitExt(name)
	if info, err := os.Lstat(path); err == nil && info.IsDir() {
		stem, ext = name, ""
	}
	if m := copySuffix.FindStringSubmatch(stem); m != nil {
		stem = m[1]
	}
	return FreeName(filepath.Dir(path), stem+" copy", ext)
}

// copySuffix matches a name that CopyName made: the original's, then
// " copy", perhaps with a number
var copySuffix = regexp.MustCompile(`^(.*[^ ]) copy(?: [0-9]+)?$`)
