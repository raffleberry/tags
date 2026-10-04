// Package flac reads FLAC metadata blocks, Vorbis comments, and pictures.
//
// A FLAC file starts with the marker "fLaC" followed by metadata blocks.
// Each block has a one byte header with a type and a last-block flag.
// Audio data follows the blocks. [Read] reads the stream information,
// comment, and picture blocks.
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

// Errors returned when reading a FLAC file.
var (
	// ErrNoMagic means the file does not start with the "fLaC" marker.
	ErrNoMagic = errors.New("flac: not a FLAC file")
	// ErrNoStreamInfo means the file has no stream information block. The specification requires it first.
	ErrNoStreamInfo = errors.New("flac: no stream information block")
	// ErrBlock means a metadata block is malformed.
	ErrBlock = errors.New("flac: malformed metadata block")
)

// Magic is the marker at the start of every FLAC file.
var Magic = []byte("fLaC")

// Metadata block types. The low 7 bits of the block header hold the type.
const (
	BlockStreamInfo  = 0
	BlockPadding     = 1
	BlockApplication = 2
	BlockSeektable   = 3
	BlockVorbis      = 4
	BlockCueSheet    = 5
	BlockPicture     = 6
)

// headerLen is the metadata block header size in bytes.
const headerLen = 4

// maxBlockLen is the largest length a 24 bit field can hold.
const maxBlockLen = 1<<24 - 1

// File holds FLAC stream properties, tags, and artwork.
type File struct {
	audio    tag.Audio
	tags     tag.Tag
	pictures []tag.Picture
	vendor   string
	// seektable and cuesheet hold decoded blocks. blocks holds all raw blocks.
	seektable *SeekTable
	cuesheet  *CueSheet
	blocks    []Block
}

// Format returns the file format.
func (f *File) Format() tag.Format { return tag.FLAC }

// Tags returns the normalized metadata fields. The result must not be modified.
func (f *File) Tags() tag.Tag { return f.tags }

// Audio returns the properties of the audio stream.
func (f *File) Audio() tag.Audio { return f.audio }

// Pictures returns the embedded artwork. There is one entry per picture block.
func (f *File) Pictures() []tag.Picture { return f.pictures }

// Vendor returns the writer name from the Vorbis comment. It is empty when the file has no comment block.
func (f *File) Vendor() string { return f.vendor }

// SeekTable returns the seek table. It returns nil when the file has none.
func (f *File) SeekTable() *SeekTable { return f.seektable }

// CueSheet returns the cue sheet of the file. It returns nil when the file has none.
func (f *File) CueSheet() *CueSheet { return f.cuesheet }

// Blocks returns every metadata block in the file in order. It includes undecoded blocks.
func (f *File) Blocks() []Block { return f.blocks }

// Block is one metadata block with its raw payload.
type Block struct {
	// Type is the block type. It is one of the Block constants.
	Type int
	// Data holds the block payload.
	Data []byte
}

// Matches reports whether header starts with the FLAC marker. It needs at least four bytes.
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

// Read reads a FLAC file from r. r must be seekable.
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

	// Bitrate is computed from audio size and duration. The file states neither directly.
	if f.audio.Duration > 0 {
		if size, err := audioSize(r, audioStart); err == nil && size > 0 {
			f.audio.Bitrate = int(round(float64(size) * 8 / f.audio.Duration.Seconds()))
		}
	}
	return f, nil
}

// audioSize returns the byte count from audioStart to the end of the file.
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

// readBlocks reads metadata blocks into f. It returns the audio start offset.
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

// readBlock reads one metadata block. It reports whether the block is last.
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

	// Comment and picture blocks are read by content, not by header length.
	// Some writers store a wrong length. Other blocks use the header length.
	// Other blocks have no inner length to use.
	switch blockType {
	case BlockVorbis:
		comment, err := ReadVorbisComment(r)
		if err != nil {
			return false, err
		}
		f.vendor = comment.Vendor
		// Only the first comment block is kept.
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
		case BlockCueSheet:
			if cuesheet, err := ParseCueSheet(data); err == nil && f.cuesheet == nil {
				f.cuesheet = cuesheet
			}
		}
		f.blocks = append(f.blocks, Block{Type: blockType, Data: data})
		return last, nil
	}

	// Store the raw bytes of the block just read.
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

// SeekPoint is one seek table entry. It gives the start of one frame.
type SeekPoint struct {
	// FirstSample is the first sample number. Placeholder points use the maximum uint64 value.
	FirstSample uint64
	// ByteOffset is the byte offset of the frame in the audio.
	ByteOffset uint64
	// NumSamples is the sample count of the frame.
	NumSamples uint16
}

// SeekTable holds seek points from a seek table block.
type SeekTable struct {
	Points []SeekPoint
}

// placeholderPoint is the FirstSample value of a reserved seek point.
const placeholderPoint = 1<<64 - 1

// ParseSeekTable decodes a seek table block. Each point is 18 bytes: a sample number, an offset, and a frame size.
func ParseSeekTable(data []byte) (*SeekTable, error) {
	const pointLen = 18

	// The data length must be a multiple of 18.
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

// IsPlaceholder reports whether the point is a placeholder.
func (p SeekPoint) IsPlaceholder() bool { return p.FirstSample == placeholderPoint }

// readStreamInfo fills in the stream properties from a stream information block.
//
// The block holds bit fields. They are read as bits:
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
	r.Skip(24 + 24) // Skip frame sizes.

	var audio tag.Audio
	audio.SampleRate = int(r.Read(20))
	audio.Channels = int(r.Read(3)) + 1
	audio.BitsPerSample = int(r.Read(5)) + 1
	samples := r.Read(36)
	// The next field is the MD5 signature of the audio.

	if audio.SampleRate == 0 {
		return audio, fmt.Errorf("%w: the sample rate is zero", ErrBlock)
	}
	if maxBlock < minBlock {
		return audio, fmt.Errorf("%w: the block sizes run from %d to %d", ErrBlock, minBlock, maxBlock)
	}
	audio.Duration = time.Duration(round(float64(samples) / float64(audio.SampleRate) * float64(time.Second)))
	return audio, nil
}

// readPicture decodes a picture block from r. r is left at the end of the block.
// ok is false when the block is malformed.
//
// A picture block holds a picture type, MIME type, description, four 32 bit
// dimensions, and image data. Strings and image data have 32 bit lengths.
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

// normalizePictureMIME sets the MIME type from magic bytes when it is empty.
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

// round returns the nearest integer to f.
func round(f float64) int64 { return int64(f + 0.5) }
