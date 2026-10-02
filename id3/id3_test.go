package id3

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/raffleberry/tags/tag"
)

const dataDir = "../testdata/mutagen/"

// parseFile reads the ID3v2 tag at the start of a test file.
func parseFile(t *testing.T, name string) *Tag {
	t.Helper()
	f, err := os.Open(dataDir + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	tag, err := Read(f)
	if err != nil {
		t.Fatalf("Read(%q) = %v", name, err)
	}
	return tag
}

func TestReadHeader(t *testing.T) {
	tests := []struct {
		name    string
		version Version
		body    int
	}{
		// The version byte counts 2.2, 2.3 and 2.4 as 2, 3 and 4.
		{"silence-44-s.mp3", Version{2, 3}, 1304},
		{"id3v22-test.mp3", Version{2, 2}, 2215},
		{"id3v1v2-combined.mp3", Version{2, 4}, 2215},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := os.Open(dataDir + tt.name)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()

			h, err := ReadHeader(f)
			if err != nil {
				t.Fatalf("ReadHeader() = %v", err)
			}
			if h.Version != tt.version {
				t.Errorf("Version = %v, want %v", h.Version, tt.version)
			}
			if h.Body != tt.body {
				t.Errorf("Body = %d, want %d", h.Body, tt.body)
			}
			// The header and the body together are what the reader must skip to
			// reach the audio.
			if got, want := h.Total(), h.Body+10; got != want {
				t.Errorf("Total() = %d, want %d", got, want)
			}
		})
	}
}

// TestNoTag covers data that does not begin with an ID3v2 identifier, which is
// the normal case for a file with no tags at all.
func TestNoTag(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"shorter than a header", []byte("ID3\x03\x00")},
		{"audio", []byte{0xFF, 0xFB, 0x90, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}},
		{"wrong identifier", []byte("ID4\x03\x00\x00\x00\x00\x00\x00\x00")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ReadHeader(bytes.NewReader(tt.data)); !errors.Is(err, ErrNoTag) {
				t.Errorf("ReadHeader() = %v, want %v", err, ErrNoTag)
			}
		})
	}
}

// TestUnsupportedVersion covers the version bytes outside the range this package
// reads.
func TestUnsupportedVersion(t *testing.T) {
	for _, minor := range []byte{0, 1, 5, 255} {
		data := append([]byte("ID3"), minor, 0, 0, 0, 0, 0, 0, 0)
		if _, err := ReadHeader(bytes.NewReader(data)); !errors.Is(err, ErrVersion) {
			t.Errorf("ReadHeader(version %d) = %v, want %v", minor, err, ErrVersion)
		}
	}
}

// TestNonSynchsafeSize covers a header whose size field uses all eight bits of a
// byte, which the synchsafe encoding forbids because it would be ambiguous with
// audio.
func TestNonSynchsafeSize(t *testing.T) {
	data := []byte("ID3\x03\x00\x00\x80\x00\x00\x01")
	if _, err := ReadHeader(bytes.NewReader(data)); !errors.Is(err, ErrSize) {
		t.Errorf("ReadHeader() = %v, want %v", err, ErrSize)
	}
}

// TestInvalidHeaderFlags covers the reserved bits of the header flags, which a
// conforming writer leaves clear.
func TestInvalidHeaderFlags(t *testing.T) {
	tests := []struct {
		name   string
		flags  byte
		reject bool
	}{
		{"no flags", 0x00, false},
		{"unsynchronised", 0x80, false},
		{"extended header", 0x40, false},
		{"experimental", 0x20, false},
		{"footer", 0x10, false},
		// The low nibble is reserved in ID3v2.4.
		{"reserved bit set", 0x01, true},
		// A v2.3 tag reserves more of the byte, since its compression bit was
		// never used.
		{"v2.3 compression bit", 0x08, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := append([]byte("ID3\x04\x00"), tt.flags, 0, 0, 0, 0)
			_, err := ReadHeader(bytes.NewReader(data)) //nolint:staticcheck
			if tt.reject && !errors.Is(err, ErrSize) {
				t.Errorf("ReadHeader(flags %#02x) = %v, want %v", tt.flags, err, ErrSize)
			}
			if !tt.reject && err != nil {
				t.Errorf("ReadHeader(flags %#02x) = %v, want it accepted", tt.flags, err)
			}
		})
	}
}

