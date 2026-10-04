// Package tags reads audio metadata from MP3, MP4 and FLAC files.
//
// Use [Open] for a path and [Read] for other seekable input. Both select the
// format from file content. A file with a wrong extension still reads
// correctly:
//
//	f, err := tags.Open("song.mp3")
//	if err != nil {
//	    return err
//	}
//	fmt.Println(f.Tags().Value(tag.Title), f.Audio().Duration)
//
// The result is a [File]. It is the common interface every reader in this
// module implements. For data the common view omits, use the format packages:
// package id3 for the frames of an ID3 tag, package mp4 for the atom tree of
// an MPEG-4 file, package flac for its metadata blocks.
//
// This package reads only. It does not modify files.
package tags

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/raffleberry/tags/flac"
	"github.com/raffleberry/tags/id3"
	"github.com/raffleberry/tags/m4a"
	"github.com/raffleberry/tags/mp3"
	"github.com/raffleberry/tags/mp4"
	"github.com/raffleberry/tags/tag"
)

// File is the common interface every reader in this module implements. It is an
// alias of [tag.File]. Importing this package alone names it.
type File = tag.File

// The types a [File] returns, aliased from package tag. One import is enough.
type (
	// Tag is a set of metadata fields keyed by lowercase names.
	Tag = tag.Tag
	// Audio describes the audio stream itself.
	Audio = tag.Audio
	// Picture is one piece of embedded artwork.
	Picture = tag.Picture
	// Format names the container a file was read from.
	Format = tag.Format
)

// The containers this package can read.
const (
	MP3  = tag.MP3
	M4A  = tag.M4A
	FLAC = tag.FLAC
)

// ErrUnknownFormat means the content of the file matched none of the containers
// this package reads.
var ErrUnknownFormat = errors.New("tags: unrecognized audio format")

// Detect reports the container of the file at path. It reads the first bytes.
// It does not use the extension. Extensions are often wrong or absent.
//
// A valid container without audio still returns its format.
func Detect(path string) (tag.Format, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	header := make([]byte, headerLen)
	if _, err := io.ReadFull(f, header); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return "", fmt.Errorf("%w: the file is too short to identify", ErrUnknownFormat)
		}
		return "", err
	}
	format, ok := detectFormat(header)
	if !ok {
		return "", fmt.Errorf("%w: the first bytes are % x", ErrUnknownFormat, header)
	}
	return format, nil
}

// DetectReader reports the container of the data in r. The read position is
// left after the header.
func DetectReader(r io.ReadSeeker) (tag.Format, error) {
	header := make([]byte, headerLen)
	if _, err := io.ReadFull(r, header); err != nil {
		return "", fmt.Errorf("%w: %w", ErrUnknownFormat, err)
	}
	format, ok := detectFormat(header)
	if !ok {
		return "", fmt.Errorf("%w: the first bytes are % x", ErrUnknownFormat, header)
	}
	return format, nil
}

// headerLen is the byte count read to identify a file. Each supported
// container has an identifier in its first twelve bytes.
const headerLen = 12

// detectFormat identifies a container from the first bytes of a file.
func detectFormat(header []byte) (tag.Format, bool) {
	switch {
	case flac.Matches(header):
		return tag.FLAC, true
	case mp4.Matches(header):
		return tag.M4A, true
	case mp3.Matches(header):
		return tag.MP3, true
	default:
		return "", false
	}
}

// Open reads the audio file at path. It selects a reader from file content,
// not from the extension.
func Open(path string) (tag.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Read(f)
}

// OpenAs reads the audio file at path as the given container. Use it when the
// format is already known. It skips format detection.
//
// The format must match the file content. A mismatch returns an error.
func OpenAs(path string, format tag.Format) (tag.File, error) {
	file, err := openAs(path, format)
	if err != nil {
		return nil, err
	}
	// A reader can return data from another container. This check enforces the
	// requested format.
	if got := file.Format(); got != format {
		return nil, fmt.Errorf("tags: %s holds %v audio, not %v", path, got, format)
	}
	return file, nil
}

