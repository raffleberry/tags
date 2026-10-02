// Package flac reads FLAC files: the metadata blocks that follow the "fLaC"
// marker, the Vorbis comments that hold the tags, and the pictures.
//
// A FLAC file opens with the marker "fLaC" and then a run of metadata blocks,
// each with a one byte header holding a type and a last-block flag. The audio
// follows them. [Read] walks the blocks and reads what it understands, which is
// the stream information, the comments and the pictures.
package flac

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/raffleberry/tags/internal/bits"
	"github.com/raffleberry/tags/tag"
)

// Errors reported while reading a FLAC file.
var (
	// ErrNoMagic means the file does not start with the "fLaC" marker.
	ErrNoMagic = errors.New("flac: not a FLAC file")
	// ErrNoStreamInfo means the file has no stream information block, which the
	// specification requires to be first.
	ErrNoStreamInfo = errors.New("flac: no stream information block")
	// ErrBlock means a metadata block header is out of range.
	ErrBlock = errors.New("flac: malformed metadata block")
)

// Magic begins every FLAC file.
var Magic = []byte("fLaC")

// The metadata block types, which are what the first byte of a block header
// holds below its last-block flag.
const (
	BlockStreamInfo  = 0
	BlockPadding     = 1
	BlockApplication = 2
	BlockSeektable   = 3
	BlockVorbis      = 4
	BlockCueSheet    = 5
	BlockPicture     = 6
)

// headerLen is the size of a metadata block header: one byte of type and flag,
// then a 24 bit length.
const headerLen = 4

// maxBlockLen is the largest length a 24 bit field can hold.
const maxBlockLen = 1<<24 - 1

// File is a FLAC file: its stream properties, its tags and its artwork.
type File struct {
	audio    tag.Audio
	tags     tag.Tag
	pictures []tag.Picture
	vendor   string
	// seektable and blocks are the raw metadata blocks, kept so that a caller
	// can reach the parts this package does not model.
	seektable *SeekTable
	blocks    []Block
}

// Format reports that a FLAC file was read from.
func (f *File) Format() tag.Format { return tag.FLAC }

// Tags returns the normalized metadata fields. The result must not be
// modified.
func (f *File) Tags() tag.Tag { return f.tags }

// Audio returns the properties of the audio stream.
func (f *File) Audio() tag.Audio { return f.audio }

// Pictures returns the embedded artwork, one entry per picture block.
func (f *File) Pictures() []tag.Picture { return f.pictures }

// Vendor returns the string the writer of the file put in the Vorbis comment,
// naming the program that wrote it. It is empty when there are no comments.
func (f *File) Vendor() string { return f.vendor }

// SeekTable returns the seek points of the file, or nil when it has no seek
// table block.
func (f *File) SeekTable() *SeekTable { return f.seektable }

// Blocks returns every metadata block in the file, in order, including the ones
// this package does not decode.
func (f *File) Blocks() []Block { return f.blocks }

// Block is one metadata block, kept whole so that nothing is lost.
type Block struct {
	// Type is the block type, one of the Block constants.
	Type int
	// Data is the payload of the block, which for a known type has already been
	// decoded into the file.
	Data []byte
}

// Matches reports whether header, which should hold at least the first four
// bytes of a file, looks like a FLAC file.
func Matches(header []byte) bool {
	return len(header) >= len(Magic) && string(header[:len(Magic)]) == string(Magic)
}

// Open reads the FLAC file at path.
func Open(path string) (*File, error) {
	r, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return Read(r)
}

// Read reads a FLAC file from r, which must be seekable.
func Read(r io.ReadSeeker) (*File, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("flac: seeking: %w", err)
	}
	marker := make([]byte, len(Magic))
	if _, err := io.ReadFull(r, marker); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNoMagic, err)
	}
	if !Matches(marker) {
		return nil, fmt.Errorf("%w: found %q", ErrNoMagic, marker)
	}

	f := &File{tags: tag.Tag{}}
	audioStart, err := f.readBlocks(r)
	if err != nil {
		return nil, err
	}
	if f.audio.SampleRate == 0 {
		return nil, fmt.Errorf("%w: the file has no audio", ErrNoStreamInfo)
	}

	// The bitrate follows from the length of the audio and the length of the
	// stream, neither of which the file states directly.
	if f.audio.Duration > 0 {
		if size, err := audioSize(r, audioStart); err == nil && size > 0 {
			f.audio.Bitrate = int(round(float64(size) * 8 / f.audio.Duration.Seconds()))
		}
	}
	return f, nil
}

