// perceptual hashes a media file, or scores how alike two of them are.
//
//	perceptual hash   picture.jpg
//	perceptual compare a.mp3 b.flac
//	perceptual compare 1080p.mp4 4k.mp4
//
// It is one interface over three hashes that answer the same question for
// different media: are these essentially the same picture, the same
// recording, the same video? Images use PDQ, audio uses chroma features,
// video uses TMK+PDQF. Decoding is go-avkit's.
//
// ⛔ All three are built for media shared with MINIMUM ADVERSARIALITY —
// recompressed, resized, re-encoded, re-mastered. None of them is built to
// survive cropping, heavy overlays or deliberate obfuscation, and a low score
// from a cropped file is the tool working as designed, not a miss.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/go-perceptual/chroma"
	"github.com/go-perceptual/pdq"
	"github.com/go-perceptual/tmk"
	"github.com/spf13/cobra"
)

// osExit allows tests to override os.Exit.
var osExit = os.Exit

func main() {
	if err := RootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		osExit(1)
	}
}

// RootCmd returns the top-level command, exported so tests drive the wiring
// through SetArgs/SetOut/SetErr instead of spawning a process.
func RootCmd() *cobra.Command {
	var asJSON bool

	root := &cobra.Command{
		Use:          "perceptual",
		Short:        "Hash a media file, or score how alike two of them are",
		SilenceUsage: true,
	}
	root.PersistentFlags().BoolVar(&asJSON, "json", false, "print machine-readable output")

	root.AddCommand(hashCmd(&asJSON), compareCmd(&asJSON))
	return root
}

type hashResult struct {
	File    string `json:"file"`
	Kind    Kind   `json:"kind"`
	Hash    string `json:"hash,omitempty"`
	Quality int    `json:"quality,omitempty"`
	Frames  int    `json:"frames,omitempty"`
	Rate    int    `json:"sample_rate,omitempty"`
}

func hashCmd(asJSON *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "hash <file>",
		Short: "Describe one file's perceptual hash",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := hashOne(args[0])
			if err != nil {
				return err
			}
			if *asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(res)
			}
			switch res.Kind {
			case KindImage:
				fmt.Fprintf(cmd.OutOrStdout(), "image  %s  quality %d\n", res.Hash, res.Quality)
			case KindAudio:
				fmt.Fprintf(cmd.OutOrStdout(), "audio  %d feature frame(s), decoded at %d Hz\n", res.Frames, res.Rate)
			case KindVideo:
				fmt.Fprintf(cmd.OutOrStdout(), "video  %d frame(s) at %d fps\n", res.Frames, tmk.FPS)
			}
			return nil
		},
	}
}

func hashOne(path string) (hashResult, error) {
	kind, err := identify(path)
	if err != nil {
		return hashResult{}, err
	}
	res := hashResult{File: path, Kind: kind}

	switch kind {
	case KindImage:
		img, err := loadImage(path)
		if err != nil {
			return res, err
		}
		h, q, err := pdq.Sum(img)
		if err != nil {
			return res, err
		}
		res.Hash, res.Quality = h.String(), int(q)
	case KindAudio:
		f, rate, err := audioFeatures(path)
		if err != nil {
			return res, err
		}
		res.Frames, res.Rate = len(f), rate
	case KindVideo:
		_, frames, err := videoHash(path)
		if err != nil {
			return res, err
		}
		res.Frames = frames
	}
	return res, nil
}

type compareResult struct {
	A       string  `json:"a"`
	B       string  `json:"b"`
	Kind    Kind    `json:"kind"`
	Score   float64 `json:"score"`
	Verdict string  `json:"verdict"`
	// Distance is the image Hamming distance, which is a different scale
	// from Score and is reported beside it rather than folded in.
	Distance *int `json:"distance,omitempty"`
	Offset   *int `json:"offset,omitempty"`
}

