package mp4

import (
	"encoding/binary"
	"fmt"
	"io"
	"time"
)

// Chapter is one chapter of a Nero chapter list.
type Chapter struct {
	// Start is the offset from the start of the file.
	Start time.Duration
	// Title is the chapter name.
	Title string
}

// ParseChpl decodes a Nero "chpl" atom payload. The payload holds a version
// byte, reserved bytes, a chapter count byte, and one entry per chapter. Each
// entry holds an 8 byte big endian start time in 100 nanosecond units followed
// by a Pascal string title.
func ParseChpl(data []byte) ([]Chapter, error) {
	// Eight bytes of version and reserved flags, then the count.
	if len(data) < 9 {
		return nil, fmt.Errorf("%w: chapter list of %d bytes", ErrAtom, len(data))
	}
	if data[0] != 0 && data[0] != 1 {
		return nil, fmt.Errorf("%w: chapter list version %d", ErrAtom, data[0])
	}
	for _, b := range data[1:8] {
		if b != 0 {
			return nil, fmt.Errorf("%w: chapter list reserves non zero bytes", ErrAtom)
		}
	}
	count := int(data[8])
	pos := 9
	out := make([]Chapter, 0, count)
	for range count {
		if pos+9 > len(data) {
			return nil, fmt.Errorf("%w: truncated chapter %d", ErrAtom, len(out)+1)
		}
		start := binary.BigEndian.Uint64(data[pos : pos+8])
		length := int(data[pos+8])
		pos += 9
		if pos+length > len(data) {
			return nil, fmt.Errorf("%w: truncated chapter title", ErrAtom)
		}
		title := string(data[pos : pos+length])
		pos += length
		out = append(out, Chapter{
			Start: time.Duration(start * 100),
			Title: title,
		})
	}
	if pos != len(data) {
		return nil, fmt.Errorf("%w: chapter list has %d trailing bytes", ErrAtom, len(data)-pos)
	}
	return out, nil
}

// Chapters returns the Nero chapters of the file. QuickTime chapter tracks
// remain reachable through [File.Atoms].
func (f *File) Chapters() []Chapter {
	if f == nil {
		return nil
	}
	return f.chapters
}

// readChapters parses the Nero chapter list below the movie box. It returns nil
// when the list is missing or malformed.
func readChapters(r io.ReadSeeker, moov Atom) []Chapter {
	chpl, ok := moov.Path("udta", "chpl")
	if !ok {
		return nil
	}
	data, err := chpl.Data(r)
	if err != nil {
		return nil
	}
	chapters, err := ParseChpl(data)
	if err != nil {
		return nil
	}
	return chapters
}
