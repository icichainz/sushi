package fs

import (
	"errors"
	"io"
	"os"
)

// IsBinary reports whether a file looks binary, judging by a NUL byte in its
// first 512 bytes. Empty files count as text.
func IsBinary(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}

	for _, b := range buf[:n] {
		if b == 0 {
			return true, nil
		}
	}
	return false, nil
}
