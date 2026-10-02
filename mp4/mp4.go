package mp4

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/raffleberry/tags/internal/bits"
	"github.com/raffleberry/tags/tag"
)

// File is an MPEG-4 audio file, which is the container behind the .m4a, .m4b and
// .mp4 extensions.
type File struct {
	atoms    []Atom
	moov     Atom
	ilst     *ILST
	audio    tag.Audio
	tags     tag.Tag
	pictures []tag.Picture
	// file is the reader the atoms are offsets into, kept so that the accessors
	// can fetch a payload on demand.
	file io.ReadSeeker
}

// Format reports that an MPEG-4 audio file was read from.
func (f *File) Format() tag.Format { return tag.M4A }

// Tags returns the normalized metadata fields. The result must not be
// modified.
func (f *File) Tags() tag.Tag { return f.tags }

// Audio returns the properties of the audio stream.
func (f *File) Audio() tag.Audio { return f.audio }

// Pictures returns the embedded artwork.
func (f *File) Pictures() []tag.Picture { return f.pictures }

// Atoms returns the top level atoms of the file, which is the way to reach
// anything this package does not model, such as the chapter list.
func (f *File) Atoms() []Atom { return f.atoms }

// ILST returns the iTunes metadata list the tags came from, or nil when the file
// has none.
func (f *File) ILST() *ILST { return f.ilst }

// AudioTrack returns the track whose properties [File.Audio] reports, or false
// when the file has no audio track at all.
func (f *File) AudioTrack() (Atom, bool) { return audioTrack(f.file, f.moov) }

// isAudioHandler reports whether a "hdlr" atom belongs to an audio track. The
// handler type is four bytes of "soun", "vide" and so on, after the version and
// flags fields and a reserved field.
func isAudioHandler(r io.ReadSeeker, hdlr Atom) bool {
	if r == nil {
		return false
	}
	data, err := hdlr.Data(r)
	if err != nil || len(data) < 12 {
		return false
	}
	return string(data[8:12]) == "soun"
}

// Matches reports whether header, which should hold at least the first 12 bytes
// of a file, looks like an MPEG-4 file.
//
// A file normally begins with an "ftyp" atom, which names the brands it
// conforms to, but that atom is optional and a file may open with the movie box
// or the media data instead. Any of the atoms that can come first is accepted,
// since all of them are distinctive enough that no other container this module
// reads starts with one.
func Matches(header []byte) bool {
	if len(header) < 8 {
		return false
	}
	switch string(header[4:8]) {
	case "ftyp", "moov", "mdat", "free", "skip", "wide":
		return true
	default:
		return false
	}
}

// Open reads the MPEG-4 file at path.
func Open(path string) (*File, error) {
	r, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return Read(r)
}

// Read reads an MPEG-4 file from r, which must be seekable. A file with no
// audio track is not an error: the metadata may still be readable, and an MP4
// video file carries the same tags.
func Read(r io.ReadSeeker) (*File, error) {
	atoms, err := Atoms(r)
	if err != nil {
		return nil, err
	}

	// A file of atoms that has no movie box is not an MPEG-4 file. Without this
	// check any container whose atoms happen to parse would be accepted, such as
	// a FLAC file.
	moov, ok := Find(atoms, "moov")
	if !ok {
		return nil, fmt.Errorf("%w: no moov atom", ErrNoFile)
	}

	f := &File{atoms: atoms, moov: moov}

	if ilst, ok := metadataList(moov); ok {
		if data, err := ilst.Data(r); err == nil {
			f.ilst = ParseILST(data)
			f.tags = f.ilst.Common()
			f.pictures = f.ilst.Covers()
		}
	}
	if f.tags == nil {
		f.tags = tag.Tag{}
	}

	f.file = r
	f.audio = readAudio(r, moov)
	return f, nil
}

// metadataList returns the iTunes metadata list of the movie box, which is
// where the tags live. The second result is false for a file with no tags.
func metadataList(moov Atom) (Atom, bool) { return moov.Path("udta", "meta", "ilst") }

// Track atoms whose payload describes the audio stream.
const (
	atomMovieHeader = "mvhd" // duration of the whole file
	atomMediaHeader = "mdhd" // duration and timescale of one track
	atomHandler     = "hdlr" // what kind of track this is
	atomSampleEntry = "stsd" // codec, sample rate and channel count
)

