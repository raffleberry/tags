package tag

import (
	"fmt"
	"time"
)

// Audio describes the audio stream of a file, independently of the metadata
// stored alongside it.
type Audio struct {
	// Codec names the encoding, such as "MPEG-1 Layer 3", "mp4a.40.2" or
	// "ALAC". It is empty when the container does not say.
	Codec string
	// Encoder names the tool that produced the stream, such as "LAME 3.99.1".
	// It is empty when unknown, which is common for constant bitrate streams.
	Encoder string
	// Duration of the stream. Zero when the container does not record it.
	Duration time.Duration
	// Bitrate in bits per second, averaged over the whole stream. Zero when
	// unknown.
	Bitrate int
	// BitrateMode says whether the stream is constant or variable bitrate.
	BitrateMode BitrateMode
	// SampleRate in Hz.
	SampleRate int
	// Channels is 1 for mono, 2 for stereo, and higher for multichannel.
	Channels int
	// BitsPerSample is the sample width in bits.
	BitsPerSample int
}

// String describes the stream on a single line, for logs and errors.
func (a Audio) String() string {
	return fmt.Sprintf("%s, %s, %d Hz, %d ch, %.2f s",
		orDash(a.Codec), bitrateString(a), a.SampleRate, a.Channels,
		a.Duration.Seconds())
}

func bitrateString(a Audio) string {
	if a.Bitrate == 0 {
		return "bitrate unknown"
	}
	// A bitrate below ten kbit/s is better shown with a decimal, since rounding
	// to whole kilobits would lose most of the value.
	var s string
	if a.Bitrate < 10000 {
		s = fmt.Sprintf("%.1f kbps", float64(a.Bitrate)/1000)
	} else {
		s = fmt.Sprintf("%d kbps", (a.Bitrate+500)/1000)
	}
	switch a.BitrateMode {
	case BitrateCBR:
		s += " CBR"
	case BitrateVBR:
		s += " VBR"
	case BitrateABR:
		s += " ABR"
	}
	return s
}

func orDash(s string) string {
	if s == "" {
		return "unknown codec"
	}
	return s
}

// BitrateMode describes how a stream's bitrate varies over time.
type BitrateMode int

// The bitrate modes a stream can be encoded with.
const (
	// BitrateUnknown means the stream carries no header saying either way.
	// Constant bitrate files usually fall here.
	BitrateUnknown BitrateMode = iota
	// BitrateCBR is a constant bitrate.
	BitrateCBR
	// BitrateVBR is a variable bitrate.
	BitrateVBR
	// BitrateABR is an average bitrate, a constrained form of VBR.
	BitrateABR
)

// String returns the name of the mode, or "CBR?" when it is unknown.
func (m BitrateMode) String() string {
	switch m {
	case BitrateCBR:
		return "CBR"
	case BitrateVBR:
		return "VBR"
	case BitrateABR:
		return "ABR"
	default:
		return "CBR?"
	}
}

// PictureType describes what a [Picture] shows, following the ID3v2 APIC
// picture types that the other containers copied.
type PictureType int

// The picture types in common use.
const (
	PictureOther PictureType = iota
	PictureFileIcon
	PictureOtherFileIcon
	PictureCoverFront
	PictureCoverBack
	PictureLeaflet
	PictureMedia
	PictureLeadArtist
	PictureArtist
	PictureConductor
	PictureBand
	PictureComposer
	PictureLyricist
	PictureRecordingLocation
	PictureDuringRecording
	PictureDuringPerformance
	PictureVideoScreenCapture
	PictureBrightFish
	PictureIllustration
	PictureBandLogo
	PicturePublisherLogo
)

// String returns the ID3v2 name of the picture type, such as "Cover (front)".
func (t PictureType) String() string {
	switch t {
	case PictureOther:
		return "Other"
	case PictureFileIcon:
		return "32x32 file icon"
	case PictureOtherFileIcon:
		return "Other file icon"
	case PictureCoverFront:
		return "Cover (front)"
	case PictureCoverBack:
		return "Cover (back)"
	case PictureLeaflet:
		return "Leaflet page"
	case PictureMedia:
		return "Media"
	case PictureLeadArtist:
		return "Lead artist"
	case PictureArtist:
		return "Artist"
	case PictureConductor:
		return "Conductor"
	case PictureBand:
		return "Band"
	case PictureComposer:
		return "Composer"
	case PictureLyricist:
		return "Lyricist"
	case PictureRecordingLocation:
		return "Recording location"
	case PictureDuringRecording:
		return "During recording"
	case PictureDuringPerformance:
		return "During performance"
	case PictureVideoScreenCapture:
		return "Video screen capture"
	case PictureBrightFish:
		return "A bright coloured fish"
	case PictureIllustration:
		return "Illustration"
	case PictureBandLogo:
		return "Band logotype"
	case PicturePublisherLogo:
		return "Publisher logotype"
	}
	return fmt.Sprintf("Unknown (%d)", int(t))
}

// Picture is one piece of embedded artwork.
//
// FLAC reports every field; ID3v2 and iTunes only record a MIME type and the
// bytes, leaving the rest zero.
type Picture struct {
	// Type says what the picture shows.
	Type PictureType
	// MIME type of Data, such as "image/jpeg". It may be empty when the
	// container only implied the type.
	MIME string
	// Desc is the human readable caption.
	Desc string
	// Width is in pixels.
	Width int
	// Height is in pixels.
	Height int
	// Depth is the colour depth in bits per pixel.
	Depth int
	// Colors is the size of the palette for indexed images such as GIF, and
	// zero for direct colour images.
	Colors int
	// Data is the encoded image.
	Data []byte
}

// String describes the picture on a single line, for logs and errors.
func (p Picture) String() string {
	return fmt.Sprintf("%s %s, %dx%d, %d bytes",
		p.Type, p.MIME, p.Width, p.Height, len(p.Data))
}
