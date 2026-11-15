# perceptual

A command line tool that scores how alike two media files are — two pictures,
two recordings, two videos — using the perceptual hashes in
[go-perceptual](https://github.com/go-perceptual).

It answers the question a download folder raises: *are these the same thing at
a different size or in a different format?*

```console
$ perceptual compare uhd.jpg hd.jpg
image  score 0.9844  distance 2  the same picture

$ perceptual compare album.flac album.mp3
audio  score 1.0000  the same recording

$ perceptual compare film-360p.mp4 film-180p.mp4
video  score 0.9859  a match
```

## Installing

```console
go install github.com/go-perceptual/perceptual@latest
```

## Commands

### `perceptual hash <file>`

Prints the file's perceptual hash, and what kind of file it turned out to be.

```console
$ perceptual hash photo.jpg
image  f8f8f0cee0f4a84f06370a22028f67f0b36e2ed596621e1d33e6b39c4e9c9b22  quality 100

$ perceptual hash clip.mp4
video  30 frame(s) at 15 fps

$ perceptual hash album.flac
audio  26 feature frame(s), decoded at 44100 Hz
```

### `perceptual compare <a> <b>`

Scores the two files on a scale where 1 is the same thing.

Both commands take `--json` for a machine-readable result.

Comparing a picture with a recording is **refused**, not scored. There is no
scale on which the two can be compared, and returning zero would read as
*measured, and they differ*.

## What the numbers mean

The tool reads what a file **holds**, not what it is called. A `.jpg` holding
a PNG, or an `.mp4` holding only audio, are ordinary things in a download
folder, and believing the extension produces a confident wrong answer.

### Images — PDQ, a Hamming distance over 256 bits

The bands come from the PDQ specification rather than from this tool:

| distance | meaning |
| --- | --- |
| 0 | identical hashes |
| ≤ 20 | the same picture |
| ≤ 30 | probably the same picture |
| 128 | the average of two unrelated pictures |

Measured here, on one photograph:

| pair | distance |
| --- | --- |
| 4K vs 1080p | 0 |
| 4K vs 360p | 4 |
| 4K vs a 160×90 thumbnail | 14 |
| JPEG quality 95 vs quality 25 | 0 |
| JPEG quality 95 vs quality 10 | 2 |
| two unrelated photographs | 128 |

### Audio — chroma features, matched at the best alignment

Audio matches at **0.9**, not at the 0.7 that reads naturally beside the video
threshold, and the difference was measured rather than chosen. A chroma
feature is a profile of which pitch classes are present, compared frame by
frame; two recordings built from the same notes therefore agree substantially
even when they are not the same piece.

Against an eight-chord melody:

| pair | score |
| --- | --- |
| the same file | 1.000 |
| the same melody cut two seconds short | 0.9993 (alignment found at offset 16) |
| the same melody at 48 kHz instead of 44.1 kHz | 0.99999999980 |
| **the same melody played backwards** | **0.7064** |
| an unrelated chord | 0.4374 |

A 0.7 gate admits the reversed melody. The threshold belongs in the gap
between 0.71 and 0.9993, and 0.9 sits in it. A test in this repository fails
if it is lowered.

The headroom is **smaller than that table suggests**, and the reason matters.
A reversal control's score depends on how many distinct chords there are to
reverse: with eight it scores 0.7064, but with **four** it scores **0.8176**,
because fewer distinct chords means more of the piece overlaps itself. The
margin above the threshold is therefore 0.08, not 0.19 — which is why 0.85
would be the wrong choice, and why the threshold is pinned by a test rather
than left as a number someone might round.

### Audio across real codecs

These are the same melody put through four real encoders and read back
through go-avkit's decoders, not through a filter standing in for one:

| pair | score |
| --- | --- |
| FLAC vs MP3 96k | 0.999999039 |
| FLAC vs Opus 64k | 0.999997224 |
| FLAC vs AAC 96k | 0.999996334 |
| MP3 vs Opus | 0.999995883 |
| MP3 vs AAC | 0.999995271 |
| Opus vs AAC | 0.999994444 |

Every pair is tested, including the ones where neither side is lossless —
which is what a download folder is full of, and what a comparison against a
single reference file never exercises.

Uncompressed PCM and the FLAC decoding of the same samples score **exactly
1.000000000000**. The two paths share no code — one is go-avkit's FLAC
decoder, the other this repository's PCM passthrough — so that figure
validates both at once, and a drift in either shows up as a failure rather
than as a slightly worse match that nothing notices.

Note the 0.4374 for an unrelated pair. A signal that moves through many pitch
classes partially matches anything — a *stationary* chord against an unrelated
one scored 0.0000453. A varied recording therefore has a higher floor than a
sustained one, which is an argument for the threshold sitting well above any
observed non-match rather than merely above the average one.

Sample rate is converted with a polyphase resampler before the feature is
taken, because the rate is **part of the feature**: the frame geometry and the
frequency bounds are expressed in samples at 11025 Hz, so audio fed at another
rate produces a perfectly plausible fingerprint that matches nothing.

### Video — TMK+PDQF, matched at 0.7

0.7 is the threshold TMK+PDQF states for itself.

Measured on a two-second H.264 clip and scaled copies of it:

| pair | score |
| --- | --- |
| half scale (640×360 → 320×180) | 0.9859 |
| quarter scale (640×360 → 160×90) | 0.9882 |
| a different clip | 0.0104 |

Frames are resampled to 15 fps **by time**, not by taking every nth frame: the
kernel compares phases at integer frame indices, so a 30 fps and a 25 fps copy
of the same film must land on the same indices, which happens only if each
index is filled by the frame showing at that instant.

#### A caution about synthetic fixtures

An earlier version of this measurement scored the same content at two
resolutions at **1.0000**, with no codec anywhere in the path — two scalings of
one image, hashed directly. That measures PDQ's scale invariance, which is
nearly perfect by construction, and nothing else. The 0.986 above includes an
H.264 encode, 4:2:0 chroma subsampling, a Lanczos downscale and a real decode.
Both numbers are true; only the second answers the question a file manager asks.

A related trap, met while building the fixtures: ffmpeg's `testsrc2` rendered
independently at 640×360 and 320×180 scores **0.3467**, because the filter draws
its pattern parametrically for the target size — different cell sizes,
different text — so the two clips genuinely hold different pictures. That
looked like a hash defect and was a fixture defect. A 4K and a 1080p copy of
one film are a *downscale*; the fixtures here are scaled copies of one clip.

## Formats it reads

Containers and codecs come from [go-avkit](https://github.com/go-avkit):
MP4, Matroska/WebM, MPEG-TS, Ogg, WAV, AVI, native FLAC, ADTS and MPEG audio;
H.264 and H.265 video; FLAC, MP3, Opus, AAC-LC and HE-AAC audio. Images go
through Go's own decoders: JPEG, PNG and GIF.

### Decoding lives in go-perceptual/media

Classification, decoding, downmixing, resampling and frame wrapping are
[go-perceptual/media](https://github.com/go-perceptual/media)'s. They used to
be here, copied into `dedupe` as well, and every one of them encodes a
decision that goes wrong *silently* if two copies drift apart — a downmix that
takes one channel compares a one-channel recording against silence, PCM read
at the wrong sample width is noise that still hashes, a 10-bit frame
truncated to 8 gives one film two fingerprints. None of those crash. They
produce two tools that disagree about whether two files are the same thing.

What stays here is this tool's own business: which feature, at which rate,
which threshold, and what to print.

### Linear PCM

A WAV holds samples that already *are* PCM, so `go-avkit/decode` refuses
them — rightly, since there is nothing to decode — which leaves WAV, the
simplest format in its list, unreachable through the decoder API. `media`
carries the passthrough, including the sample width that the track
configuration does not state and that has to be derived and then asserted.
See its README for the measurements; reported upstream with an implementation
offered.

## Where the test fixtures come from

They are committed so that CI runs the real decoders on a machine with no
ffmpeg, and their provenance is written down here rather than remembered.

The audio source is four seconds of a four-chord melody, generated by
`TestGenerateAudioSource` (`GENERATE_DIR=<dir> go test -run
TestGenerateAudioSource`). The 1.4 MB WAV is not committed; the encodings are:

```console
ffmpeg -i melody-source.wav -c:a flac -compression_level 12 testdata/melody.flac
ffmpeg -i melody-source.wav -c:a libmp3lame -b:a 96k              testdata/melody.mp3
ffmpeg -i melody-source.wav -c:a libopus   -b:a 64k               testdata/melody.opus
ffmpeg -i melody-source.wav -c:a aac       -b:a 96k               testdata/melody.m4a
```

The video fixtures are a two-second clip and **scaled copies of that clip**,
which is what a 4K and a 1080p release of one film are:

```console
ffmpeg -f lavfi -i "testsrc2=size=640x360:rate=15:duration=2" \
  -c:v libx264 -preset veryslow -crf 30 -pix_fmt yuv420p testdata/clip-360p.mp4
ffmpeg -i testdata/clip-360p.mp4 -vf "scale=320:180:flags=lanczos" \
  -c:v libx264 -preset veryslow -crf 30 -pix_fmt yuv420p testdata/clip-180p.mp4
ffmpeg -i testdata/clip-360p.mp4 -vf "scale=160:90:flags=lanczos" \
  -c:v libx264 -preset veryslow -crf 32 -pix_fmt yuv420p testdata/clip-90p.mp4
ffmpeg -f lavfi -i "smptebars=size=640x360:rate=15:duration=2" \
  -c:v libx264 -preset veryslow -crf 30 -pix_fmt yuv420p testdata/clip-other.mp4
```

Note that `clip-180p.mp4` is derived from `clip-360p.mp4`, not rendered
independently at that size — see the caution above for what happens otherwise.

Image fixtures are generated inside the tests, since Go's own encoders need no
external tool.

## It reports, and never acts

There is no `delete`, no `--prune`, no `--force`. A perceptual match is a
judgement with a threshold, and a threshold is sometimes wrong; a tool that
deletes on one destroys the only evidence that it was wrong. A test in this
repository fails if such a command or flag is ever added.

For sweeping a whole directory tree, see
[go-perceptual/dedupe](https://github.com/go-perceptual/dedupe), which groups
files and reports the groups — also without deleting anything.

## Licence

MIT.
