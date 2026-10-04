package bits

import (
	"testing"
)

func TestRead(t *testing.T) {
	// These field widths match an MPEG header.
	// They are in order.
	r := New([]byte{0xAB, 0xCD, 0xEF, 0x12})

	if got, want := r.Read(4), uint32(0xA); got != want {
		t.Errorf("Read(4) = %#x, want %#x", got, want)
	}
	if got, want := r.Read(4), uint32(0xB); got != want {
		t.Errorf("Read(4) = %#x, want %#x", got, want)
	}
	if got, want := r.Read(8), uint32(0xCD); got != want {
		t.Errorf("Read(8) = %#x, want %#x", got, want)
	}
	// This field crosses a byte boundary.
	if got, want := r.Read(16), uint32(0xEF12); got != want {
		t.Errorf("Read(16) = %#x, want %#x", got, want)
	}
}

func TestReadSpanningBytes(t *testing.T) {
	// This is a 20 bit field that spans three bytes.
	// It matches the sample rate layout in a FLAC stream information block.
	data := []byte{0x0A, 0xC4, 0x42, 0xF0}
	r := New(data)

	if got, want := r.Read(20), uint32(0x0AC44); got != want {
		t.Errorf("Read(20) = %#x, want %#x", got, want)
	}
	// The next field starts where the prior field ended.
	// The next read returns the low four bits of the third byte.
	if got, want := r.Read(4), uint32(0x2); got != want {
		t.Errorf("Read(4) = %#x, want %#x", got, want)
	}
}

// TestUintHelpers tests the sized readers.
// The readers read from the current position.
// They do not require a byte boundary.
func TestUintHelpers(t *testing.T) {
	r := New([]byte{0x12, 0x34, 0x56, 0x78})

	if got, want := r.Uint8(), 0x12; got != want {
		t.Errorf("Uint8() = %#x, want %#x", got, want)
	}
	if got, want := r.Uint16(), 0x3456; got != want {
		t.Errorf("Uint16() = %#x, want %#x", got, want)
	}

	// A 32 bit read from the last byte uses that byte.
	// Missing bits read as zero.
	fresh := New([]byte{0x78})
	if got, want := fresh.Uint32(), uint32(0x78000000); got != want {
		t.Errorf("Uint32() at the end = %#x, want %#x", got, want)
	}
}

// TestReadPastEnd tests reading more bits than exist.
// The reader advances its position.
// Missing bits read as zero.
// A caller does not check length at each field.
func TestReadPastEnd(t *testing.T) {
	r := New([]byte{0xFF})

	if got, want := r.Read(8), uint32(0xFF); got != want {
		t.Errorf("Read(8) = %#x, want %#x", got, want)
	}
	if got, want := r.Read(8), uint32(0); got != want {
		t.Errorf("Read(8) past the end = %#x, want %#x", got, want)
	}
	if got, want := r.Pos(), 16; got != want {
		t.Errorf("Pos() = %d, want %d", got, want)
	}

	// An empty reader returns zero.
	empty := New(nil)
	if got := empty.Uint16(); got != 0 {
		t.Errorf("Uint16() on an empty reader = %d, want 0", got)
	}
}

func TestSkip(t *testing.T) {
	r := New([]byte{0xAB, 0xCD, 0xEF})

	// Skipping half a byte places the reader mid-byte.
	// The next read spans both halves.
	r.Skip(4)
	if got, want := r.Uint8(), 0xBC; got != want {
		t.Errorf("Uint8() after Skip(4) = %#x, want %#x", got, want)
	}

	// The reader is on a byte boundary.
	// The next read spans the rest of the second byte and all of the third.
	r.Skip(4)
	if got, want := r.Uint16(), 0xEF00; got != want {
		t.Errorf("Uint16() after two skips = %#x, want %#x", got, want)
	}
}

func TestAligned(t *testing.T) {
	tests := []struct {
		name    string
		reads   []int
		aligned bool
	}{
		{"before reading", nil, true},
		{"after a byte", []int{8}, true},
		{"after two bytes", []int{8, 8}, true},
		{"after four bits", []int{4}, false},
		{"after twelve bits", []int{8, 4}, false},
		{"back on a boundary", []int{4, 4}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New(make([]byte, 8))
			for _, n := range tt.reads {
				r.Read(n)
			}
			if got := r.Aligned(); got != tt.aligned {
				t.Errorf("Aligned() = %v, want %v", got, tt.aligned)
			}
		})
	}
}

func TestLeft(t *testing.T) {
	r := New([]byte{0x12, 0x34})

	if got, want := r.Left(), 16; got != want {
		t.Errorf("Left() = %d, want %d", got, want)
	}
	r.Skip(4)
	if got, want := r.Left(), 12; got != want {
		t.Errorf("Left() after Skip(4) = %d, want %d", got, want)
	}
	// Reading past the end produces a negative count.
	r.Read(32)
	if got := r.Left(); got > 0 {
		t.Errorf("Left() after reading past the end = %d, want a negative count", got)
	}
}

func TestZeroWidthRead(t *testing.T) {
	r := New([]byte{0xFF, 0xFF})

	if got := r.Read(0); got != 0 {
		t.Errorf("Read(0) = %d, want 0", got)
	}
	// A zero width read does not change the position.
	if got, want := r.Pos(), 0; got != want {
		t.Errorf("Pos() = %d, want %d", got, want)
	}
	if got, want := r.Uint8(), 0xFF; got != want {
		t.Errorf("Uint8() = %#x, want %#x", got, want)
	}
}
