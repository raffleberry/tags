package mp4

import (
	"testing"
	"time"
)

func TestChapters(t *testing.T) {
	f := open(t, "nero-chapters.m4b")

	chapters := f.Chapters()
	if len(chapters) != 112 {
		t.Fatalf("len(Chapters()) = %d, want 112", len(chapters))
	}
	first := chapters[0]
	if first.Title != "001" || first.Start != 0 {
		t.Errorf("Chapters()[0] = %+v, want title 001 at 0", first)
	}
	// Start times use 100 nanosecond units.
	if got, want := chapters[1].Start, 17*time.Second; got < want || got > want+time.Second {
		t.Errorf("Chapters()[1].Start = %v, want ~17s", got)
	}
	if got, want := chapters[1].Title, "002"; got != want {
		t.Errorf("Chapters()[1].Title = %q, want %q", got, want)
	}
	last := chapters[len(chapters)-1]
	if last.Start <= chapters[len(chapters)-2].Start {
		t.Errorf("last chapter starts at %v, want it after the previous one", last.Start)
	}
}

func TestNoChapters(t *testing.T) {
	f := open(t, "has-tags.m4a")
	if len(f.Chapters()) != 0 {
		t.Errorf("Chapters() = %v, want none", f.Chapters())
	}
}

func TestChplTruncated(t *testing.T) {
	if _, err := ParseChpl(nil); err == nil {
		t.Error("ParseChpl(nil) = nil error, want a failure")
	}
	if _, err := ParseChpl([]byte{1, 0, 0, 0, 0, 0, 0, 0, 1}); err == nil {
		t.Error("ParseChpl(count without entries) = nil error, want a failure")
	}
}
