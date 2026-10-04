package mp4

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

func assertClose(t *testing.T, got time.Duration, want float64) {
	t.Helper()
	if math.Abs(got.Seconds()-want) > 0.001 {
		t.Errorf("Duration = %v, want ~%v s", got, want)
	}
}

func TestAudioProperties(t *testing.T) {
	tests := []struct {
		name       string
		codec      string
		encoder    string
		duration   float64
		sampleRate int
		channels   int
		bitrate    int
		depth      int
	}{
		// AAC in an MP4 container.
		{"has-tags.m4a", "mp4a.40.2", "", 3.70794, 44100, 2, 2914, 16},
		{"no-tags.m4a", "mp4a.40.2", "", 3.70794, 44100, 2, 2914, 16},
		// Apple Lossless with parameters in a magic cookie.
		{"alac.m4a", "alac", "ALAC", 3.68472, 44100, 2, 2764, 16},
		// Audiobook file.
		{"ep7.m4b", "mp4a.40.2", "", 2.02014, 44100, 2, 125591, 16},
		// 47 hour audiobook with 64 bit media header duration.
		{"nero-chapters.m4b", "mp4a.40.2", "", 169022.694, 22050, 2, 62794, 16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			audio := open(t, tt.name).Audio()

			if audio.Codec != tt.codec {
				t.Errorf("Codec = %q, want %q", audio.Codec, tt.codec)
			}
			if audio.Encoder != tt.encoder {
				t.Errorf("Encoder = %q, want %q", audio.Encoder, tt.encoder)
			}
			assertClose(t, audio.Duration, tt.duration)
			if audio.SampleRate != tt.sampleRate {
				t.Errorf("SampleRate = %d, want %d", audio.SampleRate, tt.sampleRate)
			}
			if audio.Channels != tt.channels {
				t.Errorf("Channels = %d, want %d", audio.Channels, tt.channels)
			}
			if audio.Bitrate != tt.bitrate {
				t.Errorf("Bitrate = %d, want %d", audio.Bitrate, tt.bitrate)
			}
			if audio.BitsPerSample != tt.depth {
				t.Errorf("BitsPerSample = %d, want %d", audio.BitsPerSample, tt.depth)
			}
		})
	}
}

// TestNoAudioTrack covers a file with tags and no audio track. Stream
// properties are empty. Metadata remains readable.
func TestNoAudioTrack(t *testing.T) {
	f := open(t, "64bit.mp4")

	if audio := f.Audio(); audio.SampleRate != 0 || audio.Channels != 0 {
		t.Errorf("Audio() = %v, want nothing for a file with no audio track", audio)
	}
	// The file has one flag tag.
	if got, want := f.Tags().Value(tag.Compilation), "1"; got != want {
		t.Errorf("compilation = %q, want %q", got, want)
	}
}

func TestTags(t *testing.T) {
	f := open(t, "has-tags.m4a")
	tags := f.Tags()

	if got, want := tags.Value(tag.Artist), "Test Artist"; got != want {
		t.Errorf("artist = %q, want %q", got, want)
	}
	if got, want := tags.Value(tag.EncoderSettings), "FAAC 1.24"; got != want {
		t.Errorf("encodersettings = %q, want %q", got, want)
	}

	// Freeform fields use application and field names as the key.
	if got := tags.Value("----:com.apple.itunes:itunnorm"); got == "" {
		t.Error("itunnorm is missing, want the iTunes normalization data")
	}
}

func TestNoTags(t *testing.T) {
	f := open(t, "no-tags.m4a")
	if got := f.Tags(); len(got) != 0 {
		t.Errorf("Tags() = %v, want no fields", got)
	}
	if got := f.Pictures(); len(got) != 0 {
		t.Errorf("Pictures() = %v, want none", got)
	}
	// Stream data remains readable without tags.
	if f.Audio().SampleRate == 0 {
		t.Error("SampleRate = 0, want the stream to be readable without tags")
	}
}

