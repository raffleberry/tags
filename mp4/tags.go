package mp4

import (
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/raffleberry/tags/tag"
)

// DataType is the type of a value in a "data" atom.
type DataType uint32

// Data types written by iTunes.
const (
	DataImplicit  DataType = 0  // type from atom meaning
	DataUTF8      DataType = 1  // text without terminator
	DataUTF16     DataType = 2  // UTF-16BE text
	DataSJIS      DataType = 3  // Shift-JIS text
	DataJPEG      DataType = 13 // JPEG image
	DataPNG       DataType = 14 // PNG image
	DataSignedInt DataType = 21 // signed big endian integer
	DataBool      DataType = 21 // boolean as 1 byte signed integer
)

// ILST is a parsed iTunes metadata list at "moov.udta.meta.ilst".
type ILST struct {
	// Fields maps atom name to values in file order.
	Fields map[string][]Value
	// Order holds atom names in file order.
	Order []string
}

// Value is one atom value with its declared type.
type Value struct {
	Type DataType
	// Data holds raw bytes. Text is UTF-8 unless Type states otherwise. Images
	// hold encoded image bytes.
	Data []byte
}

// Int returns the value as an integer.
func (v Value) Int() (int, error) {
	switch len(v.Data) {
	case 1:
		return int(int8(v.Data[0])), nil
	case 2:
		return int(int16(binary.BigEndian.Uint16(v.Data))), nil
	case 3:
		return int(int32(binary.BigEndian.Uint32(append(v.Data, 0))) >> 8), nil
	case 4:
		return int(int32(binary.BigEndian.Uint32(v.Data))), nil
	case 8:
		return int(int64(binary.BigEndian.Uint64(v.Data))), nil
	default:
		return 0, fmt.Errorf("mp4: %d byte integer", len(v.Data))
	}
}

// Text returns the value as a string. It decodes text by Type.
func (v Value) Text() string {
	switch v.Type {
	case DataUTF16:
		return decodeUTF16BE(v.Data)
	case DataSJIS:
		// Shift-JIS values are returned as raw bytes.
		return string(v.Data)
	default:
		return string(v.Data)
	}
}

// ParseILST reads the iTunes metadata list in the payload of an "ilst" atom.
// Malformed fields are skipped.
func ParseILST(data []byte) *ILST {
	ilst := &ILST{Fields: map[string][]Value{}}

	for pos := 0; pos+headerLen <= len(data); {
		length := int(binary.BigEndian.Uint32(data[pos : pos+4]))
		name := atomName(data[pos+4 : pos+8])
		if length < headerLen || pos+length > len(data) {
			break
		}
		body := data[pos+headerLen : pos+length]

		// A freeform atom holds application and field names inside. Multiple
		// freeform atoms share the "----" name.
		key, values := name, parseDataAtoms(body)
		if name == freeformAtom {
			if freeform, field := parseFreeform(body); len(freeform) > 0 {
				key, values = freeformKey(field), freeform
			}
		}
		if len(values) > 0 {
			if _, seen := ilst.Fields[key]; !seen {
				ilst.Order = append(ilst.Order, key)
			}
			ilst.Fields[key] = append(ilst.Fields[key], values...)
		}
		pos += length
	}
	return ilst
}

// Atom names with fixed meaning in a metadata list.
const (
	// freeformAtom marks atoms named inside the atom.
	freeformAtom = "----"
	// dataAtom holds one typed value.
	dataAtom = "data"
)

// freeformKey returns the storage key for a freeform "mean:name" pair.
func freeformKey(field string) string {
	return freeformAtom + ":" + field
}

// parseDataAtoms reads a run of "data" atoms. Each atom holds a 4 byte type
// and flags field followed by the value.
func parseDataAtoms(body []byte) []Value {
	var values []Value
	for pos := 0; pos+16 <= len(body); {
		length := int(binary.BigEndian.Uint32(body[pos : pos+4]))
		if string(body[pos+4:pos+8]) != dataAtom {
			// A "name" atom can occur here. Skip unknown atoms.
			if length < headerLen {
				break
			}
			pos += length
			continue
		}
		if length < 16 || pos+length > len(body) {
			break
		}
		// The version and flags field shares its top byte with the type.
		dataType := DataType(binary.BigEndian.Uint32(body[pos+8:pos+12]) & 0x00FFFFFF)
		values = append(values, Value{Type: dataType, Data: body[pos+16 : pos+length]})
		pos += length
	}
	return values
}

