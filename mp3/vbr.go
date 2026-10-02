package mp3

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/raffleberry/tags/internal/bits"
	"github.com/raffleberry/tags/tag"
)

// Kind names the flavour of a variable bitrate header.
type Kind string

// The variable bitrate headers in the wild. "Info" is the CBR variant of
// "Xing", written by the same encoders.
const (
	KindXing Kind = "Xing"
	KindInfo Kind = "Info"
	KindVBRI Kind = "VBRI"
)

// VBRHeader is the Xing, Info or VBRI header that a variable bitrate encoder
// writes inside the first audio frame. It describes the whole stream, which is
// why it is worth more than the frame header it sits in.
type VBRHeader struct {
	// Kind is which of the headers this is.
	Kind Kind
	// Frames is how many audio frames the stream holds, or -1 when the header
	// omits it.
	Frames int64
	// Bytes is how many bytes of audio the stream holds, or -1 when the header
	// omits it. The count includes the frame the header itself sits in.
	Bytes int64
	// Scale is the 0 to 100 quality index the encoder wrote, or -1 when the
	// header omits it. Higher is better.
	Scale int
	// LAME describes the encoder, or is nil when the stream was not LAME.
	LAME *LAMEHeader
}

// bitrateMode works out whether the stream is CBR, VBR or ABR from the header,
// falling back to what the header itself implies when LAME says nothing.
func (h *VBRHeader) bitrateMode() tag.BitrateMode {
	if h.LAME != nil {
		switch h.LAME.Method {
		case 1, 8:
			return tag.BitrateCBR
		case 2, 9:
			return tag.BitrateABR
		case 3, 4, 5, 6:
			return tag.BitrateVBR
		}
	}
	// A VBRI header only ever exists in variable bitrate files, and an "Info"
	// header only in constant bitrate ones.
	switch h.Kind {
	case KindVBRI:
		return tag.BitrateVBR
	case KindInfo:
		return tag.BitrateCBR
	}
	if h.Scale >= 0 || h.LAME != nil {
		return tag.BitrateVBR
	}
	return tag.BitrateUnknown
}

// encoder names the tool that wrote the header.
func (h *VBRHeader) encoder() string {
	switch {
	case h.LAME != nil:
		return "LAME " + h.LAME.Version
	case h.Kind == KindVBRI:
		// Fraunhofer's encoder writes a VBRI header.
		return "FhG"
	default:
		return ""
	}
}

// Flags of a Xing header, which say which of the optional fields are present.
const (
	xingFrames    = 0x1
	xingBytes     = 0x2
	xingTOC       = 0x4
	xingQuality   = 0x8
	xingHeaderLen = 8
	vbriHeaderLen = 26
)

// vbrLookahead is how many bytes are read around a candidate variable bitrate
// header. A Xing header with every field set, a LAME version string and the
// LAME extended header together need under 150.
const vbrLookahead = 192

// readVBRHeader returns the variable bitrate header inside the layer 3 frame at
// offset, or nil when the frame has none. It never fails: a frame that looks
// like it has a header but does not is simply treated as constant bitrate.
func readVBRHeader(r io.ReadSeeker, offset int64, h frameHeader) *VBRHeader {
	for _, at := range []int64{xingOffset(h), 36} {
		data, err := readAt(r, offset+at, vbrLookahead)
		if err != nil {
			return nil
		}
		switch {
		case bytes.HasPrefix(data, []byte(KindXing)), bytes.HasPrefix(data, []byte(KindInfo)):
			return parseXing(data)
		case bytes.HasPrefix(data, []byte(KindVBRI)):
			return parseVBRI(data)
		}
	}
	return nil
}

// xingOffset returns where in a layer 3 frame the Xing header sits. The frame
// holds a mix of its own side information and the channel data before it, and
// how much depends on the version and the channel mode.
func xingOffset(h frameHeader) int64 {
	if h.version == MPEG1 {
		if h.mode != Mono {
			return 36
		}
		return 21
	}
	if h.mode != Mono {
		return 21
	}
	return 13
}

