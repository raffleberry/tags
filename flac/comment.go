package flac

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"strings"

	"github.com/raffleberry/tags/tag"
)

// VorbisComment is the comment block of a FLAC file, which holds the tags as
// "KEY=value" lines. The same format is used by Ogg Vorbis and by Opus.
type VorbisComment struct {
	// Vendor is the name of the program that wrote the file.
	Vendor string
	// Fields holds the comments in file order, keeping duplicates, which the
	// format allows and which a multi valued artist needs.
	Fields []VorbisField
}

// VorbisField is one "KEY=value" comment.
type VorbisField struct {
	// Key is the name of the field, conventionally uppercase. A line with no
	// equals sign is not a field at all and is not kept.
	Key string
	// Value is everything after the first equals sign, which may itself contain
	// more of them.
	Value string
}

// ParseVorbisComment decodes a Vorbis comment block. Unlike the Ogg carriers, a
// FLAC comment block has no framing bit after the last field.
func ParseVorbisComment(data []byte) (*VorbisComment, error) {
	return ReadVorbisComment(bytes.NewReader(data))
}

// ReadVorbisComment decodes a Vorbis comment block from r, which is left at the
// end of the block.
//
// The block is a vendor string, a count of fields, and that many fields, each a
// string of the form "KEY=value". The count is advisory: some writers state more
// fields than they wrote, so the fields that are present are read even when the
// count runs past the end of the data.
func ReadVorbisComment(r io.Reader) (*VorbisComment, error) {
	c := &VorbisComment{}

	vendor, err := readCountedString(r)
	if err != nil {
		return nil, fmt.Errorf("flac: %s: reading the vendor: %w", ErrBlock, err)
	}
	c.Vendor = vendor

	var countBytes [4]byte
	if _, err := io.ReadFull(r, countBytes[:]); err != nil {
		return nil, fmt.Errorf("flac: %s: reading the field count: %w", ErrBlock, err)
	}
	count := binary.LittleEndian.Uint32(countBytes[:])

	for range count {
		var line string
		if line, err = readCountedString(r); err != nil {
			// A field that cannot be read ends the block. Anything read before
			// it is still worth reporting.
			if len(c.Fields) == 0 {
				return nil, fmt.Errorf("flac: %s: reading a field: %w", ErrBlock, err)
			}
			break
		}
		key, value, found := strings.Cut(line, "=")
		// A comment with no equals sign has no name for its value, so it is not
		// one this package can report. The specification says to ignore it.
		if !found {
			continue
		}
		c.Fields = append(c.Fields, VorbisField{Key: key, Value: value})
	}
	return c, nil
}

// readCountedString reads a length followed by that many bytes. A length larger
// than what is left in the block means the block is malformed, and reading on
// would take whatever follows it for the value.
func readCountedString(r io.Reader) (string, error) {
	var lengthBytes [4]byte
	if _, err := io.ReadFull(r, lengthBytes[:]); err != nil {
		return "", fmt.Errorf("want 4 bytes for a length: %w", err)
	}
	length := binary.LittleEndian.Uint32(lengthBytes[:])
	if int64(length) > maxBlockLen {
		return "", fmt.Errorf("string of %d bytes, which is past the end of the file", length)
	}

	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", fmt.Errorf("string of %d bytes: %w", length, err)
	}
	return string(buf), nil
}

