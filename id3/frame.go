package id3

import (
	"bytes"
	"compress/zlib"
	"io"
	"strings"

	"github.com/raffleberry/tags/tag"
)

// Frame names this package decodes rather than storing raw.
const (
	FramePicture    = "APIC" // ID3v2.3 and ID3v2.4
	FramePictureOld = "PIC"  // ID3v2.2
	FrameComment    = "COMM"
	FrameLyrics     = "USLT"
	FrameUserText   = "TXXX"
	FrameUserURL    = "WXXX"
	FrameGenre      = "TCON" // ID3v2.3 and ID3v2.4
	FrameGenreOld   = "TCO"  // ID3v2.2
	FrameUniqueFile = "UFID"
)

// The ID3v2.2 names of the frames above.
const (
	framePictureOld2 = "PIC"
	frameComment2    = "COM"
	frameLyrics2     = "ULT"
	frameUserText2   = "TXX"
	frameUserURL2    = "WXX"
)

// Frame is one decoded ID3v2 frame.
//
// Text frames use [Frame.Text]. Binary frames use [Frame.Data]. Chapter frames
// use [Frame.Chapter] and table of contents frames use [Frame.TOC]. Chapter and
// table of contents frames also keep the raw payload in [Frame.Data].
type Frame struct {
	// Name is the frame ID in upper case. ID3v2.2 uses three characters. Later versions use four.
	Name string
	// Lang is the ISO-639-2 language code of a COMM or USLT frame.
	Lang string
	// Desc is the description of a COMM, USLT, TXXX, or WXXX frame.
	Desc string
	// Text holds decoded text values in file order. For COMM and USLT frames the description is in [Frame.Desc].
	Text []string
	// Data holds the raw payload of a binary frame.
	Data []byte
	// Chapter holds the decoded CHAP data. It is nil for other frames.
	Chapter *Chapter
	// TOC holds the decoded CTOC data. It is nil for other frames.
	TOC *TableOfContents
}

// UserText is a decoded TXXX frame. It holds free-form text under a user-defined description.
type UserText struct {
	Desc string
	Text []string
}

// UserURL is a decoded WXXX frame.
type UserURL struct {
	Desc string
	URL  string
}

// Value returns the first decoded text of the named frame, or "".
func (t *Tag) Value(name string) string {
	for _, f := range t.Frames {
		if f.Name == name && len(f.Text) > 0 {
			return f.Text[0]
		}
	}
	return ""
}

// Values returns decoded text from each frame with the given name, in file order. Binary frames are skipped.
func (t *Tag) Values(name string) []string {
	var values []string
	for _, f := range t.Frames {
		if f.Name == name {
			values = append(values, f.Text...)
		}
	}
	return values
}

// Lookup returns the first frame with the given name.
func (t *Tag) Lookup(name string) (Frame, bool) {
	for _, f := range t.Frames {
		if f.Name == name {
			return f, true
		}
	}
	return Frame{}, false
}

// Has reports whether the tag contains a frame with the given name.
func (t *Tag) Has(name string) bool {
	_, ok := t.Lookup(name)
	return ok
}

// Pictures returns artwork from APIC and PIC frames.
func (t *Tag) Pictures() []tag.Picture {
	var out []tag.Picture
	for _, f := range t.Frames {
		if p, ok := decodePicture(f); ok {
			out = append(out, p)
		}
	}
	return out
}

// UserText returns every TXXX frame in the tag.
func (t *Tag) UserText() []UserText {
	var out []UserText
	for _, f := range t.Frames {
		if f.Name == FrameUserText || f.Name == frameUserText2 {
			out = append(out, UserText{Desc: f.Desc, Text: f.Text})
		}
	}
	return out
}

// UserURL returns every WXXX frame in the tag.
func (t *Tag) UserURL() []UserURL {
	var out []UserURL
	for _, f := range t.Frames {
		if f.Name == FrameUserURL || f.Name == frameUserURL2 {
			out = append(out, UserURL{Desc: f.Desc, URL: firstOrEmpty(f.Text)})
		}
	}
	return out
}