func TestFrames(t *testing.T) {
	t.Run("v2.3", func(t *testing.T) {
		tag := parseFile(t, "silence-44-s.mp3")

		if got, want := len(tag.Frames), 9; got != want {
			t.Errorf("len(Frames) = %d, want %d", got, want)
		}
		// A frame ID with a multi valued field keeps every value.
		if got, want := tag.Values("TPE1"), []string{"piman", "jzig"}; len(got) != 2 {
			t.Errorf("TPE1 = %q, want %q", got, want)
		}
		if got, want := tag.Value("TIT2"), "Silence"; got != want {
			t.Errorf("TIT2 = %q, want %q", got, want)
		}
		if !tag.Has("TRCK") {
			t.Error("Has(TRCK) = false, want true")
		}
		if got, want := tag.Value("XXXX"), ""; got != want {
			t.Errorf("Value(XXXX) = %q, want %q", got, want)
		}
	})

	t.Run("v2.2", func(t *testing.T) {
		tag := parseFile(t, "id3v22-test.mp3")

		// ID3v2.2 uses three character frame IDs, so the same field is "TT2"
		// rather than "TIT2".
		if got, want := tag.Version.Minor, 2; got != want {
			t.Errorf("Version.Minor = %d, want %d", got, want)
		}
		if got, want := tag.Value("TT2"), "cosmic american"; got != want {
			t.Errorf("TT2 = %q, want %q", got, want)
		}
		if got, want := tag.Value("TP1"), "Anais Mitchell"; got != want {
			t.Errorf("TP1 = %q, want %q", got, want)
		}
	})

	t.Run("v2.4", func(t *testing.T) {
		tag := parseFile(t, "id3v1v2-combined.mp3")

		if got, want := tag.Version.Minor, 4; got != want {
			t.Errorf("Version.Minor = %d, want %d", got, want)
		}
		if got, want := tag.Value("TIT2"), "cosmic american"; got != want {
			t.Errorf("TIT2 = %q, want %q", got, want)
		}
	})
}

// TestComments covers COMM frames, which carry a language code and a description
// alongside their text.
func TestComments(t *testing.T) {
	tag := parseFile(t, "id3v1v2-combined.mp3")

	// The bare comment, which is the one with no description, is the real
	// comment. The others are iTunes bookkeeping filed under names of its own.
	byDesc := map[string]Frame{}
	for _, f := range tag.Frames {
		if f.Name == "COMM" {
			byDesc[f.Desc] = f
		}
	}

	bare, ok := byDesc[""]
	if !ok {
		t.Fatal("no bare COMM frame, want one")
	}
	if got, want := bare.Lang, "eng"; got != want {
		t.Errorf("Lang = %q, want %q", got, want)
	}
	if got, want := bare.Text[0], "Waterbug Records, www.anaismitchell.com"; got != want {
		t.Errorf("Text = %q, want %q", got, want)
	}

	for _, want := range []string{"iTunNORM", "iTunes_CDDB_1", "iTunes_CDDB_TrackNumber"} {
		if _, ok := byDesc[want]; !ok {
			t.Errorf("no COMM frame described as %q", want)
		}
	}
}

func TestUserText(t *testing.T) {
	tag := parseFile(t, "silence-44-s.mp3")

	// This file has no TXXX frames, which is the ordinary case.
	if got := tag.UserText(); len(got) != 0 {
		t.Errorf("UserText() = %v, want none", got)
	}
	if got := tag.UserURL(); len(got) != 0 {
		t.Errorf("UserURL() = %v, want none", got)
	}
}

