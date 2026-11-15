package main

import (
	"encoding/json"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-avkit/decode"
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

// TestFrameImageRefusesWhatItCannotHashHonestly covers the two refusals in
// the video frame wrapper.
//
// ⛔ A 10-bit frame shifted down to 8 bits WOULD hash, and would hash
// differently from the same film delivered as 8-bit — so the quiet
// truncation produces two different fingerprints for one film, which is the
// single thing this tool must not do. Likewise an unhandled chroma layout
// read as 4:2:0 gives planes of the wrong size.
func TestFrameImageRefusesWhatItCannotHashHonestly(t *testing.T) {
	ten := decode.Frame{
		Y: make([]byte, 64*64*2), Cb: make([]byte, 32*32*2), Cr: make([]byte, 32*32*2),
		YStride: 128, CStride: 64, Width: 64, Height: 64,
		Chroma: decode.Chroma420, BitDepth: 10,
	}
	_, err := frameImage(ten)
	if err == nil {
		t.Error("frameImage accepted a 10-bit frame")
	} else if !strings.Contains(err.Error(), "10-bit") {
		t.Errorf("the error does not name the bit depth: %v", err)
	}

	odd := decode.Frame{
		Y: make([]byte, 64*64), Cb: make([]byte, 64*64), Cr: make([]byte, 64*64),
		YStride: 64, CStride: 64, Width: 64, Height: 64,
		Chroma: decode.Chroma(99), BitDepth: 8,
	}
	if _, err := frameImage(odd); err == nil {
		t.Error("frameImage accepted an unknown chroma layout")
	}
}

// TestFrameImageWrapsEachChromaLayout is the positive half: the three layouts
// it DOES handle must come back with the right subsample ratio, since a frame
// wrapped at the wrong ratio reads its chroma planes at the wrong stride and
// hashes a picture nobody filmed.
func TestFrameImageWrapsEachChromaLayout(t *testing.T) {
	for _, c := range []struct {
		chroma decode.Chroma
		cw, ch int
		want   image.YCbCrSubsampleRatio
	}{
		{decode.Chroma420, 32, 32, image.YCbCrSubsampleRatio420},
		{decode.Chroma422, 32, 64, image.YCbCrSubsampleRatio422},
		{decode.Chroma444, 64, 64, image.YCbCrSubsampleRatio444},
	} {
		f := decode.Frame{
			Y: make([]byte, 64*64), Cb: make([]byte, c.cw*c.ch), Cr: make([]byte, c.cw*c.ch),
			YStride: 64, CStride: c.cw, Width: 64, Height: 64,
			Chroma: c.chroma, BitDepth: 8,
		}
		img, err := frameImage(f)
		if err != nil {
			t.Errorf("%v: %v", c.chroma, err)
			continue
		}
		ycbcr, ok := img.(*image.YCbCr)
		if !ok {
			t.Errorf("%v: got %T", c.chroma, img)
			continue
		}
		if ycbcr.SubsampleRatio != c.want {
			t.Errorf("%v: ratio %v, want %v", c.chroma, ycbcr.SubsampleRatio, c.want)
		}
		if b := ycbcr.Bounds(); b.Dx() != 64 || b.Dy() != 64 {
			t.Errorf("%v: bounds %v", c.chroma, b)
		}
	}
}

// TestToMonoRefusesAnUnhandledFormat and the channel check below guard the
// downmix.
//
// ⛔ toMono AVERAGES the channels rather than taking the first. A track whose
// content sits in one channel — an interview, a badly mixed rip — would
// otherwise be compared against silence, and silence matches other silence.
func TestToMonoRefusesAnUnhandledFormat(t *testing.T) {
	if _, err := toMono(decode.Audio{Channels: 2, Format: decode.SampleFormat(99), Data: make([]byte, 16)}); err == nil {
		t.Error("toMono accepted an unknown sample format")
	}
	if _, err := toMono(decode.Audio{Channels: 0, Format: decode.SampleS16}); err == nil {
		t.Error("toMono accepted a buffer with no channels")
	}
}

func TestToMonoAveragesRatherThanTakingTheFirstChannel(t *testing.T) {
	// Left at full scale, right at silence. Taking channel one gives 1.0;
	// averaging gives 0.5. The second is what this tool must do.
	a := decode.Audio{
		Channels: 2,
		Format:   decode.SampleS16,
		// one frame: left 0x7fff, right 0x0000
		Data: []byte{0xff, 0x7f, 0x00, 0x00},
	}
	got, err := toMono(a)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("%d samples, want 1", len(got))
	}
	if want := (32767.0 / 32768) / 2; got[0] != want {
		t.Errorf("got %v, want %v -- a downmix that is not an average", got[0], want)
	}
}

// TestResampleClampsRatherThanWrapping covers the overshoot guard.
//
// ⛔ A polyphase resampler rings, and its ringing overshoots a full-scale
// signal. Converting an overshoot to int16 without clamping WRAPS it to the
// opposite sign, which puts a click into the feature — content that is not in
// the recording.
func TestResampleClampsRatherThanWrapping(t *testing.T) {
	// No resampling (already at the feature rate), so the clamp is the only
	// thing under test.
	in := []float64{0, 1.5, -1.5, 1, -1, 0.5}
	out, err := resampleToFeatureRate(in, 11025)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(in) {
		t.Fatalf("%d samples out, %d in", len(out), len(in))
	}
	for i, v := range out {
		if v == -32768 && in[i] > 0 {
			t.Errorf("sample %d: %v wrapped to %d", i, in[i], v)
		}
	}
	if out[1] != 32767 {
		t.Errorf("1.5 became %d, want 32767", out[1])
	}
	if out[2] != -32767 {
		t.Errorf("-1.5 became %d, want -32767", out[2])
	}
}

func TestGcd(t *testing.T) {
	for _, c := range [][3]int{
		{11025, 44100, 11025},
		{11025, 48000, 75},
		{11025, 11025, 11025},
		{7, 13, 1},
	} {
		if got := gcd(c[0], c[1]); got != c[2] {
			t.Errorf("gcd(%d, %d) = %d, want %d", c[0], c[1], got, c[2])
		}
	}
}
