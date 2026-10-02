// Package bits provides a most significant bit first reader, the bit order
// every compressed audio header in this module is written in.
package bits

import "errors"

// ErrShort is returned when a read runs past the end of the data.
var ErrShort = errors.New("bits: read past end of data")

// Reader reads fields of up to 32 bits from a byte slice, most significant bit
// first. The zero value is not usable; obtain one from [New].
type Reader struct {
	data []byte
	// pos is the bit offset into data.
	pos int
}

// New returns a Reader over data.
func New(data []byte) *Reader { return &Reader{data: data} }

// Read returns the next n bits as an unsigned integer, 0 to 32 of them. Bits
// beyond the end of the data read as zero.
func (r *Reader) Read(n int) uint32 {
	var v uint32
	for ; n > 0; n-- {
		if r.pos >= len(r.data)*8 {
			// Keep going so the caller still ends up at a sensible position.
			v <<= 1
			r.pos++
			continue
		}
		b := r.data[r.pos>>3]
		bit := (b >> (7 - uint(r.pos&7))) & 1
		v = v<<1 | uint32(bit)
		r.pos++
	}
	return v
}

// Uint8 returns the next 8 bits.
func (r *Reader) Uint8() int { return int(r.Read(8)) }

// Uint16 returns the next 16 bits.
func (r *Reader) Uint16() int { return int(r.Read(16)) }

// Uint32 returns the next 32 bits.
func (r *Reader) Uint32() uint32 { return r.Read(32) }

// Skip advances past the next n bits.
func (r *Reader) Skip(n int) { r.pos += n }

// Pos returns the current bit offset.
func (r *Reader) Pos() int { return r.pos }

// Aligned reports whether the reader sits on a byte boundary.
func (r *Reader) Aligned() bool { return r.pos%8 == 0 }

// Left returns how many bits remain unread.
func (r *Reader) Left() int { return len(r.data)*8 - r.pos }