// TestUniqueFileID covers the UFID frame, which pairs an identifier with the
// database that issued it.
func TestUniqueFileID(t *testing.T) {
	tag := parseFile(t, "silence-44-s.mp3")

	// This file has none, so both the value and the lookup report that.
	if got, ok := tag.UniqueFileID("http://musicbrainz.org"); got != nil || ok {
		t.Errorf("UniqueFileID() = %v, %v, want nil, false", got, ok)
	}
	if got, ok := tag.UniqueFileID(""); got != nil || ok {
		t.Errorf("UniqueFileID(\"\") = %v, %v, want nil, false", got, ok)
	}
}

// TestCommon covers the mapping onto the normalized key space.
func TestCommon(t *testing.T) {
	t.Run("v2.3", func(t *testing.T) {
		tags := parseFile(t, "silence-44-s.mp3").Common()

		want := map[string]string{
			tag.Title:      "Silence",
			tag.Album:      "Quod Libet Test Data",
			tag.Genre:      "Silence",
			tag.Date:       "2004",
			tag.Track:      "02",
			tag.TrackTotal: "10",
		}
		for key, value := range want {
			if got := tags.Value(key); got != value {
				t.Errorf("Tags()[%q] = %q, want %q", key, got, value)
			}
		}
		// TLEN is the playback time in milliseconds, which has no common
		// counterpart and so keeps its frame name.
		if got, want := tags.Value("length_ms"), "3000"; got != want {
			t.Errorf("length_ms = %q, want %q", got, want)
		}
	})

	t.Run("v2.2", func(t *testing.T) {
		tags := parseFile(t, "id3v22-test.mp3").Common()

		// The three character IDs map onto the same keys as the four character
		// ones, so a v2.2 tag is as usable as any other.
		for key, want := range map[string]string{
			tag.Title:      "cosmic american",
			tag.Artist:     "Anais Mitchell",
			tag.Album:      "Hymns for the Exiled",
			tag.Track:      "3",
			tag.TrackTotal: "11",
			tag.Date:       "2004",
			tag.EncodedBy:  "iTunes v4.6",
			tag.Comment:    "Waterbug Records, www.anaismitchell.com",
		} {
			if got := tags.Value(key); got != want {
				t.Errorf("Tags()[%q] = %q, want %q", key, got, want)
			}
		}
		// A comment with a description keeps it, so that the many comments
		// iTunes writes do not bury the real one.
		if got := tags.Value("comment:itunnorm"); got == "" {
			t.Error("comment:itunnorm is missing, want the normalization data")
		}
	})
}

func TestV1(t *testing.T) {
	tests := []struct {
		name    string
		title   string
		artist  string
		album   string
		year    string
		comment string
		track   int
		genre   string
	}{
		// An ID3v1.1 tag, which is the common form and carries a track number.
		{
			"silence-44-s-v1.mp3", "Silence", "piman", "Quod Libet Test Data",
			"2004", "", 2, "Darkwave",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := os.Open(dataDir + tt.name)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()

			st, err := f.Stat()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.Seek(st.Size()-128, 0); err != nil {
				t.Fatal(err)
			}

			v1, err := ReadV1(f)
			if err != nil {
				t.Fatalf("ReadV1() = %v", err)
			}
			if v1.Title != tt.title {
				t.Errorf("Title = %q, want %q", v1.Title, tt.title)
			}
			if v1.Artist != tt.artist {
				t.Errorf("Artist = %q, want %q", v1.Artist, tt.artist)
			}
			if v1.Album != tt.album {
				t.Errorf("Album = %q, want %q", v1.Album, tt.album)
			}
			if v1.Year != tt.year {
				t.Errorf("Year = %q, want %q", v1.Year, tt.year)
			}
			if v1.Comment != tt.comment {
				t.Errorf("Comment = %q, want %q", v1.Comment, tt.comment)
			}
			if v1.Track != tt.track {
				t.Errorf("Track = %d, want %d", v1.Track, tt.track)
			}
			if v1.Genre != tt.genre {
				t.Errorf("Genre = %q, want %q", v1.Genre, tt.genre)
			}
		})
	}
}

