package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"

	"github.com/go-avkit/avkit/container"
)

// Kind is what a file turned out to hold.
type Kind string

const (
	KindImage Kind = "image"
	KindAudio Kind = "audio"
	KindVideo Kind = "video"
)

// ErrUnknownMedia is returned for a file that is none of the three.
var ErrUnknownMedia = errors.New("not an image, and no audio or video track")

// identify says what a file holds, by READING it rather than trusting its
// name.
//
// ⛔ An extension is a claim by whoever named the file. A `.jpg` holding a
// PNG, or a `.mp4` holding only audio, are ordinary things in a download
// folder — and a tool that believes the name reports a confident wrong
// answer. The image decoders are asked first because they are cheap and
// definitive; then the container is sniffed.
func identify(path string) (Kind, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	if _, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		return KindImage, nil
	}

	r, err := container.NewReader(data)
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, ErrUnknownMedia)
	}
	hasVideo, hasAudio := false, false
	for _, tr := range r.File().Tracks {
		switch tr.Kind {
		case container.Video:
			hasVideo = true
		case container.Audio:
			hasAudio = true
		}
	}
	switch {
	case hasVideo:
		return KindVideo, nil
	case hasAudio:
		return KindAudio, nil
	}
	return "", fmt.Errorf("%s: %w", path, ErrUnknownMedia)
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
