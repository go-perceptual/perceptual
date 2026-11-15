package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGenerateAudioSource writes the melody that the committed encoded
// fixtures are made from, so they can be regenerated.
//
// It is skipped unless GENERATE_DIR is set, because it writes a 1.4 MB WAV
// that is deliberately NOT committed: the encoded copies are the fixtures,
// and they are two orders of magnitude smaller. The encoding itself is done
// with ffmpeg, outside the test, and the commands are in README.md so the
// fixtures' provenance is written down rather than remembered.
func TestGenerateAudioSource(t *testing.T) {
	dir := os.Getenv("GENERATE_DIR")
	if dir == "" {
		t.Skip("GENERATE_DIR not set; the encoded fixtures in testdata are committed")
	}
	const rate = 44100
	// Four chords, four seconds: long enough for roughly 32 feature frames,
	// which leaves room for the alignment search to have an answer.
	p := filepath.Join(dir, "melody-source.wav")
	writeWAV(t, p, melody(eightChords()[:4], rate), rate)
	t.Logf("wrote %s", p)
}