func TestPictures(t *testing.T) {
	f := open(t, "has-tags.m4a")
	pictures := f.Pictures()

	if len(pictures) != 2 {
		t.Fatalf("len(Pictures()) = %d, want 2", len(pictures))
	}
	want := []struct {
		mime string
		size int
	}{
		// One PNG and one JPEG in the same atom.
		{"image/png", 79},
		{"image/jpeg", 287},
	}
	for i, w := range want {
		if pictures[i].MIME != w.mime {
			t.Errorf("Pictures()[%d].MIME = %q, want %q", i, pictures[i].MIME, w.mime)
		}
		if len(pictures[i].Data) != w.size {
			t.Errorf("len(Pictures()[%d].Data) = %d, want %d", i, len(pictures[i].Data), w.size)
		}
		if pictures[i].Type != tag.PictureCoverFront {
			t.Errorf("Pictures()[%d].Type = %v, want %v", i, pictures[i].Type, tag.PictureCoverFront)
		}
	}
}

// TestPictureWithNameAtom covers a cover atom with an extra "name" atom before
// the image data.
func TestPictureWithNameAtom(t *testing.T) {
	f := open(t, "covr-with-name.m4a")

	pictures := f.Pictures()
	if len(pictures) != 2 {
		t.Fatalf("len(Pictures()) = %d, want 2", len(pictures))
	}
	if pictures[0].MIME != "image/png" || pictures[1].MIME != "image/jpeg" {
		t.Errorf("MIMEs = %q and %q, want image/png and image/jpeg",
			pictures[0].MIME, pictures[1].MIME)
	}
}

func TestAtomTree(t *testing.T) {
	f := open(t, "has-tags.m4a")

	// The "ftyp" atom is first in most MPEG-4 files.
	if len(f.Atoms()) == 0 {
		t.Fatal("Atoms() is empty, want the top level atoms")
	}
	if got, want := f.Atoms()[0].Name, "ftyp"; got != want {
		t.Errorf("Atoms()[0].Name = %q, want %q", got, want)
	}

	moov, ok := Find(f.Atoms(), "moov")
	if !ok {
		t.Fatal("no moov atom, want one")
	}
	if _, ok := moov.Path("udta", "meta", "ilst"); !ok {
		t.Error("no ilst atom, want the metadata list")
	}

	// A leaf has no children.
	ftyp := f.Atoms()[0]
	if len(ftyp.Children) != 0 {
		t.Errorf("ftyp has %d children, want none", len(ftyp.Children))
	}
}

// Test64BitAtomLengths covers atoms with 64 bit lengths. The header is then
// twice the size.
func Test64BitAtomLengths(t *testing.T) {
	// All atoms in this file use 64 bit lengths.
	f := open(t, "64bit.mp4")

	moov, ok := Find(f.Atoms(), "moov")
	if !ok {
		t.Fatal("no moov atom, want one")
	}
	if !moov.longLength {
		t.Error("moov does not record a 64 bit length, want one")
	}
	// Tags are inside moov. Reading them checks the header size.
	ilst, ok := moov.Path("udta", "meta", "ilst")
	if !ok {
		t.Fatal("no ilst atom, want one below moov")
	}
	data, err := ilst.Data(mustOpen(t, "64bit.mp4"))
	if err != nil {
		t.Fatalf("Data() = %v", err)
	}
	if !bytes.Contains(data, []byte("cpil")) {
		t.Errorf("ilst data does not contain cpil: %q", data)
	}
}

func mustOpen(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open(dataDir + name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

// TestRejectedAtomHeaders covers malformed atom headers in truncated or damaged
// files.
func TestRejectedAtomHeaders(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"shorter than a header", []byte{0x00, 0x00}},
		{"length below the header size", []byte{0x00, 0x00, 0x00, 0x02, 'a', 't', 'o', 'm'}},
		{"truncated 64 bit length", []byte{0x00, 0x00, 0x00, 0x01, 'a', 't', 'o', 'm', 0x00}},
		{"64 bit length below the header size", []byte{
			0x00, 0x00, 0x00, 0x01, 'a', 't', 'o', 'm',
			0, 0, 0, 0, 0, 0, 0, 8,
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Atoms(bytes.NewReader(tt.data)); err == nil {
				t.Error("Atoms() = nil error, want a rejection")
			}
		})
	}
}

