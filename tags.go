// Package tags reads audio metadata from MP3, MP4 and FLAC files without naming
// a format.
//
// Use [Open] for a path and [Read] for anything else that is seekable. Both pick
// the format from the content of the file, so an .mp3 that is really an MP4, or
// a file with the wrong extension, still reads correctly:
//
//	f, err := tags.Open("song.mp3")
//	if err != nil {
//	    return err
//	}
//	fmt.Println(f.Tags().Value(tag.Title), f.Audio().Duration)
//
// The result is a [File], the common interface every reader in this module
// implements. For anything the common view does not model, the format packages
// hold the full picture: package id3 for the frames of an ID3 tag, package mp4
// for the atom tree of an MPEG-4 file, package flac for its metadata blocks.
//
// This package reads only. Nothing here modifies a file.
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
// alias of [tag.File] so that a caller need only import this package to name it.
type File = tag.File

// The types a [File] returns, aliased from package tag so that one import is
// enough.
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

// Format reports which container the file at path holds, by looking at its
// first few bytes. The extension is not consulted, since it is often wrong or
// absent.
//
// A file that is a valid container but holds no audio returns its format anyway.
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

// DetectReader reports which container the data in r holds. r is left wherever
// reading the header left it.
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

// headerLen is how many bytes are read to identify a file. Every container this
// package reads names itself within its first twelve.
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

// Open reads the audio file at path, choosing a reader from the content of the
// file rather than from its extension.
func Open(path string) (tag.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Read(f)
}

// OpenAs reads the audio file at path, insisting that it holds the given
// container. It is what a caller uses when the format is already known, such as
// when walking a directory of one kind of file, and it saves a format that would
// otherwise be guessed.
//
// The format must match the file, so a caller that guesses wrong hears about it
// rather than silently reading something else.
func OpenAs(path string, format tag.Format) (tag.File, error) {
	file, err := openAs(path, format)
	if err != nil {
		return nil, err
	}
	// A container can be read from data that belongs to another one, which would
	// hand back a file of a different format than the one that was asked for.
	// Checking that here is what makes the format argument mean something.
	if got := file.Format(); got != format {
		return nil, fmt.Errorf("tags: %s holds %v audio, not %v", path, got, format)
	}
	return file, nil
}

// openAs dispatches to the reader for the given format without checking that the
// file really is of that format.
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

// Read reads an audio file from r, which must be seekable, choosing a reader
// from the content of the file.
//
// The whole file is read from the current position of r, so a caller who has
// already consumed a prefix should pass a reader positioned at the start of the
// audio.
func Read(r io.ReadSeeker) (tag.File, error) {
	header := make([]byte, headerLen)
	if _, err := io.ReadFull(r, header); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, fmt.Errorf("%w: the data is too short to identify", ErrUnknownFormat)
		}
		return nil, err
	}

	// Every reader wants to start at the beginning of the file, since the
	// containers all put their identifying atoms first.
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

// Extensions returns the file extensions that usually hold the given container,
// which is worth having when walking a directory. The extension is only a hint:
// use [Detect] to find out what a file really is.
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

// LooksLike reports whether name has an extension that usually holds one of the
// containers this package reads. It saves reading files that cannot be audio, and
// says nothing about what a file with a matching extension really is.
func LooksLike(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp3", ".mp2", ".mpga", ".m4a", ".m4b", ".m4p", ".mp4", ".aac", ".flac":
		return true
	default:
		return false
	}
}

// The formats this package can read, which is what a caller needs when it wants
// to report what it supports or loop over a registry.
var formats = []tag.Format{tag.MP3, tag.M4A, tag.FLAC}

// Formats returns the containers this package can read.
func Formats() []tag.Format { return slices.Clone(formats) }

// The ID3 types are re-exported so that a caller working with MP3 files need not
// import a second package to reach the frame level view.
type (
	// ID3Tag is a parsed ID3v2 tag.
	ID3Tag = id3.Tag
	// ID3Frame is one frame of an ID3 tag.
	ID3Frame = id3.Frame
	// ID3v1 is a parsed ID3v1 tag.
	ID3v1 = id3.V1
)

// ID3v2 reads the ID3v2 tag at the start of r, which is what package mp3 uses and
// what a caller needs when the tags of an MP3 file are wanted on their own.
func ID3v2(r io.ReadSeeker) (*id3.Tag, error) { return id3.Read(r) }

// ID3v1From reads the ID3v1 tag held in the last 128 bytes of the file in r.
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

// id3v1Size is the length of an ID3v1 tag, and how far from the end of the file
// it sits.
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
