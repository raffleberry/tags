// Package m4a reads MP4 audio files with .m4a, .m4b, .m4p, and .mp4 extensions.
// These files hold AAC, ALAC, or AC-3 audio in an MPEG-4 container.
//
// Parsing is in package mp4. This package wraps it. Use [Open] or [Read] for
// files. Use package mp4 for direct access to the atom tree.
//
// Tags are in the iTunes metadata list. They are normalized to the same keys
// as the other formats in this module.
package m4a

import (
	"io"

	"github.com/raffleberry/tags/mp4"
	"github.com/raffleberry/tags/tag"
)

// File is an MP4 audio file.
type File = mp4.File

// Open reads the MP4 audio file at path.
func Open(path string) (*File, error) { return mp4.Open(path) }

// Read reads an MP4 audio file from r. R must be seekable.
func Read(r io.ReadSeeker) (*File, error) { return mp4.Read(r) }

// Matches reports whether header looks like an MPEG-4 file. Header must hold
// at least the first 12 bytes of the file.
func Matches(header []byte) bool { return mp4.Matches(header) }

// Format reports that an MP4 audio file was read from.
const Format = tag.M4A

// Atom and ILST are re-exported from package mp4.
type (
	// Atom is one node of the tree of boxes in an MPEG-4 file.
	Atom = mp4.Atom
	// ILST is a parsed iTunes metadata list.
	ILST = mp4.ILST
)

// Atoms reads the top level atoms of the file in r. R must be seekable.
func Atoms(r io.ReadSeeker) ([]Atom, error) { return mp4.Atoms(r) }

// Find returns the first top level atom with the given name. The second result
// is false when there is no such atom.
func Find(atoms []Atom, name string) (Atom, bool) { return mp4.Find(atoms, name) }

// ParseILST reads an iTunes metadata list from the payload of an "ilst" atom.
func ParseILST(data []byte) *ILST { return mp4.ParseILST(data) }
