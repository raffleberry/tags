package flac

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/raffleberry/tags/tag"
)

const dataDir = "../testdata/mutagen/"

func open(t *testing.T, name string) *File {
	t.Helper()
	f, err := Open(dataDir + name)
	if err != nil {
		t.Fatalf("Open(%q) = %v", name, err)
	}
	return f
}

func assertClose(t *testing.T, got time.Duration, want float64) {
	t.Helper()
	if math.Abs(got.Seconds()-want) > 0.001 {
		t.Errorf("Duration = %v, want ~%v s", got, want)
	}
}

func TestAudioProperties(t *testing.T) {
	tests := []struct {
		name     string
		duration float64
		rate     int
		channels int
		depth    int
	}{
		{"silence-44-s.flac", 3.68472, 44100, 2, 16},
		{"no-tags.flac", 3.68472, 44100, 2, 16},
		{"variable-block.flac", 261.68, 44100, 2, 16},
		{"flac_application.flac", 273.64, 44100, 2, 16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			audio := open(t, tt.name).Audio()

			assertClose(t, audio.Duration, tt.duration)
			if audio.SampleRate != tt.rate {
				t.Errorf("SampleRate = %d, want %d", audio.SampleRate, tt.rate)
			}
			if audio.Channels != tt.channels {
				t.Errorf("Channels = %d, want %d", audio.Channels, tt.channels)
			}
			if audio.BitsPerSample != tt.depth {
				t.Errorf("BitsPerSample = %d, want %d", audio.BitsPerSample, tt.depth)
			}
			if audio.Bitrate <= 0 {
				t.Error("Bitrate = 0, want the size of the audio to give one")
			}
		})
	}
}

func TestTags(t *testing.T) {
	f := open(t, "silence-44-s.flac")
	tags := f.Tags()

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

	// Two ARTIST comments become two values of one key, which is why the tag is
	// a map of slices.
	if got, want := tags.Values(tag.Artist), []string{"piman", "jzig"}; len(got) != 2 {
		t.Errorf("artists = %q, want %q", got, want)
	}

	// The writer names itself in the block.
	if got, want := f.Vendor(), "reference libFLAC 1.1.0 20030126"; got != want {
		t.Errorf("Vendor() = %q, want %q", got, want)
	}
}

// TestSplitTrackNumber covers a comment that holds the position and the total
// together, as "02/10", which is split so that a caller can read either half.
func TestSplitTrackNumber(t *testing.T) {
	tags := open(t, "silence-44-s.flac").Tags()

	if got, want := tags.Value(tag.Track), "02"; got != want {
		t.Errorf("track = %q, want %q", got, want)
	}
	if got, want := tags.Value(tag.TrackTotal), "10"; got != want {
		t.Errorf("tracktotal = %q, want %q", got, want)
	}
}

// TestSeparateTrackTotal covers a file that gives the position and the total as
// two comments of their own, which is the other way a tagger writes it.
func TestSeparateTrackTotal(t *testing.T) {
	tags := open(t, "variable-block.flac").Tags()

	if got, want := tags.Value(tag.Track), "01"; got != want {
		t.Errorf("track = %q, want %q", got, want)
	}
	// This file states the total in both TOTALTRACKS and TRACKTOTAL, which are
	// the same field under two names.
	if got, want := tags.Value(tag.TrackTotal), "11"; got != want {
		t.Errorf("tracktotal = %q, want %q", got, want)
	}
	if got, want := tags.Value(tag.Disc), "1"; got != want {
		t.Errorf("disc = %q, want %q", got, want)
	}
	if got, want := tags.Value(tag.DiscTotal), "2"; got != want {
		t.Errorf("disctotal = %q, want %q", got, want)
	}
}

// TestUnknownFieldsKeepTheirNames covers comments with no common meaning, which
// are kept under a lowercased version of their own name so that nothing is lost.
func TestUnknownFieldsKeepTheirNames(t *testing.T) {
	tags := open(t, "variable-block.flac").Tags()

	// A Japanese title, which also checks that a non ASCII value survives.
	if got, want := tags.Value("japanese title"), "アップルシード オリジナル・サウンドトラック"; got != want {
		t.Errorf("japanese title = %q, want %q", got, want)
	}
	// A ripper tool, which has no common key.
	if got := tags.Value("ripper"); got == "" {
		t.Error("ripper is missing, want the name of the tool that made the file")
	}
	if got, want := tags.Value("discid"), "AA0B360B"; got != want {
		t.Errorf("discid = %q, want %q", got, want)
	}
}

