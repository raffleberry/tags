package id3

import (
	"strings"

	"github.com/raffleberry/tags/tag"
)

// frameKeys maps the ID3 frame IDs that have a common equivalent onto the
// normalized key. The v2.2 name is listed first where the two versions spell
// the frame differently; the ID3v2.4 name follows, which is also what ID3v2.3
// writers commonly use for the same field.
var frameKeys = map[string]string{
	// Titles and names.
	"TIT2": tag.Title, "TT2": tag.Title,
	"TIT3": tag.Subtitle, "TT3": tag.Subtitle,
	"TPE1": tag.Artist, "TP1": tag.Artist,
	"TPE2": tag.AlbumArtist, "TP2": tag.AlbumArtist,
	"TALB": tag.Album, "TAL": tag.Album,
	"TCOM": tag.Composer, "TCM": tag.Composer,
	"TPE3": tag.Conductor, "TP3": tag.Conductor,
	"TEXT": tag.Lyricist, "TXT": tag.Lyricist,
	"TOAL": tag.OriginalFilename,
	"TOPE": "originalartist",

	// Organization.
	"TPUB": tag.Publisher,
	"TCOP": tag.Copyright, "TCR": tag.Copyright,
	"TENC": tag.EncodedBy, "TEN": tag.EncodedBy,
	"TSSE": tag.EncoderSettings, "TSS": tag.EncoderSettings,
	"TBPM": tag.BPM, "TBP": tag.BPM,
	"TSRC": tag.ISRC, "TRC": tag.ISRC,
	"TCMP": tag.Compilation, "TCP": tag.Compilation,
	"TMED": "media",
	"TLAN": tag.Language, "TLA": tag.Language,

	// Dates. TYER and TDAT are the ID3v2.3 spelling of what v2.4 records in a
	// single TDRC frame.
	"TDRC": tag.Date,
	"TYER": tag.Date, "TYE": tag.Date,
	"TDAT": "date_day_month",
	"TDOR": tag.OriginalDate, "TORY": tag.OriginalDate,
	"TDRL": tag.ReleaseDate, "TDA": tag.ReleaseDate,
	"TIME": "time",

	// Positions.
	"TRCK": tag.Track, "TRK": tag.Track,
	"TPOS": tag.Disc, "TPA": tag.Disc,

	// Sort names.
	"TSOA": tag.AlbumSort,
	"TSOT": tag.TitleSort,
	"TSOP": tag.ArtistSort,
	"TSO2": tag.AlbumArtistSort,

	// Free text.
	"TCON": tag.Genre, "TCO": tag.Genre,
	"TLEN": "length_ms", "TLE": "length_ms",
	"TMOO": tag.Mood,
	"TKEY": "initialkey",
	"TKWD": "keywords",
	"TDES": tag.Comment,
	"TCAT": "category",
	"WFED": "podcasturl",

	// Identifiers, which ID3 carries in binary frames.
	"MVNM": "movementname",
	"MVIN": "movementnumber",
	"GRP1": "grouping",
}

// userTextKeys maps the description of the TXXX frames that other formats
// record under a name of their own. Descriptions not listed here become the
// normalized key themselves, lowercased.
var userTextKeys = map[string]string{
	"replaygain_track_gain":        tag.ReplayGainTrackGain,
	"replaygain_track_peak":        tag.ReplayGainTrackPeak,
	"replaygain_album_gain":        tag.ReplayGainAlbumGain,
	"replaygain_album_peak":        tag.ReplayGainAlbumPeak,
	"musicbrainz track id":         tag.MusicBrainzRecordingID,
	"musicbrainz release track id": tag.MusicBrainzReleaseID,
	"musicbrainz artist id":        tag.MusicBrainzArtistID,
	"musicbrainz album artist id":  tag.MusicBrainzAlbumArtistID,
	"musicbrainz album id":         tag.MusicBrainzReleaseID,
	"musicbrainz disc id":          "musicbrainz_discid",
	"barcode":                      tag.Barcode,
	"catalognumber":                tag.CatalogNumber,
	"upc":                          tag.UPC,
	"isrc":                         tag.ISRC,
	"label":                        tag.Label,
	"organization":                 tag.Publisher,
	"script":                       tag.Script,
}

// Common renders the tag as normalized [tag.Tag] fields.
//
// Frames whose ID has no common equivalent are kept under a lowercased name:
// the frame ID itself for plain text frames, the TXXX description for user
// text, and "comment:<description>" or "lyrics:<description>" for the
// described variants of COMM and USLT. Artwork is not text and is left out;
// see [Tag.Pictures].
func (t *Tag) Common() tag.Tag {
	out := tag.Tag{}
	if t == nil {
		return out
	}

	for _, f := range t.Frames {
		switch {
		case f.Name == FrameGenre || f.Name == FrameGenreOld:
			// TCON packs numbers and names into its values; resolve both.
			out.Add(tag.Genre, ParseGenres(f.Text)...)

		case f.Name == FrameUserText || f.Name == frameUserText2:
			key, ok := userTextKeys[strings.ToLower(strings.TrimSpace(f.Desc))]
			if !ok {
				key = strings.ToLower(strings.TrimSpace(f.Desc))
			}
			if key != "" {
				out.Add(key, f.Text...)
			}

		case f.Name == FrameComment || f.Name == frameComment2:
			addDescribed(out, tag.Comment, f)

		case f.Name == FrameLyrics || f.Name == frameLyrics2:
			addDescribed(out, tag.Lyrics, f)

		case f.Name == FrameUserURL || f.Name == frameUserURL2:
			key := "url"
			if f.Desc != "" {
				key = "url:" + strings.ToLower(strings.TrimSpace(f.Desc))
			}
			out.Add(key, f.Text...)

		case len(f.Text) > 0:
			key, ok := frameKeys[f.Name]
			if !ok {
				key = strings.ToLower(f.Name)
			}
			out.Add(key, f.Text...)
		}
	}

	finishPositions(out)
	return out
}

// addDescribed files a COMM or USLT frame under its bare common key when it has
// no description, and under a "key:description" key when it does, so that the
// many comments iTunes files under a name do not bury the real one.
func addDescribed(out tag.Tag, key string, f Frame) {
	desc := strings.TrimSpace(f.Desc)
	if desc == "" {
		out.Add(key, f.Text...)
		return
	}
	out.Add(key+":"+strings.ToLower(desc), f.Text...)
}

// finishPositions splits the "3/11" values of track and disc into a position
// and a total, and drops the parts the file never recorded.
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
	}
}
