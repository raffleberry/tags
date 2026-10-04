# tags

Read audio metadata in Go from MP3, MP4 and FLAC files.

This library reads. It does not modify files.

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

One package per container. Shared types are in `tag`. Format detection is in
`tags`.

| Package | Reads | Notes |
| --- | --- | --- |
| `tags` | any of the three | Auto-detection by content, not extension |
| `tag` | — | The normalized key space, `Tag`, `Audio`, `Picture` |
| `mp3` | `.mp3` `.mp2` `.mpga` | MPEG frame headers, Xing/Info/VBRI, LAME, ID3v1 and ID3v2 |
| `m4a` | `.m4a` `.m4b` `.m4p` `.mp4` | Calls `mp4` |
| `mp4` | — | The atom tree, the iTunes metadata list, AAC/ALAC/AC-3 properties |
| `flac` | `.flac` | Metadata blocks, Vorbis comments, pictures, seek table |
| `id3` | — | ID3v2.2, 2.3 and 2.4 frames; ID3v1 |
| `ape` | — | APEv2 trailers, as appended to MP3 files |

`mp4`, `flac` and `id3` are also tag formats in other containers. MP4 files
store tags in `mp4`. Files can store ID3 tags.

## The common interface

Every reader implements one interface. Callers read a title without naming a
format.

```go
type File interface {
    Format() Format
    Tags() Tag
    Audio() Audio
    Pictures() []Picture
}
```

`Tags()` returns a `map[string][]string` keyed by lowercase names. Keys are
listed as constants in package `tag`. Lowercase custom keys before lookup.

## Normalization

Every container names its fields differently: ID3v2 uses four character frame
IDs such as `TIT2`, iTunes uses atoms such as `©nam`, FLAC uses Vorbis comments
such as `TITLE`. A `Tag` is the format independent view of all three.

A native name with a known common meaning maps to its common key:

| | ID3v2 | iTunes | Vorbis comment |
| --- | --- | --- | --- |
| `title` | `TIT2` | `©nam` | `TITLE` |
| `artist` | `TPE1` | `©ART` | `ARTIST` |
| `album` | `TALB` | `©alb` | `ALBUM` |
| `date` | `TDRC` | `©day` | `DATE` |
| `genre` | `TCON` | `gnre` | `GENRE` |

Other names keep their lowercased native name. All fields are kept:

```go
// A TXXX ReplayGain value and an MP4 freeform atom both use this key.
tags.Value(tag.ReplayGainTrackGain)

// A field no common key covers keeps its own name.
tags.Value("ripper")
```

Positions are split. Containers record them as `"3/11"` or as two numbers.
`tag.Track` holds `"3"`. `tag.TrackTotal` holds `"11"`.

Artwork is binary, not text. It is not in `Tags()`. It is in `Pictures()`.
FLAC records full metadata. MP3 and MP4 record only image bytes.

## Beyond the common view

Normalization omits format-specific data. Each format package provides complete
data for callers that need it.

```go
f, _ := mp3.Open("song.mp3")

f.Stream().VBRHeader   // the Xing header, with LAME settings
f.Stream().Sketchy     // whether the frame chain is confirmed
f.V1()                 // the trailing ID3v1 tag
f.APE()                // the trailing APEv2 tag, if present
f.Chapters()           // the CHAP frames of the ID3v2 tag

t, _ := mp4.Open("book.m4b")
t.ILST().Fields        // the iTunes metadata list
t.Atoms()              // the atom tree
t.Chapters()           // the Nero chapter list, if present

fl, _ := flac.Open("song.flac")
fl.SeekTable()         // seekable frames
fl.CueSheet()          // the cue sheet, if present
fl.Blocks()            // all metadata blocks, including undecoded blocks
```

## Detection

`tags.Open` and `tags.Read` identify a file from content. Misnamed files still
read. `tags.Detect` returns the format only. `tags.OpenAs` requires a specific
format.

```go
format, err := tags.Detect("song.mp3")  // returns flac if content is FLAC
```

For a directory scan, check the extension first:

```go
if !tags.LooksLike(name) {
    return nil // not audio
}
```

## Scope

This module reads three containers. It does not cover:

- Writing. This module does not modify files.
- Encrypted ID3v2 frames. They are skipped. Compressed and grouped frames are
  decoded. APEv2 trailers on MP3 files are read and merged with ID3v2 values.
- Some chapter formats. Supported: ID3 `CHAP`/`CTOC` frames (`id3.Chapter`),
  FLAC cue sheet blocks (`flac.CueSheet`), Nero chapter lists (`mp4.Chapter`).
  QuickTime chapter tracks are not modeled. Use `mp4.Atoms` to access them.

## Tests

Tests use sample files in [`testdata/mutagen`](testdata/mutagen). The set
includes valid and damaged files. Damaged files test error paths.

```
go test ./...
```

## Licence

MIT. See [LICENSE](LICENSE).

The files under `testdata/` come from mutagen. They use GPL-2.0-or-later. See
[testdata/mutagen/README.md](testdata/mutagen/README.md). No mutagen code is
compiled into this module.