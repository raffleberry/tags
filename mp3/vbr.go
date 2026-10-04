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

// Kind identifies the type of variable bitrate header.
type Kind string

// KindXing, KindInfo and KindVBRI are the variable bitrate header kinds.
// Info is the CBR variant of Xing.
const (
	KindXing Kind = "Xing"
	KindInfo Kind = "Info"
	KindVBRI Kind = "VBRI"
)

// VBRHeader is the Xing, Info or VBRI header in the first audio frame. It
// describes the whole stream. It holds more data than the frame header.
type VBRHeader struct {
	// Kind is the header type.
	Kind Kind
	// Frames is the audio frame count, or -1 when the header omits it.
	Frames int64
	// Bytes is the audio byte count, or -1 when the header omits it. The count
	// includes the frame that holds the header.
	Bytes int64
	// Scale is the 0 to 100 quality index, or -1 when the header omits it.
	// Higher values mean higher quality.
	Scale int
	// LAME describes the encoder. It is nil for non-LAME streams.
	LAME *LAMEHeader
}

// bitrateMode returns CBR, VBR or ABR from the header. It uses the header
// kind when LAME data is absent.
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
	// A VBRI header marks a variable bitrate file. An Info header marks a
	// constant bitrate file.
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

// encoder returns the name of the tool that wrote the header.
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

// xingFrames, xingBytes, xingTOC and xingQuality are Xing header flags. They
// list present optional fields. xingHeaderLen and vbriHeaderLen are header
// sizes.
const (
	xingFrames    = 0x1
	xingBytes     = 0x2
	xingTOC       = 0x4
	xingQuality   = 0x8
	xingHeaderLen = 8
	vbriHeaderLen = 26
)

// vbrLookahead is the byte count read around a candidate variable bitrate
// header. A full Xing header with LAME version string and extended header
// needs under 150 bytes.
const vbrLookahead = 192

// readVBRHeader returns the variable bitrate header in the layer 3 frame at
// offset. It returns nil when the frame has none. It never returns an error.
// A false header match is treated as constant bitrate.
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

// xingOffset returns the Xing header position in a layer 3 frame. The offset
// depends on version and channel mode. It skips side information and channel
// data before the header.
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
		// Seek table. It is not needed for reading.
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
	// Bytes are at offset 10. Frames are at offset 14.
	return &VBRHeader{
		Kind:   KindVBRI,
		Frames: int64(binary.BigEndian.Uint32(data[14:18])),
		Bytes:  int64(binary.BigEndian.Uint32(data[10:14])),
		Scale:  -1,
	}
}

// parseLAMEVersion decodes the LAME version string at the end of a Xing
// header, such as "LAME3.99.1". It returns the version as written. It returns
// the offset of the extended header relative to the start of data. ok is false
// when data holds no LAME version string.
//
// Versions before 3.90 lack the extended header. A 3.90 prerelease with a long
// version string also lacks it.
func parseLAMEVersion(data []byte) (version string, extAt int, ok bool) {
	// The version string uses a fixed 20 byte field. The first 9 bytes hold
	// the string. The last 11 bytes are reserved. Reading past the field
	// misreads audio as flags.
	const fieldLen = 20
	const tailLen = 11
	if len(data) < fieldLen {
		return "", 0, false
	}
	field := data[:fieldLen]
	if !bytes.HasPrefix(field, []byte("LAME")) && !bytes.HasPrefix(field, []byte("L3.99")) {
		return "", 0, false
	}

	// The prefix has multiple spellings. It is skipped by character class,
	// not by length.
	rest := bytes.TrimLeft(field, "EMAL")
	major, rest := rest[:1], bytes.TrimLeft(rest[1:], ".")
	minor := leadingDigits(rest)
	rest = rest[len(minor):]

	majorNum, minorNum := atoi(string(major)), atoi(minor)

	// Versions before 3.90 lack the extended header. A 3.90 prerelease with a
	// long version string also lacks it. An open parenthesis in the reserved
	// field marks this case.
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
	// The extended header starts at the reserved field. Its offset does not
	// depend on version string spelling.
	return string(major) + "." + string(minor) + patch + suffix, fieldLen - tailLen, true
}

// leadingDigits returns digits at the start of data.
func leadingDigits(data []byte) string {
	i := 0
	for ; i < len(data) && data[i] >= '0' && data[i] <= '9'; i++ {
	}
	return string(data[:i])
}

// atoi parses a digit string. It returns -1 for other input.
func atoi(s string) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return -1
}

// lameLen is the size of the LAME extended header. The header describes
// encoder settings. See http://gabriel.mp3-tech.org/mp3infotag.html.
const lameLen = 27

