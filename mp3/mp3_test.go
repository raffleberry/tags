package mp3

import (
	"bytes"
	"io"
	"math"
	"os"
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

// assertClose compares two durations with 0.001 s tolerance.
func assertClose(t *testing.T, got time.Duration, want float64) {
	t.Helper()
	if math.Abs(got.Seconds()-want) > 0.001 {
		t.Errorf("duration = %v, want ~%v s", got, want)
	}
}

func TestStreamProperties(t *testing.T) {
	// Every file holds 3.7 s of silence.
	tests := []struct {
		name       string
		version    MPEGVersion
		layer      int
		sampleRate int
		bitrate    int
		duration   float64
	}{
		// Constant bitrate with an ID3v2 tag at the front.
		{"silence-44-s.mp3", MPEG1, 3, 44100, 32000, 3.7675},
		// The same audio with only an ID3v1 tag.
		{"silence-44-s-v1.mp3", MPEG1, 3, 44100, 32000, 3.7675},
		// MPEG 2 and 2.5, both variable bitrate with a LAME header.
		{"silence-44-s-mpeg2.mp3", MPEG2, 3, 24000, 17783, 3.68475},
		{"silence-44-s-mpeg25.mp3", MPEG2_5, 3, 12000, 8900, 3.68475},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := open(t, tt.name)
			s := f.Stream()

			if s.Version != tt.version {
				t.Errorf("Version = %v, want %v", s.Version, tt.version)
			}
			if s.Layer != tt.layer {
				t.Errorf("Layer = %d, want %d", s.Layer, tt.layer)
			}
			if s.SampleRate != tt.sampleRate {
				t.Errorf("SampleRate = %d, want %d", s.SampleRate, tt.sampleRate)
			}
			if s.Bitrate != tt.bitrate {
				t.Errorf("Bitrate = %d, want %d", s.Bitrate, tt.bitrate)
			}
			if s.Channels != 2 {
				t.Errorf("Channels = %d, want 2", s.Channels)
			}
			assertClose(t, s.Duration, tt.duration)
			if s.Sketchy {
				t.Error("Sketchy = true, want false")
			}
			if got := f.Format(); got != tag.MP3 {
				t.Errorf("Format() = %v, want %v", got, tag.MP3)
			}
		})
	}
}

func TestTags(t *testing.T) {
	f := open(t, "silence-44-s.mp3")
	tags := f.Tags()

	want := map[string]string{
		tag.Title:  "Silence",
		tag.Artist: "piman",
		tag.Album:  "Quod Libet Test Data",
		tag.Genre:  "Silence",
		tag.Date:   "2004",
	}
	for key, value := range want {
		if got := tags.Value(key); got != value {
			t.Errorf("Tags()[%q] = %q, want %q", key, got, value)
		}
	}

	// TPE1 holds two artists separated by a NUL.
	if got, want := tags.Values(tag.Artist), []string{"piman", "jzig"}; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("artists = %q, want %q", got, want)
	}

	// TRCK holds "02/10", which is split into a position and a total.
	if got, want := tags.Value(tag.Track), "02"; got != want {
		t.Errorf("Tags()[%q] = %q, want %q", tag.Track, got, want)
	}
	if got, want := tags.Value(tag.TrackTotal), "10"; got != want {
		t.Errorf("Tags()[%q] = %q, want %q", tag.TrackTotal, got, want)
	}
}

func TestID3v1Only(t *testing.T) {
	f := open(t, "silence-44-s-v1.mp3")

	// The ID3v1 tag supplies everything, since this file has no ID3v2 tag.
	if got, want := f.Tags().Value(tag.Title), "Silence"; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
	if got, want := f.Tags().Value(tag.Artist), "piman"; got != want {
		t.Errorf("artist = %q, want %q", got, want)
	}
	// The genre byte indexes the ID3v1 genre list.
	if got, want := f.Tags().Value(tag.Genre), "Darkwave"; got != want {
		t.Errorf("genre = %q, want %q", got, want)
	}
	// ID3v1.1 stores the track in the last byte of the comment.
	if got, want := f.Tags().Value(tag.Track), "2"; got != want {
		t.Errorf("track = %q, want %q", got, want)
	}

	if f.V1() == nil {
		t.Error("V1() = nil, want the ID3v1 tag")
	}
}

