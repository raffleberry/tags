// Package id3 reads ID3 tags: ID3v2.2, ID3v2.3 and ID3v2.4 frame tags, and the
// fixed 128 byte ID3v1 tag that still turns up at the end of older files.
//
// ID3v2 tags appear at the start of an MP3 file, so [Read] expects the reader
// to be positioned on the "ID3" identifier. [ReadHeader] is the cheap way to
// find out whether a tag is there and how much of the file it covers.
//
// An [Tag] keeps every frame it could decode. [Tag.Common] folds the frames
// onto the normalized key space of package tag.
package id3

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Errors reported while reading an ID3 tag.
var (
	// ErrNoTag means the data did not begin with an ID3v2 identifier.
	ErrNoTag = errors.New("id3: no ID3v2 tag")
	// ErrVersion means the tag claims a version this package cannot read.
	ErrVersion = errors.New("id3: unsupported ID3v2 version")
	// ErrSize means a length field in the tag was out of range or not
	// synchsafe.
	ErrSize = errors.New("id3: invalid tag size")
)

// Magic begins every ID3v2 tag.
var Magic = []byte("ID3")

// Version is an ID3v2 version, always major 2.
type Version struct {
	Major int // 2
	Minor int // 2, 3 or 4
}

// String returns the version as "2.4.0".
func (v Version) String() string { return fmt.Sprintf("2.%d.0", v.Minor) }

// Header is an ID3v2 tag header: what the tag claims about itself, without
// reading any frames.
type Header struct {
	Version Version
	// Unsynchronised reports whether the whole tag body had 0xFF 0x00 pairs
	// inserted, which must be undone before frames can be read.
	Unsynchronised bool
	// Extended reports whether an extended header precedes the frames.
	Extended bool
	// Experimental reports the tag author's experimental flag.
	Experimental bool
	// Footer reports whether a 10 byte footer follows the frames.
	Footer bool
	// Body is the size of everything after the header, as recorded in the tag.
	Body int
}

// Total is the number of bytes the tag occupies in the file, header included.
func (h Header) Total() int { return 10 + h.Body }

// ReadHeader reads the 10 byte ID3v2 header at the reader's position. It
// returns [ErrNoTag] when the data does not start with an ID3v2 identifier,
// which lets a caller treat tagging as optional.
func ReadHeader(r io.Reader) (Header, error) {
	var h Header

	// Data too short to hold a header cannot hold a tag either, which is a
	// different answer from a read failure and one a caller checking for a tag
	// needs to be able to tell.
	var buf [10]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return h, ErrNoTag
		}
		return h, fmt.Errorf("id3: reading header: %w", err)
	}
	if !bytes.Equal(buf[:3], Magic) {
		return h, ErrNoTag
	}

	// The version byte counts 2.2, 2.3 and 2.4 as 2, 3 and 4. There has only
	// ever been a major version of 2.
	h.Version = Version{Major: 2, Minor: int(buf[3])}
	if h.Version.Minor < 2 || h.Version.Minor > 4 {
		return h, fmt.Errorf("%w: 2.%d", ErrVersion, h.Version.Minor)
	}

	flags := buf[5]
	h.Unsynchronised = flags&0x80 != 0
	h.Extended = flags&0x40 != 0
	h.Experimental = flags&0x20 != 0
	h.Footer = h.Version.Minor >= 4 && flags&0x10 != 0

	body, ok := unsynchsafe(buf[6:10])
	if !ok {
		return h, fmt.Errorf("%w: header size is not synchsafe", ErrSize)
	}
	h.Body = body

	// Only the low nibble is defined in v2.4; earlier versions also reserve a
	// compression bit that was never used.
	bad := flags & 0x0f
	if h.Version.Minor < 4 {
		bad = flags & 0x1f
	}
	if bad != 0 {
		return h, fmt.Errorf("%w: header flags are %#02x", ErrSize, flags)
	}
	return h, nil
}

// unsynchsafe decodes a 28 bit synchsafe integer, reporting whether the bytes
// really used the encoding. The seven bit groups are the whole point of the
// scheme, so data that has been through a non compliant writer is rejected.
func unsynchsafe(b []byte) (int, bool) {
	if len(b) < 4 {
		return 0, false
	}
	if b[0]&0x80 != 0 || b[1]&0x80 != 0 || b[2]&0x80 != 0 || b[3]&0x80 != 0 {
		return 0, false
	}
	return int(b[0])<<21 | int(b[1])<<14 | int(b[2])<<7 | int(b[3]), true
}

// synchsafe encodes n as a 28 bit synchsafe integer, which is how ID3v2.4
// stores sizes and how every version stores the tag size.
func synchsafe(n int) []byte {
	return []byte{
		byte(n>>21) & 0x7f,
		byte(n>>14) & 0x7f,
		byte(n>>7) & 0x7f,
		byte(n) & 0x7f,
	}
}

// Frame names with a fixed meaning that [Tag.Common] relies on.
const (
	FrameTrack  = "TRCK" // ID3v2.3 and ID3v2.4
	FrameTrack2 = "TRK"  // ID3v2.2
	FrameDisc   = "TPOS"
	FrameDisc2  = "TPA"
	FrameDate   = "TDRC" // ID3v2.4, the recording time
	FrameYear   = "TYER" // ID3v2.3
	FrameLength = "TLEN" // playback time in milliseconds
)

// Tag is a decoded ID3v2 tag.
type Tag struct {
	// Header the tag was read from.
	Header
	// Frames in the order they appeared in the tag.
	Frames []Frame
}

