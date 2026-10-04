package id3

import (
	"bytes"
	"encoding/binary"
	"time"
)

// Frame names for chapters and the table of contents.
const (
	FrameChapter = "CHAP"
	FrameTOC     = "CTOC"
)

// noOffset marks an unused chapter byte offset. Time values are used instead.
const noOffset = 0xFFFFFFFF

// Chapter is one CHAP frame. It describes a span of audio.
type Chapter struct {
	// ElementID identifies the chapter within the tag. It is not a display title.
	ElementID string
	// Start and End are time offsets from the start of the file.
	Start, End time.Duration
	// StartOffset and EndOffset are byte offsets of the chapter start and end. noOffset means the value is unused.
	StartOffset, EndOffset uint32
	// Frames holds embedded sub-frames in file order.
	Frames []Frame
}

// Title returns the first TIT2 sub-frame text. It returns "" when absent.
func (c Chapter) Title() string {
	for _, f := range c.Frames {
		if (f.Name == "TIT2" || f.Name == "TT2") && len(f.Text) > 0 {
			return f.Text[0]
		}
	}
	return ""
}

// TableOfContents is one CTOC frame. It lists chapters or other tables.
type TableOfContents struct {
	// ElementID identifies the table within the tag.
	ElementID string
	// TopLevel reports whether this is the root table.
	TopLevel bool
	// Ordered reports whether entries form a sequence.
	Ordered bool
	// Entries holds element IDs of listed chapters and tables.
	Entries []string
	// Frames holds embedded sub-frames in file order.
	Frames []Frame
}

// Title returns the first TIT2 sub-frame text. It returns "" when absent.
func (c TableOfContents) Title() string {
	for _, f := range c.Frames {
		if (f.Name == "TIT2" || f.Name == "TT2") && len(f.Text) > 0 {
			return f.Text[0]
		}
	}
	return ""
}

// Chapters returns tag chapters in file order.
func (t *Tag) Chapters() []Chapter {
	var out []Chapter
	for _, f := range t.Frames {
		if f.Name == FrameChapter && f.Chapter != nil {
			out = append(out, *f.Chapter)
		}
	}
	return out
}

// Tables returns tag tables of contents in file order.
func (t *Tag) Tables() []TableOfContents {
	var out []TableOfContents
	for _, f := range t.Frames {
		if f.Name == FrameTOC && f.TOC != nil {
			out = append(out, *f.TOC)
		}
	}
	return out
}

// layoutOf returns the frame layout for the tag version.
func (t *Tag) layoutOf() frameLayout {
	switch t.Version.Minor {
	case 2:
		return frameLayout{idLen: 3}
	case 3:
		return frameLayout{idLen: 4}
	default:
		return frameLayout{idLen: 4, synchsafeSize: true}
	}
}

// decodeChapter decodes a CHAP frame payload. Embedded sub-frames use the tag layout.
func (t *Tag) decodeChapter(name string, payload []byte) (Frame, bool) {
	frame := Frame{Name: name}
	id, rest, ok := cutString(payload)
	if !ok || len(rest) < 16 {
		return Frame{}, false
	}
	ch := &Chapter{ElementID: id}
	ch.Start = time.Duration(binary.BigEndian.Uint32(rest[0:4])) * time.Millisecond
	ch.End = time.Duration(binary.BigEndian.Uint32(rest[4:8])) * time.Millisecond
	ch.StartOffset = binary.BigEndian.Uint32(rest[8:12])
	ch.EndOffset = binary.BigEndian.Uint32(rest[12:16])
	ch.Frames = t.decodeEmbedded(rest[16:])
	frame.Chapter = ch
	frame.Data = payload
	return frame, true
}

// decodeTOC decodes a CTOC frame payload.
func (t *Tag) decodeTOC(name string, payload []byte) (Frame, bool) {
	frame := Frame{Name: name}
	id, rest, ok := cutString(payload)
	if !ok || len(rest) < 2 {
		return Frame{}, false
	}
	toc := &TableOfContents{ElementID: id}
	// Bit 1 marks the top level table. Bit 0 marks an ordered table.
	toc.TopLevel = rest[0]&0x02 != 0
	toc.Ordered = rest[0]&0x01 != 0
	count := int(rest[1])
	rest = rest[2:]
	for range count {
		entry, next, ok := cutString(rest)
		if !ok {
			return Frame{}, false
		}
		toc.Entries = append(toc.Entries, entry)
		rest = next
	}
	toc.Frames = t.decodeEmbedded(rest)
	frame.TOC = toc
	frame.Data = payload
	return frame, true
}

// decodeEmbedded decodes sub-frames in a CHAP or CTOC payload. A malformed sub-frame ends the sequence.
func (t *Tag) decodeEmbedded(data []byte) []Frame {
	layout := t.layoutOf()
	var out []Frame
	idLen, headerLen := layout.idLen, layout.headerLen()
	for off := 0; off+headerLen <= len(data); {
		id := data[off : off+idLen]
		if isPadding(id) {
			break
		}
		name := string(bytes.ToUpper(bytes.TrimRight(id, "\x00")))
		size, ok := layout.frameSize(data[off:], len(data)-off)
		if !ok {
			break
		}
		payload := data[off+headerLen:]
		flags := [2]byte{}
		if idLen == 4 {
			flags = [2]byte{data[off+8], data[off+9]}
		}
		if frame, ok := t.decodeFrame(name, payload[:size], flags); ok {
			out = append(out, frame)
		}
		off += headerLen + size
	}
	return out
}

// cutString splits the NUL-terminated string at the start of data from the rest. ok is false without a terminator.
func cutString(data []byte) (string, []byte, bool) {
	i := bytes.IndexByte(data, 0)
	if i < 0 {
		return "", nil, false
	}
	return string(data[:i]), data[i+1:], true
}
