package tags

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raffleberry/tags/id3"
	"github.com/raffleberry/tags/tag"
)

const dataDir = "testdata/mutagen/"

func TestOpen(t *testing.T) {
	tests := []struct {
		name    string
		format  tag.Format
		title   string
		duraton float64
	}{
		// One file per container.
		{"silence-44-s.mp3", tag.MP3, "Silence", 3.7675},
		{"silence-44-s.flac", tag.FLAC, "Silence", 3.68472},
		// This MP4 file has no title.
		{"has-tags.m4a", tag.M4A, "", 3.70794},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := Open(dataDir + tt.name)
			if err != nil {
				t.Fatalf("Open(%q) = %v", tt.name, err)
			}

			if got := f.Format(); got != tt.format {
				t.Errorf("Format() = %v, want %v", got, tt.format)
			}
			if f.Audio().SampleRate == 0 {
				t.Error("SampleRate = 0, want the stream to be readable")
			}
			if tt.title != "" && f.Tags().Value(tag.Title) != tt.title {
				t.Errorf("title = %q, want %q", f.Tags().Value(tag.Title), tt.title)
			}
			// Duration sources differ by format. Duration is checked within 0.01
			// seconds here. Format packages test exact values.
			if d := f.Audio().Duration.Seconds(); d < tt.duraton-0.01 || d > tt.duraton+0.01 {
				t.Errorf("Duration = %v s, want ~%v s", d, tt.duraton)
			}
		})
	}
}

// TestReadFromReader checks the same files through a reader. It covers reads
// from archives or network streams.
func TestReadFromReader(t *testing.T) {
	for _, name := range []string{"silence-44-s.mp3", "has-tags.m4a", "silence-44-s.flac"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(dataDir + name)
			if err != nil {
				t.Fatal(err)
			}
			f, err := Read(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("Read() = %v", err)
			}
			byPath, err := Open(dataDir + name)
			if err != nil {
				t.Fatal(err)
			}
			if f.Format() != byPath.Format() {
				t.Errorf("Format() = %v, want %v", f.Format(), byPath.Format())
			}
		})
	}
}

// TestDetect checks file identification without a full read. Content
// determines the result. The extension does not.
func TestDetect(t *testing.T) {
	tests := []struct {
		name   string
		format tag.Format
	}{
		{"silence-44-s.mp3", tag.MP3},
		{"silence-44-s-v1.mp3", tag.MP3},
		{"silence-44-s.flac", tag.FLAC},
		{"has-tags.m4a", tag.M4A},
		{"ep7.m4b", tag.M4A},
		// An MP4 file with an .mp4 extension.
		{"64bit.mp4", tag.M4A},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Detect(dataDir + tt.name)
			if err != nil {
				t.Fatalf("Detect(%q) = %v", tt.name, err)
			}
			if got != tt.format {
				t.Errorf("Detect(%q) = %v, want %v", tt.name, got, tt.format)
			}
		})
	}
}

// TestDetectWrongExtension checks that content determines the format. The file
// name does not.
func TestDetectWrongExtension(t *testing.T) {
	dir := t.TempDir()

	// A FLAC file named as an MP3.
	wrong := filepath.Join(dir, "not-really.mp3")
	copyFile(t, dataDir+"silence-44-s.flac", wrong)

	got, err := Detect(wrong)
	if err != nil {
		t.Fatalf("Detect() = %v", err)
	}
	if got != tag.FLAC {
		t.Errorf("Detect() = %v, want %v", got, tag.FLAC)
	}

	// Reading it returns FLAC tags.
	f, err := Open(wrong)
	if err != nil {
		t.Fatalf("Open() = %v", err)
	}
	if f.Format() != tag.FLAC {
		t.Errorf("Format() = %v, want %v", f.Format(), tag.FLAC)
	}
	if got, want := f.Tags().Value(tag.Title), "Silence"; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	data, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestUnknownFormat checks data that matches no supported container. Read
// returns an error.
func TestUnknownFormat(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"one byte", []byte("x")},
		{"shorter than a header", []byte("ID3\x04\x00\x00")},
		{"text", []byte("this is a text file, not audio at all")},
		// A PNG header. It is a valid file format. This package does not read it.
		{"png", []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Read(bytes.NewReader(tt.data))
			if !errors.Is(err, ErrUnknownFormat) {
				t.Errorf("Read() = %v, want %v", err, ErrUnknownFormat)
			}
		})
	}
}