// readAudio works out the properties of the audio stream. The first track with
// an audio handler wins; a file with no audio track keeps the duration from the
// movie header if there is one, and otherwise reports nothing.
func readAudio(r io.ReadSeeker, moov Atom) tag.Audio {
	audio := tag.Audio{}

	track, ok := audioTrack(r, moov)
	if !ok {
		// No audio track, so fall back to the length of the whole file.
		if mvhd, ok := moov.Path(atomMovieHeader); ok {
			if data, err := mvhd.Data(r); err == nil {
				audio.Duration = readMovieDuration(data)
			}
		}
		return audio
	}

	// The media header holds the timescale, which is what scales the duration
	// from a count of units to seconds.
	if mdhd, ok := track.Path("mdia", atomMediaHeader); ok {
		if data, err := mdhd.Data(r); err == nil {
			if timescale, units := readMediaDuration(data); timescale > 0 {
				audio.Duration = time.Duration(
					round64(float64(units) / float64(timescale) * float64(time.Second)))
			}
		}
	}

	if stsd, ok := track.Path("mdia", "minf", "stbl", atomSampleEntry); ok {
		if data, err := stsd.Data(r); err == nil {
			readSampleDescription(data, &audio)
		}
	}
	return audio
}

// audioTrack returns the first track whose handler says it holds audio. A file
// with both a video and an audio track has the audio one.
func audioTrack(r io.ReadSeeker, moov Atom) (Atom, bool) {
	for _, trak := range moov.Children {
		if trak.Name != "trak" {
			continue
		}
		if hdlr, ok := trak.Path("mdia", atomHandler); ok && isAudioHandler(r, hdlr) {
			return trak, true
		}
	}
	return Atom{}, false
}

// readMovieDuration returns the length of the whole file, from the movie header
// atom.
func readMovieDuration(data []byte) time.Duration {
	if len(data) < 4 {
		return 0
	}
	version := data[0]
	switch version {
	case 0:
		if len(data) < 16 {
			return 0
		}
		// Four bytes of version and flags, then creation and modification.
		timescale := beUint32(data[12:16])
		units := int64(beUint32(data[16:20]))
		if timescale == 0 {
			return 0
		}
		return time.Duration(round64(float64(units) / float64(timescale) * float64(time.Second)))

	case 1:
		if len(data) < 28 {
			return 0
		}
		// The 64 bit version widens the creation and modification times.
		timescale := beUint32(data[20:24])
		units := int64(beUint64(data[24:32]))
		if timescale == 0 {
			return 0
		}
		return time.Duration(round64(float64(units) / float64(timescale) * float64(time.Second)))

	default:
		return 0
	}
}

// readMediaDuration returns the timescale and the duration in those units, from
// a media header atom.
func readMediaDuration(data []byte) (timescale int, units int64) {
	if len(data) < 4 {
		return 0, 0
	}
	switch data[0] {
	case 0:
		if len(data) < 20 {
			return 0, 0
		}
		timescale = int(beUint32(data[12:16]))
		units = int64(beUint32(data[16:20]))
	case 1:
		if len(data) < 32 {
			return 0, 0
		}
		timescale = int(beUint32(data[20:24]))
		units = int64(beUint64(data[24:32]))
	}
	return timescale, units
}

// readSampleDescription reads the codec, channel count, sample rate and bitrate
// from the first entry of a sample description atom.
func readSampleDescription(data []byte, audio *tag.Audio) {
	if len(data) < 8 || data[0] != 0 {
		return
	}
	// Four bytes of version and flags, then the entry count.
	if beUint32(data[4:8]) == 0 {
		return
	}

	// Each entry is itself an atom, whose header holds the length and the name of
	// the codec. Only the first entry is read, which is the one a file with a
	// single audio track uses.
	entry, _, ok := readSubAtom(data, headerLen)
	if !ok || len(entry) < 28 {
		return
	}
	audio.Codec = atomName(data[headerLen+4 : headerLen+8])

	// The sample entry format reserves eight bytes, then the audio fields follow:
	// the channel count, the sample size, two more reserved fields, and the
	// sample rate as a 16.16 fixed point number of which the integer half is the
	// rate.
	audio.Channels = int(binary16(entry[16:18]))
	audio.BitsPerSample = int(binary16(entry[18:20]))
	audio.SampleRate = int(binary16(entry[24:26]))

	// The codec specific configuration follows the fixed fields.
	if extra, ok := childAtoms(entry[28:]); ok {
		for _, atom := range extra {
			switch atom.name {
			case "esds":
				readESDS(atom.payload, audio)
			case "alac":
				readALAC(atom.payload, audio)
			case "dac3":
				readAC3(atom.payload, audio)
			}
		}
	}
}