func TestReplayGain(t *testing.T) {
	tags := open(t, "variable-block.flac").Tags()

	want := map[string]string{
		tag.ReplayGainTrackGain: "-9.61 dB",
		tag.ReplayGainTrackPeak: "1.000000",
		tag.ReplayGainAlbumGain: "-8.68 dB",
		tag.ReplayGainAlbumPeak: "1.000000",
	}
	for key, value := range want {
		if got := tags.Value(key); got != value {
			t.Errorf("Tags()[%q] = %q, want %q", key, got, value)
		}
	}
}

func TestMusicBrainzIDs(t *testing.T) {
	tags := open(t, "flac_application.flac").Tags()

	want := map[string]string{
		tag.MusicBrainzRecordingID:   "e65fb332-0c1e-4172-85e0-59cd37e5669e",
		tag.MusicBrainzReleaseID:     "359a91e9-3bb3-4b60-a823-8aaa4bad1e36",
		tag.MusicBrainzAlbumArtistID: "e5c7b94f-e264-473c-bb0f-37c85d4d5c70",
	}
	for key, value := range want {
		if got := tags.Value(key); got != value {
			t.Errorf("Tags()[%q] = %q, want %q", key, got, value)
		}
	}
	// The names this file uses have no entry in the table, so they keep their own
	// lowercased names. The values are what identify the release, so check that
	// they are reachable.
	if got, want := tags.Value("musicbrainz_albumid"), want[tag.MusicBrainzReleaseID]; got != want {
		t.Errorf("musicbrainz_albumid = %q, want %q", got, want)
	}
	if got, want := tags.Value("labelid"), "RTRADLP480"; got != want {
		t.Errorf("labelid = %q, want %q", got, want)
	}
}

func TestNoTags(t *testing.T) {
	f := open(t, "no-tags.flac")

	if got := f.Tags(); len(got) != 0 {
		t.Errorf("Tags() = %v, want no fields", got)
	}
	if got := f.Pictures(); len(got) != 0 {
		t.Errorf("Pictures() = %v, want none", got)
	}
	if got := f.Vendor(); got != "" {
		t.Errorf("Vendor() = %q, want empty", got)
	}
	// The stream is still readable.
	if f.Audio().SampleRate != 44100 {
		t.Errorf("SampleRate = %d, want 44100", f.Audio().SampleRate)
	}
}

func TestPictures(t *testing.T) {
	f := open(t, "silence-44-s.flac")
	pictures := f.Pictures()

	if len(pictures) != 1 {
		t.Fatalf("len(Pictures()) = %d, want 1", len(pictures))
	}
	p := pictures[0]

	// A one pixel PNG, which FLAC describes in full where the other formats only
	// record the image.
	if got, want := p.MIME, "image/png"; got != want {
		t.Errorf("MIME = %q, want %q", got, want)
	}
	if got, want := p.Type, tag.PictureCoverFront; got != want {
		t.Errorf("Type = %v, want %v", got, want)
	}
	if got, want := p.Desc, "A pixel."; got != want {
		t.Errorf("Desc = %q, want %q", got, want)
	}
	if p.Width != 1 || p.Height != 1 {
		t.Errorf("size = %dx%d, want 1x1", p.Width, p.Height)
	}
	if got, want := p.Depth, 24; got != want {
		t.Errorf("Depth = %d, want %d", got, want)
	}
	if p.Colors != 0 {
		t.Errorf("Colors = %d, want 0", p.Colors)
	}
	if got, want := len(p.Data), 150; got != want {
		t.Errorf("len(Data) = %d, want %d", got, want)
	}
}

func TestSeekTable(t *testing.T) {
	f := open(t, "silence-44-s.flac")

	table := f.SeekTable()
	if table == nil {
		t.Fatal("SeekTable() = nil, want one")
	}
	// Six points, the last of which is a placeholder that names no frame.
	if got, want := len(table.Points), 6; got != want {
		t.Fatalf("len(Points) = %d, want %d", got, want)
	}

	first := table.Points[0]
	if first.FirstSample != 0 || first.ByteOffset != 0 || first.NumSamples != 4608 {
		t.Errorf("first point = %+v, want the start of the file", first)
	}
	if last := table.Points[len(table.Points)-1]; !last.IsPlaceholder() {
		t.Errorf("last point = %+v, want a placeholder", last)
	}
}