// openAs selects the reader for the given format. It does not verify the file
// content.
func openAs(path string, format tag.Format) (tag.File, error) {
	switch format {
	case tag.MP3:
		return mp3.Open(path)
	case tag.M4A:
		return m4a.Open(path)
	case tag.FLAC:
		return flac.Open(path)
	default:
		return nil, fmt.Errorf("tags: %q is not a format this package reads", format)
	}
}

// Read reads an audio file from r. r must be seekable. It selects a reader
// from file content.
//
// Data is read from the current position of r. To read a complete file,
// position r at the start of the audio data.
func Read(r io.ReadSeeker) (tag.File, error) {
	header := make([]byte, headerLen)
	if _, err := io.ReadFull(r, header); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, fmt.Errorf("%w: the data is too short to identify", ErrUnknownFormat)
		}
		return nil, err
	}

	// Seek to the start. Readers start at the start of the file.
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("tags: seeking: %w", err)
	}

	switch format, ok := detectFormat(header); {
	case !ok:
		return nil, fmt.Errorf("%w: the first bytes are % x", ErrUnknownFormat, header)

	case format == tag.MP3:
		return mp3.Read(r)
	case format == tag.M4A:
		return m4a.Read(r)
	default:
		return flac.Read(r)
	}
}

// Extensions returns the file extensions used for the given container. Use it
// when walking a directory. An extension is only a hint. Use [Detect] to
// identify file content.
func Extensions(format tag.Format) []string {
	switch format {
	case tag.MP3:
		return []string{".mp3", ".mp2", ".mpga"}
	case tag.M4A:
		return []string{".m4a", ".m4b", ".mp4", ".m4p", ".aac"}
	case tag.FLAC:
		return []string{".flac"}
	default:
		return nil
	}
}

// LooksLike reports whether name has an extension used for a supported
// container. It avoids reading files that cannot be audio. It does not verify
// file content.
func LooksLike(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp3", ".mp2", ".mpga", ".m4a", ".m4b", ".m4p", ".mp4", ".aac", ".flac":
		return true
	default:
		return false
	}
}

// formats holds the supported containers.
var formats = []tag.Format{tag.MP3, tag.M4A, tag.FLAC}

// Formats returns the containers this package can read.
func Formats() []tag.Format { return slices.Clone(formats) }

// The ID3 types are re-exported for MP3 callers. One import covers frame level
// access.
type (
	// ID3Tag is a parsed ID3v2 tag.
	ID3Tag = id3.Tag
	// ID3Frame is one frame of an ID3 tag.
	ID3Frame = id3.Frame
	// ID3v1 is a parsed ID3v1 tag.
	ID3v1 = id3.V1
)

// ID3v2 reads the ID3v2 tag at the start of r. Package mp3 uses it. Use it to
// read MP3 tags without audio data.
func ID3v2(r io.ReadSeeker) (*id3.Tag, error) { return id3.Read(r) }

// ID3v1From reads the ID3v1 tag in the last 128 bytes of the file in r.
func ID3v1From(r io.ReadSeeker) (*id3.V1, error) {
	size, err := fileSize(r)
	if err != nil {
		return nil, err
	}
	if size < id3v1Size {
		return nil, id3.ErrNoV1
	}
	if _, err := r.Seek(size-id3v1Size, io.SeekStart); err != nil {
		return nil, fmt.Errorf("tags: seeking: %w", err)
	}
	return id3.ReadV1(r)
}

// id3v1Size is the length of an ID3v1 tag. It is the offset from the end of
// the file.
const id3v1Size = 128

// fileSize returns the number of bytes in r.
func fileSize(r io.ReadSeeker) (int64, error) {
	end, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, fmt.Errorf("tags: seeking: %w", err)
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return 0, fmt.Errorf("tags: seeking: %w", err)
	}
	return end, nil
}
