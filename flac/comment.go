package flac

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"strings"

	"github.com/raffleberry/tags/tag"
)

// VorbisComment holds tags as "KEY=value" lines. Ogg Vorbis and Opus use the same format.
type VorbisComment struct {
	// Vendor is the name of the program that wrote the file.
	Vendor string
	// Fields holds comments in file order. It keeps duplicate keys.
	Fields []VorbisField
}

// VorbisField is one "KEY=value" comment.
type VorbisField struct {
	// Key is the field name. It is conventionally uppercase. Lines without "=" are ignored.
	Key string
	// Value is the text after the first "=". It can contain "=".
	Value string
}

// ParseVorbisComment decodes a Vorbis comment block. A FLAC block has no framing bit.
func ParseVorbisComment(data []byte) (*VorbisComment, error) {
	return ReadVorbisComment(bytes.NewReader(data))
}

// ReadVorbisComment decodes a Vorbis comment block from r. r is left at the end of the block.
//
// The block holds a vendor string, a field count, and fields. Each field
// is "KEY=value". The count can be wrong. Present fields are read even
// when the count is too large.
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
			// A bad field ends the block. Earlier fields are kept.
			if len(c.Fields) == 0 {
				return nil, fmt.Errorf("flac: %s: reading a field: %w", ErrBlock, err)
			}
			break
		}
		key, value, found := strings.Cut(line, "=")
		// Lines without "=" are ignored.
		if !found {
			continue
		}
		c.Fields = append(c.Fields, VorbisField{Key: key, Value: value})
	}
	return c, nil
}

// readCountedString reads a length followed by that many bytes. It reports an error when the length exceeds the limit.
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

// commentKeys maps Vorbis comment names to normalized keys. Names are compared in lower case.
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
// Known names map to common keys. Other names are kept lowercased.
// Artwork is excluded. See [File.Pictures].
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

	// Split "3/11" track values into position and total. Keep existing totals.
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
