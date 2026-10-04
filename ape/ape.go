// Package ape reads APEv2 tags.
// Taggers write APETAGEX blocks at the end of an MP3 file.
// A file may also contain ID3.
// An APEv2 tag contains a header, items, and a footer.
// Each header and footer is 32 bytes.
// The header or the footer may be absent.
// A tag at the end of a file has a footer.
// [Read] finds the footer.
// [Parse] decodes a tag with known bounds.
// [Tag.Common] maps items to normalized keys of package tag.
package ape

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/raffleberry/tags/tag"
)

// Errors reported while reading an APEv2 tag.
var (
	// ErrNoTag indicates no APEv2 footer was found.
	ErrNoTag = errors.New("ape: no APEv2 tag")
	// ErrMalformed indicates a header, footer, or item references bytes outside the tag.
	ErrMalformed = errors.New("ape: malformed APEv2 tag")
)

// Magic begins an APEv2 header and footer.
var Magic = []byte("APETAGEX")

// Supported versions.
const (
	version1 = 1000
	version2 = 2000
)

// headerLen is the size of an APEv2 header and of a footer.
const headerLen = 32

// Item types are stored in bits 1 and 2 of the item flags.
const (
	typeText    = 0 // UTF-8 text with NUL separated values.
	typeBinary  = 1 // Binary data.
	typeLocator = 2 // A URL stored as text.
)

// Item is one key to value mapping in an APEv2 tag.
type Item struct {
	// Key is the item key as written, such as "Title" or "Cover Art (Front)".
	Key string
	// Flags holds the raw item flags.
	Flags uint32
	// Values holds decoded text values of a text or locator item.
	// Values are in file order.
	// Binary items leave Values empty and store payload in Data.
	Values []string
	// Data holds raw value bytes of a binary item.
	// It contains the description, a NUL byte, and the payload.
	// Text items leave Data empty.
	Data []byte
}

// Type returns the item type: 0 for text, 1 for binary, 2 for a locator.
func (it Item) Type() int { return int(it.Flags>>1) & 3 }

// ReadOnly reports whether the item is marked read-only.
func (it Item) ReadOnly() bool { return it.Flags&1 != 0 }

// Tag is a decoded APEv2 tag.
type Tag struct {
	// Items holds items in file order.
	Items []Item
	// HasHeader reports whether the tag has a header before its items.
	HasHeader bool
}

// Lookup returns the first item with the given key.
// Key comparison ignores case.
func (t *Tag) Lookup(key string) (Item, bool) {
	for _, it := range t.Items {
		if strings.EqualFold(it.Key, key) {
			return it, true
		}
	}
	return Item{}, false
}

// Values returns text values of every item with the given key.
// Values are in file order.
// Binary items add no values.
func (t *Tag) Values(key string) []string {
	var out []string
	for _, it := range t.Items {
		if strings.EqualFold(it.Key, key) {
			out = append(out, it.Values...)
		}
	}
	return out
}

// Value returns the first text value of the named item, or "".
func (t *Tag) Value(key string) string {
	for _, it := range t.Items {
		if strings.EqualFold(it.Key, key) && len(it.Values) > 0 {
			return it.Values[0]
		}
	}
	return ""
}

// Pictures returns artwork stored in "Cover Art (Front)" and
// "Cover Art (Back)" items.
func (t *Tag) Pictures() []tag.Picture {
	var out []tag.Picture
	for _, it := range t.Items {
		if p, ok := decodeCover(it); ok {
			out = append(out, p)
		}
	}
	return out
}

// decodeCover decodes a cover art item.
// The value is a NUL terminated description followed by encoded image data.
func decodeCover(it Item) (tag.Picture, bool) {
	if !isCoverKey(it.Key) || it.Type() != typeBinary {
		return tag.Picture{}, false
	}
	raw := it.Data
	if len(raw) == 0 {
		return tag.Picture{}, false
	}
	descEnd := bytes.IndexByte(raw, 0)
	var desc string
	var data []byte
	if descEnd < 0 {
		data = raw
	} else {
		desc = string(raw[:descEnd])
		data = raw[descEnd+1:]
	}
	if len(data) == 0 {
		return tag.Picture{}, false
	}
	p := tag.Picture{Desc: desc, Data: data}
	lower := strings.ToLower(it.Key)
	if strings.Contains(lower, "back") {
		p.Type = tag.PictureCoverBack
	} else {
		p.Type = tag.PictureCoverFront
	}
	p.MIME = mimeFromImage(data)
	return p, true
}

// isCoverKey reports whether the key names embedded artwork.
func isCoverKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	return lower == "cover art (front)" || lower == "cover art (back)"
}

