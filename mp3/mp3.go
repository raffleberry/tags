// Package mp3 reads MPEG audio files: the audio stream properties of the MPEG
// frame headers, the Xing, Info and VBRI headers that variable bitrate
// encoders write inside the first frame, and the ID3 tags that surround them.
//
// The MPEG layer is not checked, so the package reads MP1, MP2 and MP3 streams,
// which is what an .mp3 file usually holds.
package mp3

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"github.com/raffleberry/tags/id3"
	"github.com/raffleberry/tags/tag"
)

// Errors reported while reading an MPEG audio file.
var (
	// ErrNoFrame means no MPEG frame header could be found in the audio data.
	ErrNoFrame = errors.New("mp3: no MPEG frame header found")
	// ErrShortFile means the file is too small to hold any audio at all.
	ErrShortFile = errors.New("mp3: file is too short")
)

// fileHeaderLen is the size of an MPEG frame header, and of the magic ID3v2
// identifier that may precede the audio.
const fileHeaderLen = 4

// MPEGVersion identifies an MPEG audio version.
type MPEGVersion float64

// The MPEG audio versions. Version 2.5 is a reduced version 2.
const (
	MPEG1   MPEGVersion = 1
	MPEG2   MPEGVersion = 2
	MPEG2_5 MPEGVersion = 2.5
)

// String returns "MPEG 1", "MPEG 2" or "MPEG 2.5".
func (v MPEGVersion) String() string {
	switch v {
	case MPEG1:
		return "MPEG 1"
	case MPEG2:
		return "MPEG 2"
	case MPEG2_5:
		return "MPEG 2.5"
	default:
		return fmt.Sprintf("MPEG %g", float64(v))
	}
}

// Mode is how the two channels of a frame relate to each other.
type Mode int

// The channel modes of an MPEG frame.
const (
	// Stereo means the channels are independent.
	Stereo Mode = iota
	// JointStereo means the encoder shares data between the channels.
	JointStereo
	// DualChannel means two independent mono streams.
	DualChannel
	// Mono means a single channel.
	Mono
)

// String returns the name of the mode.
func (m Mode) String() string {
	switch m {
	case Stereo:
		return "stereo"
	case JointStereo:
		return "joint stereo"
	case DualChannel:
		return "dual channel"
	case Mono:
		return "mono"
	default:
		return fmt.Sprintf("unknown mode %d", int(m))
	}
}

// Stream holds what could be learned about the MPEG audio stream itself.
type Stream struct {
	tag.Audio

	// Version of the MPEG audio the frames are encoded in.
	Version MPEGVersion
	// Layer of the encoding, 1, 2 or 3.
	Layer int
	// Mode says how the two channels relate.
	Mode Mode
	// Codec names the encoding, such as "MPEG-1 Layer 3".
	Codec string
	// Sketchy reports that no run of consecutive frames could be confirmed, so
	// the properties were read from a single frame and may be wrong. Files
	// that begin with something other than audio, such as an ID3v2 tag
	// followed by padding, are the usual cause.
	Sketchy bool
	// VBRHeader describes the Xing, Info or VBRI header found in the first
	// frame, or nil when the stream has none and is assumed to be constant
	// bitrate.
	VBRHeader *VBRHeader
}

// File is an MPEG audio file: its stream properties, its tags and its artwork.
type File struct {
	stream   Stream
	tags     tag.Tag
	pictures []tag.Picture
	// v1 is the trailing ID3v1 tag, when the file has one.
	v1 *id3.V1
}

// Format reports that an MPEG audio file was read from.
func (f *File) Format() tag.Format { return tag.MP3 }

// Tags returns the normalized metadata fields. The result must not be
// modified.
func (f *File) Tags() tag.Tag { return f.tags }

// Audio returns the properties of the audio stream.
func (f *File) Audio() tag.Audio { return f.stream.Audio }

// Pictures returns the embedded artwork.
func (f *File) Pictures() []tag.Picture { return f.pictures }

// Stream returns the MPEG specific details, such as the layer and channel mode.
func (f *File) Stream() Stream { return f.stream }

