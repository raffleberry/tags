// Package mp3 reads MPEG audio files. It reads audio stream properties from
// MPEG frame headers. It reads Xing, Info and VBRI headers from the first
// frame. It reads surrounding ID3 tags.
//
// The MPEG layer is not checked. The package reads MP1, MP2 and MP3 streams.
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

	"github.com/raffleberry/tags/ape"
	"github.com/raffleberry/tags/id3"
	"github.com/raffleberry/tags/tag"
)

// ErrNoFrame and ErrShortFile are errors reported while reading an MPEG audio file.
var (
	// ErrNoFrame means no MPEG frame header was found in the audio data.
	ErrNoFrame = errors.New("mp3: no MPEG frame header found")
	// ErrShortFile means the file is too small to hold audio.
	ErrShortFile = errors.New("mp3: file is too short")
)

// fileHeaderLen is the size of an MPEG frame header. It is also the size of
// the ID3v2 identifier that may precede the audio.
const fileHeaderLen = 4

// MPEGVersion identifies an MPEG audio version.
type MPEGVersion float64

// MPEG1, MPEG2 and MPEG2_5 are the MPEG audio versions. Version 2.5 is
// reduced version 2.
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

// Stereo, JointStereo, DualChannel and Mono are the channel modes of an MPEG frame.
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

// Stream holds properties of the MPEG audio stream.
type Stream struct {
	tag.Audio

	// Version is the MPEG audio version of the frames.
	Version MPEGVersion
	// Layer is the encoding layer, 1, 2 or 3.
	Layer int
	// Mode describes how the two channels relate.
	Mode Mode
	// Codec names the encoding, such as "MPEG-1 Layer 3".
	Codec string
	// Sketchy is true when properties come from a single frame. No run of
	// consecutive frames was confirmed. The values may be wrong. Files
	// starting with non-audio data are the common cause.
	Sketchy bool
	// VBRHeader describes the Xing, Info or VBRI header in the first frame.
	// It is nil when the stream has none. Such streams are treated as
	// constant bitrate.
	VBRHeader *VBRHeader
}

// File is an MPEG audio file. It holds stream properties, tags and artwork.
type File struct {
	stream   Stream
	tags     tag.Tag
	pictures []tag.Picture
	// v2 is the leading ID3v2 tag, when the file has one. It is kept for
	// chapter and frame-level access.
	v2 *id3.Tag
	// v1 is the trailing ID3v1 tag, when the file has one.
	v1 *id3.V1
	// apeTag is the trailing APEv2 tag, when the file has one.
	apeTag *ape.Tag
}

// Format returns tag.MP3.
func (f *File) Format() tag.Format { return tag.MP3 }

// Tags returns the normalized metadata fields. The result must not be
// modified.
func (f *File) Tags() tag.Tag { return f.tags }

// Audio returns the properties of the audio stream.
func (f *File) Audio() tag.Audio { return f.stream.Audio }

// Pictures returns the embedded artwork.
func (f *File) Pictures() []tag.Picture { return f.pictures }

// Stream returns MPEG details, such as layer and channel mode.
func (f *File) Stream() Stream { return f.stream }

// Matches reports whether header looks like an MPEG audio file. header should
// hold at least the first [fileHeaderLen] bytes of a file. An ID3v2
// identifier also matches. Many MP3 files start with it.
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

	// An MPEG file starts with an ID3v2 tag or a frame sync. This check runs
	// first. It rejects other formats with 0xFF bytes that resemble a frame.
	header, err := readAt(r, 0, fileHeaderLen)
	if err != nil {
		return nil, err
	}
	if !HasMagic(header) {
		return nil, fmt.Errorf("%w: the file does not start with an ID3 tag or a frame", ErrNoFrame)
	}

	f := &File{}

	tags, pictures, v2, v1, at, err := readTags(r, size)
	if err != nil {
		return nil, err
	}
	f.tags, f.pictures, f.v2, f.v1, f.apeTag = tags, pictures, v2, v1, at

	stream, err := readStream(r, size)
	if err != nil {
		return nil, err
	}
	f.stream = stream
	return f, nil
}

