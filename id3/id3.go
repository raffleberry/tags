// Package id3 reads ID3v2.2, ID3v2.3, and ID3v2.4 tags and 128 byte ID3v1 tags.
//
// ID3v2 tags are at the start of a file. [Read] requires the reader to be
// positioned at the "ID3" identifier. [ReadHeader] reports whether a tag is
// present and its size.
//
// [Tag] stores each decoded frame. [Tag.Common] maps frames to the key space
// of package tag.
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
	// ErrNoTag indicates the data does not start with an ID3v2 identifier.
	ErrNoTag = errors.New("id3: no ID3v2 tag")
	// ErrVersion indicates an unsupported ID3v2 version.
	ErrVersion = errors.New("id3: unsupported ID3v2 version")
	// ErrSize indicates an invalid or non-synchsafe length field.
	ErrSize = errors.New("id3: invalid tag size")
)

// Magic is the identifier at the start of every ID3v2 tag.
var Magic = []byte("ID3")

// Version is an ID3v2 version with major version 2.
type Version struct {
	Major int // 2
	Minor int // 2, 3 or 4
}

// String returns the version as "2.4.0".
func (v Version) String() string { return fmt.Sprintf("2.%d.0", v.Minor) }

// Header is an ID3v2 tag header without frame data.
type Header struct {
	Version Version
	// Unsynchronised reports whether 0xFF 0x00 pairs must be removed before parsing frames.
	Unsynchronised bool
	// Extended reports whether an extended header precedes the frames.
	Extended bool
	// Experimental reports the experimental flag.
	Experimental bool
	// Footer reports whether a 10 byte footer follows the frames.
	Footer bool
	// Body is the size in bytes after the header.
	Body int
}

// Total returns the tag size in bytes, including the header.
func (h Header) Total() int { return 10 + h.Body }

// ReadHeader reads the 10 byte ID3v2 header at the reader position. It
// returns [ErrNoTag] when the data does not start with an ID3v2 identifier.
func ReadHeader(r io.Reader) (Header, error) {
	var h Header

	// Short input returns ErrNoTag. Other read errors are returned directly.
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

	// The version byte stores 2, 3, or 4 for ID3v2.2, ID3v2.3, and ID3v2.4.
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

	// Only the low nibble is checked in v2.4. Earlier versions also check bit 0x08.
	bad := flags & 0x0f
	if h.Version.Minor < 4 {
		bad = flags & 0x1f
	}
	if bad != 0 {
		return h, fmt.Errorf("%w: header flags are %#02x", ErrSize, flags)
	}
	return h, nil
}

// unsynchsafe decodes a 28 bit synchsafe integer. It reports false when a high bit is set.
func unsynchsafe(b []byte) (int, bool) {
	if len(b) < 4 {
		return 0, false
	}
	if b[0]&0x80 != 0 || b[1]&0x80 != 0 || b[2]&0x80 != 0 || b[3]&0x80 != 0 {
		return 0, false
	}
	return int(b[0])<<21 | int(b[1])<<14 | int(b[2])<<7 | int(b[3]), true
}

// synchsafe encodes n as a 28 bit synchsafe integer.
func synchsafe(n int) []byte {
	return []byte{
		byte(n>>21) & 0x7f,
		byte(n>>14) & 0x7f,
		byte(n>>7) & 0x7f,
		byte(n) & 0x7f,
	}
}

// Frame names used by [Tag.Common].
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
	// Header is the source tag header.
	Header
	// Frames holds frames in file order.
	Frames []Frame
}

// Read decodes the ID3v2 tag at the reader position. The reader must be at the tag start.
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

// parse decodes frames from a tag body without the 10 byte header.
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

// frameLayout describes frame header fields for one ID3v2 version.
type frameLayout struct {
	// idLen is the frame ID length in bytes.
	idLen int
	// synchsafeSize reports whether the frame size is synchsafe. ID3v2.4 uses synchsafe sizes.
	synchsafeSize bool
}

// headerLen returns the frame header size in bytes.
func (l frameLayout) headerLen() int {
	if l.idLen == 4 {
		return 10
	}
	return 6
}

// frameSize decodes the frame size from header. ok is false when the size exceeds the remaining tag data.
func (l frameLayout) frameSize(header []byte, rest int) (size int, ok bool) {
	switch {
	case l.idLen == 3:
		// A 24 bit plain integer.
		size = int(header[3])<<16 | int(header[4])<<8 | int(header[5])

	case l.synchsafeSize:
		size, ok = unsynchsafe(header[4:8])
		if !ok {
			// Some writers use plain integers for ID3v2.4 sizes. Accept a plain value within the remaining data.
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

// parseFrames decodes frames from body. The extended header must already be removed.
func (t *Tag) parseFrames(body []byte, layout frameLayout) error {
	idLen, headerLen := layout.idLen, layout.headerLen()

	for off := 0; off+headerLen <= len(body); {
		id := body[off : off+idLen]
		// All-zero IDs mark padding. Parsing stops.
		if isPadding(id) {
			break
		}
		name := string(bytes.ToUpper(bytes.TrimRight(id, "\x00")))

		size, ok := layout.frameSize(body[off:], len(body)-off)
		if !ok {
			// The size exceeds the remaining data. Parsing stops.
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

// isPadding reports whether id is all zeros.
func isPadding(id []byte) bool {
	return bytes.Equal(id, make([]byte, len(id)))
}

// skipExtended returns body without the extended header.
func skipExtended(v Version, body []byte) ([]byte, error) {
	// v2.3 stores a plain size excluding the size field. v2.4 stores a synchsafe size including it.
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

// deunsynchronise removes 0x00 bytes following 0xFF in unsynchronised data.
func deunsynchronise(data []byte) []byte {
	if !bytes.Contains(data, []byte{0xFF, 0x00}) {
		return data
	}
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); i++ {
		out = append(out, data[i])
		// A 0x00 byte after 0xFF is removed.
		if data[i] == 0xFF && i+1 < len(data) && data[i+1] == 0x00 {
			i++
		}
	}
	return out
}