// Matches reports whether header, which should hold at least the first
// [fileHeaderLen] bytes of a file, looks like an MPEG audio file. An ID3v2
// identifier also matches, since that is what most MP3 files start with.
func Matches(header []byte) bool {
	return HasMagic(header)
}

// Open reads the MPEG audio file at path.
func Open(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Read(f)
}

// Read reads an MPEG audio file from r, which must be seekable.
func Read(r io.ReadSeeker) (*File, error) {
	size, err := sizeOf(r)
	if err != nil {
		return nil, err
	}
	if size < fileHeaderLen {
		return nil, ErrShortFile
	}

	// An MPEG file starts with an ID3v2 tag or a frame sync. Checking that first
	// keeps a file of some other format, whose bytes may hold enough 0xFF to
	// look like a frame, from being read as audio.
	header, err := readAt(r, 0, fileHeaderLen)
	if err != nil {
		return nil, err
	}
	if !HasMagic(header) {
		return nil, fmt.Errorf("%w: the file does not start with an ID3 tag or a frame", ErrNoFrame)
	}

	f := &File{}

	tags, pictures, v1, err := readTags(r, size)
	if err != nil {
		return nil, err
	}
	f.tags, f.pictures, f.v1 = tags, pictures, v1

	stream, err := readStream(r, size)
	if err != nil {
		return nil, err
	}
	f.stream = stream
	return f, nil
}

// V1 returns the trailing ID3v1 tag, or nil when the file has none. Use it when
// the ID3v2 tags are missing or incomplete, since a file can carry both and
// they do not always agree.
func (f *File) V1() *id3.V1 { return f.v1 }

// readTags reads the tags of an MPEG audio file: the ID3v2 tag at the start
// and the ID3v1 tag at the end, either of which may be absent.
//
// The two are merged with the ID3v2 winning, since it holds strictly more
// information.
func readTags(r io.ReadSeeker, size int64) (tag.Tag, []tag.Picture, *id3.V1, error) {
	t := tag.Tag{}
	var pictures []tag.Picture

	if v2, err := readID3v2(r); err == nil {
		t = v2.Common()
		pictures = v2.Pictures()
	} else if !errors.Is(err, id3.ErrNoTag) {
		return nil, nil, nil, err
	}

	v1, err := readID3v1(r, size)
	if err != nil && !errors.Is(err, id3.ErrNoV1) {
		return nil, nil, nil, err
	}
	if v1 != nil {
		mergeV1(t, v1)
	}
	return t, pictures, v1, nil
}

// readID3v2 reads the ID3v2 tag at the start of r, returning [id3.ErrNoTag]
// when there is none.
func readID3v2(r io.ReadSeeker) (*id3.Tag, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("mp3: seeking: %w", err)
	}
	return id3.Read(r)
}

// readID3v1 reads the ID3v1 tag in the last 128 bytes of the file, returning
// [id3.ErrNoV1] when there is none.
func readID3v1(r io.ReadSeeker, size int64) (*id3.V1, error) {
	if size < id3V1Size {
		return nil, id3.ErrNoV1
	}
	if _, err := r.Seek(size-id3V1Size, io.SeekStart); err != nil {
		return nil, fmt.Errorf("mp3: seeking: %w", err)
	}
	return id3.ReadV1(r)
}

// id3V1Size is the length of an ID3v1 tag, and how far from the end of the file
// it sits.
const id3V1Size = 128

// v1CommentKey holds the comment of an ID3v1 tag in a file that also has an
// ID3v2 comment. The two are separate pieces of text and both are usually
// wanted, so the older one is filed under its own key rather than dropped.
const v1CommentKey = "comment:id3v1 comment"

// mergeV1 adds the fields of an ID3v1 tag that the ID3v2 tag left out. ID3v1
// holds one value per field and predates multi valued tags, so a field that v2
// already filled in keeps the v2 value.
func mergeV1(t tag.Tag, v1 *id3.V1) {
	for key, values := range v1.Common() {
		if key != tag.Comment {
			if len(values) > 0 {
				t.SetDefault(key, values[0])
			}
			continue
		}
		// The v2 tag has a comment of its own only when it has a COMM frame, so
		// an empty key here means the v1 comment is the only one there is.
		if v1.Comment != "" && t.Value(tag.Comment) == "" {
			t.Set(tag.Comment, v1.Comment)
		} else if v1.Comment != "" {
			t.SetDefault(v1CommentKey, v1.Comment)
		}
	}
}