// V1 returns the trailing ID3v1 tag, or nil when the file has none. A file
// can carry both ID3v1 and ID3v2. The two tags do not always agree.
func (f *File) V1() *id3.V1 { return f.v1 }

// APE returns the trailing APEv2 tag, or nil when the file has none. Some
// files store APEv2 with ID3 or without ID3. A file with no ID3v2 tag may
// still hold metadata here.
func (f *File) APE() *ape.Tag { return f.apeTag }

// ID3v2 returns the leading ID3v2 tag, or nil when the file has none. It
// provides frame-level access, such as chapters.
func (f *File) ID3v2() *id3.Tag { return f.v2 }

// Chapters returns the chapters of the ID3v2 tag, if any.
func (f *File) Chapters() []id3.Chapter {
	if f.v2 == nil {
		return nil
	}
	return f.v2.Chapters()
}

// Tables returns the tables of contents of the ID3v2 tag, if any.
func (f *File) Tables() []id3.TableOfContents {
	if f.v2 == nil {
		return nil
	}
	return f.v2.Tables()
}

// readTags reads the tags of an MPEG audio file: the ID3v2 tag at the start,
// the APEv2 tag at the end and the ID3v1 tag at the end. Any of them may be
// absent.
//
// ID3v2 values win. APEv2 fills remaining fields. ID3v1 fills what is still
// empty.
func readTags(r io.ReadSeeker, size int64) (tag.Tag, []tag.Picture, *id3.Tag, *id3.V1, *ape.Tag, error) {
	t := tag.Tag{}
	var pictures []tag.Picture

	var v2 *id3.Tag
	if v, err := readID3v2(r); err == nil {
		v2 = v
		t = v2.Common()
		pictures = v2.Pictures()
	} else if !errors.Is(err, id3.ErrNoTag) {
		return nil, nil, nil, nil, nil, err
	}

	var at *ape.Tag
	if a, err := readAPE(r); err == nil {
		at = a
		mergeAPE(t, at, &pictures)
	} else if !errors.Is(err, ape.ErrNoTag) {
		return nil, nil, nil, nil, nil, err
	}

	v1, err := readID3v1(r, size)
	if err != nil && !errors.Is(err, id3.ErrNoV1) {
		return nil, nil, nil, nil, nil, err
	}
	if v1 != nil {
		mergeV1(t, v1)
	}
	return t, pictures, v2, v1, at, nil
}

// readAPE reads the APEv2 tag at the end of r, returning [ape.ErrNoTag]
// when there is none.
func readAPE(r io.ReadSeeker) (*ape.Tag, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("mp3: seeking: %w", err)
	}
	return ape.Read(r)
}

