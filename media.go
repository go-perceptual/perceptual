package main

import (
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"

	"github.com/go-perceptual/media"
)

// Kind is what a file turned out to hold.
//
// It is an alias for media.Kind rather than a type of its own: the JSON this
// tool prints carries these strings, and a second enumeration that had to be
// mapped onto the first is how the two drift apart.
type Kind = media.Kind

const (
	KindImage = media.KindImage
	KindAudio = media.KindAudio
	KindVideo = media.KindVideo
)

// ErrUnknownMedia is returned for a file that is none of the three.
var ErrUnknownMedia = errors.New("not an image, and no audio or video track")

// identify says what a file holds, by READING it rather than trusting its
// name. See media.Classify for why, and for how little of the file it needs.
//
// The only thing added here is the error for KindOther: media reports "not
// media" as a fact rather than a failure, because most of a directory tree is
// not media — but a CLI handed one file by name has been asked a question
// about THAT file, and "it is not media" is the answer, not a silence.
func identify(path string) (Kind, error) {
	kind, err := media.Classify(path)
	if err != nil {
		return media.KindOther, err
	}
	if kind == media.KindOther {
		return media.KindOther, fmt.Errorf("%s: %w", path, ErrUnknownMedia)
	}
	return kind, nil
}

func loadImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}