// audioSize returns the number of bytes of audio in the file, which is the
// distance from the start of the audio to the end of the file.
func audioSize(r io.ReadSeeker, audioStart int64) (int64, error) {
	end, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, fmt.Errorf("flac: seeking: %w", err)
	}
	size := end - audioStart
	if _, err := r.Seek(audioStart, io.SeekStart); err != nil {
		return 0, fmt.Errorf("flac: seeking: %w", err)
	}
	return size, nil
}

// readBlocks walks the metadata blocks, filling in the file, and returns where
// the audio starts.
func (f *File) readBlocks(r io.ReadSeeker) (audioStart int64, err error) {
	for last := false; !last; {
		last, err = f.readBlock(r)
		if err != nil {
			return 0, err
		}
	}

	pos, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, fmt.Errorf("flac: seeking: %w", err)
	}
	return pos, nil
}

// readBlock reads one metadata block header and its payload, and reports whether
// it was the last one.
func (f *File) readBlock(r io.ReadSeeker) (last bool, err error) {
	header := make([]byte, headerLen)
	if _, err := io.ReadFull(r, header); err != nil {
		return false, fmt.Errorf("%w: reading block header: %w", ErrBlock, err)
	}

	blockType := int(header[0] & 0x7F)
	last = header[0]&0x80 != 0
	length := int64(header[1])<<16 | int64(header[2])<<8 | int64(header[3])
	start, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return false, fmt.Errorf("flac: seeking: %w", err)
	}

	// The comment and picture blocks are read by their own structure rather than
	// by the length in the header. Some writers of the day wrote a length that
	// does not match their content, and the reference implementation copes, so a
	// file such a writer produced is still readable. Any other block is read at
	// the length given, since its payload has no length of its own to fall back
	// on.
	switch blockType {
	case BlockVorbis:
		comment, err := ReadVorbisComment(r)
		if err != nil {
			return false, err
		}
		f.vendor = comment.Vendor
		// A file with several comment blocks keeps the first, as the reference
		// implementation does.
		if len(f.tags) == 0 {
			f.tags = comment.Common()
		}

	case BlockPicture:
		if picture, ok := readPicture(r); ok {
			normalizePictureMIME(&picture)
			f.pictures = append(f.pictures, picture)
		}

	default:
		data := make([]byte, length)
		if _, err := io.ReadFull(r, data); err != nil {
			return false, fmt.Errorf("%w: block %d claims %d bytes: %w", ErrBlock, blockType, length, err)
		}
		switch blockType {
		case BlockStreamInfo:
			audio, err := readStreamInfo(data)
			if err != nil {
				return false, err
			}
			f.audio = audio
		case BlockSeektable:
			seektable, err := ParseSeekTable(data)
			if err == nil {
				f.seektable = seektable
			}
		}
		f.blocks = append(f.blocks, Block{Type: blockType, Data: data})
		return last, nil
	}

	// The block that was just read by its own structure, kept whole for a caller
	// who wants it.
	end, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return false, fmt.Errorf("flac: seeking: %w", err)
	}
	if _, err := r.Seek(start, io.SeekStart); err != nil {
		return false, fmt.Errorf("flac: seeking: %w", err)
	}
	data := make([]byte, end-start)
	if _, err := io.ReadFull(r, data); err != nil {
		return false, fmt.Errorf("%w: reading block %d: %w", ErrBlock, blockType, err)
	}
	if _, err := r.Seek(end, io.SeekStart); err != nil {
		return false, fmt.Errorf("flac: seeking: %w", err)
	}
	f.blocks = append(f.blocks, Block{Type: blockType, Data: data})
	return last, nil
}

// SeekPoint is one entry of a seek table: where in the stream a frame starts.
type SeekPoint struct {
	// FirstSample is the sample number the frame starts at, or -1 for a
	// placeholder point.
	FirstSample uint64
	// ByteOffset is how far into the audio the frame starts.
	ByteOffset uint64
	// NumSamples is how many samples the frame holds.
	NumSamples uint16
}

// SeekTable is the list of frames a file lets a player seek to. It is an
// optimisation rather than a requirement, so most files have one.
type SeekTable struct {
	Points []SeekPoint
}

// placeholderPoint is the first sample number of a placeholder seek point, which
// marks an entry reserved for a frame the writer did not need.
const placeholderPoint = 1<<64 - 1

// ParseSeekTable decodes a seek table block. Each point is 18 bytes: a sample
// number, an offset and a frame size.
func ParseSeekTable(data []byte) (*SeekTable, error) {
	const pointLen = 18

	// A table whose size is not a whole number of points is malformed. Being
	// strict here is what keeps a damaged block from being read as valid points.
	if len(data)%pointLen != 0 {
		return nil, fmt.Errorf("%w: seek table of %d bytes", ErrBlock, len(data))
	}
	table := &SeekTable{}
	for pos := 0; pos+pointLen <= len(data); pos += pointLen {
		table.Points = append(table.Points, SeekPoint{
			FirstSample: binary.BigEndian.Uint64(data[pos : pos+8]),
			ByteOffset:  binary.BigEndian.Uint64(data[pos+8 : pos+16]),
			NumSamples:  binary.BigEndian.Uint16(data[pos+16 : pos+18]),
		})
	}
	return table, nil
}