// mimeFromImage returns a MIME type from image magic bytes.
func mimeFromImage(data []byte) string {
	switch {
	case len(data) > 2 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "image/jpeg"
	case len(data) > 3 && string(data[:4]) == "\x89PNG":
		return "image/png"
	case len(data) > 3 && string(data[:4]) == "GIF8":
		return "image/gif"
	case len(data) > 1 && string(data[:2]) == "BM":
		return "image/bmp"
	default:
		return ""
	}
}

// footer holds the fields of an APEv2 footer.
type footer struct {
	version int
	size    int
	count   int
	flags   uint32
}

// parseFooter decodes the 32 bytes at the start of data as a footer.
func parseFooter(data []byte) (footer, error) {
	var f footer
	if len(data) < headerLen || !bytes.Equal(data[:8], Magic) {
		return f, ErrNoTag
	}
	f.version = int(binary.LittleEndian.Uint32(data[8:12]))
	if f.version != version1 && f.version != version2 {
		return f, fmt.Errorf("%w: version %d", ErrMalformed, f.version)
	}
	f.size = int(binary.LittleEndian.Uint32(data[12:16]))
	f.count = int(binary.LittleEndian.Uint32(data[16:20]))
	f.flags = binary.LittleEndian.Uint32(data[20:24])
	if f.size < headerLen {
		return f, fmt.Errorf("%w: tag size of %d bytes", ErrMalformed, f.size)
	}
	if f.count < 0 || f.count > 10000 {
		return f, fmt.Errorf("%w: item count of %d", ErrMalformed, f.count)
	}
	for _, b := range data[24:32] {
		if b != 0 {
			return f, fmt.Errorf("%w: reserved bytes are not zero", ErrMalformed)
		}
	}
	return f, nil
}

// isHeaderFlags reports whether flags belong to a header.
// Bit 29 marks the block as a header.
func isHeaderFlags(flags uint32) bool { return flags&0x20000000 != 0 }

// Parse decodes the APEv2 tag in data.
// Data must start at the tag start and end at the footer end.
// The tag start is the header if the tag has one.
func Parse(data []byte) (*Tag, error) {
	if len(data) < headerLen {
		return nil, ErrNoTag
	}
	// A tag with a header starts with a header.
	// Otherwise items start the tag and the footer ends it.
	itemsStart := 0
	t := &Tag{}
	if bytes.Equal(data[:8], Magic) {
		f, err := parseFooter(data[:headerLen])
		if err != nil {
			return nil, err
		}
		if !isHeaderFlags(f.flags) {
			return nil, fmt.Errorf("%w: header block is not marked as one", ErrMalformed)
		}
		t.HasHeader = true
		itemsStart = headerLen
	}
	if len(data)-itemsStart < headerLen {
		return nil, fmt.Errorf("%w: tag of %d bytes", ErrMalformed, len(data))
	}
	footOff := len(data) - headerLen
	f, err := parseFooter(data[footOff:])
	if err != nil {
		return nil, err
	}
	if isHeaderFlags(f.flags) {
		return nil, fmt.Errorf("%w: footer block is marked as a header", ErrMalformed)
	}
	items := data[itemsStart:footOff]
	parsed, err := parseItems(items)
	if err != nil {
		return nil, err
	}
	t.Items = parsed
	return t, nil
}

// parseItems decodes items from data.
// The footer item count is advisory.
// Files may contain a different count.
// Items are read until data is exhausted.
func parseItems(data []byte) ([]Item, error) {
	var out []Item
	pos := 0
	for pos+8 <= len(data) {
		valueLen := int(binary.LittleEndian.Uint32(data[pos : pos+4]))
		flags := binary.LittleEndian.Uint32(data[pos+4 : pos+8])
		if valueLen < 0 || pos+8+valueLen > len(data) {
			return nil, fmt.Errorf("%w: item claims %d bytes in %d left", ErrMalformed, valueLen, len(data)-pos-8)
		}
		rest := data[pos+8:]
		nul := bytes.IndexByte(rest, 0)
		if nul < 0 {
			return nil, fmt.Errorf("%w: item key is not terminated", ErrMalformed)
		}
		key := string(rest[:nul])
		if len(key) < 2 || len(key) > 255 || !isKeyChars(key) {
			return nil, fmt.Errorf("%w: item key %q", ErrMalformed, key)
		}
		value := rest[nul+1:]
		if len(value) < valueLen {
			return nil, fmt.Errorf("%w: item %q is truncated", ErrMalformed, key)
		}
		value = value[:valueLen]
		out = append(out, decodeItem(key, flags, value))
		pos += 8 + nul + 1 + valueLen
	}
	return out, nil
}

// isKeyChars reports whether every byte of key is allowed.
// Allowed bytes are printable ASCII except NUL.
func isKeyChars(key string) bool {
	for i := 0; i < len(key); i++ {
		if key[i] < 0x20 || key[i] > 0x7E {
			return false
		}
	}
	return true
}