// commentKeys maps the Vorbis comment names that have a common meaning onto the
// normalized key. Names are conventionally uppercase, and are compared in lower
// case, so a file that uses lowercase names works too.
var commentKeys = map[string]string{
	"TITLE":                      tag.Title,
	"SUBTITLE":                   tag.Subtitle,
	"ARTIST":                     tag.Artist,
	"ALBUMARTIST":                tag.AlbumArtist,
	"ALBUM ARTIST":               tag.AlbumArtist,
	"ALBUM":                      tag.Album,
	"COMPOSER":                   tag.Composer,
	"CONDUCTOR":                  tag.Conductor,
	"ARRANGER":                   tag.Arranger,
	"LYRICIST":                   tag.Lyricist,
	"GENRE":                      tag.Genre,
	"DATE":                       tag.Date,
	"YEAR":                       tag.Year,
	"RELEASEDATE":                tag.ReleaseDate,
	"ORIGINALDATE":               tag.OriginalDate,
	"TRACKNUMBER":                tag.Track,
	"TRACKTOTAL":                 tag.TrackTotal,
	"TOTALTRACKS":                tag.TrackTotal,
	"DISCNUMBER":                 tag.Disc,
	"DISCTOTAL":                  tag.DiscTotal,
	"TOTALDISCS":                 tag.DiscTotal,
	"COMMENT":                    tag.Comment,
	"DESCRIPTION":                tag.Comment,
	"LYRICS":                     tag.Lyrics,
	"UNSYNCEDLYRICS":             tag.Lyrics,
	"LANGUAGE":                   tag.Language,
	"GROUPING":                   tag.Grouping,
	"MOOD":                       tag.Mood,
	"LABEL":                      tag.Label,
	"PUBLISHER":                  tag.Publisher,
	"ORGANIZATION":               tag.Publisher,
	"COPYRIGHT":                  tag.Copyright,
	"ENCODEDBY":                  tag.EncodedBy,
	"ENCODED-BY":                 tag.EncodedBy,
	"ENCODERSETTINGS":            tag.EncoderSettings,
	"ENCODER":                    tag.EncoderSettings,
	"BPM":                        tag.BPM,
	"ISRC":                       tag.ISRC,
	"BARCODE":                    tag.Barcode,
	"CATALOGNUMBER":              tag.CatalogNumber,
	"UPC":                        tag.UPC,
	"COMPILATION":                tag.Compilation,
	"ORIGINALFILENAME":           tag.OriginalFilename,
	"TITLESORT":                  tag.TitleSort,
	"ALBUMSORT":                  tag.AlbumSort,
	"ARTISTSORT":                 tag.ArtistSort,
	"ALBUMARTISTSORT":            tag.AlbumArtistSort,
	"REPLAYGAIN_TRACK_GAIN":      tag.ReplayGainTrackGain,
	"REPLAYGAIN_TRACK_PEAK":      tag.ReplayGainTrackPeak,
	"REPLAYGAIN_ALBUM_GAIN":      tag.ReplayGainAlbumGain,
	"REPLAYGAIN_ALBUM_PEAK":      tag.ReplayGainAlbumPeak,
	"MUSICBRAINZ_TRACKID":        tag.MusicBrainzRecordingID,
	"MUSICBRAINZ_ALBUMID":        tag.MusicBrainzReleaseID,
	"MUSICBRAINZ_ARTISTID":       tag.MusicBrainzArtistID,
	"MUSICBRAINZ_ALBUMARTISTID":  tag.MusicBrainzAlbumArtistID,
	"MUSICBRAINZ_RELEASETRACKID": tag.MusicBrainzReleaseID,
}

// Common renders the comments as normalized [tag.Tag] fields.
//
// A name with a known common meaning is folded onto that key, and anything else
// is kept under its own lowercased name, so no comment is lost. Artwork is not
// text and is left out; see [File.Pictures].
func (c *VorbisComment) Common() tag.Tag {
	out := tag.Tag{}
	if c == nil {
		return out
	}
	for _, field := range c.Fields {
		key, ok := commentKeys[strings.ToUpper(field.Key)]
		if !ok {
			key = strings.ToLower(field.Key)
		}
		if key == "" {
			continue
		}
		out.Add(key, field.Value)
	}

	// A track comment may hold "3/11" where the total also has a comment of its
	// own. Split the one and leave the other alone when it is already there.
	if position, total := tag.SplitTotal(out.Value(tag.Track)); total != "" {
		out.SetDefault(tag.TrackTotal, total)
		if position != "" {
			out.Set(tag.Track, position)
		}
	}
	if position, total := tag.SplitTotal(out.Value(tag.Disc)); total != "" {
		out.SetDefault(tag.DiscTotal, total)
		if position != "" {
			out.Set(tag.Disc, position)
		}
	}
	return out
}