// TestOpenAs checks reading with a required format.
func TestOpenAs(t *testing.T) {
	tests := []struct {
		name    string
		format  tag.Format
		wantErr bool
	}{
		{"silence-44-s.mp3", tag.MP3, false},
		{"silence-44-s.flac", tag.FLAC, false},
		{"has-tags.m4a", tag.M4A, false},
		// A wrong format returns an error.
		{"silence-44-s.mp3", tag.FLAC, true},
		{"silence-44-s.flac", tag.MP3, true},
		// A format this package does not read.
		{"silence-44-s.mp3", tag.Format("ogg"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name+"/"+string(tt.format), func(t *testing.T) {
			f, err := OpenAs(dataDir+tt.name, tt.format)
			if tt.wantErr {
				if err == nil {
					t.Errorf("OpenAs(%q, %q) = nil error, want a failure", tt.name, tt.format)
				}
				return
			}
			if err != nil {
				t.Fatalf("OpenAs(%q, %q) = %v", tt.name, tt.format, err)
			}
			if f.Format() != tt.format {
				t.Errorf("Format() = %v, want %v", f.Format(), tt.format)
			}
		})
	}
}

// TestCommonView checks that the same field uses the same key in each
// container.
func TestCommonView(t *testing.T) {
	tests := []struct {
		name  string
		field string
		want  string
	}{
		// A title in three forms: an ID3 frame, a Vorbis comment, an iTunes atom.
		{"silence-44-s.mp3", tag.Title, "Silence"},
		{"silence-44-s.flac", tag.Title, "Silence"},
		{"id3v22-test.mp3", tag.Artist, "Anais Mitchell"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := Open(dataDir + tt.name)
			if err != nil {
				t.Fatal(err)
			}
			if got := f.Tags().Value(tt.field); got != tt.want {
				t.Errorf("Tags()[%q] = %q, want %q", tt.field, got, tt.want)
			}
		})
	}
}

// TestNormalizedKeys checks that all keys are lowercase.
func TestNormalizedKeys(t *testing.T) {
	for _, name := range []string{"silence-44-s.mp3", "has-tags.m4a", "silence-44-s.flac"} {
		t.Run(name, func(t *testing.T) {
			f, err := Open(dataDir + name)
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range f.Tags().Keys() {
				if key != strings.ToLower(key) {
					t.Errorf("key %q is not lowercase", key)
				}
			}
		})
	}
}

// TestExtensions checks the extension lists for directory scans.
func TestExtensions(t *testing.T) {
	tests := []struct {
		format tag.Format
		want   []string
	}{
		{tag.MP3, []string{".mp3", ".mp2", ".mpga"}},
		{tag.M4A, []string{".m4a", ".m4b", ".mp4", ".m4p", ".aac"}},
		{tag.FLAC, []string{".flac"}},
		{tag.Format("ogg"), nil},
	}

	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			got := Extensions(tt.format)
			if len(got) != len(tt.want) {
				t.Fatalf("Extensions(%q) = %v, want %v", tt.format, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("Extensions(%q)[%d] = %q, want %q", tt.format, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestLooksLike(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"song.mp3", true},
		{"song.MP3", true},
		{"book.m4b", true},
		{"video.mp4", true},
		{"song.flac", true},
		{"cover.jpg", false},
		{"notes.txt", false},
		{"noextension", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LooksLike(tt.name); got != tt.want {
				t.Errorf("LooksLike(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

// TestFormats checks the list of supported containers.
func TestFormats(t *testing.T) {
	got := Formats()
	want := []tag.Format{tag.MP3, tag.M4A, tag.FLAC}
	if len(got) != len(want) {
		t.Fatalf("Formats() = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("Formats()[%d] = %v, want %v", i, got[i], want[i])
		}
	}

	// The result is a copy. Callers cannot change supported formats.
	got[0] = tag.Format("wav")
	if Formats()[0] != tag.MP3 {
		t.Error("Formats() returned its own list, want a copy")
	}
}

// TestID3Helpers checks direct access to the ID3 tag of an MP3 file.
func TestID3Helpers(t *testing.T) {
	f, err := os.Open(dataDir + "silence-44-s.mp3")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	v2, err := ID3v2(f)
	if err != nil {
		t.Fatalf("ID3v2() = %v", err)
	}
	if got, want := v2.Version.String(), "2.3.0"; got != want {
		t.Errorf("Version = %q, want %q", got, want)
	}
	if got, want := v2.Value("TIT2"), "Silence"; got != want {
		t.Errorf("TIT2 = %q, want %q", got, want)
	}

	// This file also has an ID3v1 tag at its end. It is read separately from the
	// ID3v2 tag at the start.
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	v1, err := ID3v1From(f)
	if err != nil {
		t.Fatalf("ID3v1From() = %v", err)
	}
	if got, want := v1.Title, "Silence"; got != want {
		t.Errorf("ID3v1 title = %q, want %q", got, want)
	}
	if got, want := v1.Track, 2; got != want {
		t.Errorf("ID3v1 track = %d, want %d", got, want)
	}
	// The genre byte in this file is the unset value. No genre is returned. The ID3v2
	// tag has a genre.
	if got := v1.Genre; got != "" {
		t.Errorf("ID3v1 genre = %q, want empty for an unset genre byte", got)
	}

	// A file without an ID3v1 tag returns an error. This is expected.
	if _, err := os.Open(dataDir + "silence-44-s.flac"); err != nil {
		t.Fatal(err)
	}
	flacFile, err := os.Open(dataDir + "silence-44-s.flac")
	if err != nil {
		t.Fatal(err)
	}
	defer flacFile.Close()
	if _, err := ID3v1From(flacFile); !errors.Is(err, id3.ErrNoV1) {
		t.Errorf("ID3v1From(flac) = %v, want %v", err, id3.ErrNoV1)
	}
}

// TestMissingFile checks a missing path. It differs from non-audio data.
func TestMissingFile(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "absent.mp3")); err == nil {
		t.Error("Open(absent) = nil error, want a failure")
	}
	if _, err := Detect(filepath.Join(t.TempDir(), "absent.mp3")); err == nil {
		t.Error("Detect(absent) = nil error, want a failure")
	}
}