// mergeAPE adds APEv2 fields that earlier tags left out. A filled field keeps
// its value. Artwork from both tags is kept.
func mergeAPE(t tag.Tag, at *ape.Tag, pictures *[]tag.Picture) {
	for key, values := range at.Common() {
		if len(values) == 0 || len(t[key]) > 0 {
			continue
		}
		t[key] = append([]string(nil), values...)
	}
	*pictures = append(*pictures, at.Pictures()...)
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

// id3V1Size is the length of an ID3v1 tag. It is the distance from the end
// of the file.
const id3V1Size = 128

// v1CommentKey holds the comment of an ID3v1 tag in a file that also has an
// ID3v2 comment. The two comments are separate text. The older one is stored
// under its own key.
const v1CommentKey = "comment:id3v1 comment"

// mergeV1 adds ID3v1 fields that the ID3v2 tag left out. ID3v1 holds one
// value per field. A field with a v2 value keeps the v2 value.
func mergeV1(t tag.Tag, v1 *id3.V1) {
	for key, values := range v1.Common() {
		if key != tag.Comment {
			if len(values) > 0 {
				t.SetDefault(key, values[0])
			}
			continue
		}
		// A v2 tag has its own comment only with a COMM frame. An empty key
		// means the v1 comment is the only comment.
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

// streamVersion is the key of the bitrate and frame size tables. Version 2.5
// maps to version 2. Version 2 layer 3 maps to layer 2. Those table rows are
// identical.
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

// tableKey maps a version and layer to a row of the shared tables.
func tableKey(v MPEGVersion, layer int) streamVersion {
	if v == MPEG2_5 {
		v = MPEG2
	}
	if v == MPEG2 && layer == 3 {
		layer = 2
	}
	return streamVersion{version: v, layer: layer}
}

// frameSamples returns samples per frame and bytes per sample. Layer 1 uses
// different values. Non-MPEG1 layer 3 uses different values.
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
	// offset is the file position where the header starts.
	offset int64
	// version, layer and mode are values from the header.
	version MPEGVersion
	layer   int
	mode    Mode
	// bitrate is in bit/s. sampleRate is in Hz. Both come from the header.
	bitrate    int
	sampleRate int
	// length is the total frame size in bytes, header included.
	length int64
	// samples is the audio sample count the frame decodes to.
	samples int
	// vbr is the Xing, Info or VBRI header in the frame, if any.
	vbr *VBRHeader
}

// parseFrameHeader decodes the frame header at the reader position. The
// reader is left after the four header bytes.
//
// Every reserved combination returns an error. This prevents 0xFF bytes in
// pictures or ID3 tags from matching as audio.
func parseFrameHeader(r io.Reader) (frameHeader, error) {
	var buf [fileHeaderLen]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return frameHeader{}, fmt.Errorf("%w: reading frame header", ErrNoFrame)
	}
	b := buf[:]
	if b[0] != 0xFF || b[1]&0xE0 != 0xE0 {
		return frameHeader{}, ErrNoFrame
	}

	// Bit positions are counted from the start of the second byte. Fields
	// after the eleven bit sync start there.
	versionBits := (b[1] >> 3) & 0x03 // 01 is MPEG1, 10 is MPEG2, 00 is 2.5
	layerBits := (b[1] >> 1) & 0x03   // 01 is layer 3, 10 is layer 2, 11 is layer 1
	bitrateIdx := b[2] >> 4
	rateIdx := (b[2] >> 2) & 0x03
	padding := b[2]&0x02 != 0
	mode := Mode((b[3] >> 6) & 0x03)

	// Version 01 is reserved. Layer 00 is reserved. Sample rate 3 and
	// bitrate index 15 are reserved.
	if versionBits == 1 || layerBits == 0 || rateIdx == 3 || bitrateIdx == 15 {
		return frameHeader{}, ErrNoFrame
	}
	// Bitrate index 0 means free format. This package does not decode it.
	// The frame length is unknown without a bitrate.
	if bitrateIdx == 0 {
		return frameHeader{}, ErrNoFrame
	}

	h := frameHeader{
		// The version field maps 00 to 2.5, 10 to 2 and 11 to 1. Index 1 is
		// reserved and was rejected above.
		version: [4]MPEGVersion{MPEG2_5, 0, MPEG2, MPEG1}[versionBits],
		// The layer field maps 01 to layer 3, 10 to layer 2 and 11 to layer 1.
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

	// samples is samples per frame. slot is bytes of frame per sample.
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

// codecName returns the encoding name, such as "MPEG-1 Layer 3".
func (h frameHeader) codecName() string {
	return fmt.Sprintf("%s Layer %d", h.version, h.layer)
}

// readStream finds the first audio frame and returns stream properties. It
// prefers a variable bitrate header over the frame header.
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

// channelsOf returns the channel count for a mode.
func channelsOf(m Mode) int {
	if m == Mono {
		return 1
	}
	return 2
}

// applyVBR sets duration, bitrate, encoder and bitrate mode from a variable
// bitrate header. It estimates duration from file size when the header is
// missing or incomplete.
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
		// A VBRI header counts every frame, including its own frame. It states
		// its own duration. Both values are used as written.
		samples := float64(first.samples) * float64(vbr.Frames)
		seconds := samples / float64(first.sampleRate)
		if seconds > 0 {
			stream.Bitrate = int(float64(vbr.Bytes) * 8 / seconds)
			stream.Duration = time.Duration(round(seconds * float64(time.Second)))
		}
		return
	}

	// A Xing header counts only frames after its own frame. Its byte count
	// includes its own frame. The own frame length is subtracted.
	audioBytes := max(0, vbr.Bytes-first.length)
	samples := int64(first.samples) * vbr.Frames
	if audioBytes > 0 && samples > 0 {
		stream.Bitrate = round(float64(audioBytes) * 8 * float64(first.sampleRate) / float64(samples))
	}

	if vbr.LAME != nil {
		// LAME reports encoder delay and padding. These samples are not audible
		// audio. Delay and padding are subtracted after bitrate calculation.
		samples -= int64(vbr.LAME.Delay)
		samples -= int64(vbr.LAME.Padding)
	}
	// Some short files from early LAME versions have delay larger than the
	// audio.
	samples = max(0, samples)
	stream.Duration = time.Duration(round(float64(samples) / float64(first.sampleRate) * float64(time.Second)))
}

// estimateDuration estimates audio duration from byte size and bitrate.
func estimateDuration(size, bitrate int64) time.Duration {
	if bitrate <= 0 || size <= 0 {
		return 0
	}
	seconds := float64(size) * 8 / float64(bitrate)
	return time.Duration(round(seconds * float64(time.Second)))
}

// skipID3 moves the reader past ID3v2 tags at its position. Some files store
// several tags in a row. This function skips all of them.
func skipID3(r io.ReadSeeker) error {
	for {
		cur, err := r.Seek(0, io.SeekCurrent)
		if err != nil {
			return fmt.Errorf("mp3: seeking: %w", err)
		}
		var buf [10]byte
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			// Too little data remains for another header. No audio remains. The
			// reader position is restored.
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
		// The header was already read. Only the body remains to skip.
		if _, err := r.Seek(int64(h.Body), io.SeekCurrent); err != nil {
			return fmt.Errorf("mp3: seeking: %w", err)
		}
	}
}

// findFirstFrame scans audio data for a frame header. It then checks for a
// run of frames that agree. A single frame can be a false match. Several
// consecutive frames are required.
//
// The second result reports a weak frame chain. In this case properties come
// from a single frame and may be wrong.
func findFirstFrame(r io.ReadSeeker, size int64) (frameHeader, bool, error) {
	const (
		// Search is limited to the first megabyte. Padding beyond that holds
		// no useful data.
		maxSearch = 1024 * 1024
		// Search stops after 1500 sync candidates. This limits time in files
		// with many false syncs.
		maxSyncs = 1500
		// wantFrames is the consecutive frame count for a trusted stream.
		wantFrames = 4
		// anyFrames is the frame count for a report when no wantFrames run
		// exists.
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
				// A variable bitrate header describes the whole stream. One frame
				// with this header determines the properties.
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

// readFrameHeader parses the frame header at offset. It includes any variable
// bitrate header in the frame.
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

// scanSyncs calls fn for each candidate frame sync offset in the first
// maxRead bytes of r. Reading stops when fn returns false. Offsets are in
// increasing order.
//
// A candidate is a 0xFF byte followed by a byte with the top three bits set.
// Every MPEG frame header starts with this pattern.
func scanSyncs(r io.ReadSeeker, maxRead int64, fn func(offset int64) bool) error {
	var read int64
	// Reads use doubling chunk sizes. A header at the start is found after one
	// small read. A long file requires fewer reads.
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

		// Candidates in this chunk are collected first. The callback reads the
		// file and moves the reader.
		var offsets []int64
		if last == 0xFF && data[0]&0xE0 == 0xE0 {
			// The last byte of the previous chunk may be 0xFF. It may start a
			// sync with the first byte of this chunk.
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
			// Reading resumes where this chunk ended. The callback may have moved
			// the reader.
			if _, err := r.Seek(chunkStart+int64(n), io.SeekStart); err != nil {
				return fmt.Errorf("mp3: seeking: %w", err)
			}
		}
	}
	return nil
}

// readFull fills data. It allows a short read at end of file.
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

// readUint32BE reads a big-endian uint32.
func readUint32BE(b []byte) uint32 { return binary.BigEndian.Uint32(b) }

// round returns the nearest integer to f. MPEG headers require rounding.
// Rounding avoids bias from truncation.
func round(f float64) int { return int(math.Round(f)) }
