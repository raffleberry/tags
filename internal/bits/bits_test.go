package bits

import (
	"testing"
)

func TestRead(t *testing.T) {
	// The field widths an MPEG header uses, in order, which is how the layout
	// reads in the specification.
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
	// A field that crosses a byte boundary.
	if got, want := r.Read(16), uint32(0xEF12); got != want {
		t.Errorf("Read(16) = %#x, want %#x", got, want)
	}
}

func TestReadSpanningBytes(t *testing.T) {
	// A 20 bit field that spans three bytes, the layout of the sample rate in a
	// FLAC stream information block.
	data := []byte{0x0A, 0xC4, 0x42, 0xF0}
	r := New(data)

	if got, want := r.Read(20), uint32(0x0AC44); got != want {
		t.Errorf("Read(20) = %#x, want %#x", got, want)
	}
	// What follows starts exactly where the field ended, so the next read picks
	// up the low four bits of the third byte.
	if got, want := r.Read(4), uint32(0x2); got != want {
		t.Errorf("Read(4) = %#x, want %#x", got, want)
	}
}

// TestUintHelpers covers the sized readers, which read from the current position
// rather than from a byte boundary, as the bit fields they are used for require.
func TestUintHelpers(t *testing.T) {
	r := New([]byte{0x12, 0x34, 0x56, 0x78})

	if got, want := r.Uint8(), 0x12; got != want {
		t.Errorf("Uint8() = %#x, want %#x", got, want)
	}
	if got, want := r.Uint16(), 0x3456; got != want {
		t.Errorf("Uint16() = %#x, want %#x", got, want)
	}

	// A 32 bit read from the last byte of the data takes the byte it covers and
	// reads the missing bits as zero.
	fresh := New([]byte{0x78})
	if got, want := fresh.Uint32(), uint32(0x78000000); got != want {
		t.Errorf("Uint32() at the end = %#x, want %#x", got, want)
	}
}

// TestReadPastEnd covers reading more bits than there are. The reader keeps its
// position sensible and reads the missing bits as zero, so a caller walking a
// structure does not have to check the length at every field.
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

	// An empty reader reads nothing but zero.
	empty := New(nil)
	if got := empty.Uint16(); got != 0 {
		t.Errorf("Uint16() on an empty reader = %d, want 0", got)
	}
}

func TestSkip(t *testing.T) {
	r := New([]byte{0xAB, 0xCD, 0xEF})

	// Skipping half a byte lands in the middle of it, so the next read takes
	// the two halves it spans.
	r.Skip(4)
	if got, want := r.Uint8(), 0xBC; got != want {
		t.Errorf("Uint8() after Skip(4) = %#x, want %#x", got, want)
	}

	// The reader is now at a byte boundary, so the next read spans the rest of
	// the second byte and the whole of the third.
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
	// Reading past the end leaves a negative count, which is what a caller would
	// want to know.
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
	// Reading nothing moves nothing.
	if got, want := r.Pos(), 0; got != want {
		t.Errorf("Pos() = %d, want %d", got, want)
	}
	if got, want := r.Uint8(), 0xFF; got != want {
		t.Errorf("Uint8() = %#x, want %#x", got, want)
	}
}
