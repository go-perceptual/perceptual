package main

import (
	"fmt"

	"github.com/go-perceptual/chroma"
	"github.com/go-perceptual/media"
)

// audioFeatures returns a file's chroma features, and the rate its audio was
// stored at.
//
// The decoding, downmixing and resampling are go-perceptual/media's: see that
// package for why each step is the way it is, and why none of them should
// exist in two places. What stays here is the one thing that is this tool's
// business — which feature, at which rate.
//
// ⛔ MonoFloat and Resample rather than Mono, which would do both in one call:
// the rate the file was STORED at is reported to the user, and Mono does not
// hand it back. Calling Mono and then MonoFloat for the rate would decode the
// file twice.
func audioFeatures(path string) ([][]float64, int, error) {
	mono, rate, err := media.MonoFloat(path, 0)
	if err != nil {
		return nil, 0, fmt.Errorf("%s: %w", path, err)
	}
	// ⛔ chroma.SampleRate, not a number. The rate is part of the FEATURE: the
	// frame geometry and the frequency bounds are expressed in samples at it,
	// so audio fed at another rate produces a perfectly plausible fingerprint
	// that matches nothing.
	pcm, err := media.Resample(mono, rate, chroma.SampleRate)
	if err != nil {
		return nil, 0, fmt.Errorf("%s: %w", path, err)
	}
	features, err := chroma.Features(pcm)
	if err != nil {
		return nil, 0, fmt.Errorf("%s: %w", path, err)
	}
	return features, rate, nil
}
