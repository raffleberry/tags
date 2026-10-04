package ape

import (
	"strings"

	"github.com/raffleberry/tags/tag"
)

// apeKeys maps lowercased APEv2 item keys to normalized keys.
// It contains keys with a common meaning.
var apeKeys = map[string]string{
	"title":                       tag.Title,
	"subtitle":                    tag.Subtitle,
	"artist":                      tag.Artist,
	"album artist":                tag.AlbumArtist,
	"albumartist":                 tag.AlbumArtist,
	"album":                       tag.Album,
	"composer":                    tag.Composer,
	"conductor":                   tag.Conductor,
	"arranger":                    tag.Arranger,
	"lyricist":                    tag.Lyricist,
	"grouping":                    tag.Grouping,
	"mood":                        tag.Mood,
	"genre":                       tag.Genre,
	"label":                       tag.Label,
	"publisher":                   tag.Publisher,
	"organization":                tag.Publisher,
	"copyright":                   tag.Copyright,
	"encoded by":                  tag.EncodedBy,
	"encodedby":                   tag.EncodedBy,
	"encoder":                     tag.EncoderSettings,
	"encoder settings":            tag.EncoderSettings,
	"barcode":                     tag.Barcode,
	"catalognumber":               tag.CatalogNumber,
	"catalog number":              tag.CatalogNumber,
	"upc":                         tag.UPC,
	"isrc":                        tag.ISRC,
	"compilation":                 tag.Compilation,
	"original filename":           tag.OriginalFilename,
	"date":                        tag.Date,
	"year":                        tag.Date,
	"record date":                 tag.Date,
	"release date":                tag.ReleaseDate,
	"original date":               tag.OriginalDate,
	"track":                       tag.Track,
	"disc":                        tag.Disc,
	"comment":                     tag.Comment,
	"lyrics":                      tag.Lyrics,
	"language":                    tag.Language,
	"script":                      tag.Script,
	"bpm":                         tag.BPM,
	"replaygain_track_gain":       tag.ReplayGainTrackGain,
	"replaygain_track_peak":       tag.ReplayGainTrackPeak,
	"replaygain_album_gain":       tag.ReplayGainAlbumGain,
	"replaygain_album_peak":       tag.ReplayGainAlbumPeak,
	"musicbrainz_trackid":         tag.MusicBrainzRecordingID,
	"musicbrainz_releaseartistid": tag.MusicBrainzAlbumArtistID,
	"musicbrainz_albumartistid":   tag.MusicBrainzAlbumArtistID,
	"musicbrainz_artistid":        tag.MusicBrainzArtistID,
	"musicbrainz_albumid":         tag.MusicBrainzReleaseID,
	"musicbrainz_releasetrackid":  tag.MusicBrainzReleaseID,
}

// Common returns the tag as normalized [tag.Tag] fields.
// An item with a known meaning is stored under that key.
// Other items keep their lowercased names.
// Binary cover art is excluded.
// See [Tag.Pictures].
func (t *Tag) Common() tag.Tag {
	out := tag.Tag{}
	if t == nil {
		return out
	}
	for _, it := range t.Items {
		if len(it.Values) == 0 {
			continue
		}
		if isCoverKey(it.Key) {
			continue
		}
		lower := strings.ToLower(strings.TrimSpace(it.Key))
		if key, ok := apeKeys[lower]; ok {
			out.Add(key, it.Values...)
			continue
		}
		// APE keys such as Album Artist contain a space.
		// The normalized key has no space.
		// The spaceless form is also checked.
		if key, ok := apeKeys[strings.ReplaceAll(lower, " ", "")]; ok {
			out.Add(key, it.Values...)
			continue
		}
		out.Add(lower, it.Values...)
	}

	finishPositions(out)
	return out
}

// finishPositions splits "3/11" values of track and disc into position and total.
func finishPositions(out tag.Tag) {
	for _, key := range []struct{ position, total string }{
		{tag.Track, tag.TrackTotal},
		{tag.Disc, tag.DiscTotal},
	} {
		position, total := tag.SplitTotal(out.Value(key.position))
		switch {
		case position == "":
			out.Delete(key.position)
		case position != out.Value(key.position):
			out.Set(key.position, position)
		}
		if total == "" {
			out.Delete(key.total)
		} else {
			out.SetDefault(key.total, total)
		}
		// A number in the total field is used when the position has no total.
		// Some taggers write the total this way.
		_ = strings.TrimSpace
	}
}
