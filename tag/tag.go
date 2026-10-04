// Package tag defines the types used by every audio metadata reader in this
// module: the normalized [Tag] key space, the common [File] interface, and the
// [Audio] and [Picture] values.
//
// # Normalization
//
// Each container uses different field names: ID3v2 uses four character frame
// IDs such as "TIT2", iTunes uses atoms such as "©nam", FLAC uses Vorbis
// comments such as "TITLE". A [Tag] is the format independent view of that
// data. Keys are lowercase. Each key holds one or more string values.
//
// Normalization keeps all fields. A native name with a known common meaning
// maps to its common key. Other names keep their lowercased native name.
// Artwork is binary data. Pictures are in the [Picture] values returned by
// [File.Pictures]. They are not in [Tag].
//
// Fields not modeled here remain available in format packages: package id3 for
// frame level access to an ID3 tag, package mp4 for the atom tree, package
// flac for its metadata blocks.
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
