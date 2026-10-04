package flac

import (
	"encoding/binary"
	"fmt"
)

// CueSheet holds the track and index layout of the audio.
type CueSheet struct {
	// Catalog is the media catalog number. It is often empty.
	Catalog string
	// LeadIn is the lead-in sample count. It is often two seconds of audio.
	LeadIn uint64
	// CompactDisc is true when the flag marks a compact disc.
	CompactDisc bool
	// Tracks holds tracks in file order. The last track is lead-out.
	Tracks []CueTrack
}

// CueTrack is one track of a cue sheet.
type CueTrack struct {
	// Offset is the track start in samples.
	Offset uint64
	// Number is the track number. 255 marks the lead-out track.
	Number int
	// ISRC is the recording code. It is often empty.
	ISRC string
	// Audio is true when the track holds audio.
	Audio bool
	// PreEmphasis is true when the track uses pre-emphasis.
	PreEmphasis bool
	// Indices holds positions within the track.
	Indices []CueIndex
}

// CueIndex is one index point within a cue sheet track.
type CueIndex struct {
	// Offset is the index start in samples relative to the track.
	Offset uint64
	// Number is the index number.
	Number int
}

// ParseCueSheet decodes a cue sheet block payload.
func ParseCueSheet(data []byte) (*CueSheet, error) {
	// The header holds a 128 byte catalog, an 8 byte lead-in count, a flag byte, 258 reserved bytes, and a track count byte.
	const headerLen = 128 + 8 + 1 + 258 + 1
	if len(data) < headerLen {
		return nil, fmt.Errorf("%w: cue sheet of %d bytes", ErrBlock, len(data))
	}
	sheet := &CueSheet{
		Catalog:     nulTerminated(data[:128]),
		LeadIn:      binary.BigEndian.Uint64(data[128:136]),
		CompactDisc: data[136]&0x80 != 0,
	}
	for _, b := range data[137:395] {
		if b != 0 {
			return nil, fmt.Errorf("%w: cue sheet reserves non zero bytes", ErrBlock)
		}
	}
	count := int(data[395])
	pos := headerLen
	for range count {
		track, next, err := parseCueTrack(data, pos)
		if err != nil {
			return nil, err
		}
		sheet.Tracks = append(sheet.Tracks, track)
		pos = next
	}
	if pos != len(data) {
		return nil, fmt.Errorf("%w: cue sheet has %d trailing bytes", ErrBlock, len(data)-pos)
	}
	return sheet, nil
}

// trackLen is the size of a cue sheet track record without its indices.
const trackLen = 36

// indexLen is the size of one cue sheet index point.
const indexLen = 12

// parseCueTrack decodes the track at pos. It returns the track and the next offset.
func parseCueTrack(data []byte, pos int) (CueTrack, int, error) {
	var track CueTrack
	if pos+trackLen > len(data) {
		return track, pos, fmt.Errorf("%w: truncated cue sheet track", ErrBlock)
	}
	rec := data[pos : pos+trackLen]
	track.Offset = binary.BigEndian.Uint64(rec[0:8])
	track.Number = int(rec[8])
	track.ISRC = nulTerminated(rec[9:21])
	flags := rec[21]
	track.Audio = flags&0x80 == 0
	track.PreEmphasis = flags&0x40 != 0
	for _, b := range rec[22:35] {
		if b != 0 {
			return track, pos, fmt.Errorf("%w: cue sheet track reserves non zero bytes", ErrBlock)
		}
	}
	indices := int(rec[35])
	pos += trackLen
	for range indices {
		if pos+indexLen > len(data) {
			return track, pos, fmt.Errorf("%w: truncated cue sheet index", ErrBlock)
		}
		idx := data[pos : pos+indexLen]
		track.Indices = append(track.Indices, CueIndex{
			Offset: binary.BigEndian.Uint64(idx[0:8]),
			Number: int(idx[8]),
		})
		for _, b := range idx[9:12] {
			if b != 0 {
				return track, pos, fmt.Errorf("%w: cue sheet index reserves non zero bytes", ErrBlock)
			}
		}
		pos += indexLen
	}
	return track, pos, nil
}

// nulTerminated returns the string up to the first NUL byte.
func nulTerminated(data []byte) string {
	for i, b := range data {
		if b == 0 {
			return string(data[:i])
		}
	}
	return string(data)
}