// Read decodes the ID3v2 tag at the reader's position. The reader must be
// positioned on the start of the tag; [ReadHeader] can check that first.
func Read(r io.Reader) (*Tag, error) {
	h, err := ReadHeader(r)
	if err != nil {
		return nil, err
	}
	body := make([]byte, h.Body)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("id3: reading %d byte tag body: %w", h.Body, err)
	}
	t := &Tag{Header: h}
	if err := t.parse(body); err != nil {
		return nil, err
	}
	return t, nil
}

// parse decodes the frames of a tag body, which excludes the 10 byte header.
func (t *Tag) parse(body []byte) error {
	if t.Unsynchronised {
		body = deunsynchronise(body)
	}

	if t.Extended {
		var err error
		if body, err = skipExtended(t.Version, body); err != nil {
			return err
		}
	}

	switch t.Version.Minor {
	case 2:
		return t.parseFrames(body, frameLayout{idLen: 3})
	case 3:
		return t.parseFrames(body, frameLayout{idLen: 4})
	default:
		return t.parseFrames(body, frameLayout{idLen: 4, synchsafeSize: true})
	}
}

// frameLayout describes how one version of the specification writes frame
// headers.
type frameLayout struct {
	// idLen is the length of a frame ID: three characters in ID3v2.2, four
	// afterwards.
	idLen int
	// synchsafeSize reports whether the frame size is a synchsafe integer.
	// ID3v2.4 uses one; ID3v2.3 and ID3v2.2 use a plain integer.
	synchsafeSize bool
}

// headerLen is the size of the frame header, ID and size fields included.
func (l frameLayout) headerLen() int {
	if l.idLen == 4 {
		return 10
	}
	return 6
}

// frameSize decodes the size field of the frame header at the start of header,
// which holds headerLen bytes and is followed by the rest of the tag. ok is
// false when the size does not fit in what is left of the tag, which means the
// header is not really a frame header.
func (l frameLayout) frameSize(header []byte, rest int) (size int, ok bool) {
	switch {
	case l.idLen == 3:
		// A 24 bit plain integer.
		size = int(header[3])<<16 | int(header[4])<<8 | int(header[5])

	case l.synchsafeSize:
		size, ok = unsynchsafe(header[4:8])
		if !ok {
			// iTunes writes ID3v2.4 frame sizes as plain integers, and other
			// taggers copied that. A synchsafe size can never exceed the space
			// left in the tag, so a plain reading within it is the better guess.
			size = int(binary.BigEndian.Uint32(header[4:8]))
		}

	default:
		// ID3v2.3 uses a plain 32 bit integer.
		size = int(binary.BigEndian.Uint32(header[4:8]))
	}

	if size < 0 || size > rest-l.headerLen() {
		return 0, false
	}
	return size, true
}

// parseFrames walks the frame headers in body, which is the tag body with any
// extended header already removed.
func (t *Tag) parseFrames(body []byte, layout frameLayout) error {
	idLen, headerLen := layout.idLen, layout.headerLen()

	for off := 0; off+headerLen <= len(body); {
		id := body[off : off+idLen]
		// A run of zero bytes is padding, and any frame ID containing one is
		// the start of padding by convention.
		if isPadding(id) {
			break
		}
		name := string(bytes.ToUpper(bytes.TrimRight(id, "\x00")))

		size, ok := layout.frameSize(body[off:], len(body)-off)
		if !ok {
			// The tag claims more frames than it has bytes. Stop rather than
			// inventing frames out of audio data.
			break
		}

		payload := body[off+headerLen:]
		flags := [2]byte{}
		if idLen == 4 {
			flags = [2]byte{body[off+8], body[off+9]}
		}
		frame, ok := t.decodeFrame(name, payload[:size], flags)
		if ok {
			t.Frames = append(t.Frames, frame)
		}
		off += headerLen + size
	}
	return nil
}

// isPadding reports whether a frame ID is all zeros, which marks the padding
// that fills the unused space at the end of most tags.
func isPadding(id []byte) bool {
	return bytes.Equal(id, make([]byte, len(id)))
}

// skipExtended drops an extended header, returning the frames that follow it.
func skipExtended(v Version, body []byte) ([]byte, error) {
	// The size field is present either way. v2.3 stores a plain integer that
	// excludes itself; v2.4 stores a synchsafe integer covering the whole
	// extended header.
	if len(body) < 4 {
		return nil, fmt.Errorf("%w: truncated extended header", ErrSize)
	}
	size := int(binary.BigEndian.Uint32(body[:4]))
	if v.Minor >= 4 {
		n, ok := unsynchsafe(body[:4])
		if !ok {
			return nil, fmt.Errorf("%w: extended header size is not synchsafe", ErrSize)
		}
		size = n - 4
	}
	if size < 0 || size+4 > len(body) {
		return nil, fmt.Errorf("%w: extended header claims %d bytes", ErrSize, size)
	}
	return body[4+size:], nil
}

// deunsynchronise removes the 0x00 bytes that follow 0xFF in unsynchronised
// data, which is what stops a tag from looking like a false frame sync to a
// player. The whole tag body is unsynchronised at once when the header flag is
// set, so this runs before frames are split out.
func deunsynchronise(data []byte) []byte {
	if !bytes.Contains(data, []byte{0xFF, 0x00}) {
		return data
	}
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); i++ {
		out = append(out, data[i])
		// A zero byte after 0xFF was inserted and is not part of the data.
		if data[i] == 0xFF && i+1 < len(data) && data[i+1] == 0x00 {
			i++
		}
	}
	return out
}
