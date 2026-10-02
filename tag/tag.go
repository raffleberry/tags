// Package tag holds the vocabulary that every audio metadata reader in this
// module speaks: the normalized [Tag] key space, the common [File] interface,
// and the [Audio] and [Picture] values the readers fill in.
//
// # Normalization
//
// Every container names its metadata fields differently: ID3v2 uses four
// character frame IDs such as "TIT2", iTunes uses atoms such as "©nam", FLAC
// uses Vorbis comments such as "TITLE". A [Tag] is the format independent view
// of that data: lowercase keys drawn from the constants declared here, each
// holding one or more string values.
//
// Normalization does not throw anything away. A native name with a known
// common meaning is folded onto its common key; anything else is kept under
// its own lowercased native name. Artwork is not a string, so pictures live in
// the [Picture] values returned by [File.Pictures] instead of in [Tag].
//
// Fields this package does not model remain reachable through the package that
// knows about them: package id3 for frame level access to an ID3 tag, package
// mp4 for the atom tree, package flac for its metadata blocks.
package tag

// Format identifies the container a [File] was read from.
type Format string

// The containers this module can read.
const (
	MP3  Format = "mp3"
	M4A  Format = "m4a"
	FLAC Format = "flac"
)

// String returns the format name, such as "flac".
func (f Format) String() string { return string(f) }

// File is the common interface implemented by every reader in this module. Use
// package tags to obtain one without naming a format.
type File interface {
	// Format reports the container this file was read from.
	Format() Format
	// Tags returns the normalized metadata fields. The result must not be
	// modified.
	Tags() Tag
	// Audio returns the properties of the audio stream itself, such as its
	// duration and bitrate.
	Audio() Audio
	// Pictures returns the embedded artwork, if any.
	Pictures() []Picture
}
