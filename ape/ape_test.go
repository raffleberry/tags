package ape

import (
	"os"
	"testing"

	"github.com/raffleberry/tags/tag"
)

const dataDir = "../testdata/mutagen/"

func TestReadRawAPE(t *testing.T) {
	for _, name := range []string{"oldtag.apev2", "brokentag.apev2"} {
		t.Run(name, func(t *testing.T) {
			f, err := os.Open(dataDir + name)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			at, err := Read(f)
			if err != nil {
				t.Fatalf("Read() = %v", err)
			}
			if got := at.Value("Title"); got != "Some Music" {
				t.Errorf("Title = %q, want %q", got, "Some Music")
			}
			if got := at.Common().Value(tag.Title); got != "Some Music" {
				t.Errorf("common title = %q, want %q", got, "Some Music")
			}
		})
	}
}

func TestReadMP3Trailer(t *testing.T) {
	f, err := os.Open(dataDir + "audacious-trailing-id32-apev2.mp3")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	at, err := Read(f)
	if err != nil {
		t.Fatalf("Read() = %v", err)
	}
	if got := at.Value("Artist"); got != "adfsasaf" {
		t.Errorf("Artist = %q, want %q", got, "adfsasaf")
	}
	// The tag has a header as well as a footer.
	if !at.HasHeader {
		t.Error("HasHeader = false, want true")
	}
}

func TestNoTag(t *testing.T) {
	f, err := os.Open(dataDir + "silence-44-s.mp3")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := Read(f); err == nil {
		t.Error("Read(mp3 without APE) = nil error, want ErrNoTag")
	}
}

func TestCommonMapping(t *testing.T) {
	f, err := os.Open(dataDir + "audacious-trailing-id32-apev2.mp3")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	at, err := Read(f)
	if err != nil {
		t.Fatal(err)
	}
	tags := at.Common()
	if got, want := tags.Value(tag.Artist), "adfsasaf"; got != want {
		t.Errorf("artist = %q, want %q", got, want)
	}
	if got, want := tags.Value(tag.Track), "32"; got != want {
		t.Errorf("track = %q, want %q", got, want)
	}
	if got, want := tags.Value(tag.Date), "2001"; got != want {
		t.Errorf("date = %q, want %q", got, want)
	}
}
