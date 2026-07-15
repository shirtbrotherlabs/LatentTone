// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package extract

import (
	"context"
	"hash/fnv"
	"io"
	"math"
	"os"

	"github.com/shirtbrotherlabs/LatentTone/internal/db"
)

// FileSig samples file bytes (read-only) into a compact signature vector.
type FileSig struct {
	Version string
}

func (f *FileSig) Name() string { return "filesig" }

func (f *FileSig) Extract(ctx context.Context, libraryRoot string, track *db.TrackEmbedBrief) (*Result, error) {
	const dim = 32
	path := AbsPath(libraryRoot, track.Path)
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	const sampleSize = 64 * 1024
	buf := make([]byte, sampleSize)
	var hist [16]float64
	var total int
	var energy float64

	// Read head + mid + tail samples
	offsets := []int64{0}
	if track.FileSize > sampleSize*2 {
		offsets = append(offsets, track.FileSize/2-sampleSize/2, track.FileSize-sampleSize)
	}
	for _, off := range offsets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if off < 0 {
			off = 0
		}
		if _, err := file.Seek(off, io.SeekStart); err != nil {
			continue
		}
		n, _ := io.ReadFull(file, buf)
		if n <= 0 {
			continue
		}
		for i := 0; i < n; i++ {
			b := buf[i]
			hist[b>>4]++
			energy += float64(b) * float64(b)
			total++
		}
	}

	vec := make([]float32, dim)
	features := map[string]any{
		"bytes_sampled": total,
		"file_size":     track.FileSize,
		"path":          track.Path,
	}
	if total == 0 {
		L2Normalize(vec)
		return &Result{Name: f.Name(), ModelVersion: f.Version, Features: features, Vector: vec}, nil
	}
	for i := 0; i < 16; i++ {
		vec[i] = float32(hist[i] / float64(total))
	}
	vec[16] = float32(math.Sqrt(energy/float64(total)) / 255)
	h := fnv.New64a()
	_, _ = h.Write(buf[:min(total, len(buf))])
	sum := h.Sum64()
	for i := 0; i < 15; i++ {
		vec[17+i] = float32((sum>>uint(i*4))&0xf) / 15
	}
	features["rms_byte"] = vec[16]
	features["hist16"] = vec[:16]

	L2Normalize(vec)
	return &Result{Name: f.Name(), ModelVersion: f.Version, Features: features, Vector: vec}, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
