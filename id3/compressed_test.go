package id3

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"testing"
)

// buildTag builds an ID3v2 tag with the given version and frames.
func buildTag(minor int, frames ...[]byte) []byte {
	var body bytes.Buffer
	for _, f := range frames {
		body.Write(f)
	}
	b := body.Bytes()
	header := []byte{'I', 'D', '3', byte(minor), 0, 0}
	header = append(header, synchsafe(len(b))...)
	return append(header, b...)
}

// buildFrame builds one v2.3 or v2.4 frame.
func buildFrame(id string, payload []byte, flags [2]byte, synchsafeSize bool) []byte {
	out := []byte(id)
	size := len(payload)
	if synchsafeSize {
		out = append(out, synchsafe(size)...)
	} else {
		var tmp [4]byte
		binary.BigEndian.PutUint32(tmp[:], uint32(size))
		out = append(out, tmp[:]...)
	}
	out = append(out, flags[0], flags[1])
	return append(out, payload...)
}

func compress(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestCompressedV23(t *testing.T) {
	raw := append([]byte{3}, "Hello compressed"...)
	deflated := compress(t, raw)
	var sizeBytes [4]byte
	binary.BigEndian.PutUint32(sizeBytes[:], uint32(len(raw)))
	payload := append(sizeBytes[:], deflated...)
	frame := buildFrame("TIT2", payload, [2]byte{0, 0x80}, false)

	tag, err := Read(bytes.NewReader(buildTag(3, frame)))
	if err != nil {
		t.Fatalf("Read() = %v", err)
	}
	if got, want := tag.Value("TIT2"), "Hello compressed"; got != want {
		t.Errorf("TIT2 = %q, want %q", got, want)
	}
}

func TestCompressedV24(t *testing.T) {
	raw := append([]byte{3}, "Hello v24"...)
	deflated := compress(t, raw)
	payload := append(synchsafe(len(raw)), deflated...)
	frame := buildFrame("TIT2", payload, [2]byte{0, 0x08 | 0x01}, true)

	tag, err := Read(bytes.NewReader(buildTag(4, frame)))
	if err != nil {
		t.Fatalf("Read() = %v", err)
	}
	if got, want := tag.Value("TIT2"), "Hello v24"; got != want {
		t.Errorf("TIT2 = %q, want %q", got, want)
	}
}

func TestGroupedFrame(t *testing.T) {
	// Grouped frames start with a group identifier.
	payload := append([]byte{0x01, 3}, "Grouped"...)
	frame := buildFrame("TIT2", payload, [2]byte{0, 0x20}, false)

	tag, err := Read(bytes.NewReader(buildTag(3, frame)))
	if err != nil {
		t.Fatalf("Read() = %v", err)
	}
	if got, want := tag.Value("TIT2"), "Grouped"; got != want {
		t.Errorf("TIT2 = %q, want %q", got, want)
	}
}

func TestEncryptedSkipped(t *testing.T) {
	payload := append([]byte{3}, "Secret"...)
	frame := buildFrame("TIT2", payload, [2]byte{0, 0x40}, false)

	tag, err := Read(bytes.NewReader(buildTag(3, frame)))
	if err != nil {
		t.Fatalf("Read() = %v", err)
	}
	if tag.Has("TIT2") {
		t.Error("Has(TIT2) = true for an encrypted frame, want it skipped")
	}
}

func TestCorruptCompressedSkipped(t *testing.T) {
	payload := append([]byte{0, 0, 0, 5}, "not zlib"...)
	frame := buildFrame("TIT2", payload, [2]byte{0, 0x80}, false)

	tag, err := Read(bytes.NewReader(buildTag(3, frame)))
	if err != nil {
		t.Fatalf("Read() = %v", err)
	}
	if tag.Has("TIT2") {
		t.Error("Has(TIT2) = true for corrupt compressed data, want it skipped")
	}
}
