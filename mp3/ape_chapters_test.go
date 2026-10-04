package mp3

import "testing"

func TestAPETrailer(t *testing.T) {
	f := open(t, "audacious-trailing-id32-apev2.mp3")

	at := f.APE()
	if at == nil {
		t.Fatal("APE() = nil, want a tag")
	}
	if got, want := at.Value("Artist"), "adfsasaf"; got != want {
		t.Errorf("APE Artist = %q, want %q", got, want)
	}
	// APE fields are merged into the common view.
	if got, want := f.Tags().Value("artist"), "adfsasaf"; got != want {
		t.Errorf("Tags()[artist] = %q, want %q", got, want)
	}
}

func TestAPEPriority(t *testing.T) {
	// The file holds both ID3v2 and APEv2. The ID3v2 title is kept. The leading
	// tag wins over the trailing tag.
	f := open(t, "apev2-lyricsv2.mp3")

	if at := f.APE(); at == nil {
		t.Fatal("APE() = nil, want a tag")
	}
	if got := f.Tags().Value("title"); got == "" {
		t.Error("title is empty, want the ID3v2 value to win")
	}
	if f.ID3v2() == nil {
		t.Error("ID3v2() = nil, want the leading tag")
	}
}

func TestNoAPE(t *testing.T) {
	f := open(t, "silence-44-s.mp3")
	if f.APE() != nil {
		t.Error("APE() != nil, want none")
	}
}

func TestNoChapters(t *testing.T) {
	f := open(t, "silence-44-s.mp3")
	if len(f.Chapters()) != 0 {
		t.Errorf("Chapters() = %v, want none", f.Chapters())
	}
	if len(f.Tables()) != 0 {
		t.Errorf("Tables() = %v, want none", f.Tables())
	}
}