// TestV1Genre covers the genre byte, which indexes a list of 192 names and where
// the value 255 means no genre at all.
func TestV1Genre(t *testing.T) {
	tests := []struct {
		name  string
		byte  byte
		genre string
	}{
		// 255 is what an encoder writes when there is no genre.
		{"unset", 255, ""},
		{"first of the list", 0, "Blues"},
		{"rock", 17, "Rock"},
		{"last of the list", 191, "Psybient"},
		// A byte past the end of the list names nothing this package knows.
		{"past the end", 254, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v1 := buildV1(v1Tag{genre: tt.byte})
			if got := v1.Genre; got != tt.genre {
				t.Errorf("Genre = %q, want %q", got, tt.genre)
			}
		})
	}
}

// TestV1NoTag covers data that does not begin with "TAG", which is what a file
// with no ID3v1 tag looks like at the end.
func TestV1NoTag(t *testing.T) {
	if _, err := ParseV1(make([]byte, 128)); !errors.Is(err, ErrNoV1) {
		t.Errorf("ParseV1() = %v, want %v", err, ErrNoV1)
	}
	if _, err := ParseV1([]byte("TAG")); !errors.Is(err, ErrNoV1) {
		t.Errorf("ParseV1(short) = %v, want %v", err, ErrNoV1)
	}
	if _, err := ParseV1(nil); !errors.Is(err, ErrNoV1) {
		t.Errorf("ParseV1(empty) = %v, want %v", err, ErrNoV1)
	}
}

// TestV1Track covers the two forms the tag comes in: ID3v1.0 has no track
// number, and ID3v1.1 takes the last two bytes of the comment for one.
func TestV1Track(t *testing.T) {
	// ID3v1.0: the whole thirty byte comment is the text.
	// ID3v1.0: the whole thirty byte comment field is text, with no marker at
	// its end.
	v10 := buildV1(v1Tag{comment: "a comment that fills the field entirely"})
	if v10.Track != 0 {
		t.Errorf("Track = %d, want 0 for an ID3v1.0 tag", v10.Track)
	}
	if !strings.HasPrefix(v10.Comment, "a comment") {
		t.Errorf("Comment = %q, want the whole field", v10.Comment)
	}

	// ID3v1.1: a zero byte and a track number at the end of the comment mark the
	// shorter form.
	v11 := buildV1(v1Tag{comment: "short comment", track: 7})
	if v11.Track != 7 {
		t.Errorf("Track = %d, want 7", v11.Track)
	}
	if got, want := v11.Comment, "short comment"; got != want {
		t.Errorf("Comment = %q, want %q", got, want)
	}
}

// v1Tag describes the fields of an ID3v1 tag that the tests set.
type v1Tag struct {
	title   string
	artist  string
	album   string
	year    string
	comment string
	// track is written as an ID3v1.1 marker: a zero byte then the number.
	track int
	// genre is the genre byte, where 255 means none.
	genre byte
}

// buildV1 assembles an ID3v1 tag from its fields.
func buildV1(fields v1Tag) *V1 {
	data := make([]byte, 128)
	copy(data, "TAG")
	copy(data[3:33], fields.title)
	copy(data[33:63], fields.artist)
	copy(data[63:93], fields.album)
	copy(data[93:97], fields.year)
	copy(data[97:127], fields.comment)
	if fields.track > 0 {
		data[125] = 0
		data[126] = byte(fields.track)
	}
	if fields.genre != 0 {
		data[127] = fields.genre
	}

	v1, err := ParseV1(data)
	if err != nil {
		panic(err)
	}
	return v1
}

