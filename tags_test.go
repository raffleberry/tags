package tags_test

import (
	"testing"

	"github.com/raffleberry/tags"
)

func Eq(t *testing.T, want, got any) {
	if want != got {
		t.Fatalf("want = %v\ngot = %v\n", want, got)
	}
}

func TestRead(t *testing.T) {
	t.Run("tag,audio,properties", func(t *testing.T) {
		got, err := tags.Read("test/alexanders_ragtime_band__billy_murray.ogg")
		if err != nil {
			t.Fatal(err)
		}
		defer got.Close()

		Eq(t, "Alexander's ragtime band", got.Tag.Title)
		Eq(t, "Billy Murray", got.Tag.Artist)
		Eq(t, "Edison Amberol cylinder 4M-817", got.Tag.Album)
		Eq(t, "Popular songs, Tin Pan Alley", got.Tag.Genre)
		Eq(t, 1911, got.Tag.Year)

		Eq(t, 20, got.Audio.Length)
		Eq(t, 101, got.Audio.Bitrate)
		Eq(t, 44100, got.Audio.Samplerate)
		Eq(t, 1, got.Audio.Channels)

		e, ok := got.Props["ENCODER"]

		if !ok {
			t.Fatalf("`ENCODER` key not found in props: %v\n", got.Props)
		}

		Eq(t, len(e), 1)
		Eq(t, e[0], "Lavf55.21.100")

	})
}

func TestReadUnicode(t *testing.T) {
	t.Run("tag,audio,properties", func(t *testing.T) {
		got, err := tags.Read("test/alexanders_ragtime_band__billy_murray_üñîçõdë.ogg")
		if err != nil {
			t.Fatal(err)
		}
		defer got.Close()
		Eq(t, "Alexander's ragtime band", got.Tag.Title)
		Eq(t, "Billy Murray", got.Tag.Artist)
		Eq(t, "Edison Amberol cylinder 4M-817", got.Tag.Album)
		Eq(t, "Popular songs, Tin Pan Alley", got.Tag.Genre)
		Eq(t, 1911, got.Tag.Year)

		Eq(t, 20, got.Audio.Length)
		Eq(t, 101, got.Audio.Bitrate)
		Eq(t, 44100, got.Audio.Samplerate)
		Eq(t, 1, got.Audio.Channels)

		e, ok := got.Props["ENCODER"]

		if !ok {
			t.Fatalf("`ENCODER` key not found in props: %v\n", got.Props)
		}

		Eq(t, len(e), 1)
		Eq(t, e[0], "Lavf55.21.100")

	})
}