// parseXing decodes a Xing or Info header from the start of data.
func parseXing(data []byte) *VBRHeader {
	if len(data) < xingHeaderLen {
		return nil
	}
	h := &VBRHeader{
		Kind:   Kind(data[:4]),
		Frames: -1,
		Bytes:  -1,
		Scale:  -1,
	}
	if h.Kind != KindXing && h.Kind != KindInfo {
		return nil
	}

	flags := readUint32BE(data[4:8])
	pos := xingHeaderLen
	need := func(n int) bool { return pos+n <= len(data) }

	if flags&xingFrames != 0 && need(4) {
		h.Frames = int64(readUint32BE(data[pos:]))
		pos += 4
	}
	if flags&xingBytes != 0 && need(4) {
		h.Bytes = int64(readUint32BE(data[pos:]))
		pos += 4
	}
	if flags&xingTOC != 0 {
		// A seek table, of no interest when only reading.
		pos += 100
	}
	if flags&xingQuality != 0 && need(4) {
		h.Scale = int(readUint32BE(data[pos:]))
		pos += 4
	}

	if version, extAt, ok := parseLAMEVersion(data[pos:]); ok && extAt >= 0 {
		h.LAME = parseLAME(data[pos+extAt:], version, h.Scale)
	}
	return h
}

// parseVBRI decodes a VBRI header from the start of data.
func parseVBRI(data []byte) *VBRHeader {
	if len(data) < vbriHeaderLen {
		return nil
	}
	// Version, delay, quality: only the quality indicator is of interest.
	return &VBRHeader{
		Kind:   KindVBRI,
		Frames: int64(binary.BigEndian.Uint32(data[14:18])),
		Bytes:  int64(binary.BigEndian.Uint32(data[10:14])),
		Scale:  -1,
	}
}

// parseLAMEVersion decodes the "LAME3.99.1" string a Xing header ends with. It
// returns the version as written, the offset of the LAME extended header
// relative to the start of data, and whether that header is there at all. ok is
// false when data does not hold a LAME version string.
//
// Versions before 3.90 predate the extended header, and a 3.90 prerelease that
// carries a long version string is treated as one of them too.
func parseLAMEVersion(data []byte) (version string, extAt int, ok bool) {
	// The version string occupies a fixed 20 byte field, the first nine of which
	// hold it and the last eleven of which are reserved. Reading past the field
	// would take audio for flag characters.
	const fieldLen = 20
	const tailLen = 11
	if len(data) < fieldLen {
		return "", 0, false
	}
	field := data[:fieldLen]
	if !bytes.HasPrefix(field, []byte("LAME")) && !bytes.HasPrefix(field, []byte("L3.99")) {
		return "", 0, false
	}

	// The prefix is spelled in a couple of ways, so step over it by character
	// class rather than by length.
	rest := bytes.TrimLeft(field, "EMAL")
	major, rest := rest[:1], bytes.TrimLeft(rest[1:], ".")
	minor := leadingDigits(rest)
	rest = rest[len(minor):]

	majorNum, minorNum := atoi(string(major)), atoi(minor)

	// Versions before 3.90 predate the extended header. A 3.90 prerelease that
	// still carries a long version string is one of them too, and the open
	// parenthes in the reserved field is how that shows up.
	if majorNum < 3 || (majorNum == 3 && minorNum < 90) ||
		(majorNum == 3 && minorNum == 90 && rest[len(rest)-tailLen] == '(') {
		flag := strings.TrimRight(string(bytes.Trim(rest, "\x00")), " \x00")
		return string(major) + "." + string(minor) + flag, -1, true
	}

	flag := bytes.TrimRight(rest[:len(rest)-tailLen], "\x00")
	var patch, suffix string
	switch string(flag) {
	case "a":
		suffix = " (alpha)"
	case "b":
		suffix = " (beta)"
	case "r":
		patch = ".1+"
	case " ":
		patch = ".0+"
		if majorNum == 3 && minorNum > 96 {
			patch = ".0"
		}
	case "", ".":
		patch = ".0+"
	default:
		suffix = " (?)"
	}
	// The extended header starts where the reserved field does, so its offset
	// does not depend on how the version string was spelled.
	return string(major) + "." + string(minor) + patch + suffix, fieldLen - tailLen, true
}

// leadingDigits returns the run of digits at the start of data.
func leadingDigits(data []byte) string {
	i := 0
	for ; i < len(data) && data[i] >= '0' && data[i] <= '9'; i++ {
	}
	return string(data[:i])
}

// atoi parses a string of digits, returning -1 when it is not one.
func atoi(s string) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return -1
}

// lameLen is the size of the LAME extended header, which describes how the
// encoder produced the stream. See http://gabriel.mp3-tech.org/mp3infotag.html.
const lameLen = 27

