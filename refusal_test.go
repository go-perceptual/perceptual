package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The tests in this file exercise the REFUSALS.
//
// ⛔ They are not here for a coverage number. Every branch below is a place
// where the tool could instead return a plausible answer: a hash of
// truncated 10-bit video, a feature taken from PCM read at the wrong width, a
// score between two files it cannot actually compare. A happy-path suite goes
// green while every one of those is broken, because a wrong answer and a
// right one have the same shape. What is tested here is that the tool says
// NO, and says why.

func TestHashEachKind(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "a.png")
	writePNG(t, img, 300, 200)

	for _, c := range []struct {
		path string
		want Kind
	}{
		{img, KindImage},
		{"testdata/melody.flac", KindAudio},
		{"testdata/clip-360p.mp4", KindVideo},
	} {
		out, err := run(t, "hash", "--json", c.path)
		if err != nil {
			t.Fatalf("hash %s: %v", c.path, err)
		}
		var res hashResult
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatalf("decoding %q: %v", out, err)
		}
		if res.Kind != c.want {
			t.Errorf("hash %s: kind %q, want %q", c.path, res.Kind, c.want)
		}
		switch c.want {
		case KindImage:
			// A PDQ hash is 256 bits: 64 hexadecimal digits, always.
			if len(res.Hash) != 64 {
				t.Errorf("hash %s: %d hex digits, want 64", c.path, len(res.Hash))
			}
			if res.Quality == 0 {
				t.Errorf("hash %s: quality 0 for a structured picture", c.path)
			}
		case KindAudio:
			if res.Frames == 0 || res.Rate == 0 {
				t.Errorf("hash %s: %d frames at %d Hz", c.path, res.Frames, res.Rate)
			}
		case KindVideo:
			if res.Frames == 0 {
				t.Errorf("hash %s: no frames", c.path)
			}
		}

		// And the human-readable form, which is a separate code path from
		// the JSON one and so is separately wrong.
		plain, err := run(t, "hash", c.path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(plain, string(c.want)) {
			t.Errorf("hash %s printed %q, which does not start with %q", c.path, plain, c.want)
		}
	}
}

func TestHashRefusesWhatItCannotRead(t *testing.T) {
	dir := t.TempDir()

	notMedia := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(notMedia, []byte("plain text"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A file that LOOKS like a PNG by name and holds nothing of the kind: the
	// decoder must refuse rather than hash whatever bytes it found.
	lying := filepath.Join(dir, "broken.png")
	if err := os.WriteFile(lying, []byte("\x89PNG\r\n\x1a\nnot really"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{notMedia, lying, filepath.Join(dir, "absent.jpg")} {
		if _, err := run(t, "hash", p); err == nil {
			t.Errorf("hash %s succeeded", p)
		}
	}
}

// TestComparePrintsBothShapes covers the two printed forms.
//
// ⛔ Images print a distance beside the score and the other media do not, so
// the human-readable output has two branches — and it is a SEPARATE code path
// from the JSON one, which the rest of these tests use. A suite that only
// ever asks for --json leaves the output every user actually reads untested.
func TestComparePrintsBothShapes(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.png")
	b := filepath.Join(dir, "b.png")
	writePNG(t, a, 512, 512)
	writePNG(t, b, 128, 128)

	out, err := run(t, "compare", a, b)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "image  score ") || !strings.Contains(out, "distance ") {
		t.Errorf("an image comparison printed %q", out)
	}

	out, err = run(t, "compare", "testdata/melody.flac", "testdata/melody.mp3")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "audio  score ") {
		t.Errorf("an audio comparison printed %q", out)
	}
	if strings.Contains(out, "distance") {
		t.Errorf("an audio comparison printed a Hamming distance: %q", out)
	}

	out, err = run(t, "compare", "testdata/clip-360p.mp4", "testdata/clip-180p.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "video  score ") {
		t.Errorf("a video comparison printed %q", out)
	}
}

func TestCompareRefusesAMissingFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "a.png")
	writePNG(t, good, 128, 128)

	if _, err := run(t, "compare", good, filepath.Join(dir, "gone.png")); err == nil {
		t.Error("comparing against a missing file succeeded")
	}
	if _, err := run(t, "compare", filepath.Join(dir, "gone.png"), good); err == nil {
		t.Error("comparing from a missing file succeeded")
	}
}

// ⛔ The tests for frameImage, toMono, resampleToFeatureRate and gcd are GONE
// FROM HERE, not deleted: those functions moved into
// github.com/go-perceptual/media, and so did their tests, which are stronger
// there than they were here. Each property was checked present before this
// file lost it:
//
//	frameImage refuses 10-bit, names the depth  -> TestFrameImageRefusesWhatItCannotHashHonestly
//	frameImage wraps each chroma layout         -> TestFrameImageWrapsEachChromaLayout
//	the downmix AVERAGES, never takes channel 1 -> TestDownmixAveragesRatherThanTakingTheFirstChannel
//	every sample format is handled              -> TestDownmixHandlesEachSampleFormat
//	an unknown format and 0 channels refused    -> TestDownmixRefusesWhatItCannotRead
//	the resampler CLAMPS instead of wrapping    -> TestResampleClampsRatherThanWrapping
//	an impossible rate is refused               -> TestResampleRefusesAnImpossibleRate
//
// And media tests two things this file never did, because they needed a real
// file rather than a byte slice: every PCM sample WIDTH through a generated
// WAV (8, 16, 24, 32-bit and float32, with the sample COUNT asserted, which
// is what catches a bad width derivation), and that frame index n at 15 fps
// holds the same picture as index 2n at 30 fps.
//
// What remains in this file is what is this tool's own: the commands, the
// printed shapes, and the refusals a CLI owes the person who typed a path.