// LAMEHeader is the extended header LAME appends to a Xing header.
type LAMEHeader struct {
	// Version is the encoder version string, such as "3.99.1+".
	Version string
	// Method is the bitrate selection method. 1 is CBR. 2 is ABR. 3 to 6 are
	// VBR modes. 0 means unspecified.
	Method int
	// Bitrate is the target bitrate in kbit/s for CBR and ABR. For VBR it is
	// the lowest bitrate used.
	Bitrate int
	// Preset is the LAME preset, or 0 when no preset was used.
	Preset int
	// Quality is the -V quality, 0 to 9. It is derived from the 0 to 100 index
	// in the Xing header.
	Quality int
	// FineQuality is the second digit of that index. It identifies presets.
	FineQuality int
	// LowPass is the lowpass filter frequency in Hz, 0 when unknown.
	LowPass int
	// EncodingFlags is the raw flag byte. LAME 3.90 to 3.92 used it to mark
	// an average bitrate preset.
	EncodingFlags int
	// ATHType is the ATH type the encoder used.
	ATHType int
	// TrackPeak is the peak sample amplitude of the stream. 1.0 is the
	// maximum.
	TrackPeak float64
	// TrackGain and AlbumGain are ReplayGain adjustments in dB. They are nil
	// when the encoder did not write them.
	TrackGain *float64
	AlbumGain *float64
	// Delay and Padding are samples LAME added at the start and end of the
	// stream. They are not part of the audio.
	Delay   int
	Padding int
}

// parseLAME decodes the extended header at the start of data. version is the
// prior version string. scale is the 0 to 100 quality index from the Xing
// header. It is the same setting in a different form.
//
// parseLAME returns nil when data is too short to hold a header.
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
	// The Xing index is inverted relative to -V values. Index 100 maps to
	// -V 0.
	if scale >= 0 {
		l.Quality = (100 - scale) / 10
		l.FineQuality = (100 - scale) % 10
	}

	// The peak amplitude is a 32 bit fraction, not a packed field.
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

	// Skip source sample rate, settings flag, stereo mode and noise shaping.
	r.Skip(2 + 1 + 3 + 2)
	// Skip MP3 gain.
	r.Skip(1 + 7)
	// Skip reserved data and surround info.
	r.Skip(2 + 3)
	l.Preset = int(r.Read(11))
	return l
}

// gain reads one ReplayGain field. The field holds a three bit type, a three
// bit origin, a sign bit and a nine bit adjustment in tenths of a decibel.
// want marks the field as valid for this gain. 1 is track gain. 2 is album
// gain. Other types return nil.
func gain(r *bits.Reader, want uint32) *float64 {
	kind := r.Read(3)
	r.Read(3) // origin bits
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

// Settings returns the estimated LAME command line for the stream, such as
// "-V 2" or "--preset standard". The result is valid for common presets and
// rate options. It is empty when the header lacks data.
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

	case 3: // variable bitrate, old code path
		// LAME replaced its VBR implementation in 3.98. The old implementation
		// stayed available with a flag. The flag is used only from 3.98 on.
		if version.atLeast(3, 98) {
			return fmt.Sprintf("-V %d --vbr-old", l.Quality)
		}
		return fmt.Sprintf("-V %d", l.Quality)

	case 4, 5: // variable bitrate, new code path
		// From 3.98 on the new path is the default. No flag is needed.
		if version.atLeast(3, 98) {
			return fmt.Sprintf("-V %d", l.vbrQuality())
		}
		return fmt.Sprintf("-V %d --vbr-new", l.Quality)

	default:
		// LAME 3.93 to 3.97 used a fixed VBR method for named presets. The
		// preset number identifies the encoding.
		if version.atLeast(3, 93) && version.below(3, 98) {
			return namedPreset(l.Preset)
		}
		return ""
	}
}

// namedPreset maps LAME 3.93 to 3.97 preset numbers to the option that chose
// them.
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

// lameVersion is a LAME major and minor version.
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

// majorMinor parses the version from the [LAMEHeader.Version] string. The
// string has form "3.99.1+" and may include a suffix such as " (beta)".
func (l *LAMEHeader) majorMinor() lameVersion {
	major, rest, _ := strings.Cut(l.Version, ".")
	minor, _, _ := strings.Cut(rest, ".")
	return lameVersion{major: atoi(major), minor: leadingInt(minor)}
}

// leadingInt parses leading digits of s. It stops at the first non-digit.
func leadingInt(s string) int {
	n := 0
	for i := 0; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
		n = n*10 + int(s[i]-'0')
	}
	return n
}

// vbrQuality corrects quality values for known LAME encoding errors.
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

// atLeast formats a bitrate. 255 means 255 or more.
func atLeast(bitrate int) string {
	if bitrate >= 255 {
		return "255+"
	}
	return strconv.Itoa(bitrate)
}

// readAt reads up to n bytes at offset. It does not restore the reader
// position.
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
