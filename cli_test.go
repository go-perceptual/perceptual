package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-perceptual/chroma"
	"github.com/spf13/pflag"
	"golang.org/x/image/draw"
)

// run drives the CLI the way a user does, through cobra, and returns what it
// printed.
func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := RootCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func runJSON(t *testing.T, args ...string) compareResult {
	t.Helper()
	out, err := run(t, args...)
	if err != nil {
		t.Fatalf("%v: %v", args, err)
	}
	var res compareResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("decoding %q: %v", out, err)
	}
	return res
}

// --- fixtures ------------------------------------------------------------

// writeWAV emits 16-bit stereo PCM as a RIFF/WAVE file, which the container
// reports as the codec "ipcm" and no decoder will touch.
func writeWAV(t *testing.T, path string, mono []int16, rate int) {
	t.Helper()
	n := len(mono)
	dataBytes := n * 2 * 2

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := func(v any) {
		if err := binary.Write(f, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	f.WriteString("RIFF")
	w(uint32(36 + dataBytes))
	f.WriteString("WAVEfmt ")
	w(uint32(16))
	w(uint16(1)) // linear PCM
	w(uint16(2)) // channels
	w(uint32(rate))
	w(uint32(rate * 2 * 2))
	w(uint16(4))  // block align
	w(uint16(16)) // bits per sample
	f.WriteString("data")
	w(uint32(dataBytes))
	for _, s := range mono {
		w(s)
		w(s)
	}
}

// melody renders chords in sequence, one second each, with a phase that runs
// continuously so the only discontinuity is a chord change.
//
// Every frequency is placed away from a chroma band edge. The bands are
// centred on the equal-tempered semitones above A0 = 27.5 Hz, so a tone on a
// boundary splits its energy between two bands and a probe ends up measuring
// the band geometry instead of the thing under test.
func melody(chords [][]float64, rate int) []int16 {
	perChord := rate
	out := make([]int16, 0, perChord*len(chords))
	phase := 0
	for _, freqs := range chords {
		for i := 0; i < perChord; i++ {
			var v float64
			for _, f := range freqs {
				v += math.Sin(2 * math.Pi * f * float64(phase) / float64(rate))
			}
			out = append(out, int16(v/float64(len(freqs))*0.7*32767))
			phase++
		}
	}
	return out
}

func eightChords() [][]float64 {
	return [][]float64{
		{261.10, 329.30, 391.60},
		{293.30, 369.60, 440.90},
		{329.30, 415.00, 493.30},
		{349.60, 440.90, 523.60},
		{391.60, 493.30, 587.60},
		{440.90, 555.60, 659.60},
		{493.30, 622.60, 739.60},
		{523.60, 659.60, 784.30},
	}
}

// writePNG renders a picture with enough structure to hash, at a given size.
func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	src := image.NewRGBA(image.Rect(0, 0, 512, 512))
	for y := 0; y < 512; y++ {
		for x := 0; x < 512; x++ {
			// Concentric rings plus a diagonal ramp: structure at several
			// scales, so decimation has something to preserve.
			dx, dy := float64(x-256), float64(y-256)
			r := math.Sqrt(dx*dx + dy*dy)
			v := (math.Sin(r/9) + 1) / 2 * 200
			src.Set(x, y, color.RGBA{
				R: uint8(v),
				G: uint8(float64(x) / 512 * 255),
				B: uint8(float64(y) / 512 * 255),
				A: 255,
			})
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, dst); err != nil {
		t.Fatal(err)
	}
}

// --- identification -----------------------------------------------------

func TestIdentifyReadsBytesNotNames(t *testing.T) {
	dir := t.TempDir()

	// ⛔ Each fixture is given a LYING extension. An extension is a claim by
	// whoever named the file, and a download folder is full of wrong ones; a
	// tool that believes the name returns a confident wrong answer.
	img := filepath.Join(dir, "actually-a-png.mp4")
	writePNG(t, img, 256, 256)

	wav := filepath.Join(dir, "actually-a-wav.jpg")
	writeWAV(t, wav, melody(eightChords()[:2], 44100), 44100)

	for _, c := range []struct {
		path string
		want Kind
	}{
		{img, KindImage},
		{wav, KindAudio},
		{"testdata/clip-360p.mp4", KindVideo},
	} {
		got, err := identify(c.path)
		if err != nil {
			t.Fatalf("identify(%s): %v", c.path, err)
		}
		if got != c.want {
			t.Errorf("identify(%s) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestIdentifyRefusesSomethingElse(t *testing.T) {
	p := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(p, []byte("this is not media at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := identify(p); err == nil {
		t.Fatal("identify accepted a text file")
	}
}

func TestCompareRefusesCrossMedia(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "a.png")
	writePNG(t, img, 256, 256)

	_, err := run(t, "compare", img, "testdata/clip-360p.mp4")
	if err == nil {
		t.Fatal("comparing a picture with a video was accepted")
	}
	// ⛔ A score of zero would read as "measured, and they differ". There is
	// no scale on which a picture and a video can be scored, so the refusal
	// has to be an error, not a low number.
	if !strings.Contains(err.Error(), "cannot compare") {
		t.Errorf("error does not explain the refusal: %v", err)
	}
}

// --- images -------------------------------------------------------------

func TestImageSurvivesDownscaling(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "big.png")
	small := filepath.Join(dir, "small.png")
	writePNG(t, big, 1024, 1024)
	writePNG(t, small, 256, 256)

	res := runJSON(t, "compare", "--json", big, small)
	if res.Kind != KindImage {
		t.Fatalf("kind %q", res.Kind)
	}
	if res.Distance == nil {
		t.Fatal("no distance reported for an image pair")
	}
	// The specification's own band: 30 or less is a confident match.
	if *res.Distance > 30 {
		t.Errorf("distance %d across a 4x downscale, want <= 30", *res.Distance)
	}
	t.Logf("1024px vs 256px: distance %d, score %.4f, %q", *res.Distance, res.Score, res.Verdict)
}

// --- audio --------------------------------------------------------------

// TestAudioPCMPassthrough is the regression test for the gap this CLI found:
// the container demuxes a WAV and reports its codec as linear PCM, and
// go-avkit's decoder refuses it — rightly, since the samples already ARE
// PCM. Without a passthrough the tool cannot read a WAV at all.
func TestAudioPCMPassthrough(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "m.wav")
	writeWAV(t, p, melody(eightChords(), 44100), 44100)

	features, rate, err := audioFeatures(p)
	if err != nil {
		t.Fatalf("reading a WAV: %v", err)
	}
	if rate != 44100 {
		t.Errorf("rate %d, want 44100", rate)
	}
	if len(features) == 0 {
		t.Fatal("no features from eight seconds of audio")
	}
	for i, f := range features {
		if len(f) != 12 {
			t.Fatalf("frame %d has %d bands, want 12", i, len(f))
		}
	}
}

func TestAudioSurvivesResampling(t *testing.T) {
	dir := t.TempDir()
	chords := eightChords()
	a := filepath.Join(dir, "a-44k.wav")
	b := filepath.Join(dir, "b-48k.wav")
	writeWAV(t, a, melody(chords, 44100), 44100)
	writeWAV(t, b, melody(chords, 48000), 48000)

	// ⛔ 44100 and 48000 reach the feature's 11025 Hz by COMPLETELY different
	// ratios -- 1/4 and 147/640 -- so this exercises the polyphase resampler
	// rather than a trivial decimation.
	res := runJSON(t, "compare", "--json", a, b)
	if res.Score < 0.999 {
		t.Errorf("the same melody at 44.1 and 48 kHz scored %.6f, want >= 0.999", res.Score)
	}
	if res.Offset == nil || *res.Offset != 0 {
		t.Errorf("offset %v, want 0 for two takes that start together", res.Offset)
	}
}

// TestAudioFindsTheAlignment proves the offset search, which a stationary
// signal cannot.
//
// ⛔ Measured 2026-11-15: the same sustained CHORD at two sample rates scored
// 0.9999999996 and reported offset 25, while an UNRELATED chord reported
// offset 22. Both are arbitrary -- when every frame looks like every other,
// every alignment scores alike and the argmax is picked by rounding noise. A
// perfect score on such a pair says the feature survived resampling and says
// NOTHING about the alignment. A melody has exactly one right answer.
func TestAudioFindsTheAlignment(t *testing.T) {
	dir := t.TempDir()
	const rate = 44100
	full := melody(eightChords(), rate)
	a := filepath.Join(dir, "full.wav")
	b := filepath.Join(dir, "late.wav")
	writeWAV(t, a, full, rate)
	writeWAV(t, b, full[2*rate:], rate) // the first two seconds cut off

	res := runJSON(t, "compare", "--json", a, b)
	if res.Score < 0.99 {
		t.Errorf("score %.6f for a two-second clip of the same melody", res.Score)
	}
	if res.Offset == nil {
		t.Fatal("no offset reported")
	}
	// Two seconds, in feature frames: 2 * 11025 / 1365 = 16.15.
	framesPerSecond := float64(chroma.SampleRate) / float64(chroma.Hop)
	want := int(2 * framesPerSecond)
	if *res.Offset != want {
		t.Errorf("offset %d, want %d (two seconds at %d samples a hop)", *res.Offset, want, chroma.Hop)
	}
}

// TestAudioThresholdRejectsReversedMelody defends AudioThreshold.
//
// ⛔ This test exists so that lowering the threshold to the 0.7 that reads
// naturally beside TMK's video threshold FAILS. Chroma is a profile of which
// pitch classes are present; the same notes in the wrong order therefore
// agree substantially. If this ever passes at 0.7, the tool has started
// calling a reversed recording a match.
func TestAudioThresholdRejectsReversedMelody(t *testing.T) {
	dir := t.TempDir()
	const rate = 44100
	chords := eightChords()
	reversed := make([][]float64, len(chords))
	for i := range chords {
		reversed[i] = chords[len(chords)-1-i]
	}

	a := filepath.Join(dir, "forwards.wav")
	b := filepath.Join(dir, "backwards.wav")
	writeWAV(t, a, melody(chords, rate), rate)
	writeWAV(t, b, melody(reversed, rate), rate)

	res := runJSON(t, "compare", "--json", a, b)
	t.Logf("the same eight chords reversed: score %.4f", res.Score)

	if res.Score >= AudioThreshold {
		t.Errorf("a reversed melody scored %.4f, at or above the %.2f threshold", res.Score, AudioThreshold)
	}
	if res.Verdict != "different" {
		t.Errorf("verdict %q for a reversed melody", res.Verdict)
	}
	// The other half of the claim: the score is high ENOUGH that a 0.7 gate
	// would have admitted it. Without this, the test would still pass if
	// chroma stopped seeing the shared notes at all, which would be a
	// different defect wearing this test's green tick.
	if res.Score < 0.6 {
		t.Errorf("score %.4f is so low that this no longer demonstrates why 0.7 is too lenient", res.Score)
	}
}

// --- video --------------------------------------------------------------

// TestVideoSurvivesDownscaling uses committed H.264 fixtures.
//
// ⛔ The fixtures are a clip and SCALED COPIES OF THAT CLIP, not the same
// pattern rendered at several sizes. Measured 2026-11-15: ffmpeg's testsrc2
// rendered independently at 640x360 and 320x180 scored 0.3467, because the
// filter draws its pattern parametrically for the target size -- different
// cell sizes, different text -- so the two clips genuinely hold different
// pictures. That number looked like a hash defect and was a fixture defect.
// A 4K and a 1080p copy of one film are a DOWNSCALE, which is what these are.
func TestVideoSurvivesDownscaling(t *testing.T) {
	for _, c := range []struct{ name, path string }{
		{"half scale", "testdata/clip-180p.mp4"},
		{"quarter scale", "testdata/clip-90p.mp4"},
	} {
		res := runJSON(t, "compare", "--json", "testdata/clip-360p.mp4", c.path)
		if res.Kind != KindVideo {
			t.Fatalf("%s: kind %q", c.name, res.Kind)
		}
		if res.Score < 0.95 {
			t.Errorf("%s: score %.4f, want >= 0.95", c.name, res.Score)
		}
		t.Logf("%s: score %.4f %q", c.name, res.Score, res.Verdict)
	}
}

func TestVideoRejectsADifferentClip(t *testing.T) {
	res := runJSON(t, "compare", "--json", "testdata/clip-360p.mp4", "testdata/clip-other.mp4")
	if res.Score >= 0.7 {
		t.Errorf("two different clips scored %.4f", res.Score)
	}
	if res.Verdict != "different" {
		t.Errorf("verdict %q", res.Verdict)
	}
	t.Logf("a different clip: score %.4f", res.Score)
}

func TestHashReportsFrameCount(t *testing.T) {
	out, err := run(t, "hash", "testdata/clip-360p.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "video") || !strings.Contains(out, "15 fps") {
		t.Errorf("hash printed %q", out)
	}
}

// --- verdicts -----------------------------------------------------------

func TestImageVerdictFollowsTheSpecification(t *testing.T) {
	for _, c := range []struct {
		d    int
		want string
	}{
		{0, "identical hashes"},
		{20, "the same picture"},
		{21, "probably the same picture"},
		{30, "probably the same picture"},
		{31, "different pictures"},
		{128, "different pictures"},
	} {
		if got := imageVerdict(c.d); got != c.want {
			t.Errorf("imageVerdict(%d) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestThresholdVerdictBands(t *testing.T) {
	for _, c := range []struct {
		score, threshold float64
		want             string
	}{
		{1, 0.9, "the same recording"},
		{0.999, 0.9, "the same recording"},
		{0.9989, 0.9, "a match"},
		{0.9, 0.9, "a match"},
		{0.8999, 0.9, "different"},
		{0, 0.9, "different"},
	} {
		if got := thresholdVerdict(c.score, c.threshold); got != c.want {
			t.Errorf("thresholdVerdict(%v, %v) = %q, want %q", c.score, c.threshold, got, c.want)
		}
	}
}

// TestNoDeletionCommandExists keeps this tool a measuring instrument.
//
// ⛔ The CLI reports and never acts. A perceptual match is a JUDGEMENT with a
// threshold, and a threshold is sometimes wrong; a tool that deletes on one
// destroys the only evidence that it was wrong. Wiring into a file manager
// means the PERSON clicks delete, having seen the score.
func TestNoDeletionCommandExists(t *testing.T) {
	cmd := RootCmd()
	for _, sub := range cmd.Commands() {
		switch sub.Name() {
		case "delete", "remove", "rm", "prune", "clean", "purge", "dedupe":
			t.Errorf("the CLI grew an acting command: %q", sub.Name())
		}
		sub.Flags().VisitAll(func(f *pflag.Flag) {
			switch f.Name {
			case "delete", "remove", "prune", "force", "apply", "act":
				t.Errorf("%s grew an acting flag: --%s", sub.Name(), f.Name)
			}
		})
	}
}
