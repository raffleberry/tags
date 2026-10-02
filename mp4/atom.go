// Package mp4 reads MPEG-4 files, the container behind .m4a, .m4b and .mp4
// audio.
//
// The container is a tree of atoms, and the metadata lives in the iTunes
// metadata list under "moov.udta.meta.ilst". [Read] finds that list and turns it
// into a [tag.Tag]; [Atoms] exposes the tree itself for anything this package
// does not model, such as chapters.
package mp4

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Errors reported while reading an MPEG-4 file.
var (
	// ErrNoFile means the data does not hold the atoms an MPEG-4 file needs.
	ErrNoFile = errors.New("mp4: not an MPEG-4 file")
	// ErrAtom means an atom header is truncated or its length is out of range.
	ErrAtom = errors.New("mp4: malformed atom")
)

// Atom is one node of the tree of boxes an MPEG-4 file is made of. Atoms are
// called atoms in the ISO specification and boxes in the QuickTime one.
type Atom struct {
	// Name is the four character atom name, such as "moov" or "ilst". Some
	// atoms use the 0xA9 prefix for a field iTunes named after a copyright
	// symbol, which is spelled out in the name.
	Name string
	// Offset is where the atom starts in the file.
	Offset int64
	// Length is the size of the whole atom, its header included.
	Length int64
	// Children of a container atom, and nil for a leaf. Padding atoms are
	// leaves here even though a file may nest anything inside them.
	Children []Atom
	// longLength records that the length was written as 64 bits, which makes
	// the header sixteen bytes rather than eight.
	longLength bool
}

// headerLen is the size of an atom header: a length, a name, and possibly a
// 64 bit length field.
const (
	headerLen     = 8
	longLenPrefix = 16
	// freeLen is how many bytes follow the header of a "meta" atom before its
	// children start: a version and flags field.
	freeLen = 4
)

// containers are the atoms this package descends into. It is deliberately not
// the full set: only the atoms on the path to the metadata and the track
// description are needed, and parsing every atom of a large file is wasted work.
var containers = map[string]bool{
	"moov": true,
	"trak": true,
	"mdia": true,
	"minf": true,
	"stbl": true,
	"udta": true,
	"meta": true,
	"ilst": true,
	"moof": true,
	"traf": true,
}

// childrenOffset is how many bytes into a container atom its children begin. A
// "meta" atom is a full box, so four bytes of version and flags come first.
func childrenOffset(name string) int64 {
	if name == "meta" {
		return freeLen
	}
	return 0
}

// Atoms reads the top level atoms of the file in r. r must be seekable.
func Atoms(r io.ReadSeeker) ([]Atom, error) {
	size, err := sizeOf(r)
	if err != nil {
		return nil, err
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("mp4: seeking: %w", err)
	}

	var atoms []Atom
	for offset := int64(0); offset+headerLen <= size; {
		atom, err := readAtom(r, offset, 0)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				// A file may be truncated or have trailing bytes that are not an
				// atom at all. The atoms found so far are still usable.
				break
			}
			return nil, err
		}
		atoms = append(atoms, atom)
		if atom.Length <= 0 {
			break
		}
		offset += atom.Length
	}
	if len(atoms) == 0 {
		return nil, ErrNoFile
	}
	return atoms, nil
}

// Find returns the first top level atom with the given name. The second result
// is false when there is none.
func Find(atoms []Atom, name string) (Atom, bool) { return findChild(atoms, name) }

// Path returns the first atom reachable from a by following the given names
// through its descendants. The second result is false when the path does not
// exist, which is normal for a file that does not hold, say, any tags.
func (a Atom) Path(names ...string) (Atom, bool) {
	current := a
	for _, name := range names {
		child, ok := current.Child(name)
		if !ok {
			return Atom{}, false
		}
		current = child
	}
	return current, true
}

func findChild(atoms []Atom, name string) (Atom, bool) {
	for _, atom := range atoms {
		if atom.Name == name {
			return atom, true
		}
	}
	return Atom{}, false
}

