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

	// Two ARTIST comments give two values for one key.
	if got, want := tags.Values(tag.Artist), []string{"piman", "jzig"}; len(got) != 2 {
		t.Errorf("artists = %q, want %q", got, want)
	}

	// The block holds the writer name.
	if got, want := f.Vendor(), "reference libFLAC 1.1.0 20030126"; got != want {
		t.Errorf("Vendor() = %q, want %q", got, want)
	}
}

// TestSplitTrackNumber checks "02/10" values. The position and total are split.
func TestSplitTrackNumber(t *testing.T) {
	tags := open(t, "silence-44-s.flac").Tags()

	if got, want := tags.Value(tag.Track), "02"; got != want {
		t.Errorf("track = %q, want %q", got, want)
	}
	if got, want := tags.Value(tag.TrackTotal), "10"; got != want {
		t.Errorf("tracktotal = %q, want %q", got, want)
	}
}

// TestSeparateTrackTotal checks separate position and total comments.
func TestSeparateTrackTotal(t *testing.T) {
	tags := open(t, "variable-block.flac").Tags()

	if got, want := tags.Value(tag.Track), "01"; got != want {
		t.Errorf("track = %q, want %q", got, want)
	}
	// TOTALTRACKS and TRACKTOTAL hold the same field.
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

// TestUnknownFieldsKeepTheirNames checks unknown comments. They keep lowercased names.
func TestUnknownFieldsKeepTheirNames(t *testing.T) {
	tags := open(t, "variable-block.flac").Tags()

	// Check that a non ASCII title survives.
	if got, want := tags.Value("japanese title"), "アップルシード オリジナル・サウンドトラック"; got != want {
		t.Errorf("japanese title = %q, want %q", got, want)
	}
	// Check a ripper value with no common key.
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
	// These names have no table entry. They keep lowercased names.
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

	// The file holds a one pixel PNG with full description.
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
	// The table has six points. The last point is a placeholder.
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

// TestBlocks checks all metadata blocks. It includes undecoded block types.
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

	// The file has an application block. It must still be present.
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

// readMarker returns the first bytes of a test file.
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

// TestNotFLAC checks non FLAC files and short files.
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

// TestDamagedFiles checks files with short data. Readable files pass. Unreadable files return errors.
func TestDamagedFiles(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		// A short stream information block is unreadable.
		{"106-invalid-streaminfo.flac", true},
		// A picture block with a wrong size is still readable. The picture states its own length.
		{"106-short-picture-block-size.flac", false},
		// Tags were overwritten in place. The padding is short.
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

// TestVorbisComment checks the comment format without a file.
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
	// Fields keep file order.
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

// TestCommentWithoutEquals checks a line without "=". It is skipped.
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

// TestCommentWithRepeatedEquals checks a value with "=". Only the first "=" splits.
func TestCommentWithRepeatedEquals(t *testing.T) {
	comment, err := ParseVorbisComment(buildComment("tagger", "comment=a=b=c"))
	if err != nil {
		t.Fatalf("ParseVorbisComment() = %v", err)
	}
	if got, want := comment.Fields[0].Value, "a=b=c"; got != want {
		t.Errorf("Value = %q, want %q", got, want)
	}
}

// TestTruncatedComment checks a block with a large count. Read fields are kept.
func TestTruncatedComment(t *testing.T) {
	data := buildComment("tagger", "TITLE=A Title")
	// Set the count to 100.
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

// TestEmptyComment checks a block with no fields.
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

// buildComment builds a Vorbis comment block from a vendor string and fields.
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

// writeCounted writes a little endian length followed by bytes.
func writeCounted(buf *bytes.Buffer, s string) {
	buf.Write([]byte{byte(len(s)), byte(len(s) >> 8), byte(len(s) >> 16), byte(len(s) >> 24)})
	buf.WriteString(s)
}

// TestReadStreamInfo checks a stream information block alone. Its fields are bit fields.
func TestReadStreamInfo(t *testing.T) {
	// This is the test file block: 4608 blocks of 44100 Hz stereo 16 bit audio.
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

// TestInvalidStreamInfo checks short blocks and bad field values.
func TestInvalidStreamInfo(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"shorter than the block", make([]byte, 33)},
		// Zero sample rate is invalid.
		{"zero sample rate", streamInfoWith(sampleRateField, 0)},
		// Reversed block sizes are invalid.
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

// Bit positions in a stream information block.
const (
	minBlockField    = 0
	sampleRateField  = 80
	blockSizeBitLen  = 16
	sampleRateBitLen = 20
)

// streamInfoWith returns a valid block with one field set.
func streamInfoWith(field int, value int) []byte {
	// Copy the test block and overwrite the field.
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
		// The minimum block size is the first field.
		copy(data, []byte{byte(value >> 8), byte(value)})
	}
	return data
}

// hexDecode decodes a hex string. It ignores spaces.
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

// TestPictureMIMEFromMagic checks a picture with no MIME type. Magic bytes supply it.
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

	// A set MIME type is unchanged.
	p = tag.Picture{MIME: "image/gif", Data: png}
	normalizePictureMIME(&p)
	if got, want := p.MIME, "image/gif"; got != want {
		t.Errorf("MIME = %q, want %q", got, want)
	}
}

func TestStringConversions(t *testing.T) {
	// TestStringConversions checks that reading a comment leaves the reader after the block.
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