// TestBlocks covers the metadata blocks as a whole, including the ones this
// package does not decode.
func TestBlocks(t *testing.T) {
	f := open(t, "silence-44-s.flac")

	want := []int{
		BlockStreamInfo, BlockSeektable, BlockVorbis,
		BlockCueSheet, BlockPicture, BlockPadding,
	}
	blocks := f.Blocks()
	if len(blocks) != len(want) {
		t.Fatalf("len(Blocks()) = %d, want %d", len(blocks), len(want))
	}
	for i, blockType := range want {
		if blocks[i].Type != blockType {
			t.Errorf("Blocks()[%d].Type = %d, want %d", i, blocks[i].Type, blockType)
		}
	}

	// The file has an application block this package does not decode, which must
	// still be present.
	app := open(t, "flac_application.flac")
	found := false
	for _, block := range app.Blocks() {
		if block.Type == BlockApplication {
			found = true
		}
	}
	if !found {
		t.Error("no application block, want one")
	}
}

func TestMatches(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"silence-44-s.flac", true},
		{"no-tags.flac", true},
		{"has-tags.m4a", false},
		{"silence-44-s.mp3", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Matches(readMarker(t, tt.name)); got != tt.want {
				t.Errorf("Matches(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

// readMarker returns the first bytes of a test file, which is what a sniffing
// reader gets to look at.
func readMarker(t *testing.T, name string) []byte {
	t.Helper()
	f, err := os.Open(dataDir + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	marker := make([]byte, 4)
	if _, err := io.ReadFull(f, marker); err != nil {
		t.Fatal(err)
	}
	return marker
}

// TestNotFLAC covers files that start with something else, and files too short to
// hold a marker.
func TestNotFLAC(t *testing.T) {
	for _, name := range []string{"has-tags.m4a", "silence-44-s.mp3"} {
		if _, err := Open(dataDir + name); err == nil {
			t.Errorf("Open(%q) = nil error, want a failure", name)
		}
	}
	for _, name := range []string{"emptyfile.mp3", "too-short.mp3"} {
		if _, err := Open(dataDir + name); err == nil {
			t.Errorf("Open(%q) = nil error, want a failure", name)
		}
	}
}

// TestDamagedFiles covers files whose metadata blocks claim more bytes than the
// file holds. Some are still readable, and those that are not must fail rather
// than be read as something else.
func TestDamagedFiles(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		// A stream information block cut short leaves nothing to report.
		{"106-invalid-streaminfo.flac", true},
		// A picture block whose size field is wrong is still readable, since the
		// picture says how long it is.
		{"106-short-picture-block-size.flac", false},
		// Tags that were overwritten in place, leaving the padding short.
		{"52-overwritten-metadata.flac", false},
		{"52-too-short-block-size.flac", false},
		// A file that ends in the middle of a comment block.
		{"ooming-header.flac", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := Open(dataDir + tt.name)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Open(%q) = nil error, want a failure", tt.name)
				}
				return
			}
			if err != nil {
				t.Fatalf("Open(%q) = %v, want the file to be readable", tt.name, err)
			}
			if f.Audio().SampleRate == 0 {
				t.Error("SampleRate = 0, want the stream to be readable")
			}
		})
	}
}

// TestVorbisComment covers the comment format on its own, away from a file.
func TestVorbisComment(t *testing.T) {
	comment, err := ParseVorbisComment(buildComment("tagger", "TITLE=A Title", "ARTIST=An Artist", "ALBUM=An Album"))
	if err != nil {
		t.Fatalf("ParseVorbisComment() = %v", err)
	}

	if got, want := comment.Vendor, "tagger"; got != want {
		t.Errorf("Vendor = %q, want %q", got, want)
	}
	if got, want := len(comment.Fields), 3; got != want {
		t.Fatalf("len(Fields) = %d, want %d", got, want)
	}
	// The fields keep the order they were written in, which a map of slices
	// alone would lose.
	if got, want := comment.Fields[0].Key, "TITLE"; got != want {
		t.Errorf("Fields[0].Key = %q, want %q", got, want)
	}

	tags := comment.Common()
	for key, want := range map[string]string{
		tag.Title: "A Title", tag.Artist: "An Artist", tag.Album: "An Album",
	} {
		if got := tags.Value(key); got != want {
			t.Errorf("Tags()[%q] = %q, want %q", key, got, want)
		}
	}
}