// UniqueFileID returns the identifier in a UFID frame with the given owner.
func (t *Tag) UniqueFileID(owner string) ([]byte, bool) {
	for _, f := range t.Frames {
		if f.Name != FrameUniqueFile {
			continue
		}
		got, id, found := bytes.Cut(f.Data, []byte{0})
		if found && string(got) == owner && len(id) > 0 {
			return id, true
		}
	}
	return nil, false
}

// decodeFrame decodes a raw frame payload into a [Frame]. ok is false for encrypted, empty, or malformed frames.
func (t *Tag) decodeFrame(name string, payload []byte, flags [2]byte) (Frame, bool) {
	payload, ok := t.applyFrameFlags(name, payload, flags)
	if !ok || len(payload) == 0 {
		return Frame{}, false
	}

	frame := Frame{Name: name}
	switch {
	case name == FrameChapter:
		return t.decodeChapter(name, payload)

	case name == FrameTOC:
		return t.decodeTOC(name, payload)

	case name == FramePicture || name == framePictureOld2:
		// Stored raw. The frame name identifies the layout during picture decoding.
		frame.Data = payload

	case name == FrameComment || name == frameComment2:
		lang, desc, text, ok := decodeLangText(payload)
		if !ok {
			return Frame{}, false
		}
		frame.Lang, frame.Desc, frame.Text = lang, desc, text

	case name == FrameLyrics || name == frameLyrics2:
		lang, desc, text, ok := decodeLangText(payload)
		if !ok {
			return Frame{}, false
		}
		frame.Lang, frame.Desc, frame.Text = lang, desc, text

	case name == FrameUserText || name == frameUserText2:
		desc, text, ok := decodeTextWithDesc(payload)
		if !ok {
			return Frame{}, false
		}
		frame.Desc, frame.Text = desc, text

	case name == FrameUserURL || name == frameUserURL2:
		desc, rest := cutTerminated(encoding(payload[0]), payload[1:])
		url := strings.TrimSpace(decodeLatin1(trimNuls(rest)))
		if desc == "" && url == "" {
			return Frame{}, false
		}
		frame.Desc, frame.Text = desc, []string{url}

	case strings.HasPrefix(name, "T"):
		// Remaining T frames are text frames.
		frame.Text = decodeText(encoding(payload[0]), payload)

	case strings.HasPrefix(name, "W"):
		// W frames hold a Latin-1 URL string.
		frame.Text = []string{strings.TrimSpace(decodeLatin1(payload))}

	default:
		frame.Data = payload
	}

	if len(frame.Text) == 0 && len(frame.Data) == 0 && frame.Chapter == nil && frame.TOC == nil {
		return Frame{}, false
	}
	return frame, true
}

// applyFrameFlags applies ID3v2.3 and ID3v2.4 frame flags to a payload. ok is false for skipped frames.
//
// Compressed frames are decompressed. Group identifiers are removed. Frame-level unsynchronisation is removed first.
func (t *Tag) applyFrameFlags(name string, payload []byte, flags [2]byte) ([]byte, bool) {
	var compressed, encrypted, grouped, unsynched, hasLengthIndicator bool
	if t.Version.Minor >= 4 {
		grouped = flags[1]&0x40 != 0
		compressed = flags[1]&0x08 != 0
		encrypted = flags[1]&0x04 != 0
		unsynched = flags[1]&0x02 != 0
		hasLengthIndicator = flags[1]&0x01 != 0
	} else if t.Version.Minor == 3 {
		compressed = flags[1]&0x80 != 0
		encrypted = flags[1]&0x40 != 0
		grouped = flags[1]&0x20 != 0
	}
	// Encrypted frames are skipped.
	if encrypted {
		return nil, false
	}

	if t.Version.Minor >= 4 && unsynched {
		payload = deunsynchronise(payload)
	}

	// Extra header bytes precede the data: group identifier, then length indicator in v2.4 or decompressed size in v2.3.
	if grouped {
		if len(payload) < 1 {
			return nil, false
		}
		payload = payload[1:]
	}

	if t.Version.Minor == 3 && compressed {
		if len(payload) < 4 {
			return nil, false
		}
		payload = payload[4:]
		return inflate(payload)
	}

	if t.Version.Minor >= 4 && compressed {
		if len(payload) < 4 {
			return nil, false
		}
		// The indicator holds the decompressed size. Both synchsafe and plain integers are accepted.
		payload = payload[4:]
		return inflate(payload)
	}

	if hasLengthIndicator && len(payload) >= 4 {
		// The indicator repeats the frame size. It is skipped only when it matches the payload length.
		if n, ok := unsynchsafe(payload); ok && n == len(payload)-4 {
			payload = payload[4:]
		} else if n := int(payload[0])<<24 | int(payload[1])<<16 | int(payload[2])<<8 | int(payload[3]); n == len(payload)-4 {
			payload = payload[4:]
		}
	}
	return payload, true
}