func TestGenres(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   []string
	}{
		{"a plain name", []string{"Rock"}, []string{"Rock"}},
		// The numbers the nineties taggers wrote.
		{"a bare number", []string{"17"}, []string{"Rock"}},
		{"a number in parentheses", []string{"(17)"}, []string{"Rock"}},
		// A number with a name after it, which wins over the number.
		{"number and name", []string{"(17)Britpop"}, []string{"Rock", "Britpop"}},
		// Several numbers.
		{"two numbers", []string{"(17)(20)"}, []string{"Rock", "Alternative"}},
		// The two named tokens.
		{"remix", []string{"(RX)"}, []string{"Remix"}},
		{"cover", []string{"(CR)"}, []string{"Cover"}},
		// A number past the end of the list resolves to nothing.
		{"number past the list", []string{"(255)"}, nil},
		{"several values", []string{"Rock", "Jazz"}, []string{"Rock", "Jazz"}},
		{"empty", []string{""}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseGenres(tt.values)
			if len(got) != len(tt.want) {
				t.Fatalf("ParseGenres(%q) = %q, want %q", tt.values, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ParseGenres(%q)[%d] = %q, want %q", tt.values, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestGenreRepetition covers a name that repeats the number it came with, which
// should not be reported twice.
func TestGenreRepetition(t *testing.T) {
	if got, want := ParseGenres([]string{"(17)Rock"}), []string{"Rock"}; len(got) != 1 || got[0] != want[0] {
		t.Errorf("ParseGenres() = %q, want %q", got, want)
	}
}

func TestGenreByNumber(t *testing.T) {
	// ID3 counts genres from zero, so the first entry is 0.
	if got, ok := Genre(0); !ok || got != "Blues" {
		t.Errorf("Genre(0) = %q, %v, want %q, true", got, ok, "Blues")
	}
	if got, ok := Genre(17); !ok || got != "Rock" {
		t.Errorf("Genre(17) = %q, %v, want %q, true", got, ok, "Rock")
	}
	if _, ok := Genre(-1); ok {
		t.Error("Genre(-1) = ok, want false")
	}
	if _, ok := Genre(len(tag.Genres)); ok {
		t.Error("Genre(past the end) = ok, want false")
	}
}

// TestTextEncodings covers the four encodings a text frame may use.
func TestTextEncodings(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		// Latin-1, where a byte above 0x7F is a character rather than part of a
		// multi byte sequence.
		{"latin1", []byte{0, 'a', 'f', 0xFC, 'r'}, "afür"},
		{"latin1 and utf8 agree on ascii", []byte{3, 'a', 'b', 'c'}, "abc"},
		// UTF-8, with a character above the basic plane.
		{"utf8", []byte{3, 0xE3, 0x81, 0x82, 0xE3, 0x81, 0x84, 0xE3, 0x81, 0x86}, "あいう"},
		// UTF-16 with a byte order mark, little endian.
		{"utf16 little endian", []byte{1, 0xFF, 0xFE, 'a', 0x00, 'b', 0x00}, "ab"},
		// UTF-16 with a byte order mark, big endian.
		{"utf16 big endian", []byte{1, 0xFE, 0xFF, 0x00, 'a', 0x00, 'b'}, "ab"},
		// UTF-16 with no mark, which the specification says to read as big
		// endian.
		{"utf16 without a mark", []byte{2, 0x00, 'a', 0x00, 'b'}, "ab"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeText(encoding(tt.data[0]), tt.data)
			if len(got) != 1 || got[0] != tt.want {
				t.Errorf("decodeText() = %q, want [%q]", got, tt.want)
			}
		})
	}
}

// TestTextValues covers a frame with more than one value, which are separated by
// the encoding's terminator.
func TestTextValues(t *testing.T) {
	// Two UTF-8 values.
	multi := []byte{3, 'a', 0, 'b', 0}
	if got, want := decodeText(3, multi), []string{"a", "b"}; len(got) != 2 {
		t.Errorf("decodeText() = %q, want %q", got, want)
	}
	// Two UTF-16 values, where the terminator is two bytes. A single zero byte is
	// part of a character and must not be taken for the end of a value.
	utf16Multi := []byte{2, 0, 'a', 0, 0, 'b', 0, 0}
	if got, want := decodeText(2, utf16Multi), []string{"a", "b"}; len(got) != 2 {
		t.Errorf("decodeText() = %q, want %q", got, want)
	}
	// Trailing padding produces empty values, which carry no information.
	padded := []byte{3, 'a', 0, 0, 0, 0}
	if got, want := decodeText(3, padded), []string{"a"}; len(got) != 1 {
		t.Errorf("decodeText(padded) = %q, want %q", got, want)
	}
}

// TestUnsynchro covers the unsynchronisation scheme, where a byte equal to 0xFF
// inside the data is followed by a zero byte that must be dropped. Without it a
// tag would look like a false sync to a player.
func TestUnsynchro(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want []byte
	}{
		{"nothing to do", []byte{1, 2, 3}, []byte{1, 2, 3}},
		{"one pair", []byte{1, 0xFF, 0x00, 2}, []byte{1, 0xFF, 2}},
		{"two pairs", []byte{0xFF, 0x00, 0xFF, 0x00}, []byte{0xFF, 0xFF}},
		{"zero without a preceding 0xFF", []byte{1, 0, 2}, []byte{1, 0, 2}},
		// A trailing 0xFF with nothing after it is left alone.
		{"trailing 0xFF", []byte{1, 0xFF}, []byte{1, 0xFF}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deunsynchronise(tt.in)
			if !bytes.Equal(got, tt.want) {
				t.Errorf("deunsynchronise(% x) = % x, want % x", tt.in, got, tt.want)
			}
		})
	}
}