// LAMEHeader is the extended header LAME appends to a Xing header.
type LAMEHeader struct {
	// Version is the encoder version string, such as "3.99.1+".
	Version string
	// Method says how LAME chose the bitrate: 1 for CBR, 2 for ABR, 3 to 6 for
	// the flavours of VBR, and 0 when it does not say.
	Method int
	// Bitrate is the target bitrate in kbit/s for CBR and ABR, and the lowest
	// bitrate used for VBR.
	Bitrate int
	// Preset is the LAME preset, or 0 when the encoder was given none.
	Preset int
	// Quality is the -V quality, 0 to 9, derived from the 0 to 100 index in the
	// Xing header. This is the setting that matters most for a VBR file.
	Quality int
	// FineQuality is the second digit of that index, which LAME's own preset
	// guesses need.
	FineQuality int
	// LowPass is the low pass filter frequency in Hz, 0 when unknown.
	LowPass int
	// EncodingFlags is the raw flag byte, which LAME 3.90 to 3.92 used to mark
	// an average bitrate preset.
	EncodingFlags int
	// ATHType is the amplitude thresholding type the encoder chose.
	ATHType int
	// TrackPeak is the highest sample amplitude of the stream, 1.0 being the
	// maximum.
	TrackPeak float64
	// TrackGain and AlbumGain are the ReplayGain adjustments in dB, nil when
	// the encoder did not write them.
	TrackGain *float64
	AlbumGain *float64
	// Delay and Padding are the samples LAME added at the start and end of the
	// stream to align it with the encoding delay, which are not part of the
	// audio.
	Delay   int
	Padding int
}

// parseLAME decodes the extended header at the start of data. version is the
// version string that preceded it and scale the 0 to 100 quality index from the
// Xing header, which is the same setting written in a different form.
//
// nil is returned when data is too short to hold a header.
func parseLAME(data []byte, version string, scale int) *LAMEHeader {
	if len(data) < lameLen {
		return nil
	}
	r := bits.New(data)

	if revision := r.Read(4); revision != 0 {
		return nil
	}
	l := &LAMEHeader{
		Version: version,
		Method:  int(r.Read(4)),
		LowPass: int(r.Read(8)) * 100,
	}
	// The Xing index counts the other way round: 100 is the best encoding,
	// which is what -V 0 means.
	if scale >= 0 {
		l.Quality = (100 - scale) / 10
		l.FineQuality = (100 - scale) % 10
	}

	// The peak amplitude is a plain 32 bit fraction rather than a packed field.
	if peak := binary.BigEndian.Uint32(data[r.Pos()/8:]); peak != 0 {
		l.TrackPeak = float64(peak) / (1 << 23)
	}
	r.Skip(32)

	l.TrackGain = gain(r, 1)
	l.AlbumGain = gain(r, 2)
	l.EncodingFlags = int(r.Read(4))
	l.ATHType = int(r.Read(4))
	l.Bitrate = r.Uint8()
	l.Delay = int(r.Read(12))
	l.Padding = int(r.Read(12))

	// Source sample rate, unwise settings flag, stereo mode, noise shaping.
	r.Skip(2 + 1 + 3 + 2)
	r.Skip(1 + 7) // MP3 gain
	r.Skip(2 + 3) // reserved and surround info
	l.Preset = int(r.Read(11))
	return l
}

// gain reads one of the ReplayGain fields: a three bit type, a three bit
// origin, a sign and a nine bit adjustment in tenths of a decibel. want is the
// type that marks the field as meaningful for this gain, 1 for the track gain
// and 2 for the album gain; the other type only holds a value for the other
// gain, so it is discarded.
func gain(r *bits.Reader, want uint32) *float64 {
	kind := r.Read(3)
	r.Read(3) // origin
	negative := r.Read(1) == 1
	value := float64(r.Read(9)) / 10
	if kind != want {
		return nil
	}
	if negative {
		value = -value
	}
	return &value
}

