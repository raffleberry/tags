package id3

import (
	"bytes"
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
// Text frames fill [Frame.Text]. Binary frames, such as an attached picture or
// a private frame, fill [Frame.Data] instead.
type Frame struct {
	// Name is the frame ID, upper case: three characters for ID3v2.2, four for
	// later versions.
	Name string
	// Lang is the three letter ISO-639-2 code of a COMM or USLT frame.
	Lang string
	// Desc is the short description of a COMM, USLT, TXXX or WXXX frame. It is
	// where iTunes keeps normalization data and ReplayGain values.
	Desc string
	// Text holds the decoded values of a text frame, in file order. For a
	// comment or a set of lyrics the description is kept in [Frame.Desc]
	// instead of being the first value.
	Text []string
	// Data holds the raw payload of a binary frame.
	Data []byte
}

// UserText is a decoded TXXX frame: free form text filed under a user defined
// description, the conventional home of ReplayGain values and MusicBrainz
// identifiers.
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

// Values returns the decoded text of every frame with the given name, in file
// order. Binary frames contribute nothing.
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

// Has reports whether the tag holds a frame with the given name.
func (t *Tag) Has(name string) bool {
	_, ok := t.Lookup(name)
	return ok
}

// Pictures returns the artwork held in APIC and PIC frames.
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

// UniqueFileID returns the identifier stored in a UFID frame owned by owner,
// such as "http://musicbrainz.org".
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

// decodeFrame turns a raw frame payload into a [Frame]. ok is false when the
// frame carries nothing this package can represent: a compressed, encrypted or
// grouped frame, an empty one, or one whose payload does not fit the layout its
// ID promises.
func (t *Tag) decodeFrame(name string, payload []byte, flags [2]byte) (Frame, bool) {
	payload, ok := t.applyFrameFlags(name, payload, flags)
	if !ok || len(payload) == 0 {
		return Frame{}, false
	}

	frame := Frame{Name: name}
	switch {
	case name == FramePicture || name == framePictureOld2:
		// Kept raw: Pictures needs the frame name to tell the two layouts
		// apart, and the result is a tag.Picture rather than text.
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
		// Every remaining T frame is a plain text frame.
		frame.Text = decodeText(encoding(payload[0]), payload)

	case strings.HasPrefix(name, "W"):
		// URL frames: a bare Latin-1 string.
		frame.Text = []string{strings.TrimSpace(decodeLatin1(payload))}

	default:
		frame.Data = payload
	}

	if len(frame.Text) == 0 && len(frame.Data) == 0 {
		return Frame{}, false
	}
	return frame, true
}

// applyFrameFlags resolves the per frame flags of ID3v2.3 and ID3v2.4, which
// decide whether a frame can be read at all and how its payload is encoded. ok
// is false for a frame that must be skipped.
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
	if compressed || encrypted || grouped {
		return nil, false
	}

	if t.Version.Minor >= 4 && unsynched {
		payload = deunsynchronise(payload)
	}
	if hasLengthIndicator && len(payload) >= 4 {
		// The indicator repeats the frame size as a synchsafe integer. Some
		// taggers set the flag without writing one, so only step over a value
		// that actually agrees with the payload.
		if n, ok := unsynchsafe(payload); ok && n == len(payload)-4 {
			payload = payload[4:]
		}
	}
	return payload, true
}

// decodeTextWithDesc splits a payload that begins with an encoding byte, skips
// a terminated description and decodes the values that follow.
func decodeTextWithDesc(payload []byte) (desc string, text []string, ok bool) {
	enc := encoding(payload[0])
	desc, rest := cutTerminated(enc, payload[1:])
	text = decodeText(enc, append([]byte{byte(enc)}, rest...))
	return desc, text, len(text) > 0
}

// decodeLangText splits a payload laid out as encoding, a three character
// language code, a terminated description and then the values.
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

// decodePicture decodes an APIC frame, or an ID3v2.2 PIC frame which stores a
// three letter image format where APIC stores a MIME type.
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

// mimeFromImage guesses a MIME type from the magic bytes of an image, for
// taggers that left the type out.
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

// cutTerminated returns the first terminated string in data and the bytes after
// it. A string with no terminator runs to the end of the payload.
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