// sizeOf returns the number of bytes left in r.
func sizeOf(r io.ReadSeeker) (int64, error) {
	cur, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, fmt.Errorf("mp3: seeking: %w", err)
	}
	end, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, fmt.Errorf("mp3: seeking: %w", err)
	}
	if _, err := r.Seek(cur, io.SeekStart); err != nil {
		return 0, fmt.Errorf("mp3: seeking: %w", err)
	}
	return end - cur, nil
}

// streamVersion is the key of the bitrate and frame size tables: an MPEG
// version with a version 2.5 folded onto version 2, and a version 2 layer 3
// folded onto layer 2, since those rows of both tables are identical.
type streamVersion struct {
	version MPEGVersion
	layer   int
}

// frameBitrates lists the frame bitrate in kbit/s for each version and layer,
// indexed by the four bit bitrate field of a frame header. Indices 0 and 15
// mean "free format" and "invalid".
var frameBitrates = map[streamVersion][]int{
	{1, 1}: {0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448},
	{1, 2}: {0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384},
	{1, 3}: {0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320},
	{2, 1}: {0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256},
	{2, 2}: {0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160},
}

// sampleRates lists the sample rate in Hz for each version, indexed by the two
// bit sample rate field of a frame header.
var sampleRates = map[MPEGVersion][]int{
	MPEG1:   {44100, 48000, 32000},
	MPEG2:   {22050, 24000, 16000},
	MPEG2_5: {11025, 12000, 8000},
}

// tableKey folds a version and layer onto the row of the shared tables.
// tableKey folds a version and layer onto the row of the shared tables.
func tableKey(v MPEGVersion, layer int) streamVersion {
	if v == MPEG2_5 {
		v = MPEG2
	}
	if v == MPEG2 && layer == 3 {
		layer = 2
	}
	return streamVersion{version: v, layer: layer}
}

// frameSamples returns how many audio samples one frame decodes to, and how many
// bytes of frame each sample takes. Layer 1 and the reduced versions differ from
// the common case, which is why this is not a constant.
func frameSamples(v MPEGVersion, layer int) (samples, slot int) {
	switch {
	case layer == 1:
		return 384, 4
	case v != MPEG1 && layer == 3:
		return 576, 1
	default:
		return 1152, 1
	}
}

// frameHeader is one parsed MPEG audio frame header.
type frameHeader struct {
	// offset is where the header starts in the file.
	offset int64
	// version, layer and mode come straight out of the header.
	version MPEGVersion
	layer   int
	mode    Mode
	// bitrate in bit/s, and sampleRate in Hz, both from the header.
	bitrate    int
	sampleRate int
	// length is the total size of the frame in bytes, header included.
	length int64
	// samples is how many audio samples the frame decodes to.
	samples int
	// vbr is the Xing, Info or VBRI header found inside the frame, if any.
	vbr *VBRHeader
}

