package main

import (
	"path/filepath"
	"testing"
)

// TestAudioAcrossCodecs is the real cross-codec measurement.
//
// ⛔ An earlier version of this measurement used a PROXY for lossy encoding:
// a low-pass filter plus requantisation, applied in Go. That measures what
// the author imagined an encoder does. These fixtures are the same four-chord
// melody put through FOUR REAL ENCODERS — FLAC, LAME, libopus and ffmpeg's
// AAC — and read back through go-avkit's own decoders, which is the path a
// file in a download folder actually takes.
//
// The fixtures are committed rather than generated, so this runs in CI on a
// machine with no ffmpeg. TestGenerateAudioSource writes the source WAV and
// README.md records the encoding commands.
func TestAudioAcrossCodecs(t *testing.T) {
	fixtures := []string{
		"testdata/melody.flac",
		"testdata/melody.mp3",
		"testdata/melody.opus",
		"testdata/melody.m4a",
	}

	// Every pair is the same recording through a different codec, so every
	// pair must agree -- including the pairs where NEITHER side is lossless,
	// which is the case a download folder is full of and the case a
	// comparison against one reference file never tests.
	for i, a := range fixtures {
		for _, b := range fixtures[i+1:] {
			res := runJSON(t, "compare", "--json", a, b)
			if res.Kind != KindAudio {
				t.Errorf("%s vs %s: kind %q", a, b, res.Kind)
			}
			if res.Score < 0.999 {
				t.Errorf("%s vs %s: score %.9f, want >= 0.999", a, b, res.Score)
			}
			if res.Verdict != "the same recording" {
				t.Errorf("%s vs %s: verdict %q", a, b, res.Verdict)
			}
			t.Logf("%-24s vs %-24s %.9f", filepath.Base(a), filepath.Base(b), res.Score)
		}
	}
}

// TestLosslessAgreesWithPassthrough cross-checks the two independent ways
// this tool can arrive at PCM.
//
// ⛔ FLAC is lossless, so decoding the FLAC fixture and reading an
// uncompressed WAV of the same samples must produce the SAME feature. The two
// paths share no code: one runs go-avkit's FLAC decoder, the other is this
// repository's PCM passthrough. A score of 1 therefore validates both at
// once, and a drift in either shows up here rather than as a slightly worse
// match that nothing notices.
func TestLosslessAgreesWithPassthrough(t *testing.T) {
	p := filepath.Join(t.TempDir(), "same.wav")
	writeWAV(t, p, melody(eightChords()[:4], 44100), 44100)

	res := runJSON(t, "compare", "--json", p, "testdata/melody.flac")
	t.Logf("PCM passthrough vs the FLAC decoder: %.12f", res.Score)
	if res.Score < 0.99999 {
		t.Errorf("the FLAC decoder and the PCM passthrough disagree: %.12f", res.Score)
	}
	if res.Offset == nil || *res.Offset != 0 {
		t.Errorf("offset %v, want 0 for the same samples", res.Offset)
	}
}

// TestEncodedAudioRejectsADifferentPiece is the control for the pairs above.
//
// Without it, TestAudioAcrossCodecs would still pass if the comparison had
// become blind and returned a high score for everything.
func TestEncodedAudioRejectsADifferentPiece(t *testing.T) {
	chords := eightChords()[:4]
	reversed := make([][]float64, len(chords))
	for i := range chords {
		reversed[i] = chords[len(chords)-1-i]
	}
	p := filepath.Join(t.TempDir(), "backwards.wav")
	writeWAV(t, p, melody(reversed, 44100), 44100)

	res := runJSON(t, "compare", "--json", p, "testdata/melody.flac")
	t.Logf("the same four chords reversed, against the FLAC fixture: %.4f", res.Score)
	if res.Score >= AudioThreshold {
		t.Errorf("a reversed melody scored %.4f against the real encoding", res.Score)
	}
}
