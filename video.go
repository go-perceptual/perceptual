package main

import (
	"fmt"
	"image"
	"os"

	"github.com/go-avkit/avkit/container"
	"github.com/go-avkit/decode"
	"github.com/go-perceptual/pdq"
	"github.com/go-perceptual/tmk"
)

// videoHash decodes a file's first video track and hashes it.
//
// ⛔ Frames are resampled to tmk.FPS by TIME, not by taking every nth frame.
// The kernel compares phases at integer frame indices, so a 30 fps and a
// 25 fps copy of the same film must produce the same indices — which they do
// only if each index is filled by the frame showing at that instant. Taking
// every other frame of a 30 fps file and every frame of a 15 fps file would
// give two valid-looking hashes that cannot be compared.
func videoHash(path string) (*tmk.Hash, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	r, err := container.NewReader(data)
	if err != nil {
		return nil, 0, err
	}
	id, err := decode.FirstVideoTrack(r)
	if err != nil {
		return nil, 0, err
	}
	frames, err := decode.DecodeTrack(r, id, decode.Config{})
	if err != nil {
		return nil, 0, err
	}
	if len(frames) == 0 {
		return nil, 0, fmt.Errorf("%s: the video track decoded to nothing", path)
	}

	// Presentation times are in the track timescale; turn them into seconds.
	var timescale uint32 = 1
	for _, tr := range r.File().Tracks {
		if tr.ID == id {
			timescale = tr.Timescale
		}
	}
	if timescale == 0 {
		return nil, 0, fmt.Errorf("%s: the track declares a timescale of zero", path)
	}
	seconds := func(i int) float64 { return float64(frames[i].PTS) / float64(timescale) }

	duration := seconds(len(frames)-1) - seconds(0)
	steps := int(duration*tmk.FPS) + 1

	h := tmk.New()
	used := 0
	cursor := 0
	for step := 0; step < steps; step++ {
		want := seconds(0) + float64(step)/tmk.FPS
		// The frame showing at that instant: the last one presented at or
		// before it.
		for cursor+1 < len(frames) && seconds(cursor+1) <= want {
			cursor++
		}
		img, err := frameImage(frames[cursor])
		if err != nil {
			return nil, 0, err
		}
		feature, _, err := pdq.Float(img)
		if err != nil {
			return nil, 0, err
		}
		h.Add(step, feature)
		used++
	}

	hash, err := h.Finish()
	return hash, used, err
}

// frameImage wraps a decoded frame's planes as an image without copying
// them, which is what the hash reads.
func frameImage(f decode.Frame) (image.Image, error) {
	if f.BitDepth != 8 {
		// ⛔ Said plainly rather than silently truncating: a 10-bit frame
		// shifted down to 8 would hash, and would hash DIFFERENTLY from the
		// same film delivered as 8-bit, which is the one thing the comparison
		// must not do quietly.
		return nil, fmt.Errorf("%d-bit video is not handled yet (8-bit only)", f.BitDepth)
	}
	var ratio image.YCbCrSubsampleRatio
	switch f.Chroma {
	case decode.Chroma420:
		ratio = image.YCbCrSubsampleRatio420
	case decode.Chroma422:
		ratio = image.YCbCrSubsampleRatio422
	case decode.Chroma444:
		ratio = image.YCbCrSubsampleRatio444
	default:
		return nil, fmt.Errorf("chroma sampling %v is not handled", f.Chroma)
	}
	return &image.YCbCr{
		Y:              f.Y,
		Cb:             f.Cb,
		Cr:             f.Cr,
		YStride:        f.YStride,
		CStride:        f.CStride,
		SubsampleRatio: ratio,
		Rect:           image.Rect(0, 0, f.Width, f.Height),
	}, nil
}