// TestXing covers a file with a Xing header outside the first frame. Some
// encoders write this layout. Stream data comes from the frame header and
// file size.
func TestXing(t *testing.T) {
	f := open(t, "xing.mp3")

	if vbr := f.Stream().VBRHeader; vbr != nil {
		t.Errorf("VBRHeader = %+v, want nil for a header outside the first frame", vbr)
	}
	if got, want := f.Audio().BitrateMode, tag.BitrateUnknown; got != want {
		t.Errorf("BitrateMode = %v, want %v", got, want)
	}
	if got, want := f.Audio().Bitrate, 32000; got != want {
		t.Errorf("Bitrate = %d, want %d", got, want)
	}
	assertClose(t, f.Audio().Duration, 2.052)
}

// TestXingInFirstFrame covers a header in the first frame. The header
// describes the whole stream.
func TestXingInFirstFrame(t *testing.T) {
	f := open(t, "lame.mp3")

	vbr := f.Stream().VBRHeader
	if vbr == nil {
		t.Fatal("VBRHeader = nil, want a Xing header")
	}
	if got, want := vbr.Kind, KindXing; got != want {
		t.Errorf("Kind = %v, want %v", got, want)
	}
	if got, want := vbr.Frames, int64(4); got != want {
		t.Errorf("Frames = %d, want %d", got, want)
	}
	if got, want := vbr.Bytes, int64(2086); got != want {
		t.Errorf("Bytes = %d, want %d", got, want)
	}
	if vbr.LAME == nil {
		t.Fatal("LAME = nil, want the extended header")
	}
}

// TestXingFlags covers the flags byte. The flags list present optional fields
// in a Xing header. A minimal header sets only the frame count.
func TestXingFlags(t *testing.T) {
	// All fields: frames, bytes, seek table and quality. LAME writes all
	// fields.
	all := []byte("Xing\x00\x00\x00\x0f" +
		"\x00\x00\x00\x04" + // 4 frames
		"\x00\x00\x08&" + // 2086 bytes
		string(make([]byte, 100)) + // seek table
		"\x00\x00\x00P") // quality index 80
	got := parseXing(all)
	if got == nil {
		t.Fatal("parseXing = nil, want a header")
	}
	if got.Frames != 4 || got.Bytes != 2086 || got.Scale != 80 {
		t.Errorf("parseXing = %+v, want 4 frames, 2086 bytes and scale 80", got)
	}

	// Header with only the frame count.
	minimal := parseXing([]byte("Xing\x00\x00\x00\x01\x00\x00\x00\x07"))
	if minimal == nil {
		t.Fatal("parseXing = nil, want a header")
	}
	if minimal.Frames != 7 {
		t.Errorf("Frames = %d, want 7", minimal.Frames)
	}
	if minimal.Bytes != -1 || minimal.Scale != -1 {
		t.Errorf("Bytes = %d and Scale = %d, want -1 for absent fields", minimal.Bytes, minimal.Scale)
	}
}

// TestInfoHeader covers the Info header. Info is the constant bitrate variant
// of Xing. Its presence marks a file as constant bitrate.
func TestInfoHeader(t *testing.T) {
	got := parseXing([]byte("Info\x00\x00\x00\x03\x00\x00\x00\x0a\x00\x00\x08&6"))
	if got == nil {
		t.Fatal("parseXing = nil, want a header")
	}
	if got.Kind != KindInfo {
		t.Errorf("Kind = %v, want %v", got.Kind, KindInfo)
	}
	if got.Frames != 10 || got.Bytes != 2086 {
		t.Errorf("Frames = %d and Bytes = %d, want 10 and 2086", got.Frames, got.Bytes)
	}
	if mode := got.bitrateMode(); mode != tag.BitrateCBR {
		t.Errorf("bitrateMode() = %v, want %v", mode, tag.BitrateCBR)
	}
	if encoder := got.encoder(); encoder != "" {
		t.Errorf("encoder() = %q, want empty for a non LAME stream", encoder)
	}
}

func TestVBRI(t *testing.T) {
	f := open(t, "vbri.mp3")

	vbr := f.Stream().VBRHeader
	if vbr == nil || vbr.Kind != KindVBRI {
		t.Fatalf("VBRHeader = %+v, want a VBRI header", vbr)
	}
	// A VBRI header marks the Fraunhofer encoder.
	if got, want := f.Audio().Encoder, "FhG"; got != want {
		t.Errorf("Encoder = %q, want %q", got, want)
	}
	assertClose(t, f.Audio().Duration, 222.19755)
	if got, want := f.Audio().Bitrate, 233260; got != want {
		t.Errorf("Bitrate = %d, want %d", got, want)
	}
	if got, want := f.Audio().BitrateMode, tag.BitrateVBR; got != want {
		t.Errorf("BitrateMode = %v, want %v", got, want)
	}
}