// parseFreeform reads a "----" atom. It holds a "mean" atom, a "name" atom,
// then the values. Names form the key to separate applications.
func parseFreeform(body []byte) ([]Value, string) {
	mean, pos, ok := readNamed(body, 0, "mean")
	if !ok {
		return nil, ""
	}
	name, pos, ok := readNamed(body, pos, "name")
	if !ok {
		return nil, ""
	}
	return parseDataAtoms(body[pos:]), string(mean) + ":" + string(name)
}

// readNamed returns the payload of the atom with the given name at pos, and
// the offset after it. "mean" and "name" payloads start after 4 bytes of
// version and flags.
func readNamed(body []byte, pos int, want string) ([]byte, int, bool) {
	if pos+headerLen > len(body) {
		return nil, pos, false
	}
	length := int(binary.BigEndian.Uint32(body[pos : pos+4]))
	if length < headerLen || pos+length > len(body) {
		return nil, pos, false
	}
	if string(body[pos+4:pos+8]) != want {
		return nil, pos, false
	}
	return body[pos+headerLen+freeLen : pos+length], pos + length, true
}

// Common returns the metadata list as normalized [tag.Tag] fields.
//
// Atoms with known meaning map to common keys. Other atoms keep their own
// lowercased names. Freeform fields are keyed by "mean:name". Artwork is
// excluded. See [ILST.Covers].
func (i *ILST) Common() tag.Tag {
	out := tag.Tag{}
	if i == nil {
		return out
	}
	for _, name := range i.Order {
		values := i.Fields[name]

		// Flags and numbers are stored as text in [tag.Tag].
		if key, ok := flagKeys[name]; ok {
			for _, v := range values {
				if n, err := v.Int(); err == nil {
					out.Set(key, tag.Bool(n != 0))
				}
			}
			continue
		}
		if key, ok := numberKeys[name]; ok {
			for _, v := range values {
				if n, err := v.Int(); err == nil {
					out.Set(key, strconv.Itoa(n))
				}
			}
			continue
		}
		if key, ok := atomKeys[name]; ok {
			out.Add(key, textValues(values)...)
			continue
		}
		switch name {
		case "trkn", "disk":
			// Two numbers: position and total.
			position, total, ok := parsePair(values)
			if !ok {
				continue
			}
			first, second := tag.Track, tag.TrackTotal
			if name == "disk" {
				first, second = tag.Disc, tag.DiscTotal
			}
			if position > 0 {
				out.Set(first, strconv.Itoa(position))
			}
			if total > 0 {
				out.SetDefault(second, strconv.Itoa(total))
			}
			continue

		case "gnre":
			// One based index into the ID3v1 genre list.
			for _, v := range values {
				n, err := v.Int()
				if err != nil {
					continue
				}
				// The atom counts from 1. Subtract 1 for the list index.
				if n > 0 && n <= len(tag.Genres) {
					out.Add(tag.Genre, tag.Genres[n-1])
				}
			}
			continue

		case "covr":
			continue // artwork, see Covers
		}

		key := strings.ToLower(name)
		out.Add(key, textValues(values)...)
	}
	finishPositions(out)
	return out
}

