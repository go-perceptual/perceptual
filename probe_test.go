package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-avkit/avkit/container"
)

// TestWAVTimescaleIsItsSampleRate guards the assumption the PCM passthrough
// rests on.
//
// ⛔ A PCM track needs a sample WIDTH, and TrackConfig carries none: pcmMono
// derives it from a chunk's byte length divided by its frame count. That only
// works while a sample's Duration counts FRAMES, which is only true while the
// track's timescale IS its sample rate. Measured on go-avkit's WAV reader
// (2026-11-15): timescale 44100, rate 44100, 4096 frames in 16384 bytes of
// two-channel audio, so 2 bytes a sample.
//
// pcmMono asserts that equality at run time and refuses otherwise, so a
// change upstream produces an error rather than a wrong width. This test is
// the other half: it tells us the equality BROKE, instead of leaving every
// WAV silently unreadable until somebody files a bug.
func TestWAVTimescaleIsItsSampleRate(t *testing.T) {
	for _, rate := range []int{44100, 48000, 22050, 8000} {
		p := filepath.Join(t.TempDir(), "probe.wav")
		writeWAV(t, p, melody(eightChords()[:1], rate), rate)

		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		r, err := container.NewReader(data)
		if err != nil {
			t.Fatalf("%d Hz: %v", rate, err)
		}
		tracks := r.File().Tracks
		if len(tracks) != 1 {
			t.Fatalf("%d Hz: %d tracks, want 1", rate, len(tracks))
		}
		cfg, err := r.TrackConfig(tracks[0].ID)
		if err != nil {
			t.Fatal(err)
		}

		if !isPCM(cfg.Codec) {
			t.Errorf("%d Hz: codec %q is not one the passthrough handles", rate, cfg.Codec)
		}
		if cfg.SampleRate != rate {
			t.Errorf("%d Hz: reader says %d", rate, cfg.SampleRate)
		}
		if cfg.Timescale != uint32(cfg.SampleRate) {
			t.Fatalf("%d Hz: timescale %d is no longer the sample rate %d -- the PCM width can no longer be derived from a sample's duration",
				rate, cfg.Timescale, cfg.SampleRate)
		}

		// And the derivation itself, end to end: 16-bit stereo must come out
		// as 2 bytes a sample.
		samples, err := r.Samples(tracks[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(samples) == 0 {
			t.Fatalf("%d Hz: no samples", rate)
		}
		s := samples[0]
		width := len(s.Data) / (int(s.Duration) * cfg.Channels)
		if width != 2 {
			t.Errorf("%d Hz: derived width %d bytes, want 2 for 16-bit PCM", rate, width)
		}
	}
}

// TestPCMPassthroughRefusesAnUnderivableWidth is the negative witness.
//
// ⛔ Without it, the timescale guard in pcmMono is code no test executes: the
// measurement above shows the equality HOLDS, so the refusal branch never
// runs. A guard that has never refused anything is a guard whose message and
// condition are both unverified.
func TestPCMPassthroughRefusesAnUnderivableWidth(t *testing.T) {
	if _, err := pcmValue("ipcm", []byte{0, 0, 0, 0, 0, 0}, 6); err == nil {
		t.Error("pcmValue accepted a 6-byte integer sample")
	}
	if _, err := pcmValue("mp4a", []byte{0, 0}, 2); err == nil {
		t.Error("pcmValue accepted a codec that is not PCM at all")
	}
	for _, codec := range []string{"mp4a", "Opus", "fLaC", ""} {
		if isPCM(codec) {
			t.Errorf("isPCM(%q) is true", codec)
		}
	}
	for _, codec := range []string{"ipcm", "fpcm", "raw "} {
		if !isPCM(codec) {
			t.Errorf("isPCM(%q) is false", codec)
		}
	}
}

// TestPCMValueScalesToUnit checks each width against its full-scale value,
// since a wrong divisor reads as a quieter recording that still hashes.
func TestPCMValueScalesToUnit(t *testing.T) {
	for _, c := range []struct {
		name  string
		codec string
		b     []byte
		width int
		want  float64
	}{
		{"16-bit max", "ipcm", []byte{0xff, 0x7f}, 2, 32767.0 / 32768},
		{"16-bit min", "ipcm", []byte{0x00, 0x80}, 2, -1},
		{"16-bit zero", "ipcm", []byte{0x00, 0x00}, 2, 0},
		{"24-bit max", "ipcm", []byte{0xff, 0xff, 0x7f}, 3, 8388607.0 / 8388608},
		{"24-bit min", "ipcm", []byte{0x00, 0x00, 0x80}, 3, -1},
		{"24-bit minus one", "ipcm", []byte{0xff, 0xff, 0xff}, 3, -1.0 / 8388608},
		{"32-bit min", "ipcm", []byte{0x00, 0x00, 0x00, 0x80}, 4, -1},
		{"8-bit unsigned silence", "raw ", []byte{128}, 1, 0},
		{"8-bit unsigned min", "raw ", []byte{0}, 1, -1},
		{"8-bit unsigned max", "raw ", []byte{255}, 1, 127.0 / 128},
		{"float32 one", "fpcm", []byte{0x00, 0x00, 0x80, 0x3f}, 4, 1},
	} {
		got, err := pcmValue(c.codec, c.b, c.width)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