// IsPlaceholder reports whether the point is a placeholder, which reserves a slot
// without naming a frame.
func (p SeekPoint) IsPlaceholder() bool { return p.FirstSample == placeholderPoint }

// readStreamInfo fills in the stream properties from a stream information block.
//
// The block is defined as a series of bit fields that do not line up with the
// bytes they occupy, so it is read as bits:
//
//	 16 bits  the smallest block size in samples
//	 16 bits  the largest block size in samples
//	 24 bits  the smallest frame size in bytes
//	 24 bits  the largest frame size in bytes
//	 20 bits  the sample rate in Hz
//	  3 bits  the channel count, stored as one less
//	  5 bits  the sample width in bits, stored as one less
//	 36 bits  the total number of samples
//	128 bits  the MD5 signature of the unencoded audio
func readStreamInfo(data []byte) (tag.Audio, error) {
	const infoLen = 34
	if len(data) < infoLen {
		return tag.Audio{}, fmt.Errorf("%w: stream information of %d bytes", ErrBlock, len(data))
	}
	r := bits.New(data)

	minBlock := int(r.Read(16))
	maxBlock := int(r.Read(16))
	r.Skip(24 + 24) // the frame sizes, which are a hint for the encoder

	var audio tag.Audio
	audio.SampleRate = int(r.Read(20))
	audio.Channels = int(r.Read(3)) + 1
	audio.BitsPerSample = int(r.Read(5)) + 1
	samples := r.Read(36)
	// What follows is the MD5 signature of the unencoded audio, which says
	// nothing about the stream itself.

	if audio.SampleRate == 0 {
		return audio, fmt.Errorf("%w: the sample rate is zero", ErrBlock)
	}
	if maxBlock < minBlock {
		return audio, fmt.Errorf("%w: the block sizes run from %d to %d", ErrBlock, minBlock, maxBlock)
	}
	audio.Duration = time.Duration(round(float64(samples) / float64(audio.SampleRate) * float64(time.Second)))
	return audio, nil
}

// readPicture decodes a picture block from r, which is left at the end of the
// block. ok is false when the block is malformed, in which case r is left
// wherever the failure happened.
//
// A picture block is laid out like the ID3v2 attached picture frame: the type of
// the picture, its MIME type and description as counted strings, four 32 bit
// dimensions, and then the image itself as a counted string.
func readPicture(r io.Reader) (p tag.Picture, ok bool) {
	var head [4]byte

	read32 := func() (int, error) {
		if _, err := io.ReadFull(r, head[:]); err != nil {
			return 0, err
		}
		return int(binary.BigEndian.Uint32(head[:])), nil
	}
	readString := func() (string, error) {
		n, err := read32()
		if err != nil {
			return "", err
		}
		if n < 0 || int64(n) > maxBlockLen {
			return "", fmt.Errorf("picture string of %d bytes", n)
		}
		buf := make([]byte, n)
		_, err = io.ReadFull(r, buf)
		return string(buf), err
	}

	kind, err := read32()
	if err != nil {
		return p, false
	}
	p.Type = tag.PictureType(kind)

	if p.MIME, err = readString(); err != nil {
		return p, false
	}
	if p.Desc, err = readString(); err != nil {
		return p, false
	}
	for _, field := range []*int{&p.Width, &p.Height, &p.Depth, &p.Colors} {
		if *field, err = read32(); err != nil {
			return p, false
		}
	}

	size, err := read32()
	if err != nil || size < 0 || int64(size) > maxBlockLen {
		return p, false
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(r, data); err != nil || len(data) == 0 {
		return p, false
	}
	p.Data = data
	return p, true
}

// normalizePictureMIME fills in the MIME type of a picture from its magic
// bytes, for the files whose writer left the type out.
func normalizePictureMIME(p *tag.Picture) {
	if p.MIME != "" || len(p.Data) < 4 {
		return
	}
	switch {
	case p.Data[0] == 0xFF && p.Data[1] == 0xD8 && p.Data[2] == 0xFF:
		p.MIME = "image/jpeg"
	case string(p.Data[:4]) == "\x89PNG":
		p.MIME = "image/png"
	case string(p.Data[:4]) == "GIF8":
		p.MIME = "image/gif"
	}
}

// round rounds a float to the nearest integer, which the sample count to seconds
// conversion calls for.
func round(f float64) int64 { return int64(f + 0.5) }
