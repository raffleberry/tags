package id3

import (
	"bytes"
	"encoding/binary"
	"strings"
	"unicode/utf16"
)

// encoding is the ID3v2 text encoding byte at the start of each text frame.
type encoding byte

const (
	encLatin1  encoding = 0 // ISO-8859-1, one byte terminator
	encUTF16   encoding = 1 // UTF-16 with a byte order mark, two byte terminator
	encUTF16BE encoding = 2 // UTF-16BE without a mark, two byte terminator
	encUTF8    encoding = 3 // UTF-8, one byte terminator
)

// decodeText splits a text frame payload into values and decodes each value.
// The encoding byte is skipped.
//
// Trailing padding is decoded first. Empty values from padding are removed after decoding.
func decodeText(enc encoding, data []byte) []string {
	data = dropEncoding(enc, data)

	var values []string
	for {
		chunk, rest, more := cutAtTerminator(enc, data)
		values = append(values, decodeString(enc, chunk))
		if !more {
			break
		}
		data = rest
	}
	// A trailing terminator yields one empty value; padding after the last real
	// value can yield more. They carry no information.
	for len(values) > 0 && values[len(values)-1] == "" {
		values = values[:len(values)-1]
	}
	return values
}

// decodeString decodes one text value.
func decodeString(enc encoding, data []byte) string {
	switch enc {
	case encLatin1:
		return decodeLatin1(data)
	case encUTF8:
		return decodeUTF8(data)
	case encUTF16BE:
		return decodeUTF16(data, true)
	default:
		return decodeUTF16BOM(data)
	}
}

// dropEncoding removes the leading encoding byte when present. Some frames omit it.
func dropEncoding(enc encoding, data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	if b := encoding(data[0]); b <= encUTF8 {
		return data[1:]
	}
	// Without an encoding byte the payload uses the frame encoding.
	return data
}

// cutAtTerminator splits data at its first terminator. It reports whether a terminator was found. Terminators match the encoding alignment.
func cutAtTerminator(enc encoding, data []byte) (head, rest []byte, found bool) {
	if enc == encLatin1 || enc == encUTF8 {
		if i := bytes.IndexByte(data, 0); i >= 0 {
			return data[:i], data[i+1:], true
		}
		return data, nil, false
	}
	for i := 0; i+1 < len(data); i += 2 {
		if data[i] == 0 && data[i+1] == 0 {
			return data[:i], data[i+2:], true
		}
	}
	// An odd trailing byte is kept.
	return data, nil, false
}

// trimNuls removes trailing zero bytes.
func trimNuls(data []byte) []byte {
	end := len(data)
	for end > 0 && data[end-1] == 0 {
		end--
	}
	return data[:end]
}

// decodeLatin1 decodes ISO-8859-1 data. Each byte maps to the same code point.
func decodeLatin1(data []byte) string {
	var b strings.Builder
	b.Grow(len(data))
	for _, c := range data {
		b.WriteRune(rune(c))
	}
	return b.String()
}

// decodeUTF8 decodes UTF-8 data. Invalid sequences become the replacement character.
func decodeUTF8(data []byte) string {
	return strings.ToValidUTF8(string(data), "\uFFFD")
}

// decodeUTF16 decodes UTF-16 data. A trailing odd byte is dropped.
func decodeUTF16(data []byte, bigEndian bool) string {
	n := len(data) / 2
	if n == 0 {
		return ""
	}
	units := make([]uint16, n)
	for i := range units {
		if bigEndian {
			units[i] = binary.BigEndian.Uint16(data[2*i:])
		} else {
			units[i] = binary.LittleEndian.Uint16(data[2*i:])
		}
	}
	return string(utf16.Decode(units))
}

// decodeUTF16BOM decodes UTF-16 with a byte order mark. Missing or invalid marks use big endian.
func decodeUTF16BOM(data []byte) string {
	switch {
	case len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE:
		return decodeUTF16(data[2:], false)
	case len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF:
		return decodeUTF16(data[2:], true)
	default:
		return decodeUTF16(data, true)
	}
}
