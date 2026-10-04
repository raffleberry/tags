package flac

import "testing"

func TestCueSheet(t *testing.T) {
	f, err := Open(dataDir + "silence-44-s.flac")
	if err != nil {
		t.Fatal(err)
	}
	c := f.CueSheet()
	if c == nil {
		t.Fatal("CueSheet() = nil, want one")
	}
	if got, want := c.Catalog, "1234567890123"; got != want {
		t.Errorf("Catalog = %q, want %q", got, want)
	}
	if got, want := c.LeadIn, uint64(88200); got != want {
		t.Errorf("LeadIn = %d, want %d", got, want)
	}
	if !c.CompactDisc {
		t.Error("CompactDisc = false, want true")
	}
	if len(c.Tracks) != 4 {
		t.Fatalf("len(Tracks) = %d, want 4", len(c.Tracks))
	}
	first := c.Tracks[0]
	if first.Number != 1 || first.Offset != 0 {
		t.Errorf("Tracks[0] = %+v, want number 1 at offset 0", first)
	}
	if got, want := first.ISRC, "123456789012"; got != want {
		t.Errorf("ISRC = %q, want %q", got, want)
	}
	if len(first.Indices) != 1 || first.Indices[0].Number != 1 {
		t.Errorf("Indices = %+v, want one index number 1", first.Indices)
	}
}

func TestNoCueSheet(t *testing.T) {
	f, err := Open(dataDir + "variable-block.flac")
	if err != nil {
		t.Fatal(err)
	}
	if f.CueSheet() != nil {
		t.Error("CueSheet() != nil, want none")
	}
}

func TestCueSheetTruncated(t *testing.T) {
	if _, err := ParseCueSheet(make([]byte, 100)); err == nil {
		t.Error("ParseCueSheet(short) = nil error, want a failure")
	}
	// A header claiming one track but holding none.
	data := make([]byte, 396)
	data[395] = 1
	if _, err := ParseCueSheet(data); err == nil {
		t.Error("ParseCueSheet(missing track) = nil error, want a failure")
	}
}
