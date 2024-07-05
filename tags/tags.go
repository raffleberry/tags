package tags

/*
// see -DCMAKE_INSTALL_PREFIX in `bake`

#cgo CFLAGS: -I/usr/local/include/taglib
#cgo LDFLAGS: -L/usr/local/lib/ -ltag_c -ltag -lstdc++ -lm -lz -static
#include <stdlib.h>
#include "tag_c.h"

char* get(char** arr, int i) { return arr[i]; }

char* to_char_arr(void* arr) { return arr; }

*/
import "C"
import (
	"errors"
	"sync"
	"unsafe"
)

func init() {
	C.taglib_set_string_management_enabled(0)
}

const (
	Type_MPEG = iota
	Type_OggVorbis
	Type_FLAC
	Type_MPC
	Type_OggFlac
	Type_WavPack
	Type_Speex
	Type_TrueAudio
	Type_MP4
	Type_ASF
	Type_AIFF
	Type_WAV
	Type_APE
	Type_IT
	Type_Mod
	Type_S3M
	Type_XM
	Type_Opus
	Type_DSF
	Type_DSDIF
)

type File struct {
	mu sync.Mutex

	iostream_c *C.TagLib_IOStream
	file_c     *C.TagLib_File
	tag_c      *C.TagLib_Tag
	audio_c    *C.TagLib_AudioProperties

	// Tag api.
	Tag struct {
		Title   string
		Artist  string
		Album   string
		Comment string
		Genre   string
		// 0 if the year is not set.
		Year int
		// 0 if the track number is not set.
		Track int
	}

	// Audio Properties api.
	Audio struct {
		// length of the file in seconds.
		Length int
		// bitrate of the file in kb/s.
		Bitrate int
		// sample rate of the file in Hz.
		Samplerate int
		// the number of channels in the audio stream.
		Channels int
	}

	// Properties api.
	Props map[string][]string

	// Complex Properties api.
	Complex any
}

var (
	ErrInvalidFile = errors.New("invalid file")
)

func fromCs(cs *C.char) string {
	if cs == nil {
		return ""
	}
	defer C.taglib_free(unsafe.Pointer(cs))
	return C.GoString(cs)
}

func fromCsArr(a_cs **C.char) []string {
	var r []string
	defer C.taglib_property_free(a_cs)
	for i := 0; ; i++ {
		cs := C.get(a_cs, C.int(i))
		if cs == nil {
			break
		}
		r = append(r, C.GoString(cs))
	}
	return r
}

// taglib tries to guess the file type
func New(filename string) (*File, error) {
	var f File

	fp_cs := C.CString(filename)
	defer C.free(unsafe.Pointer(fp_cs))

	f.file_c = C.taglib_file_new(fp_cs)
	if f.file_c == nil || int(C.taglib_file_is_valid(f.file_c)) == 0 {
		return &f, ErrInvalidFile
	}

	return &f, nil
}

// you tell taglib the file type
//
// filetype can be : tags.Type_*
func NewFromType(filename string, filetype int) (*File, error) {
	var f File

	filename_cs := C.CString(filename)
	defer C.free(unsafe.Pointer(filename_cs))

	f.file_c = C.taglib_file_new_type(filename_cs, C.TagLib_File_Type(filetype))
	if f.file_c == nil || int(C.taglib_file_is_valid(f.file_c)) == 0 {
		return &f, ErrInvalidFile
	}

	return &f, nil
}

func NewFromMemory(b []byte) (*File, error) {
	var f File
	stream_c := C.CBytes(b)
	defer C.free(stream_c)

	f.iostream_c = C.taglib_memory_iostream_new(C.to_char_arr(stream_c), C.uint(len(b)))

	f.file_c = C.taglib_file_new_iostream(f.iostream_c)

	if f.file_c == nil || int(C.taglib_file_is_valid(f.file_c)) == 0 {
		return &f, ErrInvalidFile
	}

	return &f, nil

}

func (f *File) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()

	C.taglib_file_free(f.file_c)

	C.taglib_iostream_free(f.iostream_c)
}
