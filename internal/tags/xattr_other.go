//go:build !darwin

package tags

import "errors"

// errUnsupported is returned by Write where there are no Finder tags
var errUnsupported = errors.New("Finder tags need macOS")

// Supported reports whether files here can have Finder tags
func Supported() bool { return false }

// Read returns no tags: only macOS has them
func Read(path string) ([]Tag, error) { return nil, nil }

// Write fails: only macOS has Finder tags
func Write(path string, list []Tag) error { return errUnsupported }
