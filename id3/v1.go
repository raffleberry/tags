package id3

import (
	"bytes"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/raffleberry/tags/tag"
)

// ErrNoV1 means no ID3v1 tag was found in the last 128 bytes of the data.
var ErrNoV1 = errors.New("id3: no ID3v1 tag")

// v1Size is the length of an ID3v1 tag, and the distance from the end of the
// file it sits at.
const v1Size = 128

// V1 is an ID3v1 tag: the fixed 128 byte block taggers have appended to the
// end of a file since 1993. It holds one Latin-1 value per field, each of a
// fixed width.
//
// Anything with a real ID3v2 tag normally uses that instead; V1 exists because
// plenty of files still carry only this.
type V1 struct {
	Title   string
	Artist  string
	Album   string
	Year    string
	Comment string
	// Track is 0 for ID3v1.0, which has no track field.
	Track int
	// Genre is the name the genre byte refers to, or "" when it is unset or
	// points past the end of [Genres].
	Genre string
}

// Common renders the tag as normalized [tag.Tag] fields. An unset Track is
// omitted, since ID3v1.0 does not record one.
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

// ReadV1 reads the ID3v1 tag held in the 128 bytes r returns. It returns
// [ErrNoV1] when those bytes do not begin with "TAG", which is the normal case
// for a file with no ID3v1 tag at all.
//
// Note that an APEv2 tag also ends in the bytes "TAG", so an APEv2 file can be
// mistaken for an ID3v1 one.
func ReadV1(r io.Reader) (*V1, error) {
	var buf [v1Size]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return nil, ErrNoV1
	}
	return ParseV1(buf[:])
}

// ParseV1 decodes a 128 byte ID3v1 tag.
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
	// ID3v1.1 stole the last two bytes of the comment for a track number and
	// marks itself with a zero byte followed by a non zero one.
	if data[125] == 0 && data[126] != 0 {
		v.Track = int(data[126])
		v.Comment = latin1Field(data[97:125])
	}
	// 255 means "no genre", which is what most encoders write.
	if g := int(data[127]); g != 255 {
		if name, ok := Genre(g); ok {
			v.Genre = name
		}
	}
	return v, nil
}

// latin1Field decodes a fixed width ID3v1 field, stopping at the first NUL.
func latin1Field(field []byte) string {
	if i := bytes.IndexByte(field, 0); i >= 0 {
		field = field[:i]
	}
	return strings.TrimSpace(decodeLatin1(field))
}