// TestSynchsafe covers the integer encoding that keeps a length from looking like
// a frame sync, and the rejection of data that does not use it.
func TestSynchsafe(t *testing.T) {
	tests := []struct {
		value int
		want  []byte
	}{
		{0, []byte{0, 0, 0, 0}},
		{1, []byte{0, 0, 0, 1}},
		{127, []byte{0, 0, 0, 127}},
		{128, []byte{0, 0, 1, 0}},
		{2097151, []byte{0, 127, 127, 127}},
		{268435455, []byte{127, 127, 127, 127}},
	}

	for _, tt := range tests {
		got := synchsafe(tt.value)
		if !bytes.Equal(got, tt.want) {
			t.Errorf("synchsafe(%d) = % x, want % x", tt.value, got, tt.want)
		}
		// What was written must read back as the same value.
		back, ok := unsynchsafe(got)
		if !ok || back != tt.value {
			t.Errorf("unsynchsafe(% x) = %d, %v, want %d, true", got, back, ok, tt.value)
		}
	}

	// A value with a high bit set is not synchsafe, which is how a plain integer
	// is told from a real one.
	if _, ok := unsynchsafe([]byte{0xFF, 0xFF, 0xFF, 0xFF}); ok {
		t.Error("unsynchsafe(all high bits) = ok, want false")
	}
	if _, ok := unsynchsafe([]byte{1, 2}); ok {
		t.Error("unsynchsafe(short) = ok, want false")
	}
}

// TestPictureMIMEFromMagic covers the MIME type of a picture that was left out,
// which the magic bytes of the image can make up for.
func TestPictureMIMEFromMagic(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0}, "image/jpeg"},
		{"png", []byte{0x89, 'P', 'N', 'G', 0x0D}, "image/png"},
		{"gif", []byte("GIF89a"), "image/gif"},
		{"bmp", []byte("BM\x00\x00"), "image/bmp"},
		{"unknown", []byte("not an image"), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mimeFromImage(tt.data); got != tt.want {
				t.Errorf("mimeFromImage(% x) = %q, want %q", tt.data, got, tt.want)
			}
		})
	}
}
