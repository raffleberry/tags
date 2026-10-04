package m4a

import (
	"os"
	"testing"

	"github.com/raffleberry/tags/tag"
)

const dataDir = "../testdata/mutagen/"

func TestOpen(t *testing.T) {
	tests := []struct {
		name    string
		codec   string
		duraton float64
	}{
		// AAC in an MP4 container.
		{"has-tags.m4a", "mp4a.40.2", 3.70794},
		// Apple Lossless with properties in a magic cookie.
		{"alac.m4a", "alac", 3.68472},
		// Audiobook file.
		{"ep7.m4b", "mp4a.40.2", 2.02014},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := Open(dataDir + tt.name)
			if err != nil {
				t.Fatalf("Open(%q) = %v", tt.name, err)
			}

			if got := f.Format(); got != tag.M4A {
				t.Errorf("Format() = %v, want %v", got, tag.M4A)
			}
			if got, want := f.Audio().Codec, tt.codec; got != want {
				t.Errorf("Codec = %q, want %q", got, want)
			}
			if d := f.Audio().Duration.Seconds(); d < tt.duraton-0.01 || d > tt.duraton+0.01 {
				t.Errorf("Duration = %v s, want ~%v s", d, tt.duraton)
			}
		})
	}
}

func TestTags(t *testing.T) {
	f, err := Open(dataDir + "has-tags.m4a")
	if err != nil {
		t.Fatal(err)
	}

	if got, want := f.Tags().Value(tag.Artist), "Test Artist"; got != want {
		t.Errorf("artist = %q, want %q", got, want)
	}
	if got := len(f.Pictures()); got != 2 {
		t.Errorf("len(Pictures()) = %d, want 2", got)
	}
}

func TestNoTags(t *testing.T) {
	f, err := Open(dataDir + "no-tags.m4a")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(f.Tags()); got != 0 {
		t.Errorf("len(Tags()) = %d, want 0", got)
	}
	// Stream data remains readable without tags.
	if f.Audio().SampleRate != 44100 {
		t.Errorf("SampleRate = %d, want 44100", f.Audio().SampleRate)
	}
}

// TestNotMP4 covers non MPEG-4 files.
func TestNotMP4(t *testing.T) {
	for _, name := range []string{"silence-44-s.flac", "silence-44-s.mp3", "emptyfile.mp3"} {
		if _, err := Open(dataDir + name); err == nil {
			t.Errorf("Open(%q) = nil error, want a failure", name)
		}
	}
}

func TestMatches(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"has-tags.m4a", true},
		{"no-tags.m4a", true},
		{"alac.m4a", true},
		{"silence-44-s.flac", false},
		{"silence-44-s.mp3", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := os.Open(dataDir + tt.name)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()

			header := make([]byte, 12)
			if _, err := f.Read(header); err != nil {
				t.Fatal(err)
			}
			if got := Matches(header); got != tt.want {
				t.Errorf("Matches(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

// TestAtoms covers access to the atom tree.
func TestAtoms(t *testing.T) {
	f, err := os.Open(dataDir + "has-tags.m4a")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	atoms, err := Atoms(f)
	if err != nil {
		t.Fatalf("Atoms() = %v", err)
	}
	if len(atoms) == 0 {
		t.Fatal("Atoms() is empty, want the top level atoms")
	}
	// The file type atom is first in most MPEG-4 files.
	if got, want := atoms[0].Name, "ftyp"; got != want {
		t.Errorf("Atoms()[0].Name = %q, want %q", got, want)
	}
}

// TestParseILST covers metadata list parsing from a payload.
func TestParseILST(t *testing.T) {
	f, err := os.Open(dataDir + "has-tags.m4a")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	atoms, err := Atoms(f)
	if err != nil {
		t.Fatal(err)
	}
	moov, ok := Find(atoms, "moov")
	if !ok {
		t.Fatal("no moov atom, want one")
	}
	ilst, ok := moov.Path("udta", "meta", "ilst")
	if !ok {
		t.Fatal("no ilst atom, want one")
	}
	data, err := ilst.Data(f)
	if err != nil {
		t.Fatal(err)
	}

	parsed := ParseILST(data)
	if parsed == nil {
		t.Fatal("ParseILST() = nil, want a metadata list")
	}
	if got, want := parsed.Common().Value(tag.Artist), "Test Artist"; got != want {
		t.Errorf("artist = %q, want %q", got, want)
	}
	if got := len(parsed.Covers()); got != 2 {
		t.Errorf("len(Covers()) = %d, want 2", got)
	}
}

// TestFormatConstant checks the format alias in this package.
func TestFormatConstant(t *testing.T) {
	if Format != tag.M4A {
		t.Errorf("Format = %q, want %q", Format, tag.M4A)
	}
}
