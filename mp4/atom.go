// Package mp4 reads MPEG-4 files with .m4a, .m4b, and .mp4 extensions.
//
// The container is a tree of atoms. Metadata is in the iTunes metadata list at
// "moov.udta.meta.ilst". [Read] converts that list to a [tag.Tag]. [Atoms]
// exposes the atom tree for data this package does not model, such as chapters.
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

// Atom is one node of the tree of boxes in an MPEG-4 file. The ISO
// specification calls them atoms. The QuickTime specification calls them boxes.
type Atom struct {
	// Name is the 4 character atom name, such as "moov" or "ilst". Atoms with a
	// 0xA9 prefix use "©" in the name.
	Name string
	// Offset is the offset of the atom start in the file.
	Offset int64
	// Length is the size of the whole atom, including the header.
	Length int64
	// Children holds the children of a container atom. It is nil for a leaf.
	Children []Atom
	// longLength records a 64 bit length field. The header is then 16 bytes.
	longLength bool
}

// headerLen is the size of an atom header: a length and a name.
const (
	headerLen     = 8
	longLenPrefix = 16
	// freeLen is the bytes after a "meta" atom header before children start: a
	// version and flags field.
	freeLen = 4
)

// containers lists the atoms parsed for children. It holds only atoms on the
// path to metadata and track data.
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

// childrenOffset returns the offset of children within a container atom.
// A "meta" atom holds 4 bytes of version and flags first.
func childrenOffset(name string) int64 {
	if name == "meta" {
		return freeLen
	}
	return 0
}

// Atoms reads the top level atoms of the file in r. R must be seekable.
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
				// Truncated files and trailing non-atom bytes stop parsing. Atoms
				// found so far remain usable.
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
// is false when there is no such atom.
func Find(atoms []Atom, name string) (Atom, bool) { return findChild(atoms, name) }

// Path returns the first atom found by following names through descendants. The
// second result is false when the path does not exist.
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

// readAtom reads the atom at offset. It parses children when the atom is a
// container.
func readAtom(r io.ReadSeeker, offset int64, depth int) (Atom, error) {
	// Depth is limited to 12. This stops infinite recursion on corrupt lengths.
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

	// Length 1 means a 64 bit length follows. Length 0 means the atom runs to
	// the end of the file.
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

	// Descend into the container. Parsing stops at the container end.
	end := offset + atom.Length
	pos, err := r.Seek(offset+headLen+childrenOffset(atom.Name), io.SeekStart)
	if err != nil {
		return atom, fmt.Errorf("mp4: seeking: %w", err)
	}
	for pos+headerLen <= end {
		child, err := readAtom(r, pos, depth+1)
		if err != nil {
			// A damaged child does not discard the parent atom.
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

// headerSize returns the header size in bytes: 8, or 16 with a 64 bit length.
func (a Atom) headerSize() int64 {
	if a.longLength {
		return longLenPrefix
	}
	return headerLen
}

// Data returns the payload of the atom: bytes after the header.
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

// atomName converts 4 raw bytes to an atom name. A leading 0xA9 byte is
// rendered as "©".
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