// decodeItem converts one raw value to an Item.
// Text values are split on NUL bytes.
func decodeItem(key string, flags uint32, value []byte) Item {
	it := Item{Key: key, Flags: flags}
	switch (flags >> 1) & 3 {
	case typeBinary:
		it.Data = append([]byte(nil), value...)
	default:
		// Text and locator items are NUL separated UTF-8 strings. A trailing
		// NUL is a terminator rather than an empty value.
		for _, part := range bytes.Split(value, []byte{0}) {
			if len(part) == 0 {
				continue
			}
			it.Values = append(it.Values, string(part))
		}
	}
	return it
}

// maxScan is how far from the end of a file Read searches for a footer.
// A tag is at the end of the file.
// It may precede an ID3v1 tag, a Lyrics3v2 block, or a trailing ID3v2 tag.
// A bounded scan covers these layouts.
// It avoids reading a full audio file.
const maxScan = 64 * 1024

// Read finds and decodes the APEv2 tag in r.
// R must be seekable.
// It returns [ErrNoTag] if the file has no APEv2 tag.
// A file with only ID3 has no APEv2 tag.
func Read(r io.ReadSeeker) (*Tag, error) {
	size, err := seekSize(r)
	if err != nil {
		return nil, err
	}
	if size < headerLen {
		return nil, ErrNoTag
	}
	// Only the tail of the file is scanned for a footer.
	// The tag is then read by its exact bounds.
	// A tag larger than the scan window is still read.
	tailLen := min(int64(maxScan), size)
	tail := make([]byte, tailLen)
	if _, err := r.Seek(size-tailLen, io.SeekStart); err != nil {
		return nil, fmt.Errorf("ape: seeking: %w", err)
	}
	if _, err := io.ReadFull(r, tail); err != nil {
		return nil, fmt.Errorf("ape: reading tag tail: %w", err)
	}
	base := size - tailLen
	for _, cand := range footerCandidates(tail) {
		footOff := base + int64(cand.off)
		// The size covers the items and the footer but not the header.
		tagStart := footOff + headerLen - int64(cand.footer.size)
		tagEnd := footOff + headerLen
		if tagStart < 0 {
			continue
		}
		header, err := readHeaderAt(r, tagStart-headerLen)
		if err == nil {
			tagStart -= headerLen
			_ = header
		}
		length := tagEnd - tagStart
		if length <= 0 || length > size {
			continue
		}
		if _, err := r.Seek(tagStart, io.SeekStart); err != nil {
			return nil, fmt.Errorf("ape: seeking: %w", err)
		}
		data := make([]byte, length)
		if _, err := io.ReadFull(r, data); err != nil {
			continue
		}
		if t, err := Parse(data); err == nil && len(t.Items) > 0 {
			return t, nil
		}
	}
	return nil, ErrNoTag
}

// candidate is one possible footer block found in the scanned tail.
type candidate struct {
	off    int
	footer footer
}

// footerCandidates returns every possible footer block in tail.
// Results are latest first.
// A header has the same magic.
// Headers are skipped by requiring a footer block.
func footerCandidates(tail []byte) []candidate {
	var out []candidate
	for i := len(tail) - headerLen; i >= 0; i-- {
		if !bytes.Equal(tail[i:i+8], Magic) {
			continue
		}
		f, err := parseFooter(tail[i : i+headerLen])
		if err != nil {
			continue
		}
		if isHeaderFlags(f.flags) {
			continue
		}
		out = append(out, candidate{off: i, footer: f})
	}
	return out
}

// readHeaderAt returns the header at offset, or an error when there is none.
func readHeaderAt(r io.ReadSeeker, offset int64) (footer, error) {
	var f footer
	if offset < 0 {
		return f, ErrNoTag
	}
	if _, err := r.Seek(offset, io.SeekStart); err != nil {
		return f, err
	}
	buf := make([]byte, headerLen)
	if _, err := io.ReadFull(r, buf); err != nil {
		return f, err
	}
	f, err := parseFooter(buf)
	if err != nil {
		return f, err
	}
	if !isHeaderFlags(f.flags) {
		return f, ErrNoTag
	}
	return f, nil
}

// seekSize returns the number of bytes in r.
// It restores the initial position.
func seekSize(r io.ReadSeeker) (int64, error) {
	cur, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, fmt.Errorf("ape: seeking: %w", err)
	}
	end, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, fmt.Errorf("ape: seeking: %w", err)
	}
	if _, err := r.Seek(cur, io.SeekStart); err != nil {
		return 0, fmt.Errorf("ape: seeking: %w", err)
	}
	return end - cur, nil
}

func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
