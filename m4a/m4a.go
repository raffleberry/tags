// Package m4a reads MP4 audio files: the .m4a, .m4b, .m4p and .mp4 files that
// hold AAC, ALAC or AC-3 audio in an MPEG-4 container.
//
// This package is the front door for those files; the parsing is in package mp4,
// which this one wraps. Use [Open] or [Read] here unless the atom tree itself is
// wanted, in which case package mp4 is the one to reach for.
//
// The tags live in the iTunes metadata list, where iTunes names them after
// copyright symbols, and they are normalized onto the same keys as the other
// formats this module reads.
package m4a

import (
	"io"

	"github.com/raffleberry/tags/mp4"
	"github.com/raffleberry/tags/tag"
)

// File is an MP4 audio file, which is the container behind the .m4a extension
// and its relatives.
type File = mp4.File

// Open reads the MP4 audio file at path.
func Open(path string) (*File, error) { return mp4.Open(path) }

// Read reads an MP4 audio file from r, which must be seekable.
func Read(r io.ReadSeeker) (*File, error) { return mp4.Read(r) }

// Matches reports whether header, which should hold at least the first twelve
// bytes of a file, looks like an MPEG-4 file.
func Matches(header []byte) bool { return mp4.Matches(header) }

// Format reports that an MP4 audio file was read from.
const Format = tag.M4A

// The atom tree and the metadata list are re-exported so that a caller working
// with MP4 files need not import a second package to reach them.
type (
	// Atom is one node of the tree of boxes an MPEG-4 file is made of.
	Atom = mp4.Atom
	// ILST is a parsed iTunes metadata list.
	ILST = mp4.ILST
)

// Atoms reads the top level atoms of the file in r, which is how a caller reaches
// the parts of the container that the tags do not cover, such as the chapter
// list.
func Atoms(r io.ReadSeeker) ([]Atom, error) { return mp4.Atoms(r) }

// Find returns the first top level atom with the given name, which is how a
// caller walks from the atoms towards a particular piece of the file. The
// second result is false when there is no such atom.
func Find(atoms []Atom, name string) (Atom, bool) { return mp4.Find(atoms, name) }

// ParseILST reads an iTunes metadata list from the payload of an "ilst" atom.
func ParseILST(data []byte) *ILST { return mp4.ParseILST(data) }