func TestLAME(t *testing.T) {
	tests := []struct {
		name       string
		encoder    string
		settings   string
		bitrate    int
		bitrateMod tag.BitrateMode
		trackGain  float64
	}{
		{"lame.mp3", "LAME 3.99.1+", "-V 2", 127783, tag.BitrateVBR, 6.0},
		{"lame-peak.mp3", "LAME 3.99.1+", "-b 128", 127936, tag.BitrateCBR, 6.8},
		{"lame397v9short.mp3", "LAME 3.97.0", "-V 9", 40000, tag.BitrateVBR, 14.4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := open(t, tt.name)
			lame := f.Stream().VBRHeader.LAME
			if lame == nil {
				t.Fatal("LAME header = nil, want one")
			}

			if got, want := f.Audio().Encoder, tt.encoder; got != want {
				t.Errorf("Encoder = %q, want %q", got, want)
			}
			if got, want := lame.Settings(), tt.settings; got != want {
				t.Errorf("Settings() = %q, want %q", got, want)
			}
			if got, want := f.Audio().Bitrate, tt.bitrate; got != want {
				t.Errorf("Bitrate = %d, want %d", got, want)
			}
			if got, want := f.Audio().BitrateMode, tt.bitrateMod; got != want {
				t.Errorf("BitrateMode = %v, want %v", got, want)
			}

			got := lame.TrackGain
			if got == nil {
				t.Fatal("TrackGain = nil, want a value")
			}
			if math.Abs(*got-tt.trackGain) > 0.05 {
				t.Errorf("TrackGain = %v, want ~%v", *got, tt.trackGain)
			}
		})
	}
}

// TestLAMETrackPeak checks the peak amplitude field. The field is a 32 bit
// fraction. Neighboring gain fields are packed.
func TestLAMETrackPeak(t *testing.T) {
	f := open(t, "lame-peak.mp3")
	if got, want := f.Stream().VBRHeader.LAME.TrackPeak, 0.21856; math.Abs(got-want) > 0.0001 {
		t.Errorf("TrackPeak = %v, want ~%v", got, want)
	}
}

// TestLAMEShortFile covers a file with delay larger than the audio. Early
// LAME versions wrote such files. Duration must not be negative.
func TestLAMEShortFile(t *testing.T) {
	f := open(t, "lame397v9short.mp3")
	if got := f.Audio().Duration; got != 0 {
		t.Errorf("Duration = %v, want 0", got)
	}
}

func TestID3v2Version(t *testing.T) {
	// A file with a v2.3 tag and several COMM frames.
	f := open(t, "id3v22-test.mp3")

	if got, want := f.Tags().Value(tag.Title), "cosmic american"; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
	if got, want := f.Tags().Value(tag.Artist), "Anais Mitchell"; got != want {
		t.Errorf("artist = %q, want %q", got, want)
	}
	if got, want := f.Tags().Value(tag.Track), "3"; got != want {
		t.Errorf("track = %q, want %q", got, want)
	}
	if got, want := f.Tags().Value(tag.TrackTotal), "11"; got != want {
		t.Errorf("tracktotal = %q, want %q", got, want)
	}

	// A COMM frame with no description becomes the comment. COMM frames with
	// descriptions keep separate keys.
	if got, want := f.Tags().Value(tag.Comment), "Waterbug Records, www.anaismitchell.com"; got != want {
		t.Errorf("comment = %q, want %q", got, want)
	}
	if got, want := f.Tags().Value("comment:itunnorm"), iTunesNORM; got != want {
		t.Errorf("comment:itunnorm = %q, want %q", got, want)
	}
	if got, want := f.Tags().Value("comment:itunes_cddb_tracknumber"), "3"; got != want {
		t.Errorf("comment:itunes_cddb_tracknumber = %q, want %q", got, want)
	}
}

// iTunesNORM is normalization data. iTunes stores it as a comment. The value
// is fixed per file.
const iTunesNORM = " 0000044E 00000061 00009B67 000044C3 00022478 00022182 00007FCC 00007E5C 0002245E 0002214E"

func TestCombinedID3v1AndID3v2(t *testing.T) {
	f := open(t, "id3v1v2-combined.mp3")

	// Fields in both tags agree. The v2 value wins.
	if got, want := f.Tags().Value(tag.Title), "cosmic american"; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
	// This field exists only in the v1 tag.
	if got, want := f.Tags().Value("comment:id3v1 comment"), "v1 comment"; got != want {
		t.Errorf("comment:id3v1 comment = %q, want %q", got, want)
	}
}

