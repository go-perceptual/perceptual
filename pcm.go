package main

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/go-avkit/avkit/container"
)

// isPCM says whether a track's samples are already PCM and need no decoder.
//
// ⛔ These are what the container reports for a WAV: `ipcm` signed integer,
// `fpcm` float, `raw ` unsigned 8-bit. go-avkit's decoder refuses them —
// correctly, since there is nothing to decode — so a consumer that only knows
// how to call a decoder cannot read a WAV at all. This is the passthrough
// that gap requires, and it belongs upstream rather than here: see the note
// in the README.
func isPCM(codec string) bool {
	switch codec {
	case "ipcm", "fpcm", "raw ":
		return true
	}
	return false
}

// pcmMono reads a PCM track's samples and averages the channels.
func pcmMono(r *container.Reader, tr container.Track) ([]float64, error) {
	cfg, err := r.TrackConfig(tr.ID)
	if err != nil {
		return nil, err
	}
	samples, err := r.Samples(tr.ID)
	if err != nil {
		return nil, err
	}
	ch := cfg.Channels
	if ch < 1 {
		return nil, fmt.Errorf("a PCM track with %d channels", ch)
	}

	// ⛔ The sample width is DERIVED, because TrackConfig does not carry one,
	// and the derivation is only valid while a sample's Duration counts
	// FRAMES — that is, while the track's timescale IS its sample rate.
	// Measured on go-avkit's WAV reader (2026-11-15): timescale 44100, rate
	// 44100, 4096 frames in 16384 bytes of 2-channel audio, so 2 bytes a
	// sample. That equality is asserted instead of assumed: a container that
	// chose another timescale would yield a plausible wrong width, and PCM
	// read at the wrong width is noise that still hashes — a confident wrong
	// answer where an error belongs.
	if cfg.Timescale != uint32(cfg.SampleRate) {
		return nil, fmt.Errorf("cannot derive the PCM width: timescale %d is not the sample rate %d",
			cfg.Timescale, cfg.SampleRate)
	}
	var width int
	for _, s := range samples {
		if s.Duration == 0 {
			continue
		}
		frames := int(s.Duration)
		if len(s.Data)%(frames*ch) != 0 {
			return nil, fmt.Errorf("a PCM chunk of %d bytes is not a whole number of %d-channel frames over %d frames",
				len(s.Data), ch, frames)
		}
		width = len(s.Data) / (frames * ch)
		break
	}
	switch width {
	case 1, 2, 3, 4, 8:
	default:
		return nil, fmt.Errorf("cannot tell the PCM sample width (derived %d bytes)", width)
	}

	var out []float64
	for _, s := range samples {
		frames := len(s.Data) / (width * ch)
		for i := 0; i < frames; i++ {
			var sum float64
			for c := 0; c < ch; c++ {
				off := (i*ch + c) * width
				v, err := pcmValue(cfg.Codec, s.Data[off:off+width], width)
				if err != nil {
					return nil, err
				}
				sum += v
			}
			out = append(out, sum/float64(ch))
		}
	}
	return out, nil
}

// pcmValue turns one sample's bytes into a value in [-1, 1].
func pcmValue(codec string, b []byte, width int) (float64, error) {
	switch codec {
	case "raw ": // unsigned 8-bit, silence at 128
		return (float64(b[0]) - 128) / 128, nil
	case "fpcm":
		switch width {
		case 4:
			return float64(math.Float32frombits(binary.LittleEndian.Uint32(b))), nil
		case 8:
			return math.Float64frombits(binary.LittleEndian.Uint64(b)), nil
		}
	case "ipcm":
		switch width {
		case 2:
			return float64(int16(binary.LittleEndian.Uint16(b))) / 32768, nil
		case 3:
			// 24-bit little-endian, sign-extended.
			v := int32(b[0]) | int32(b[1])<<8 | int32(b[2])<<16
			if v&0x800000 != 0 {
				v |= ^0xffffff
			}
			return float64(v) / 8388608, nil
		case 4:
			return float64(int32(binary.LittleEndian.Uint32(b))) / 2147483648, nil
		}
	}
	return 0, fmt.Errorf("PCM codec %q at %d bytes per sample is not handled", codec, width)
}
