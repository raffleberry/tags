package id3

import (
	"bytes"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/raffleberry/tags/tag"
)

// ErrNoV1 indicates no ID3v1 tag in the 128 byte input.
var ErrNoV1 = errors.New("id3: no ID3v1 tag")

// v1Size is the ID3v1 tag length in bytes.
const v1Size = 128

// V1 is an ID3v1 tag. It is a fixed 128 byte block at the end of a file. Each field holds one Latin-1 value with a fixed width.
type V1 struct {
	Title   string
	Artist  string
	Album   string
	Year    string
	Comment string
	// Track is 0 for ID3v1.0 without a track field.
	Track int
	// Genre is the genre name. It is empty when unset or out of range.
	Genre string
}

// Common returns the tag as normalized [tag.Tag] fields. An unset Track is omitted.
func (v *V1) Common() tag.Tag {
	t := tag.Tag{}
	if v == nil {
		return t
	}
	set := func(key, value string) {
		if value != "" {
			t.Set(key, value)
		}
	}
	set(tag.Title, v.Title)
	set(tag.Artist, v.Artist)
	set(tag.Album, v.Album)
	set(tag.Date, v.Year)
	set(tag.Comment, v.Comment)
	set(tag.Genre, v.Genre)
	if v.Track > 0 {
		t.Set(tag.Track, strconv.Itoa(v.Track))
	}
	return t
}

// ReadV1 reads an ID3v1 tag from the 128 bytes returned by r. It returns [ErrNoV1] when the bytes do not start with "TAG".
func ReadV1(r io.Reader) (*V1, error) {
	var buf [v1Size]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return nil, ErrNoV1
	}
	return ParseV1(buf[:])
}

// ParseV1 decodes a 128 byte ID3v1 tag. It returns [ErrNoV1] for invalid input.
func ParseV1(data []byte) (*V1, error) {
	if len(data) < v1Size || !bytes.Equal(data[:3], []byte("TAG")) {
		return nil, ErrNoV1
	}
	data = data[:v1Size]

	v := &V1{
		Title:   latin1Field(data[3:33]),
		Artist:  latin1Field(data[33:63]),
		Album:   latin1Field(data[63:93]),
		Year:    latin1Field(data[93:97]),
		Comment: latin1Field(data[97:127]),
	}
	// ID3v1.1 stores the track number in the last two comment bytes. It uses a zero byte followed by a nonzero byte.
	if data[125] == 0 && data[126] != 0 {
		v.Track = int(data[126])
		v.Comment = latin1Field(data[97:125])
	}
	// Genre byte 255 means no genre.
	if g := int(data[127]); g != 255 {
		if name, ok := Genre(g); ok {
			v.Genre = name
		}
	}
	return v, nil
}

// latin1Field decodes a fixed-width ID3v1 field. It stops at the first NUL.
func latin1Field(field []byte) string {
	if i := bytes.IndexByte(field, 0); i >= 0 {
		field = field[:i]
	}
	return strings.TrimSpace(decodeLatin1(field))
}
