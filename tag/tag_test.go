package tag

import (
	"strings"
	"testing"
	"time"
)

func TestValue(t *testing.T) {
	tags := New(
		"title", "A Title",
		"artist", "An Artist",
	)

	if got, want := tags.Value("title"), "A Title"; got != want {
		t.Errorf("Value(title) = %q, want %q", got, want)
	}
	if got, want := tags.Value("artist"), "An Artist"; got != want {
		t.Errorf("Value(artist) = %q, want %q", got, want)
	}
	// A key that is not there has no value, which is "" rather than an error.
	if got := tags.Value("album"); got != "" {
		t.Errorf("Value(album) = %q, want empty", got)
	}
}

func TestValueSkipsEmpty(t *testing.T) {
	tags := New("artist", "An Artist")
	tags.Add("artist", "", "Another Artist")

	// The first empty value is not useful, so the first non empty one is
	// returned instead.
	if got, want := tags.Value("artist"), "An Artist"; got != want {
		t.Errorf("Value(artist) = %q, want %q", got, want)
	}
}

func TestValues(t *testing.T) {
	tags := New("artist", "An Artist")
	tags.Add("artist", "Another Artist")

	if got, want := tags.Values("artist"), []string{"An Artist", "Another Artist"}; len(got) != 2 {
		t.Errorf("Values(artist) = %q, want %q", got, want)
	}
	if got := tags.Values("album"); len(got) != 0 {
		t.Errorf("Values(album) = %q, want none", got)
	}
}

// TestValuesIsACopy covers the returned slice being separate from the tag, so
// that changing it does not change the tag.
func TestValuesIsACopy(t *testing.T) {
	tags := New("artist", "An Artist")

	values := tags.Values("artist")
	values[0] = "Someone Else"
	if got, want := tags.Value("artist"), "An Artist"; got != want {
		t.Errorf("Value(artist) = %q, want %q", got, want)
	}
}

func TestInt(t *testing.T) {
	tags := New("bpm", "120", "track", "7")

	if got, ok := tags.Int("bpm"); !ok || got != 120 {
		t.Errorf("Int(bpm) = %d, %v, want 120, true", got, ok)
	}
	if got, ok := tags.Int("track"); !ok || got != 7 {
		t.Errorf("Int(track) = %d, %v, want 7, true", got, ok)
	}
	// A field that is not a number is not an error, just no value.
	if _, ok := tags.Int("title"); ok {
		t.Error("Int(title) = ok, want false")
	}
	if _, ok := tags.Int("album"); ok {
		t.Error("Int(album) = ok, want false")
	}
}

func TestBool(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"1", true},
		{"true", true},
		{"TRUE", true},
		{"yes", true},
		{" 1 ", true},
		{"0", false},
		{"false", false},
		{"no", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			tags := New("compilation", tt.value)
			if got := tags.Bool("compilation"); got != tt.want {
				t.Errorf("Bool(compilation) = %v, want %v", got, tt.want)
			}
		})
	}

	// A key that is not there is not set.
	if New().Bool("compilation") {
		t.Error("Bool on an empty tag = true, want false")
	}
}

func TestSetAndAdd(t *testing.T) {
	tags := Tag{}

	tags.Set("title", "A Title", "Another Title")
	if got, want := len(tags.Values("title")), 2; got != want {
		t.Errorf("len(Values(title)) = %d, want %d", got, want)
	}

	// Setting replaces rather than adding.
	tags.Set("title", "Only Title")
	if got, want := len(tags.Values("title")), 1; got != want {
		t.Errorf("len(Values(title)) = %d, want %d", got, want)
	}
	if got, want := tags.Value("title"), "Only Title"; got != want {
		t.Errorf("Value(title) = %q, want %q", got, want)
	}

	// Setting with no values removes the key.
	tags.Set("title")
	if _, present := tags["title"]; present {
		t.Error("Set with no values left the key, want it removed")
	}
}

func TestSetDefault(t *testing.T) {
	tags := New("title", "A Title")

	if got := tags.SetDefault("title", "Another Title"); got {
		t.Error("SetDefault on a present key = true, want false")
	}
	if got, want := tags.Value("title"), "A Title"; got != want {
		t.Errorf("Value(title) = %q, want %q", got, want)
	}

	if got := tags.SetDefault("album", "An Album"); !got {
		t.Error("SetDefault on an absent key = false, want true")
	}
	if got, want := tags.Value("album"), "An Album"; got != want {
		t.Errorf("Value(album) = %q, want %q", got, want)
	}

	// An empty value counts as absent, since it says nothing.
	tags.Set("artist", "")
	if got := tags.SetDefault("artist", "An Artist"); !got {
		t.Error("SetDefault on an empty value = false, want true")
	}
}

func TestDelete(t *testing.T) {
	tags := New("title", "A Title")
	tags.Delete("title")

	if got := len(tags); got != 0 {
		t.Errorf("len(tag) = %d, want 0", got)
	}
	// Deleting a key that is not there is not an error.
	tags.Delete("title")
}