// Settings guesses the LAME command line that produced the stream, such as
// "-V 2" or "--preset standard". The guess is only reliable for files encoded
// with the common presets and rate options, and is empty when the header holds
// nothing to go on.
func (l *LAMEHeader) Settings() string {
	version := l.majorMinor()

	switch l.Method {
	case 2: // average bitrate
		if version.atLeast(3, 90) && version.below(3, 93) && l.EncodingFlags != 0 {
			// LAME 3.90 to 3.92 called the ABR presets "alt-preset".
			return fmt.Sprintf("--alt-preset %s", atLeast(l.Bitrate))
		}
		if l.Preset != 0 {
			return fmt.Sprintf("--preset %d", l.Preset)
		}
		return fmt.Sprintf("--abr %s", atLeast(l.Bitrate))

	case 1: // constant bitrate
		switch l.Preset {
		case 0:
			return fmt.Sprintf("-b %s", atLeast(l.Bitrate))
		case 1003:
			return "--preset insane"
		default:
			return fmt.Sprintf("-b %d", l.Preset)
		}

	case 3: // variable bitrate, the old code path
		// LAME replaced its VBR implementation in 3.98 and kept the old one
		// reachable behind a flag, so the flag is only worth passing from then
		// on.
		if version.atLeast(3, 98) {
			return fmt.Sprintf("-V %d --vbr-old", l.Quality)
		}
		return fmt.Sprintf("-V %d", l.Quality)

	case 4, 5: // variable bitrate, the new code path
		// From 3.98 on this is the default, so LAME only writes it when asked.
		if version.atLeast(3, 98) {
			return fmt.Sprintf("-V %d", l.vbrQuality())
		}
		return fmt.Sprintf("-V %d --vbr-new", l.Quality)

	default:
		// LAME 3.93 to 3.97 encoded named presets as a fixed VBR method, so the
		// preset number is the only thing that says how it was encoded.
		if version.atLeast(3, 93) && version.below(3, 98) {
			return namedPreset(l.Preset)
		}
		return ""
	}
}

// namedPreset maps the LAME 3.93 to 3.97 preset numbers onto the option that
// chose them.
func namedPreset(preset int) string {
	switch preset {
	case 1001:
		return "--preset standard"
	case 1002:
		return "--preset extreme"
	case 1004:
		return "--preset fast standard"
	case 1005:
		return "--preset fast extreme"
	case 1006:
		return "--preset medium"
	case 1007:
		return "--preset fast medium"
	default:
		return ""
	}
}

// lameVersion is a LAME major and minor version number.
type lameVersion struct {
	major, minor int
}

// atLeast reports whether the version is major.minor or newer.
func (v lameVersion) atLeast(major, minor int) bool {
	if v.major != major {
		return v.major > major
	}
	return v.minor >= minor
}

// below reports whether the version is older than major.minor.
func (v lameVersion) below(major, minor int) bool { return !v.atLeast(major, minor) }

// majorMinor parses the version out of the [LAMEHeader.Version] string, which
// looks like "3.99.1+" and may carry a suffix such as " (beta)".
func (l *LAMEHeader) majorMinor() lameVersion {
	major, rest, _ := strings.Cut(l.Version, ".")
	minor, _, _ := strings.Cut(rest, ".")
	return lameVersion{major: atoi(major), minor: leadingInt(minor)}
}

// leadingInt parses the digits at the start of s, stopping at the first
// character that is not one.
func leadingInt(s string) int {
	n := 0
	for i := 0; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
		n = n*10 + int(s[i]-'0')
	}
	return n
}

// vbrQuality applies the corrections LAME's own encoder needed after the fact.
// See https://sourceforge.net/p/lame/bugs/455/.
func (l *LAMEHeader) vbrQuality() int {
	switch [3]int{l.Quality, l.Bitrate, l.LowPass} {
	case [3]int{5, 32, 0}:
		return 7
	case [3]int{5, 8, 0}:
		return 8
	case [3]int{6, 8, 0}:
		return 9
	default:
		return l.Quality
	}
}

// atLeast renders a bitrate, where 255 stands for the highest rate available.
func atLeast(bitrate int) string {
	if bitrate >= 255 {
		return "255+"
	}
	return strconv.Itoa(bitrate)
}

// readAt reads up to n bytes at offset without disturbing the reader's
// position beyond leaving it somewhere usable.
func readAt(r io.ReadSeeker, offset int64, n int) ([]byte, error) {
	if _, err := r.Seek(offset, io.SeekStart); err != nil {
		return nil, fmt.Errorf("mp3: seeking: %w", err)
	}
	buf := make([]byte, n)
	got, err := io.ReadFull(r, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, fmt.Errorf("mp3: reading: %w", err)
	}
	return buf[:got], nil
}