// readSubAtom returns the payload of the atom at pos and the offset just past
// it.
func readSubAtom(data []byte, pos int) (payload []byte, next int, ok bool) {
	if pos+headerLen > len(data) {
		return nil, pos, false
	}
	length := int(beUint32(data[pos : pos+4]))
	if length < headerLen || pos+length > len(data) {
		return nil, pos, false
	}
	return data[pos+headerLen : pos+length], pos + length, true
}

// childAtoms splits the payload of a sample entry into its nested atoms, which
// is where the codec specific configuration lives.
func childAtoms(data []byte) ([]subAtom, bool) {
	var out []subAtom
	for pos := 0; pos+headerLen <= len(data); {
		payload, next, ok := readSubAtom(data, pos)
		if !ok {
			break
		}
		out = append(out, subAtom{
			name:    atomName(data[pos+4 : pos+8]),
			payload: payload,
		})
		pos = next
	}
	return out, len(out) > 0
}

// subAtom is a nested atom and its payload.
type subAtom struct {
	name    string
	payload []byte
}

// readESDS reads the elementary stream descriptor, which is where an AAC stream
// says what it is: the sample entry alone only names the container "mp4a".
func readESDS(data []byte, audio *tag.Audio) {
	if len(data) < 5 || data[0] != 0 {
		return
	}
	rest := data[4:] // past the version and flags field

	if rest[0] != tagESDescriptor {
		return
	}
	// Past the size of the descriptor come a stream identifier and a flags byte,
	// then the decoder configuration that describes the codec.
	rest, ok := descriptorBody(rest)
	if !ok || len(rest) < esDescriptorHead {
		return
	}
	rest = rest[esDescriptorHead:]

	if len(rest) < 1 || rest[0] != tagDecoderConfig {
		return
	}
	rest, ok = descriptorBody(rest)
	if !ok || len(rest) < decoderConfigLen {
		return
	}

	// The decoder configuration holds the object type indication, the stream
	// type, a buffer size, the peak bitrate and the average bitrate. Only an
	// audio stream of the expected object type and stream type says anything
	// more.
	objectType := rest[0]
	streamType := rest[1] >> 2
	audio.Bitrate = int(beUint32(rest[9:13]))
	audio.Codec = fmt.Sprintf("%s.%X", audio.Codec, objectType)
	if objectType != objectTypeAAC || streamType != streamTypeAudio {
		return
	}

	// The audio specific configuration, which is optional.
	if len(rest) > decoderConfigLen && rest[decoderConfigLen] == tagDecoderSpecific {
		if specific, ok := descriptorBody(rest[decoderConfigLen:]); ok {
			readAudioSpecificConfig(specific, audio)
		}
	}
}

// objectTypeAAC and streamTypeAudio are the only combinations the decoder
// configuration details apply to.
const (
	objectTypeAAC   = 0x40
	streamTypeAudio = 0x05
	// decoderConfigLen is the size of the fixed part of a decoder configuration:
	// the object type, the stream type and buffer size byte, the buffer size, the
	// peak bitrate and the average bitrate.
	decoderConfigLen = 13
	// explicitSamplingFrequency is the index that means the rate is spelled out
	// in the following three bytes instead of being looked up in a table.
	explicitSamplingFrequency = 0x0F
	// sbrAudioObjectType and psAudioObjectType are the object types that say the
	// stream also carries spectral band replication or parametric stereo.
	sbrAudioObjectType = 5
	psAudioObjectType  = 29
)

// readAudioSpecificConfig reads the audio specific configuration, which gives
// the audio object type, and where it disagrees with the sample entry the sample
// rate and channel count.
func readAudioSpecificConfig(data []byte, audio *tag.Audio) {
	r := bits.New(data)

	objectType := readAudioObjectType(r)
	rateIndex := int(r.Read(4))
	channels := int(r.Read(4))

	rate := sampleRatesFromIndex[rateIndex]
	if rateIndex == explicitSamplingFrequency && r.Left() >= 24 {
		rate = int(r.Read(24))
	}

	// The object types 5 and 29 wrap the real one, and say the stream was
	// upsampled from a lower rate, so the rate after them is the one to use.
	extension := false
	if objectType == sbrAudioObjectType || objectType == psAudioObjectType {
		extension = true
		if index := int(r.Read(4)); index != explicitSamplingFrequency {
			rate = sampleRatesFromIndex[index]
		} else if r.Left() >= 24 {
			rate = int(r.Read(24))
		}
		objectType = readAudioObjectType(r)
		channels = int(r.Read(4))
	}

	// The name players quote is the object type of the container followed by the
	// audio object type, as in "mp4a.40.2".
	audio.Codec = fmt.Sprintf("%s.%d", audio.Codec, objectType)

	if rate > 0 {
		audio.SampleRate = rate
	}
	if channels > 0 {
		audio.Channels = channels
	}
	_ = extension
}

