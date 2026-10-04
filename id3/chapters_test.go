package id3

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

// textPayload builds a text frame payload.
func textPayload(enc byte, s string) []byte {
	return append([]byte{enc}, s...)
}

func TestChapter(t *testing.T) {
	// Embedded TIT2 title.
	title := buildFrame("TIT2", textPayload(3, "Chapter 1"), [2]byte{}, false)

	var payload bytes.Buffer
	payload.WriteString("ch1\x00")
	var times [16]byte
	binary.BigEndian.PutUint32(times[0:4], 1000)
	binary.BigEndian.PutUint32(times[4:8], 2000)
	binary.BigEndian.PutUint32(times[8:12], 0xFFFFFFFF)
	binary.BigEndian.PutUint32(times[12:16], 0xFFFFFFFF)
	payload.Write(times[:])
	payload.Write(title)

	frame := buildFrame("CHAP", payload.Bytes(), [2]byte{}, false)
	tag, err := Read(bytes.NewReader(buildTag(3, frame)))
	if err != nil {
		t.Fatalf("Read() = %v", err)
	}
	chapters := tag.Chapters()
	if len(chapters) != 1 {
		t.Fatalf("len(Chapters()) = %d, want 1", len(chapters))
	}
	ch := chapters[0]
	if ch.ElementID != "ch1" {
		t.Errorf("ElementID = %q, want %q", ch.ElementID, "ch1")
	}
	if ch.Start != 1000*time.Millisecond || ch.End != 2000*time.Millisecond {
		t.Errorf("Start/End = %v/%v, want 1s/2s", ch.Start, ch.End)
	}
	if ch.StartOffset != 0xFFFFFFFF {
		t.Errorf("StartOffset = %#x, want unset", ch.StartOffset)
	}
	if got := ch.Title(); got != "Chapter 1" {
		t.Errorf("Title() = %q, want %q", got, "Chapter 1")
	}
}

func TestTableOfContents(t *testing.T) {
	title := buildFrame("TIT2", textPayload(3, "Part 1"), [2]byte{}, false)

	var payload bytes.Buffer
	payload.WriteString("toc\x00")
	payload.WriteByte(0x03) // top level and ordered
	payload.WriteByte(2)
	payload.WriteString("ch1\x00ch2\x00")
	payload.Write(title)

	frame := buildFrame("CTOC", payload.Bytes(), [2]byte{}, false)
	tag, err := Read(bytes.NewReader(buildTag(3, frame)))
	if err != nil {
		t.Fatalf("Read() = %v", err)
	}
	tables := tag.Tables()
	if len(tables) != 1 {
		t.Fatalf("len(Tables()) = %d, want 1", len(tables))
	}
	toc := tables[0]
	if !toc.TopLevel || !toc.Ordered {
		t.Errorf("TopLevel/Ordered = %v/%v, want true/true", toc.TopLevel, toc.Ordered)
	}
	if len(toc.Entries) != 2 || toc.Entries[0] != "ch1" || toc.Entries[1] != "ch2" {
		t.Errorf("Entries = %q, want [ch1 ch2]", toc.Entries)
	}
	if got := toc.Title(); got != "Part 1" {
		t.Errorf("Title() = %q, want %q", got, "Part 1")
	}
}

func TestChapterTruncated(t *testing.T) {
	frame := buildFrame("CHAP", []byte("no times here"), [2]byte{}, false)
	tag, err := Read(bytes.NewReader(buildTag(3, frame)))
	if err != nil {
		t.Fatalf("Read() = %v", err)
	}
	if len(tag.Chapters()) != 0 {
		t.Error("Chapters() != empty for a truncated CHAP, want none")
	}
}
