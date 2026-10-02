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

// DataType is the type of a value stored in a "data" atom, which says how to
// read it.
type DataType uint32

// The data types iTunes actually writes.
const (
	DataImplicit  DataType = 0  // no declared type, the meaning comes from the atom
	DataUTF8      DataType = 1  // text, no terminator
	DataUTF16     DataType = 2  // UTF-16BE text
	DataSJIS      DataType = 3  // Shift-JIS text
	DataJPEG      DataType = 13 // a JPEG image
	DataPNG       DataType = 14 // a PNG image
	DataSignedInt DataType = 21 // a signed big endian integer
	DataBool      DataType = 21 // a boolean, which is a one byte signed int
)

// ILST is a parsed iTunes metadata list: the atoms under
// "moov.udta.meta.ilst", each holding one or more values.
type ILST struct {
	// Fields maps an atom name onto its values, in file order. Names are as
	// iTunes writes them, such as "©nam" and "trkn".
	Fields map[string][]Value
	// Order is the atom names in the order they appeared in the file.
	Order []string
}

// Value is one value of an atom, kept together with its declared type so that
// a caller who cares about the distinction can act on it.
type Value struct {
	Type DataType
	// Data is the raw bytes. Text values are UTF-8 unless the type says
	// otherwise, and image values are the encoded image.
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

// Text returns the value as a string, decoding it according to its type.
func (v Value) Text() string {
	switch v.Type {
	case DataUTF16:
		return decodeUTF16BE(v.Data)
	case DataSJIS:
		// Shift-JIS is not decodable without a table, and the values that use
		// it are rare enough that the bytes are more useful than a guess.
		return string(v.Data)
	default:
		return string(v.Data)
	}
}

// ParseILST reads the iTunes metadata list held in the payload of an "ilst"
// atom. A field whose payload does not follow the expected shape is skipped,
// since the rest of the file is still worth reading.
func ParseILST(data []byte) *ILST {
	ilst := &ILST{Fields: map[string][]Value{}}

	for pos := 0; pos+headerLen <= len(data); {
		length := int(binary.BigEndian.Uint32(data[pos : pos+4]))
		name := atomName(data[pos+4 : pos+8])
		if length < headerLen || pos+length > len(data) {
			break
		}
		body := data[pos+headerLen : pos+length]

		// A freeform atom is named by the application and field names inside
		// it, and several of them can share the "----" name.
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

// The atom names that carry a fixed meaning in a metadata list.
const (
	// freeformAtom is the name iTunes gives the atoms it has no name for, where
	// the real name is inside the atom.
	freeformAtom = "----"
	// dataAtom holds one typed value.
	dataAtom = "data"
)

// freeformKey renders the "mean:name" pair of a freeform atom as the key it is
// stored under.
func freeformKey(field string) string {
	return freeformAtom + ":" + field
}

// parseDataAtoms reads a run of "data" atoms, each a four byte type and flag
// field followed by the value.
func parseDataAtoms(body []byte) []Value {
	var values []Value
	for pos := 0; pos+16 <= len(body); {
		length := int(binary.BigEndian.Uint32(body[pos : pos+4]))
		if string(body[pos+4:pos+8]) != dataAtom {
			// A "name" atom may appear here, as iTunes writes one inside a
			// cover atom; skip whatever this is rather than giving up.
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

// parseFreeform reads a "----" atom, which holds a "mean" atom naming the
// application and a "name" atom naming the field, then the values. The names are
// kept as the key so that fields from different applications do not collide.
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

// readNamed returns the payload of the atom of the given name at pos, and the
// offset just past it. The payload of a "mean" or "name" atom starts after a
// four byte version and flags field.
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

// Common renders the metadata list as normalized [tag.Tag] fields.
//
// The values of an atom that has a known common meaning are folded onto that
// key, and everything else keeps its own name, lowercased. Freeform fields are
// keyed by "mean:name" as well as their own name, so that a field such as
// "com.apple.iTunes:iTunNORM" is reachable under both. Artwork is not text and
// is left out; see [ILST.Covers].
func (i *ILST) Common() tag.Tag {
	out := tag.Tag{}
	if i == nil {
		return out
	}
	for _, name := range i.Order {
		values := i.Fields[name]

		// A flag or a number is rendered as text, since that is what a
		// [tag.Tag] holds.
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
			// A pair of numbers: the position and the total.
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
			// A one based index into the ID3v1 genre list.
			for _, v := range values {
				n, err := v.Int()
				if err != nil {
					continue
				}
				// The atom counts from 1 where the genre list does too, but
				// offsets by one, so subtract to get the list index.
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

// textValues renders the text of every value, dropping the ones that are not
// text at all.
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

// parsePair reads a "trkn" or "disk" atom, which packs two unsigned shorts and
// a trailing zero into one value.
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

// Covers returns the artwork of a "covr" atom. Each value is an encoded image;
// the declared type says which of the two formats it is.
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
			// iTunes sometimes writes no type at all for a JPEG, so the magic
			// bytes are the more reliable source.
			p.MIME = mimeFromImage(v.Data)
		}
		if p.MIME != "" {
			pictures = append(pictures, p)
		}
	}
	return pictures
}

// atomKeys maps the iTunes atoms that name a field with a common meaning onto
// the normalized key. Atoms not listed here keep their own lowercased name.
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

// flagKeys maps the atoms that hold a yes or no flag onto the normalized key.
// The value is rendered as "1" or "0", the way the ID3v2.3 specification has it.
var flagKeys = map[string]string{
	"cpil": tag.Compilation,
	"pgap": "partofset",
	"pcst": "podcast",
	"hdvd": "itunes_hd_video",
}

// numberKeys maps the atoms that hold a plain integer onto the normalized key.
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

// finishPositions splits the "3/11" values of track and disc, for the files that
// store a position as a single string rather than as a pair.
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

// decodeUTF16BE decodes UTF-16 big endian, dropping a trailing odd byte and any
// unpaired surrogate.
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

// mimeFromImage guesses a MIME type from the magic bytes of an image, for
// taggers that left the type out.
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

// sizeOf returns the number of bytes left in r.
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
