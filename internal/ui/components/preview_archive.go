package components

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/icichainz/sushi/internal/utils"
)

const (
	// maxArchiveEntries is how many entries of an archive are listed
	maxArchiveEntries = 500
	// maxArchiveScan is how many tar entries are counted before giving up
	maxArchiveScan = 100_000
)

// archiveScanTime bounds how long a tar is read for: a compressed one
// must be decompressed from the start to find each entry. A variable so
// tests can shorten it.
var archiveScanTime = time.Second

// archiveKinds names the archive formats archiveFormat reports
var archiveKinds = map[string]string{
	"zip":     "ZIP Archive",
	"tar":     "TAR Archive",
	"tar.gz":  "TAR.GZ Archive",
	"tar.bz2": "TAR.BZ2 Archive",
}

// archiveFormat returns the format of an archive the preview can list,
// judging by its name, or "" for any other file
func archiveFormat(name string) string {
	name = strings.ToLower(name)
	switch {
	case strings.HasSuffix(name, ".zip"), strings.HasSuffix(name, ".jar"):
		return "zip"
	case strings.HasSuffix(name, ".tar"):
		return "tar"
	case strings.HasSuffix(name, ".tar.gz"), strings.HasSuffix(name, ".tgz"):
		return "tar.gz"
	case strings.HasSuffix(name, ".tar.bz2"), strings.HasSuffix(name, ".tbz2"):
		return "tar.bz2"
	}
	return ""
}

// archiveListing is what was found in an archive
type archiveListing struct {
	entries  []Entry // The first maxArchiveEntries
	count    int     // Entries found
	partial  bool    // Reading stopped before the end
	unpacked int64   // Size of the entries found, uncompressed
}

// add records an entry, listing it if there is still room
func (l *archiveListing) add(name string, dir bool, size int64) {
	l.count++
	l.unpacked += size
	if len(l.entries) < maxArchiveEntries {
		l.entries = append(l.entries, Entry{Name: cleanText(name), IsDir: dir, Size: size})
	}
}

// loadArchivePreview lists an archive's entries without extracting it
func loadArchivePreview(p PreviewContent, config PreviewConfig) PreviewContent {
	format := archiveFormat(p.FileInfo.Name)
	p.Kind = archiveKinds[format]
	if config.Quick {
		p.Pending = true
		return detailsView(p, "Loading...")
	}

	var l archiveListing
	var err error
	if format == "zip" {
		l, err = listZip(p.Path)
	} else {
		l, err = listTar(p.Path, format)
	}
	if err != nil {
		p.Error = err
		return withLines(p, fmt.Sprintf("Cannot read archive: %v", err))
	}

	p.Archive = true
	p.Entries = l.entries
	p.Count = l.count
	p.Partial = l.partial
	p.More = l.partial || l.count > len(l.entries)
	if !l.partial {
		p.Details = []string{utils.HumanizeSize(l.unpacked) + " unpacked"}
	}
	names := make([]string, len(l.entries))
	for i, e := range l.entries {
		names[i] = e.Name
	}
	p.Content = strings.Join(names, "\n")
	return p
}

// listZip lists a zip file from its central directory, at the end of the
// file, so nothing is decompressed
func listZip(path string) (l archiveListing, err error) {
	defer recoverAs(&err)
	r, err := zip.OpenReader(path)
	// Entries named like "../x" are only a risk when extracting
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		return l, err
	}
	defer r.Close()
	for _, f := range r.File {
		l.add(f.Name, strings.HasSuffix(f.Name, "/"), int64(f.UncompressedSize64))
	}
	return l, nil
}

// listTar reads a tar's headers in turn, skipping the contents
func listTar(path, format string) (l archiveListing, err error) {
	defer recoverAs(&err)
	f, err := os.Open(path)
	if err != nil {
		return l, err
	}
	defer f.Close()

	// An uncompressed tar is read straight from the file, so the contents
	// between headers are skipped with seeks rather than read
	var r io.Reader = f
	switch format {
	case "tar.gz":
		gz, err := gzip.NewReader(bufio.NewReader(f))
		if err != nil {
			return l, err
		}
		defer gz.Close()
		r = gz
	case "tar.bz2":
		r = bzip2.NewReader(bufio.NewReader(f))
	}

	tr := tar.NewReader(r)
	deadline := time.Now().Add(archiveScanTime)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return l, nil
		}
		if err != nil {
			if l.count == 0 {
				return l, err
			}
			// A damaged or cut short archive still lists what came before
			l.partial = true
			return l, nil
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			continue // Settings for the entries, not an entry
		}
		l.add(h.Name, h.Typeflag == tar.TypeDir, h.Size)
		if l.count >= maxArchiveScan || time.Now().After(deadline) {
			l.partial = true
			return l, nil
		}
	}
}

// recoverAs turns a panic in a decoder into an error, so a malformed file
// can't take the whole program down. Call it deferred.
func recoverAs(err *error) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("unreadable: %v", r)
	}
}