func TestNotMP3(t *testing.T) {
	// A FLAC file holds audio but no MPEG frames. The frame search finds
	// nothing.
	f, err := Open(dataDir + "silence-44-s.flac")
	if err == nil && !f.Stream().Sketchy {
		t.Error("Open(flac) = a confident result, want a failure or a sketchy one")
	}
}

func TestEmptyAndShortFiles(t *testing.T) {
	for _, name := range []string{"emptyfile.mp3", "too-short.mp3"} {
		t.Run(name, func(t *testing.T) {
			if _, err := Open(dataDir + name); err == nil {
				t.Errorf("Open(%q) = nil error, want a failure", name)
			}
		})
	}
}

func TestMatches(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		// An ID3v2 identifier starts many MP3 files.
		{"silence-44-s.mp3", true},
		{"silence-44-s-v1.mp3", true},
		// A frame sync marks a file with no tag.
		{"xing.mp3", true},
		// Neither marker marks other containers.
		{"silence-44-s.flac", false},
		{"has-tags.m4a", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Matches(openHeader(t, tt.name)); got != tt.want {
				t.Errorf("Matches(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

// openHeader returns the first fileHeaderLen bytes of a test file. Format
// detection uses these bytes.
func openHeader(t *testing.T, name string) []byte {
	t.Helper()
	f, err := os.Open(dataDir + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	header := make([]byte, fileHeaderLen)
	if _, err := io.ReadFull(f, header); err != nil {
		t.Fatal(err)
	}
	return header
}

func TestHasMagic(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{"id3", []byte("ID3\x04\x00"), true},
		{"sync", []byte{0xFF, 0xFB, 0x90, 0x00}, true},
		{"mpeg2 sync", []byte{0xFF, 0xF3, 0x90, 0x00}, true},
		{"flac", []byte("fLaC\x00"), false},
		{"short", []byte{0xFF}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasMagic(tt.data); got != tt.want {
				t.Errorf("HasMagic(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

// TestRejectedHeaders covers reserved bit combinations in a frame header.
// Acceptance would misread 0xFF bytes in a picture or ID3 tag as audio.
func TestRejectedHeaders(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"reserved version", []byte{0xFF, 0xF8, 0x90, 0x00}},
		{"reserved layer", []byte{0xFF, 0xF9, 0x90, 0x00}},
		{"reserved sample rate", []byte{0xFF, 0xFB, 0x9C, 0x00}},
		{"reserved bitrate", []byte{0xFF, 0xFB, 0xF0, 0x00}},
		{"free format", []byte{0xFF, 0xFB, 0x00, 0x00}},
		{"no sync", []byte{0xFE, 0xFB, 0x90, 0x00}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseFrameHeader(bytes.NewReader(tt.data)); err == nil {
				t.Errorf("parseFrameHeader(%x) = nil error, want a rejection", tt.data)
			}
		})
	}
}

// TestFrameLength covers frame size calculation. Frame size derives from
// bitrate and sample rate. It sets the start of the next frame search.
func TestFrameLength(t *testing.T) {
	tests := []struct {
		name       string
		samples    int
		frameBytes int64
	}{
		// 32 kbit/s at 44100 Hz: 1152 samples in 104 bytes.
		{"silence-44-s.mp3", 1152, 104},
		// 64 kbit/s at 24000 Hz: 576 samples in 192 bytes.
		{"silence-44-s-mpeg2.mp3", 576, 192},
		// 32 kbit/s at 12000 Hz: 576 samples in 192 bytes.
		{"silence-44-s-mpeg25.mp3", 576, 192},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := os.Open(dataDir + tt.name)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()

			h, err := readFrameHeader(r, firstFrameOffset(t, r))
			if err != nil {
				t.Fatal(err)
			}
			if got := h.samples; got != tt.samples {
				t.Errorf("samples per frame = %d, want %d", got, tt.samples)
			}
			if got := h.length; got != tt.frameBytes {
				t.Errorf("frame length = %d, want %d", got, tt.frameBytes)
			}
		})
	}
}

// firstFrameOffset returns the offset of the first MPEG frame. The offset is
// past any ID3v2 tag.
func firstFrameOffset(t *testing.T, r *os.File) int64 {
	t.Helper()
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if err := skipID3(r); err != nil {
		t.Fatal(err)
	}
	offset, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		t.Fatal(err)
	}
	return offset
}

func TestReadEmpty(t *testing.T) {
	if _, err := Read(bytes.NewReader(nil)); err == nil {
		t.Error("Read(empty) = nil error, want a failure")
	}
}