// TestCommentWithoutEquals covers a comment with no equals sign, which names no
// field and so is skipped as the specification says.
func TestCommentWithoutEquals(t *testing.T) {
	comment, err := ParseVorbisComment(buildComment("tagger", "TITLE=A Title", "no equals sign here"))
	if err != nil {
		t.Fatalf("ParseVorbisComment() = %v", err)
	}
	if got, want := len(comment.Fields), 1; got != want {
		t.Errorf("len(Fields) = %d, want %d", got, want)
	}
	if got := comment.Common().Value(tag.Title); got != "A Title" {
		t.Errorf("title = %q, want %q", got, "A Title")
	}
}

// TestCommentWithRepeatedEquals covers a value that itself contains an equals
// sign, which must be kept whole rather than split on every one.
func TestCommentWithRepeatedEquals(t *testing.T) {
	comment, err := ParseVorbisComment(buildComment("tagger", "comment=a=b=c"))
	if err != nil {
		t.Fatalf("ParseVorbisComment() = %v", err)
	}
	if got, want := comment.Fields[0].Value, "a=b=c"; got != want {
		t.Errorf("Value = %q, want %q", got, want)
	}
}

// TestTruncatedComment covers a block whose count of fields is larger than the
// fields it holds, which the fields already read survive.
func TestTruncatedComment(t *testing.T) {
	data := buildComment("tagger", "TITLE=A Title")
	// Raise the count from one to a hundred.
	countAt := len(data) - 4 - len("TITLE=A Title") - 4
	copy(data[countAt:countAt+4], []byte{100, 0, 0, 0})

	comment, err := ParseVorbisComment(data)
	if err != nil {
		t.Fatalf("ParseVorbisComment() = %v", err)
	}
	if got, want := len(comment.Fields), 1; got != want {
		t.Errorf("len(Fields) = %d, want %d", got, want)
	}
}

// TestEmptyComment covers a block with a vendor string and no fields at all.
func TestEmptyComment(t *testing.T) {
	comment, err := ParseVorbisComment(buildComment("", ""))
	if err != nil {
		t.Fatalf("ParseVorbisComment() = %v", err)
	}
	if got := len(comment.Fields); got != 0 {
		t.Errorf("len(Fields) = %d, want 0", got)
	}
	if got := len(comment.Common()); got != 0 {
		t.Errorf("len(Common()) = %d, want 0", got)
	}
}

// buildComment assembles a Vorbis comment block from a vendor string and fields,
// which is what a tagger writes.
func buildComment(vendor string, fields ...string) []byte {
	buf := new(bytes.Buffer)
	writeCounted(buf, vendor)

	// The count of fields is a plain number, not a counted string.
	var count [4]byte
	binary.LittleEndian.PutUint32(count[:], uint32(len(fields)))
	buf.Write(count[:])

	for _, field := range fields {
		writeCounted(buf, field)
	}
	return buf.Bytes()
}

// writeCounted writes a length and then that many bytes, both little endian.
func writeCounted(buf *bytes.Buffer, s string) {
	buf.Write([]byte{byte(len(s)), byte(len(s) >> 8), byte(len(s) >> 16), byte(len(s) >> 24)})
	buf.WriteString(s)
}

// TestReadStreamInfo covers the stream information block on its own, whose fields
// do not line up with the bytes they occupy.
func TestReadStreamInfo(t *testing.T) {
	// The stream information block of the test file, which is 4608 sample blocks
	// of 44100 Hz stereo 16 bit audio.
	data, err := hexDecode(
		"12001200" + "000279" + "00052b" + "0ac442f0" + "00027ac0" +
			"6291dbd8dcb7dc480132e4c4ba154a17")
	if err != nil {
		t.Fatal(err)
	}

	audio, err := readStreamInfo(data)
	if err != nil {
		t.Fatalf("readStreamInfo() = %v", err)
	}
	if got, want := audio.SampleRate, 44100; got != want {
		t.Errorf("SampleRate = %d, want %d", got, want)
	}
	if got, want := audio.Channels, 2; got != want {
		t.Errorf("Channels = %d, want %d", got, want)
	}
	if got, want := audio.BitsPerSample, 16; got != want {
		t.Errorf("BitsPerSample = %d, want %d", got, want)
	}
	assertClose(t, audio.Duration, 3.68472)
}