func compareCmd(asJSON *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "compare <a> <b>",
		Short: "Score how alike two files are",
		Long: `Score how alike two files are, on a scale where 1 is the same thing.

Images also report the Hamming distance between their hashes: 0 identical,
30 or less a confident match, 128 the average of two unrelated pictures.

Audio and video report a score between 0 and 1. Video matches at 0.7, the
threshold TMK+PDQF states for itself. Audio needs 0.9: measured here, the
same melody played BACKWARDS — the same notes in the wrong order — scores
0.71, so a 0.7 gate would call it a match.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := compareTwo(args[0], args[1])
			if err != nil {
				return err
			}
			if *asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(res)
			}
			w := cmd.OutOrStdout()
			if res.Distance != nil {
				fmt.Fprintf(w, "%s  score %.4f  distance %d  %s\n", res.Kind, res.Score, *res.Distance, res.Verdict)
			} else {
				fmt.Fprintf(w, "%s  score %.4f  %s\n", res.Kind, res.Score, res.Verdict)
			}
			return nil
		},
	}
}

func compareTwo(pathA, pathB string) (compareResult, error) {
	kindA, err := identify(pathA)
	if err != nil {
		return compareResult{}, err
	}
	kindB, err := identify(pathB)
	if err != nil {
		return compareResult{}, err
	}
	// ⛔ Refused rather than coerced. There is no meaningful score between a
	// picture and a recording, and inventing one — zero, say — would read as
	// "measured, and they differ".
	if kindA != kindB {
		return compareResult{}, fmt.Errorf("cannot compare %s with %s: %s against %s", pathA, pathB, kindA, kindB)
	}

	res := compareResult{A: pathA, B: pathB, Kind: kindA}

	switch kindA {
	case KindImage:
		ia, err := loadImage(pathA)
		if err != nil {
			return res, err
		}
		ib, err := loadImage(pathB)
		if err != nil {
			return res, err
		}
		ha, _, err := pdq.Sum(ia)
		if err != nil {
			return res, err
		}
		hb, _, err := pdq.Sum(ib)
		if err != nil {
			return res, err
		}
		d := ha.Distance(hb)
		res.Distance = &d
		// A similarity in the same direction as the other media: 1 at
		// distance 0, 0 at the 128 two unrelated pictures average.
		res.Score = 1 - float64(d)/128
		if res.Score < 0 {
			res.Score = 0
		}
		res.Verdict = imageVerdict(d)

	case KindAudio:
		fa, _, err := audioFeatures(pathA)
		if err != nil {
			return res, err
		}
		fb, _, err := audioFeatures(pathB)
		if err != nil {
			return res, err
		}
		score, offset := chroma.CompareAt(fa, fb)
		res.Score, res.Offset = score, &offset
		res.Verdict = thresholdVerdict(score, AudioThreshold)

	case KindVideo:
		ha, _, err := videoHash(pathA)
		if err != nil {
			return res, err
		}
		hb, _, err := videoHash(pathB)
		if err != nil {
			return res, err
		}
		score, err := tmk.Level2(ha, hb)
		if err != nil {
			return res, err
		}
		res.Score = score
		res.Verdict = thresholdVerdict(score, tmk.DefaultThreshold)
	}
	return res, nil
}

// AudioThreshold is the chroma score at or above which two recordings are
// called a match.
//
// ⛔ It is 0.9, not the 0.7 that reads naturally beside TMK's video
// threshold, and the difference was MEASURED rather than chosen. Chroma is a
// profile of which pitch classes are present, compared frame by frame at the
// best alignment; two recordings built from the same notes therefore agree
// substantially even when they are not the same piece. On 2026-11-15, against
// an eight-chord melody:
//
//	the same file                      1.000
//	the same melody cut 2 s short      0.9993  (alignment found at offset 16)
//	the same melody at 48 kHz          0.99999999980
//	THE SAME MELODY REVERSED           0.7064
//	an unrelated chord                 0.4374
//
// A 0.7 gate admits the reversed melody. The gap between 0.71 and 0.9993 is
// where the threshold belongs, and 0.9 sits in it.
//
// ⛔ The headroom is SMALLER than that table suggests. A reversal control's
// score depends on how many distinct chords there are to reverse: with eight
// it scores 0.7064, but with FOUR it scores 0.8176, because fewer distinct
// chords means more of the piece overlaps itself. The real margin is 0.08,
// which is why 0.85 would be the wrong choice.
//
// Note the unrelated score, 0.4374: a signal that moves through many pitch
// classes partially matches anything, where a STATIONARY chord against an
// unrelated one scored 0.0000453. A varied recording therefore has a higher
// floor than a sustained one, which is an argument for the threshold being
// well above any observed non-match, not just above the average one.
const AudioThreshold = 0.9

// imageVerdict names the bands the specification gives, rather than inventing
// its own.
func imageVerdict(d int) string {
	switch {
	case d == 0:
		return "identical hashes"
	case d <= 20:
		return "the same picture"
	case d <= 30:
		return "probably the same picture"
	default:
		return "different pictures"
	}
}

func thresholdVerdict(score, threshold float64) string {
	switch {
	case score >= 0.999:
		return "the same recording"
	case score >= threshold:
		return "a match"
	default:
		return "different"
	}
}
