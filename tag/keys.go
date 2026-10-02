package tag

import "strings"

// The normalized field names used across every supported container. Keys are
// lowercase; a Tag produced by this module never uses uppercase keys, so
// callers may normalize a key of their own with strings.ToLower before looking
// it up.
const (
	// Descriptive fields.
	Title       = "title"
	Subtitle    = "subtitle"
	Artist      = "artist"
	AlbumArtist = "albumartist"
	Album       = "album"
	Composer    = "composer"
	Conductor   = "conductor"
	Arranger    = "arranger"
	Remixer     = "remixer"
	Lyricist    = "lyricist"
	Grouping    = "grouping"
	Mood        = "mood"
	Genre       = "genre"

	// Organization and rights.
	Label            = "label"
	Publisher        = "publisher"
	Copyright        = "copyright"
	EncodedBy        = "encodedby"
	EncoderSettings  = "encodersettings"
	Barcode          = "barcode"
	CatalogNumber    = "catalognumber"
	ISRC             = "isrc"
	UPC              = "upc"
	Compilation      = "compilation"
	OriginalFilename = "originalfilename"

	// Dates.
	Date         = "date"
	Year         = "year"
	ReleaseDate  = "releasedate"
	OriginalDate = "originaldate"

	// Positions within a release. Track and Disc hold a position, optionally
	// suffixed with a total as in "3/11"; TrackTotal and DiscTotal hold the
	// totals on their own when the source recorded them separately.
	Track      = "track"
	TrackTotal = "tracktotal"
	Disc       = "disc"
	DiscTotal  = "disctotal"

	// Sort names.
	TitleSort       = "titlesort"
	AlbumSort       = "albumsort"
	ArtistSort      = "artistsort"
	AlbumArtistSort = "albumartistsort"

	// Free text.
	Comment  = "comment"
	Lyrics   = "lyrics"
	Language = "language"
	Script   = "script"

	// Miscellaneous numbers and identifiers.
	BPM = "bpm"

	// ReplayGain, as the "-8.08 dB" and "0.9976" strings the taggers wrote.
	ReplayGainTrackGain = "replaygain_track_gain"
	ReplayGainTrackPeak = "replaygain_track_peak"
	ReplayGainAlbumGain = "replaygain_album_gain"
	ReplayGainAlbumPeak = "replaygain_album_peak"

	// MusicBrainz identifiers.
	MusicBrainzRecordingID   = "musicbrainz_trackid"
	MusicBrainzReleaseID     = "musicbrainz_albumid"
	MusicBrainzArtistID      = "musicbrainz_artistid"
	MusicBrainzAlbumArtistID = "musicbrainz_albumartistid"
)

// SplitTotal splits a positional tag value such as "3/11" into its position and
// its total. A value without a total returns an empty total, and an empty input
// returns two empty strings. Surrounding space is trimmed from both halves.
func SplitTotal(value string) (position, total string) {
	before, after, found := strings.Cut(value, "/")
	if !found {
		return strings.TrimSpace(before), ""
	}
	return strings.TrimSpace(before), strings.TrimSpace(after)
}

// JoinTotal renders a position and total as "3/11", or just the position when
// the total is empty.
func JoinTotal(position, total string) string {
	if total == "" {
		return position
	}
	return position + "/" + total
}