// TestInvalidStreamInfo covers a block that is too short, or whose fields do not
// describe an audio stream.
func TestInvalidStreamInfo(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"shorter than the block", make([]byte, 33)},
		// A sample rate of zero, which no audio can have.
		{"zero sample rate", streamInfoWith(sampleRateField, 0)},
		// Block sizes that run backwards.
		{"block sizes reversed", streamInfoWith(minBlockField, 0xFFFF)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := readStreamInfo(tt.data); err == nil {
				t.Error("readStreamInfo() = nil error, want a rejection")
			}
		})
	}
}

// Bit positions within a stream information block, for building test blocks.
const (
	minBlockField    = 0
	sampleRateField  = 80
	blockSizeBitLen  = 16
	sampleRateBitLen = 20
)

// streamInfoWith returns a valid stream information block with one field set,
// which is how the invalid cases above are built.
func streamInfoWith(field int, value int) []byte {
	// Start from the test file's block and overwrite the field.
	data, _ := hexDecode(
		"12001200" + "000279" + "00052b" + "0ac442f0" + "00027ac0" +
			"6291dbd8dcb7dc480132e4c4ba154a17")

	bitPos := field
	for range sampleRateBitLen {
		byteIndex := bitPos / 8
		shift := 7 - bitPos%8
		if value&(1<<(sampleRateBitLen-1-(bitPos-field))) != 0 {
			data[byteIndex] |= 1 << shift
		} else {
			data[byteIndex] &^= 1 << shift
		}
		bitPos++
	}
	if field == minBlockField {
		// The minimum block size is the first field, which is separate.
		copy(data, []byte{byte(value >> 8), byte(value)})
	}
	return data
}

// hexDecode decodes a hex string, ignoring the spaces, which makes the byte
// groupings above readable.
func hexDecode(s string) ([]byte, error) {
	var out []byte
	var high byte
	seen := false

	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' {
			continue
		}
		var nibble byte
		switch {
		case c >= '0' && c <= '9':
			nibble = c - '0'
		case c >= 'a' && c <= 'f':
			nibble = c - 'a' + 10
		default:
			return nil, errString("not hex: " + string(c))
		}
		if !seen {
			high, seen = nibble, true
			continue
		}
		out = append(out, high<<4|nibble)
		seen = false
	}
	if seen {
		return nil, errString("odd number of hex digits")
	}
	return out, nil
}

type errString string

func (e errString) Error() string { return string(e) }

// TestPictureMIMEFromMagic covers a picture block whose MIME type is missing,
// which the format allows and which the magic bytes can make up for.
func TestPictureMIMEFromMagic(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	p := tag.Picture{Data: png}
	normalizePictureMIME(&p)
	if got, want := p.MIME, "image/png"; got != want {
		t.Errorf("MIME = %q, want %q", got, want)
	}

	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0}
	p = tag.Picture{Data: jpeg}
	normalizePictureMIME(&p)
	if got, want := p.MIME, "image/jpeg"; got != want {
		t.Errorf("MIME = %q, want %q", got, want)
	}

	// A type that is already set is left alone.
	p = tag.Picture{MIME: "image/gif", Data: png}
	normalizePictureMIME(&p)
	if got, want := p.MIME, "image/gif"; got != want {
		t.Errorf("MIME = %q, want %q", got, want)
	}
}

func TestStringConversions(t *testing.T) {
	// The helpers the tests above rely on must not be reachable as a path that
	// leaves the reader somewhere wrong, so check that a comment read from a
	// reader leaves it exactly after the block.
	buf := new(bytes.Buffer)
	buf.Write(buildComment("tagger", "TITLE=A Title"))

	r := bytes.NewReader(buf.Bytes())
	if _, err := ReadVorbisComment(r); err != nil {
		t.Fatalf("ReadVorbisComment() = %v", err)
	}
	if got, want := r.Len(), 0; got != want {
		t.Errorf("%d bytes left in the reader, want %d", got, want)
	}
	if !strings.Contains(buf.String(), "TITLE=A Title") {
		t.Error("the block does not hold the field it was built with")
	}
}
