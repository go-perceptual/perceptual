package main

import (
	"fmt"
	"image"

	"github.com/go-perceptual/media"
	"github.com/go-perceptual/pdq"
	"github.com/go-perceptual/tmk"
)

// videoHash hashes a file's first video track, and reports how many frames
// went in.
//
// Decoding and the resampling to tmk.FPS are go-perceptual/media's — including
// the part that is easy to get wrong and impossible to notice: frames are
// resampled BY TIME, not by taking every nth frame, so a 30 fps and a 25 fps
// copy of one film land on the same indices. What stays here is the hash.
func videoHash(path string) (*tmk.Hash, int, error) {
	h := tmk.New()
	used := 0
	err := media.Frames(path, tmk.FPS, 0, func(index int, img image.Image) error {
		feature, _, err := pdq.Float(img)
		if err != nil {
			return err
		}
		// ⛔ The INDEX media hands over, not a counter of our own. tmk.Add
		// takes the frame's position in time, and the kernel compares phases
		// at those integer positions; renumbering them from zero on our side
		// would be correct only by accident.
		h.Add(index, feature)
		used++
		return nil
	})
	if err != nil {
		return nil, 0, fmt.Errorf("%s: %w", path, err)
	}
	hash, err := h.Finish()
	return hash, used, err
}
