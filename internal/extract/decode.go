// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-18

package extract

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/shirtbrotherlabs/LatentTone/internal/execprio"
)

// SharedAudio is a per-track decoded intermediate for extractors that share PCM shape.
// Path is a temp raw float32 LE mono 16 kHz file (max ~45s). Never under /music.
type SharedAudio struct {
	Path       string // raw f32le PCM
	SampleRate int
	MaxSeconds float64
}

// NeedsSharedDecode reports whether ≥2 PCM-consuming extractors are active.
// Catalog/filesig do not decode audio; Essentia keeps the original path (different
// analysis needs). Shared decode targets YAMNet + MusiCNN (identical mono/16k/45s).
func NeedsSharedDecode(names []string) bool {
	n := 0
	for _, name := range names {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "yamnet", "musicnn":
			n++
		}
	}
	return n >= 2
}

// DecodeSharedAudio runs ffmpeg once into a temp f32le PCM file under os.TempDir.
// Caller must Cleanup. ffmpegPath empty → "ffmpeg".
func DecodeSharedAudio(ctx context.Context, ffmpegPath, absAudio string) (*SharedAudio, error) {
	if absAudio == "" {
		return nil, fmt.Errorf("empty audio path")
	}
	if _, err := os.Stat(absAudio); err != nil {
		return nil, err
	}
	bin := ffmpegPath
	if bin == "" {
		bin = "ffmpeg"
	}
	const (
		sr  = 16000
		max = 45.0
	)
	tmp, err := os.CreateTemp("", "lt-pcm-*.f32")
	if err != nil {
		return nil, err
	}
	outPath := tmp.Name()
	_ = tmp.Close()

	cmd := exec.CommandContext(ctx, bin,
		"-v", "error",
		"-i", absAudio,
		"-t", fmt.Sprintf("%g", max),
		"-ac", "1",
		"-ar", fmt.Sprintf("%d", sr),
		"-f", "f32le",
		"-y", outPath,
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := execprio.Run(cmd); err != nil {
		_ = os.Remove(outPath)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("shared decode: %s", msg)
	}
	fi, err := os.Stat(outPath)
	if err != nil || fi.Size() == 0 {
		_ = os.Remove(outPath)
		return nil, fmt.Errorf("shared decode: empty output")
	}
	return &SharedAudio{Path: outPath, SampleRate: sr, MaxSeconds: max}, nil
}

// Cleanup removes the shared PCM temp file.
func (s *SharedAudio) Cleanup() {
	if s == nil || s.Path == "" {
		return
	}
	_ = os.Remove(s.Path)
	s.Path = ""
}