// readAtom reads the atom at offset, descending into it when it is a container.
func readAtom(r io.ReadSeeker, offset int64, depth int) (Atom, error) {
	// A file that nests atoms more deeply than this is not one a player would
	// write, and a cycle from a corrupt length would otherwise loop forever.
	const maxDepth = 12

	if _, err := r.Seek(offset, io.SeekStart); err != nil {
		return Atom{}, fmt.Errorf("mp4: seeking: %w", err)
	}
	header := make([]byte, headerLen)
	if _, err := io.ReadFull(r, header); err != nil {
		return Atom{}, err
	}

	atom := Atom{Offset: offset}
	atom.Name = atomName(header[4:8])

	// The length field has three meanings: a 64 bit length follows when it is 1,
	// and the atom runs to the end of the file when it is 0.
	size, headLen := int64(binary.BigEndian.Uint32(header[:4])), int64(headerLen)
	switch size {
	case 1:
		long := make([]byte, 8)
		if _, err := io.ReadFull(r, long); err != nil {
			return Atom{}, err
		}
		size, headLen = int64(binary.BigEndian.Uint64(long)), longLenPrefix
		atom.longLength = true
		if size < longLenPrefix {
			return Atom{}, fmt.Errorf("%w: 64 bit length of %d is too small", ErrAtom, size)
		}

	case 0:
		if depth > 0 {
			// Only a top level atom may run to the end of the file.
			return Atom{}, fmt.Errorf("%w: %s has a zero length", ErrAtom, atom.Name)
		}
		end, err := r.Seek(0, io.SeekEnd)
		if err != nil {
			return Atom{}, fmt.Errorf("mp4: seeking: %w", err)
		}
		size = end - offset

	default:
		if size < headerLen {
			return Atom{}, fmt.Errorf("%w: %s has a length of %d", ErrAtom, atom.Name, size)
		}
	}
	atom.Length = size

	if !containers[atom.Name] || depth >= maxDepth {
		return atom, nil
	}

	// Descend into the container, stopping at its end so a child that overruns
	// its parent cannot pull in atoms that belong to the next one.
	end := offset + atom.Length
	pos, err := r.Seek(offset+headLen+childrenOffset(atom.Name), io.SeekStart)
	if err != nil {
		return atom, fmt.Errorf("mp4: seeking: %w", err)
	}
	for pos+headerLen <= end {
		child, err := readAtom(r, pos, depth+1)
		if err != nil {
			// A damaged child should not cost us the atom holding it, which is
			// usually where the tags are.
			return atom, nil
		}
		atom.Children = append(atom.Children, child)
		if child.Length <= 0 {
			return atom, nil
		}
		if pos, err = r.Seek(pos+child.Length, io.SeekStart); err != nil {
			return atom, fmt.Errorf("mp4: seeking: %w", err)
		}
	}
	return atom, nil
}

// headerSize returns how many bytes of the atom are its header: eight normally,
// or sixteen when the length is written as 64 bits.
func (a Atom) headerSize() int64 {
	if a.longLength {
		return longLenPrefix
	}
	return headerLen
}

// Data returns the payload of the atom: everything after its header.
func (a Atom) Data(r io.ReadSeeker) ([]byte, error) {
	if _, err := r.Seek(a.Offset+a.headerSize(), io.SeekStart); err != nil {
		return nil, fmt.Errorf("mp4: seeking: %w", err)
	}
	data := make([]byte, a.Length-a.headerSize())
	n, err := io.ReadFull(r, data)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, fmt.Errorf("mp4: reading %s: %w", a.Name, err)
	}
	return data[:n], nil
}

// Child returns the first child with the given name.
func (a Atom) Child(name string) (Atom, bool) { return findChild(a.Children, name) }

// atomName renders the four bytes of an atom name. iTunes writes a 0xA9 byte
// where a copyright symbol belongs in names like "©nam", which is not printable
// on its own.
func atomName(raw []byte) string {
	name := strings.TrimRight(string(raw), "\x00")
	if name == "\xa9" {
		return "©"
	}
	if !strings.HasPrefix(name, "\xa9") {
		return name
	}
	return "©" + name[1:]
}