// parseFrameHeader decodes the frame header at the reader's position. The
// reader is left just after the four header bytes.
//
// Every reserved combination is rejected rather than guessed at, so that the
// 0xFF bytes inside a picture or an ID3 tag are not mistaken for audio.
func parseFrameHeader(r io.Reader) (frameHeader, error) {
	var buf [fileHeaderLen]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return frameHeader{}, fmt.Errorf("%w: reading frame header", ErrNoFrame)
	}
	b := buf[:]
	if b[0] != 0xFF || b[1]&0xE0 != 0xE0 {
		return frameHeader{}, ErrNoFrame
	}

	// Bit positions are counted from the start of the second byte, which is
	// where the fields after the eleven bit sync begin.
	versionBits := (b[1] >> 3) & 0x03 // 01 is MPEG1, 10 is MPEG2, 00 is 2.5
	layerBits := (b[1] >> 1) & 0x03   // 01 is layer 3, 10 is layer 2, 11 is layer 1
	bitrateIdx := b[2] >> 4
	rateIdx := (b[2] >> 2) & 0x03
	padding := b[2]&0x02 != 0
	mode := Mode((b[3] >> 6) & 0x03)

	// Version 01 is reserved, layer 00 is reserved, and a sample rate or
	// bitrate index of 15 is also reserved.
	if versionBits == 1 || layerBits == 0 || rateIdx == 3 || bitrateIdx == 15 {
		return frameHeader{}, ErrNoFrame
	}
	// A bitrate index of 0 means free format, which this package does not
	// decode: without a bitrate the frame length is not known.
	if bitrateIdx == 0 {
		return frameHeader{}, ErrNoFrame
	}

	h := frameHeader{
		// The version field counts 00 as 2.5, 10 as 2 and 11 as 1. Index 1 is
		// reserved and was already rejected above.
		version: [4]MPEGVersion{MPEG2_5, 0, MPEG2, MPEG1}[versionBits],
		// The layer field counts 01 as layer 3, 10 as layer 2 and 11 as layer 1.
		layer: 4 - int(layerBits),
		mode:  mode,
	}
	bitrates, ok := frameBitrates[tableKey(h.version, h.layer)]
	if !ok || int(bitrateIdx) >= len(bitrates) {
		return frameHeader{}, ErrNoFrame
	}
	h.bitrate = bitrates[bitrateIdx] * 1000

	rates, ok := sampleRates[h.version]
	if !ok || int(rateIdx) >= len(rates) {
		return frameHeader{}, ErrNoFrame
	}
	h.sampleRate = rates[rateIdx]

	// Samples per frame, and how many bytes of frame each sample occupies.
	samples, slot := frameSamples(h.version, h.layer)
	h.samples = samples
	h.length = int64((samples/8*h.bitrate)/h.sampleRate+boolToInt(padding)) * int64(slot)
	return h, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Codec returns the name of the encoding, such as "MPEG-1 Layer 3".
func (h frameHeader) codecName() string {
	return fmt.Sprintf("%s Layer %d", h.version, h.layer)
}

// readStream finds the first audio frame and works out what it says about the
// stream, preferring a variable bitrate header over the frame header alone.
func readStream(r io.ReadSeeker, size int64) (Stream, error) {
	first, sketchy, err := findFirstFrame(r, size)
	if err != nil {
		return Stream{}, err
	}

	stream := Stream{
		Audio: tag.Audio{
			Duration:   time.Duration(0),
			Bitrate:    first.bitrate,
			SampleRate: first.sampleRate,
			Channels:   channelsOf(first.mode),
		},
		Version: first.version,
		Layer:   first.layer,
		Mode:    first.mode,
		Codec:   first.codecName(),
		Sketchy: sketchy,
	}
	applyVBR(&stream, first, size)
	return stream, nil
}

// channelsOf returns how many channels a mode decodes to.
func channelsOf(m Mode) int {
	if m == Mono {
		return 1
	}
	return 2
}

// applyVBR fills in duration, bitrate, encoder and bitrate mode from a variable
// bitrate header, falling back to estimating the duration from the file size
// when the header is missing or incomplete.
func applyVBR(stream *Stream, first frameHeader, size int64) {
	vbr := first.vbr
	stream.BitrateMode = tag.BitrateUnknown

	if vbr == nil {
		stream.Duration = estimateDuration(size-first.offset, int64(first.bitrate))
		return
	}
	stream.VBRHeader = vbr
	stream.BitrateMode = vbr.bitrateMode()
	stream.Encoder = vbr.encoder()

	if vbr.Frames < 0 {
		stream.Duration = estimateDuration(size-first.offset, int64(first.bitrate))
		return
	}

	if vbr.Kind == KindVBRI {
		// A VBRI header counts every frame, including the one it sits in, and
		// states its own duration, so both are used as written.
		samples := float64(first.samples) * float64(vbr.Frames)
		seconds := samples / float64(first.sampleRate)
		if seconds > 0 {
			stream.Bitrate = int(float64(vbr.Bytes) * 8 / seconds)
			stream.Duration = time.Duration(round(seconds * float64(time.Second)))
		}
		return
	}

	// A Xing header counts only the frames that follow the one holding it,
	// while its byte count includes that frame, so the frame's own length is
	// dropped to make the two agree.
	audioBytes := max(0, vbr.Bytes-first.length)
	samples := int64(first.samples) * vbr.Frames
	if audioBytes > 0 && samples > 0 {
		stream.Bitrate = round(float64(audioBytes) * 8 * float64(first.sampleRate) / float64(samples))
	}

	if vbr.LAME != nil {
		// LAME reports the encoder delay and padding it applied, which are not
		// part of the audio a listener hears. This is after the bitrate, which
		// is a property of what was encoded.
		samples -= int64(vbr.LAME.Delay)
		samples -= int64(vbr.LAME.Padding)
	}
	// Very short files encoded by old LAME versions carry a delay larger than
	// the file itself.
	samples = max(0, samples)
	stream.Duration = time.Duration(round(float64(samples) / float64(first.sampleRate) * float64(time.Second)))
}