func TestKeys(t *testing.T) {
	tags := New("title", "A Title", "album", "An Album", "artist", "An Artist")

	got := tags.Keys()
	want := []string{"album", "artist", "title"}
	if len(got) != len(want) {
		t.Fatalf("Keys() = %q, want %q", got, want)
	}
	// Sorted, so the order does not depend on how the map happens to be laid out.
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("Keys()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestClone(t *testing.T) {
	tags := New("artist", "An Artist")
	tags.Add("artist", "Another Artist")

	clone := tags.Clone()
	clone.Set("artist", "Someone Else")
	clone.Set("title", "A Title")

	// The original is unchanged, including the values of the key that was
	// replaced.
	if got, want := len(tags.Values("artist")), 2; got != want {
		t.Errorf("len(Values(artist)) = %d, want %d", got, want)
	}
	if _, present := tags["title"]; present {
		t.Error("Clone added a key to the original")
	}
}

// TestNewIgnoresDanglingValue covers an odd number of arguments, which cannot be
// paired up.
func TestNewIgnoresDanglingValue(t *testing.T) {
	tags := New("title", "A Title", "artist")
	if got, want := tags.Value("title"), "A Title"; got != want {
		t.Errorf("Value(title) = %q, want %q", got, want)
	}
	if _, present := tags["artist"]; present {
		t.Error("New kept a key with no value")
	}
}

func TestSplitTotal(t *testing.T) {
	tests := []struct {
		value    string
		position string
		total    string
	}{
		{"3/11", "3", "11"},
		{"3 / 11", "3", "11"},
		{"3", "3", ""},
		{"", "", ""},
		{"/11", "", "11"},
		{"3/", "3", ""},
		// A value with more than one slash keeps everything after the first.
		{"3/11/12", "3", "11/12"},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			position, total := SplitTotal(tt.value)
			if position != tt.position {
				t.Errorf("position = %q, want %q", position, tt.position)
			}
			if total != tt.total {
				t.Errorf("total = %q, want %q", total, tt.total)
			}
		})
	}
}

func TestJoinTotal(t *testing.T) {
	tests := []struct {
		position string
		total    string
		want     string
	}{
		{"3", "11", "3/11"},
		{"3", "", "3"},
		{"", "11", "/11"},
		{"", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := JoinTotal(tt.position, tt.total); got != tt.want {
				t.Errorf("JoinTotal(%q, %q) = %q, want %q", tt.position, tt.total, got, tt.want)
			}
		})
	}
}

// TestSplitAndJoin covers the two together, which is how a track number is split
// apart and put back.
func TestSplitAndJoin(t *testing.T) {
	position, total := SplitTotal("7/12")
	if got, want := JoinTotal(position, total), "7/12"; got != want {
		t.Errorf("JoinTotal = %q, want %q", got, want)
	}
}

// TestBoolRendersTheID3Way covers the helper that writes a flag the way the
// ID3v2.3 specification has it, as a single digit.
func TestBoolRendersTheID3Way(t *testing.T) {
	tests := []struct {
		value bool
		want  string
	}{
		{true, "1"},
		{false, "0"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := Bool(tt.value); got != tt.want {
				t.Errorf("Bool(%v) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestFormat(t *testing.T) {
	formats := []Format{MP3, M4A, FLAC}
	for _, f := range formats {
		if f.String() == "" {
			t.Errorf("Format(%v).String() is empty", f)
		}
	}
}

func TestAudioString(t *testing.T) {
	audio := Audio{
		Codec:       "MP4A40",
		Bitrate:     128000,
		BitrateMode: BitrateVBR,
		SampleRate:  44100,
		Channels:    2,
		Duration:    3 * time.Second,
	}

	got := audio.String()
	for _, want := range []string{"MP4A40", "128 kbps", "VBR", "44100 Hz", "2 ch", "3.00"} {
		if !strings.Contains(got, want) {
			t.Errorf("Audio.String() = %q, want it to mention %q", got, want)
		}
	}

	// A stream that says nothing still describes itself.
	empty := Audio{Duration: time.Second}.String()
	if !strings.Contains(empty, "unknown codec") {
		t.Errorf("Audio.String() = %q, want it to say the codec is unknown", empty)
	}
}

func TestBitrateModeString(t *testing.T) {
	tests := []struct {
		mode BitrateMode
		want string
	}{
		{BitrateUnknown, "CBR?"},
		{BitrateCBR, "CBR"},
		{BitrateVBR, "VBR"},
		{BitrateABR, "ABR"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.mode.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPictureTypeString(t *testing.T) {
	// The names the ID3v2 specification gives, which the other containers copied.
	if got, want := PictureCoverFront.String(), "Cover (front)"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if got, want := PictureOther.String(), "Other"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	// A type outside the specification is named by its number, which is more
	// useful than a wrong name.
	if got, want := PictureType(200).String(), "Unknown (200)"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestGenres(t *testing.T) {
	// The list has to be non empty and hold names that are not numbers.
	if len(Genres) == 0 {
		t.Fatal("Genres is empty, want the ID3v1 list")
	}
	if got, want := Genres[0], "Blues"; got != want {
		t.Errorf("Genres[0] = %q, want %q", got, want)
	}
	if got, want := Genres[17], "Rock"; got != want {
		t.Errorf("Genres[17] = %q, want %q", got, want)
	}
	for i, name := range Genres {
		if name == "" {
			t.Errorf("Genres[%d] is empty", i)
		}
	}
}
