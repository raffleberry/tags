package tags

/*
#include "tag_c.h"
#include <stdlib.h>
char* get(char** arr, int i) {
	return arr[i];
}

TagLib_Complex_Property_Attribute*** getPictureAttributes(TagLib_File *file) {
	TagLib_Complex_Property_Attribute*** pic_c = taglib_complex_property_get(file, "PICTURE");
	return pic_c;
}
*/
import "C"
import (
	"errors"
	"os"
	"runtime"
	"sync"
	"unicode"
	"unsafe"
)

const (
	ENCODING_LATIN_1 = C.int(0)
	ENCODING_UTF_8   = C.int(1)
)

var (
	ErrInvalidFile    = errors.New("invalid file")
	ErrNoPictureFound = errors.New("no embedded picture found")
)

func init() {
	C.taglib_set_string_management_enabled(0)
	C.taglib_set_strings_unicode(ENCODING_UTF_8)
}

type Info struct {
	mu sync.Mutex

	f_stream *C.TagLib_IOStream
	file_c   *C.TagLib_File
	tag_c    *C.TagLib_Tag
	audio_c  *C.TagLib_AudioProperties

	unicodePath bool

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
}

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

/*
fp - path to the file to be read
*/
func Read(fp string) (*Info, error) {
	var t Info

	if hasUnicodePath(fp) && runtime.GOOS == "windows" {
		// fopen doesn't work well with unicode paths on windows
		// (maybe it depends on compiler)
		data, err := os.ReadFile(fp)
		if err != nil {
			return nil, err
		}

		t.f_stream = C.taglib_memory_iostream_new((*C.char)(unsafe.Pointer(&data[0])), C.uint(len(data)))
		t.file_c = C.taglib_file_new_iostream(t.f_stream)
	} else {
		fp_cs := C.CString(fp)
		defer C.free(unsafe.Pointer(fp_cs))

		t.file_c = C.taglib_file_new(fp_cs)
	}

	if t.file_c == nil || int(C.taglib_file_is_valid(t.file_c)) == 0 {
		return &t, ErrInvalidFile
	}

	// read tag
	t.tag_c = C.taglib_file_tag(t.file_c)

	t.Tag.Title = fromCs(C.taglib_tag_title(t.tag_c))
	t.Tag.Artist = fromCs(C.taglib_tag_artist(t.tag_c))
	t.Tag.Album = fromCs(C.taglib_tag_album(t.tag_c))
	t.Tag.Comment = fromCs(C.taglib_tag_comment(t.tag_c))
	t.Tag.Genre = fromCs(C.taglib_tag_genre(t.tag_c))
	t.Tag.Year = int(C.taglib_tag_year(t.tag_c))
	t.Tag.Track = int(C.taglib_tag_track(t.tag_c))

	t.audio_c = C.taglib_file_audioproperties(t.file_c)
	// read audio properties
	t.Audio.Length = int(C.taglib_audioproperties_length(t.audio_c))
	t.Audio.Bitrate = int(C.taglib_audioproperties_bitrate(t.audio_c))
	t.Audio.Samplerate = int(C.taglib_audioproperties_samplerate(t.audio_c))
	t.Audio.Channels = int(C.taglib_audioproperties_channels(t.audio_c))

	// read properties
	props_csa := C.taglib_property_keys(t.file_c)

	p := map[string][]string{}

	for i := 0; ; i++ {
		k_cs := C.get(props_csa, C.int(i))
		k := C.GoString(k_cs)

		if k == "" {
			break
		}

		v_cs := C.taglib_property_get(t.file_c, k_cs)
		v := fromCsArr(v_cs)

		p[k] = v
	}
	t.Props = p
	C.taglib_property_free(props_csa)

	return &t, nil
}

func (t *Info) GetPicture() ([]byte, error) {

	props_c := C.getPictureAttributes(t.file_c)
	defer C.taglib_complex_property_free(props_c)

	if props_c == nil {
		return nil, ErrNoPictureFound
	}

	var pic_c C.TagLib_Complex_Property_Picture_Data

	C.taglib_picture_from_complex_property(props_c, &pic_c)

	pic := C.GoBytes(unsafe.Pointer(pic_c.data), C.int(pic_c.size))

	return pic, nil
}

func (t *Info) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.f_stream == nil {
		C.taglib_file_free(t.file_c)
	} else {
		C.taglib_iostream_free(t.f_stream)
	}
}

func hasUnicodePath(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > unicode.MaxASCII {
			return true
		}
	}
	return false
}