// textValues returns the text of each value. It skips non-text values.
func textValues(values []Value) []string {
	var out []string
	for _, v := range values {
		if v.Type == DataJPEG || v.Type == DataPNG {
			continue
		}
		if s := v.Text(); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// parsePair reads a "trkn" or "disk" value. It holds 2 unsigned shorts.
func parsePair(values []Value) (position, total int, ok bool) {
	if len(values) == 0 {
		return 0, 0, false
	}
	data := values[0].Data
	if len(data) < 4 {
		return 0, 0, false
	}
	position = int(binary.BigEndian.Uint16(data[2:4]))
	total = int(binary.BigEndian.Uint16(data[4:6]))
	return position, total, true
}

// Covers returns artwork from the "covr" atom. Each value is one encoded image.
// Type states the format.
func (i *ILST) Covers() []tag.Picture {
	if i == nil {
		return nil
	}
	var pictures []tag.Picture
	for _, v := range i.Fields["covr"] {
		if len(v.Data) == 0 {
			continue
		}
		p := tag.Picture{Type: tag.PictureCoverFront, Data: v.Data}
		switch v.Type {
		case DataPNG:
			p.MIME = "image/png"
		default:
			// Type is sometimes missing for JPEG. Magic bytes apply then.
			p.MIME = mimeFromImage(v.Data)
		}
		if p.MIME != "" {
			pictures = append(pictures, p)
		}
	}
	return pictures
}

// atomKeys maps iTunes atom names to normalized keys. Unlisted atoms keep
// their lowercased names.
var atomKeys = map[string]string{
	"©nam": tag.Title,
	"©ART": tag.Artist,
	"aART": tag.AlbumArtist,
	"©alb": tag.Album,
	"©wrt": tag.Composer,
	"©day": tag.Date,
	"©cmt": tag.Comment,
	"desc": tag.Comment,
	"©gen": tag.Genre,
	"gnre": tag.Genre,
	"©lyr": tag.Lyrics,
	"©grp": tag.Grouping,
	"catg": tag.Grouping,
	"©too": tag.EncoderSettings,
	"cprt": tag.Copyright,
	"©cpy": tag.Copyright,
	"soal": tag.AlbumSort,
	"soaa": tag.AlbumArtistSort,
	"soar": tag.ArtistSort,
	"sonm": tag.TitleSort,
	"tvsh": "showmovement",
	"©mvn": "movementname",
	"©mvi": "movementnumber",
	"©wrk": "work",
	"purd": tag.ReleaseDate,
	"©pub": tag.Publisher,
	"tmpo": tag.BPM,
	"purl": "podcasturl",
}

// flagKeys maps flag atoms to normalized keys. Values are "1" or "0".
var flagKeys = map[string]string{
	"cpil": tag.Compilation,
	"pgap": "partofset",
	"pcst": "podcast",
	"hdvd": "itunes_hd_video",
}

// numberKeys maps integer atoms to normalized keys.
var numberKeys = map[string]string{
	"tmpo": tag.BPM,
	"tvsn": "tvseason",
	"tves": "tvepisode",
	"stik": "itunes_media_kind",
	"rtng": "itunes_advisory_rating",
	"akID": "itunes_adam_id",
	"cnID": "itunes_catalog_number",
	"plID": "itunes_playlist_id",
	"sfID": "itunes_storefront_id",
	"cmID": "itunes_cm_id",
	"geID": "itunes_genre_id",
	"atID": "itunes_at_id",
	"egid": "podcastepisodeguid",
}

// finishPositions splits "3/11" track and disc values into position and total.
func finishPositions(out tag.Tag) {
	for _, key := range []struct{ position, total string }{
		{tag.Track, tag.TrackTotal},
		{tag.Disc, tag.DiscTotal},
	} {
		position, total := tag.SplitTotal(out.Value(key.position))
		if position == "" {
			out.Delete(key.position)
			continue
		}
		if position != out.Value(key.position) {
			out.Set(key.position, position)
		}
		if total == "" {
			out.Delete(key.total)
		} else {
			out.SetDefault(key.total, total)
		}
	}
}

// decodeUTF16BE decodes UTF-16 big endian. It drops a trailing odd byte and
// unpaired surrogates.
func decodeUTF16BE(data []byte) string {
	n := len(data) / 2
	if n == 0 {
		return ""
	}
	units := make([]uint16, n)
	for i := range units {
		units[i] = binary.BigEndian.Uint16(data[2*i:])
	}
	return string(utf16.Decode(units))
}

// mimeFromImage returns a MIME type from image magic bytes.
func mimeFromImage(data []byte) string {
	switch {
	case len(data) > 2 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "image/jpeg"
	case len(data) > 3 && string(data[:4]) == "\x89PNG":
		return "image/png"
	case len(data) > 3 && string(data[:4]) == "GIF8":
		return "image/gif"
	case len(data) > 1 && string(data[:2]) == "BM":
		return "image/bmp"
	default:
		return ""
	}
}

// sizeOf returns the bytes left in r.
func sizeOf(r io.ReadSeeker) (int64, error) {
	cur, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, fmt.Errorf("mp4: seeking: %w", err)
	}
	end, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, fmt.Errorf("mp4: seeking: %w", err)
	}
	if _, err := r.Seek(cur, io.SeekStart); err != nil {
		return 0, fmt.Errorf("mp4: seeking: %w", err)
	}
	return end - cur, nil
}