// maxInflated is the maximum decompressed size of a compressed frame in bytes.
const maxInflated = 8 << 20

// inflate decompresses zlib data. ok is false for invalid or oversized output.
func inflate(data []byte) ([]byte, bool) {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, false
	}
	defer r.Close()
	out, err := io.ReadAll(io.LimitReader(r, maxInflated+1))
	if err != nil {
		return nil, false
	}
	if len(out) > maxInflated {
		return nil, false
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// decodeTextWithDesc decodes a payload with an encoding byte, a terminated description, and text values.
func decodeTextWithDesc(payload []byte) (desc string, text []string, ok bool) {
	enc := encoding(payload[0])
	desc, rest := cutTerminated(enc, payload[1:])
	text = decodeText(enc, append([]byte{byte(enc)}, rest...))
	return desc, text, len(text) > 0
}

// decodeLangText decodes a payload with an encoding byte, a language code, a terminated description, and text values.
func decodeLangText(payload []byte) (lang, desc string, text []string, ok bool) {
	if len(payload) < 4 {
		return "", "", nil, false
	}
	enc := encoding(payload[0])
	lang = strings.TrimSpace(string(payload[1:4]))
	desc, rest := cutTerminated(enc, payload[4:])
	text = decodeText(enc, append([]byte{byte(enc)}, rest...))
	return lang, desc, text, len(text) > 0
}

// decodePicture decodes an APIC frame or an ID3v2.2 PIC frame. PIC stores a three-letter format. APIC stores a MIME type.
func decodePicture(f Frame) (tag.Picture, bool) {
	if len(f.Data) < 2 {
		return tag.Picture{}, false
	}
	enc := encoding(f.Data[0])

	var p tag.Picture
	if f.Name == framePictureOld2 {
		if len(f.Data) < 5 {
			return p, false
		}
		p.MIME = "image/" + strings.ToLower(string(f.Data[1:4]))
		p.Type = tag.PictureType(f.Data[4])
		p.Desc, p.Data = cutTerminated(enc, f.Data[5:])
	} else {
		mime, rest, found := bytes.Cut(f.Data[1:], []byte{0})
		if !found || len(rest) == 0 {
			return p, false
		}
		p.MIME = string(mime)
		p.Type = tag.PictureType(rest[0])
		p.Desc, p.Data = cutTerminated(enc, rest[1:])
	}

	if len(p.Data) == 0 {
		return p, false
	}
	if p.MIME == "" {
		p.MIME = mimeFromImage(p.Data)
	}
	return p, true
}

// mimeFromImage returns a MIME type based on image header bytes.
func mimeFromImage(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}):
		return "image/jpeg"
	case bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G'}):
		return "image/png"
	case bytes.HasPrefix(data, []byte("GIF8")):
		return "image/gif"
	case bytes.HasPrefix(data, []byte("BM")):
		return "image/bmp"
	default:
		return ""
	}
}

// cutTerminated returns the first terminated string in data and the remaining bytes. A string without a terminator extends to the end.
func cutTerminated(enc encoding, data []byte) (string, []byte) {
	head, rest, _ := cutAtTerminator(enc, data)
	return decodeString(enc, head), rest
}

func firstOrEmpty(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