// readAudioObjectType reads the five bit audio object type, which escapes to six
// more bits when it is 31.
func readAudioObjectType(r *bits.Reader) int {
	n := int(r.Read(5))
	if n == 31 {
		n = 32 + int(r.Read(6))
	}
	return n
}

// MPEG-4 descriptor tags, which are a byte identifying the kind of descriptor.
const (
	tagESDescriptor    = 0x03
	tagDecoderConfig   = 0x04
	tagDecoderSpecific = 0x05
	// esDescriptorHead is what follows the size of an elementary stream
	// descriptor before the decoder configuration: a two byte stream identifier
	// and a flags byte.
	esDescriptorHead = 3
)

// descriptorBody returns the body of the descriptor at the start of data, which
// begins with a one byte tag and a variable length size. ok is false when the
// size runs past the end of data, which means the descriptor is malformed.
func descriptorBody(data []byte) (body []byte, ok bool) {
	if len(data) < 2 {
		return nil, false
	}
	size, pos := 0, 1
	for range 4 {
		if pos >= len(data) {
			return nil, false
		}
		size = size<<7 | int(data[pos]&0x7f)
		pos++
		if data[pos-1]&0x80 == 0 {
			break
		}
	}
	if pos+size > len(data) {
		return nil, false
	}
	return data[pos : pos+size], true
}

// readALAC reads the ALAC magic cookie, which holds the real stream parameters.
// The sample entry often just repeats the defaults, so this is the better
// source.
func readALAC(data []byte, audio *tag.Audio) {
	if len(data) < alacCookieLen {
		return
	}
	// Past the version and flags field the cookie is a frame length, a format
	// version, the sample size, three packing hints, the channel count, the
	// longest run of two equal samples, the largest frame, the average bitrate
	// and the sample rate.
	r := bits.New(data[4:])
	r.Skip(32) // frame length, which says nothing about the stream
	if r.Read(8) != 0 {
		// A nonzero format version means the layout may differ.
		return
	}
	audio.BitsPerSample = int(r.Read(8))
	r.Skip(8 + 8 + 8) // packing hints
	if channels := r.Read(8); channels > 0 {
		audio.Channels = int(channels)
	}
	r.Skip(16) // the longest run of two consecutive equal samples
	r.Skip(32) // the largest frame a decoder needs to buffer
	if bitrate := r.Read(32); bitrate > 0 {
		audio.Bitrate = int(bitrate)
	}
	if rate := r.Read(32); rate > 0 {
		audio.SampleRate = int(rate)
	}
	audio.Encoder = "ALAC"
}

// alacCookieLen is the size of an ALAC magic cookie: a version and flags field
// followed by 24 bytes of stream parameters.
const alacCookieLen = 4 + 24

// readAC3 reads the AC-3 configuration atom, which is how a Dolby Digital
// stream states its channel count and bitrate.
func readAC3(data []byte, audio *tag.Audio) {
	if len(data) < 4 {
		return
	}
	r := bits.New(data[4:])
	r.Skip(2 + 5 + 3) // sample rate, bitstream identification, bitstream mode
	acmod := r.Read(3)
	if lfe := r.Read(1); lfe == 1 {
		acmod++
	}
	if acmod := int(acmod); acmod < len(ac3Channels) {
		audio.Channels = ac3Channels[acmod]
	}
	if code := int(r.Read(5)); code < len(ac3Bitrates) {
		audio.Bitrate = ac3Bitrates[code] * 1000
	}
}

// ac3Channels maps an AC-3 audio coding mode to a channel count.
var ac3Channels = []int{2, 1, 2, 3, 3, 4, 4, 5}

// ac3Bitrates maps an AC-3 bit rate code to a bitrate in kbit/s.
var ac3Bitrates = []int{
	32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384,
	448, 512, 576, 640,
}

// sampleRatesFromIndex maps an MPEG-4 sampling frequency index to a rate in Hz.
// Index 15 means the rate is stated explicitly, which this package does not read.
var sampleRatesFromIndex = []int{
	96000, 88200, 64000, 48000, 44100, 32000,
	24000, 22050, 16000, 12000, 11025, 8000, 7350,
}

func beUint32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func beUint64(b []byte) uint64 {
	return uint64(beUint32(b[:4]))<<32 | uint64(beUint32(b[4:8]))
}

func binary16(b []byte) uint16 {
	return uint16(b[0])<<8 | uint16(b[1])
}

// round64 rounds to the nearest whole, which is how the ISO specification
// defines the timescale divisions.
func round64(f float64) int64 { return int64(f + 0.5) }