// estimateDuration guesses how long audio of the given size plays at the given
// bitrate.
func estimateDuration(size, bitrate int64) time.Duration {
	if bitrate <= 0 || size <= 0 {
		return 0
	}
	seconds := float64(size) * 8 / float64(bitrate)
	return time.Duration(round(seconds * float64(time.Second)))
}

// skipID3 moves the reader past any ID3v2 tags at its position. Windows Media
// Player writes several in a row, so this steps over as many as it finds.
func skipID3(r io.ReadSeeker) error {
	for {
		cur, err := r.Seek(0, io.SeekCurrent)
		if err != nil {
			return fmt.Errorf("mp3: seeking: %w", err)
		}
		var buf [10]byte
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			// Too little room left for another header, so there is no audio
			// left either. Leave the reader where it was.
			_, seekErr := r.Seek(cur, io.SeekStart)
			return ignoreEOF(seekErr)
		}

		h, err := id3.ReadHeader(bytes.NewReader(buf[:]))
		if err != nil {
			if _, seekErr := r.Seek(cur, io.SeekStart); seekErr != nil {
				return ignoreEOF(seekErr)
			}
			return nil
		}
		// The header has already been read, so only the body is left to step
		// over.
		if _, err := r.Seek(int64(h.Body), io.SeekCurrent); err != nil {
			return fmt.Errorf("mp3: seeking: %w", err)
		}
	}
}

// findFirstFrame scans the audio data for a frame header, then looks for a run
// of frames that agree with each other. A single frame can be a false positive,
// so several consecutive ones are wanted before the stream is trusted.
//
// The second result reports whether only a weaker chain of frames was found, in
// which case the properties come from a single frame and may be wrong.
func findFirstFrame(r io.ReadSeeker, size int64) (frameHeader, bool, error) {
	const (
		// Nothing useful lives in the first megabyte of padding, and a search
		// that far is plenty.
		maxSearch = 1024 * 1024
		// Give up rather than spend a long time in a file of false syncs.
		maxSyncs = 1500
		// Consecutive frames needed before the stream is not sketchy.
		wantFrames = 4
		// Frames that are enough to report, if no run of wantFrames turns up.
		anyFrames = 2
	)

	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return frameHeader{}, false, fmt.Errorf("mp3: seeking: %w", err)
	}
	if err := skipID3(r); err != nil {
		return frameHeader{}, false, err
	}

	var fallback frameHeader
	var haveFallback, sketchy bool
	syncs := 0

	err := scanSyncs(r, min(maxSearch, size), func(offset int64) bool {
		if syncs++; syncs > maxSyncs {
			return false
		}
		if _, err := r.Seek(offset, io.SeekStart); err != nil {
			return false
		}

		var frames []frameHeader
		next := offset
		for range wantFrames {
			h, err := readFrameHeader(r, next)
			if err != nil {
				break
			}
			frames = append(frames, h)
			if h.vbr != nil {
				// A variable bitrate header describes the whole stream, so one
				// frame carrying it settles the properties.
				break
			}
			next += h.length
		}

		if len(frames) == 0 {
			return true
		}
		last := frames[len(frames)-1]
		switch {
		case last.vbr != nil:
			fallback, haveFallback, sketchy = last, true, false
			return false
		case len(frames) >= wantFrames:
			fallback, haveFallback, sketchy = frames[0], true, false
			return false
		case len(frames) >= anyFrames && !haveFallback:
			fallback, haveFallback, sketchy = frames[0], true, true
		}
		return true
	})
	if err != nil {
		return frameHeader{}, false, err
	}
	if !haveFallback {
		return frameHeader{}, false, fmt.Errorf("%w: the file may not be MPEG audio", ErrNoFrame)
	}
	return fallback, sketchy, nil
}