// TestZeroLengthNestedAtom covers a zero length atom below the top level. Zero
// length is not allowed there. Parsing stops at the container end. Atoms found
// so far are kept.
func TestZeroLengthNestedAtom(t *testing.T) {
	file := []byte("\x00\x00\x00\x18moov" +
		"\x00\x00\x00\x08trak" + // well formed first child
		"\x00\x00\x00\x00trak" + // zero length child
		"\x00\x00\x00\x08trak")

	atoms, err := Atoms(bytes.NewReader(file))
	if err != nil {
		t.Fatalf("Atoms() = %v", err)
	}
	moov := atoms[0]
	if len(moov.Children) != 1 {
		t.Fatalf("moov has %d children, want only the well formed one", len(moov.Children))
	}
	if got, want := moov.Children[0].Length, int64(8); got != want {
		t.Errorf("child length = %d, want %d", got, want)
	}
}

// TestZeroLengthAtom covers a legal zero length: a top level atom to the end of
// the file.
func TestZeroLengthAtom(t *testing.T) {
	file := append([]byte("\x00\x00\x00\x00atom"), make([]byte, 40)...)

	atoms, err := Atoms(bytes.NewReader(file))
	if err != nil {
		t.Fatalf("Atoms() = %v", err)
	}
	if len(atoms) != 1 {
		t.Fatalf("len(Atoms()) = %d, want 1", len(atoms))
	}
	if got, want := atoms[0].Length, int64(48); got != want {
		t.Errorf("Length = %d, want %d", got, want)
	}

	// A zero length atom fills the rest of the file.
	data, err := atoms[0].Data(bytes.NewReader(file))
	if err != nil {
		t.Fatalf("Data() = %v", err)
	}
	if got, want := len(data), 40; got != want {
		t.Errorf("len(Data()) = %d, want %d", got, want)
	}
}

// TestTrailingGarbage covers a file with trailing bytes too short for an atom.
// The leading atoms are returned.
func TestTrailingGarbage(t *testing.T) {
	data := []byte("\x00\x00\x00\x08data" + "\x00\x00")
	atoms, err := Atoms(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Atoms() = %v, want the atoms before the trailing bytes", err)
	}
	if len(atoms) != 1 || atoms[0].Name != "data" {
		t.Errorf("Atoms() = %v, want one data atom", atoms)
	}
}

func TestNotMP4(t *testing.T) {
	// FLAC files are not built from atoms.
	if _, err := Open(dataDir + "silence-44-s.flac"); err == nil {
		t.Error("Open(flac) = nil error, want a failure")
	}
}

func TestMatches(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"has-tags.m4a", true},
		{"no-tags.m4a", true},
		{"silence-44-s.flac", false},
		{"silence-44-s.mp3", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Matches(openHeader(t, tt.name)); got != tt.want {
				t.Errorf("Matches(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

// openHeader returns the first bytes of a test file.
func openHeader(t *testing.T, name string) []byte {
	t.Helper()
	f := mustOpen(t, name)
	header := make([]byte, 12)
	if _, err := io.ReadFull(f, header); err != nil {
		t.Fatal(err)
	}
	return header
}

// TestTruncatedFile covers a file cut short inside an atom. The atom claims
// more bytes than are present.
func TestTruncatedFile(t *testing.T) {
	atoms, err := Atoms(bytes.NewReader([]byte("\x00\x00\x00\x20moov\x00\x00")))
	if err != nil {
		t.Fatalf("Atoms() = %v, want the atom to be readable", err)
	}
	if len(atoms) != 1 || atoms[0].Name != "moov" {
		t.Errorf("Atoms() = %v, want one moov atom", atoms)
	}
	// Data returns the bytes present without failure.
	data, err := atoms[0].Data(bytes.NewReader([]byte("\x00\x00\x00\x20moov\x00\x00")))
	if err != nil {
		t.Fatalf("Data() = %v", err)
	}
	if got, want := len(data), 2; got != want {
		t.Errorf("len(Data()) = %d, want %d", got, want)
	}
}
