package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"

	"github.com/go-avkit/avkit/container"
	"github.com/go-avkit/decode"
	"github.com/go-dsp/signal"
	"github.com/go-perceptual/chroma"
)

// audioFeatures decodes a file's first audio track and returns its chroma
// features.
//
// The chain is: demux, decode to PCM, downmix to mono, resample to the rate
// the feature is defined at, extract. Each step is somebody else's job done
// properly — the container and decoder are go-avkit's, the resampler is
// go-dsp's — and this function is only the plumbing between them.
func audioFeatures(path string) ([][]float64, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	r, err := container.NewReader(data)
	if err != nil {
		return nil, 0, err
	}
	id, err := decode.FirstAudioTrack(r)
	if err != nil {
		return nil, 0, err
	}

	// A PCM track is already decoded: there is nothing for a decoder to do,
	// and go-avkit's rightly refuses it. Read the samples straight through.
	cfg, err := r.TrackConfig(id)
	if err != nil {
		return nil, 0, err
	}
	if isPCM(cfg.Codec) {
		var tr container.Track
		for _, t := range r.File().Tracks {
			if t.ID == id {
				tr = t
			}
		}
		mono, err := pcmMono(r, tr)
		if err != nil {
			return nil, 0, fmt.Errorf("%s: %w", path, err)
		}
		if len(mono) == 0 {
			return nil, 0, fmt.Errorf("%s: the audio track holds no samples", path)
		}
		pcm, err := resampleToFeatureRate(mono, cfg.SampleRate)
		if err != nil {
			return nil, 0, err
		}
		features, err := chroma.Features(pcm)
		return features, cfg.SampleRate, err
	}

	buffers, err := decode.DecodeAudioTrack(r, id, decode.AudioConfig{})
	if err != nil {
		return nil, 0, err
	}
	if len(buffers) == 0 {
		return nil, 0, fmt.Errorf("%s: the audio track decoded to nothing", path)
	}

	rate := buffers[0].SampleRate
	var mono []float64
	for _, b := range buffers {
		m, err := toMono(b)
		if err != nil {
			return nil, 0, err
		}
		if b.SampleRate != rate {
			// ⛔ Refused rather than resampled piecewise: a track whose rate
			// changes mid-stream would need every buffer resampled on its own
			// and spliced, and splicing resampled blocks introduces exactly
			// the discontinuities the feature would then report as content.
			return nil, 0, fmt.Errorf("%s: sample rate changes mid-track (%d then %d)", path, rate, b.SampleRate)
		}
		mono = append(mono, m...)
	}

	pcm, err := resampleToFeatureRate(mono, rate)
	if err != nil {
		return nil, 0, err
	}
	features, err := chroma.Features(pcm)
	return features, rate, err
}

// toMono averages the channels of one buffer into float64 samples in
// [-1, 1].
//
// Averaging, not taking the first channel: a track whose content sits in one
// channel — an interview, a badly mixed rip — would otherwise be compared
// against silence.
func toMono(a decode.Audio) ([]float64, error) {
	n := a.Samples()
	ch := a.Channels
	if ch < 1 {
		return nil, fmt.Errorf("a buffer with %d channels", ch)
	}
	out := make([]float64, n)

	switch a.Format {
	case decode.SampleS16:
		for i := 0; i < n; i++ {
			var sum float64
			for c := 0; c < ch; c++ {
				off := (i*ch + c) * 2
				sum += float64(int16(binary.LittleEndian.Uint16(a.Data[off:]))) / 32768
			}
			out[i] = sum / float64(ch)
		}
	case decode.SampleF32:
		for i := 0; i < n; i++ {
			var sum float64
			for c := 0; c < ch; c++ {
				off := (i*ch + c) * 4
				sum += float64(math.Float32frombits(binary.LittleEndian.Uint32(a.Data[off:])))
			}
			out[i] = sum / float64(ch)
		}
	default:
		return nil, fmt.Errorf("sample format %v is not handled", a.Format)
	}
	return out, nil
}

// resampleToFeatureRate brings mono audio to the rate the chroma feature is
// defined at.
//
// ⛔ The rate is part of the feature, not a preference: the frame geometry and
// the frequency bounds are expressed in samples at that rate, so audio fed at
// another rate produces a perfectly plausible fingerprint that matches
// nothing.
func resampleToFeatureRate(mono []float64, rate int) ([]int16, error) {
	if rate != chroma.SampleRate {
		g := gcd(chroma.SampleRate, rate)
		up, down := chroma.SampleRate/g, rate/g
		out, err := signal.ResamplePoly(mono, up, down, nil)
		if err != nil {
			return nil, fmt.Errorf("resampling %d Hz to %d Hz: %w", rate, chroma.SampleRate, err)
		}
		mono = out
	}

	pcm := make([]int16, len(mono))
	for i, v := range mono {
		// Clamp: a resampler's ringing can overshoot a full-scale signal, and
		// wrapping that to the opposite sign would put a click into the
		// feature.
		switch {
		case v > 1:
			v = 1
		case v < -1:
			v = -1
		}
		pcm[i] = int16(v * 32767)
	}
	return pcm, nil
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}