// readFrameHeader parses the frame header at offset together with any variable
// bitrate header inside the frame.
func readFrameHeader(r io.ReadSeeker, offset int64) (frameHeader, error) {
	if _, err := r.Seek(offset, io.SeekStart); err != nil {
		return frameHeader{}, fmt.Errorf("mp3: seeking: %w", err)
	}
	h, err := parseFrameHeader(r)
	if err != nil {
		return frameHeader{}, err
	}
	h.offset = offset

	if h.layer == 3 {
		h.vbr = readVBRHeader(r, offset, h)
	}
	return h, nil
}

// scanSyncs calls fn for the offset of every candidate frame sync found in the
// first maxRead bytes of r. Reading stops early when fn returns false. The
// offsets come in increasing order.
//
// A candidate is a 0xFF byte followed by another byte whose top three bits are
// set, which is what every MPEG frame header starts with.
func scanSyncs(r io.ReadSeeker, maxRead int64, fn func(offset int64) bool) error {
	var read int64
	// Read in doubling chunks so a header at the very start is found after one
	// small read, while a long file is not walked one byte at a time.
	chunk := int64(2)
	var last byte

	for read < maxRead {
		chunkStart, err := r.Seek(0, io.SeekCurrent)
		if err != nil {
			return fmt.Errorf("mp3: seeking: %w", err)
		}
		data := make([]byte, min(chunk, maxRead-read))
		n, err := readFull(r, data)
		if n == 0 {
			return ignoreEOF(err)
		}
		read += int64(n)

		// The candidates in this chunk, in order. Collected first because the
		// callback reads the file itself and so moves the reader.
		var offsets []int64
		if last == 0xFF && data[0]&0xE0 == 0xE0 {
			// The last byte of the previous chunk may have been the 0xFF half of
			// a sync whose other half starts this one.
			offsets = append(offsets, chunkStart-1)
		}
		for i := 0; i+1 < n; i++ {
			if data[i] == 0xFF && data[i+1]&0xE0 == 0xE0 {
				offsets = append(offsets, chunkStart+int64(i))
			}
		}

		last = data[n-1]
		chunk *= 2

		for _, offset := range offsets {
			if !fn(offset) {
				return nil
			}
			// Resume reading where this chunk left off, wherever the callback
			// left the reader.
			if _, err := r.Seek(chunkStart+int64(n), io.SeekStart); err != nil {
				return fmt.Errorf("mp3: seeking: %w", err)
			}
		}
	}
	return nil
}

// readFull fills data, tolerating a short read at end of file.
func readFull(r io.Reader, data []byte) (int, error) {
	n, err := io.ReadFull(r, data)
	if err == io.ErrUnexpectedEOF || err == io.EOF {
		return n, nil
	}
	return n, err
}

func ignoreEOF(err error) error {
	if err == nil || err == io.EOF {
		return nil
	}
	return fmt.Errorf("mp3: reading: %w", err)
}

// HasMagic reports whether data starts with an ID3v2 identifier or an MPEG
// frame sync.
func HasMagic(data []byte) bool {
	if len(data) < fileHeaderLen {
		return false
	}
	if strings.HasPrefix(string(data), "ID3") {
		return true
	}
	return data[0] == 0xFF && data[1]&0xE0 == 0xE0
}

// readUint32BE is a small helper for the variable bitrate headers.
func readUint32BE(b []byte) uint32 { return binary.BigEndian.Uint32(b) }

// round rounds a float to the nearest integer, which is what the MPEG headers
// call for and what avoids a bias from truncating every value down.
func round(f float64) int { return int(math.Round(f)) }
