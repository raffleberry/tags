# tags

Audio metadata for Go, read from MP3, MP4 and FLAC files.

This library reads. Nothing here modifies a file.

```go
f, err := tags.Open("song.m4a")
if err != nil {
    return err
}

fmt.Println(f.Tags().Value(tag.Title))
fmt.Println(f.Audio().Duration, f.Audio().Bitrate)
for _, p := range f.Pictures() {
    fmt.Println(p.MIME, len(p.Data))
}
```

## Packages

One package per container, plus a shared vocabulary and a front door that
guesses.

| Package | Reads | Notes |
| --- | --- | --- |
| `tags` | any of the three | Auto-detection by content, not extension |
| `tag` | — | The normalized key space, `Tag`, `Audio`, `Picture` |
| `mp3` | `.mp3` `.mp2` `.mpga` | MPEG frame headers, Xing/Info/VBRI, LAME, ID3v1 and ID3v2 |
| `m4a` | `.m4a` `.m4b` `.m4p` `.mp4` | A front door to `mp4` |
| `mp4` | — | The atom tree, the iTunes metadata list, AAC/ALAC/AC-3 properties |
| `flac` | `.flac` | Metadata blocks, Vorbis comments, pictures, seek table |
| `id3` | — | ID3v2.2, 2.3 and 2.4 frames; ID3v1 |

`mp4`, `flac` and `id3` exist because those formats are also the tag formats of
other containers: an MP4 file's tags are in `mp4`, and an ID3 tag is reachable
from any file that carries one.

## The common interface

Every reader implements one interface, so code that only wants a title does not
have to name a format.

```go
type File interface {
    Format() Format
    Tags() Tag
    Audio() Audio
    Pictures() []Picture
}
```

`Tags()` returns a `map[string][]string` keyed by lowercase names. The keys are
listed as constants in package `tag`; a caller with a name of its own should
lowercase it before looking it up.

## Normalization

Every container names its fields differently: ID3v2 uses four character frame
IDs such as `TIT2`, iTunes uses atoms such as `©nam`, FLAC uses Vorbis comments
such as `TITLE`. A `Tag` is the format independent view of all three.

A native name with a known common meaning is folded onto its common key:

| | ID3v2 | iTunes | Vorbis comment |
| --- | --- | --- | --- |
| `title` | `TIT2` | `©nam` | `TITLE` |
| `artist` | `TPE1` | `©ART` | `ARTIST` |
| `album` | `TALB` | `©alb` | `ALBUM` |
| `date` | `TDRC` | `©day` | `DATE` |
| `genre` | `TCON` | `gnre` | `GENRE` |

Anything else keeps its own lowercased name, so nothing is dropped:

```go
// A TXXX frame's ReplayGain value and an MP4 freeform atom both land here.
tags.Value(tag.ReplayGainTrackGain)

// A field no common key covers keeps its own name.
tags.Value("ripper")
```

Positions are split, because containers record them either as `"3/11"` or as two
separate numbers: `tag.Track` holds `"3"` and `tag.TrackTotal` holds `"11"`.

Artwork is not text, so it is not in `Tags()`. It comes back from `Pictures()`,
where FLAC records the full description and the other two only the image bytes.

## Beyond the common view

Normalization is a lossy, uniform view, so each format package keeps the whole
picture reachable for code that needs more than a title.

```go
f, _ := mp3.Open("song.mp3")

f.Stream().VBRHeader   // the Xing header, with the LAME settings
f.Stream().Sketchy     // whether the frame chain could be confirmed
f.V1()                 // the trailing ID3v1 tag

t, _ := mp4.Open("book.m4b")
t.ILST().Fields        // the metadata list, as iTunes wrote it
t.Atoms()              // the atom tree, for chapters and the rest

fl, _ := flac.Open("song.flac")
fl.SeekTable()         // the frames a player can seek to
fl.Blocks()            // every metadata block, including the ones not decoded
```

## Detection

`tags.Open` and `tags.Read` identify a file from its content, so a mislabelled
download still reads. `tags.Detect` answers the question on its own, and
`tags.OpenAs` insists on a format when the caller already knows it.

```go
format, err := tags.Detect("song.mp3")  // flac, if that is what it is
```

For a large directory, the cheap check comes first:

```go
if !tags.LooksLike(name) {
    return nil // not audio
}
```

## Scope

Reading only, and three containers. Not covered:

- Writing. Nothing here modifies a file.
- APEv2 tags, which some MP3 files carry alongside ID3. An MP3 file tagged only
  with APEv2 has no tags here, though its audio properties are still read.
- ID3v2 frames that are compressed, encrypted or grouped, which are skipped
  rather than guessed at.
- Chapters, cue sheets and other structures: the data is reachable through each
  package but not modelled.

## Tests

The test suite runs against the sample files in
[`testdata/mutagen`](testdata/mutagen), a corpus of real and deliberately damaged
files that exercise the error paths as well as the happy ones.

```
go test ./...
```

## Licence

MIT. See [LICENSE](LICENSE).

The files under `testdata/` come from mutagen and are GPL-2.0-or-later rather
than MIT; see [testdata/mutagen/README.md](testdata/mutagen/README.md). No
mutagen code is compiled into this module.