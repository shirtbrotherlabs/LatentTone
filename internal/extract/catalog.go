// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package extract

import (
	"context"
	"database/sql"
	"hash/fnv"
	"math"
	"strings"

	"github.com/shirtbrotherlabs/LatentTone/internal/db"
)

// Catalog builds a deterministic vector from catalog metadata (OSS, no I/O beyond DB).
type Catalog struct {
	Version string
}

func (c *Catalog) Name() string { return "catalog" }

func (c *Catalog) Extract(_ context.Context, _ string, track *db.TrackEmbedBrief, _ *SharedAudio) (*Result, error) {
	const dim = 32
	vec := make([]float32, dim)
	features := map[string]any{
		"title":       track.Title,
		"artist":      track.ArtistName,
		"album":       track.AlbumTitle,
		"format":      track.Format,
		"genres":      track.Genres,
		"duration_ms": nullI64(track.DurationMS),
		"bitrate":     nullI64(track.BitrateKbps),
		"year":        nullI64(track.Year),
	}

	dur := float64(nullI64(track.DurationMS))
	vec[0] = float32(math.Log1p(dur) / 15)
	vec[1] = float32(nullI64(track.BitrateKbps)) / 320
	vec[2] = float32(nullI64(track.SampleRateHz)) / 48000
	vec[3] = float32(nullI64(track.Channels)) / 2
	yr := float64(nullI64(track.Year))
	if yr > 1900 {
		vec[4] = float32((yr - 1950) / 80)
	}
	vec[5] = formatOneHot(track.Format, 0)
	vec[6] = formatOneHot(track.Format, 1)
	vec[7] = formatOneHot(track.Format, 2)
	vec[8] = formatOneHot(track.Format, 3)

	for _, g := range strings.Split(track.Genres, "|") {
		g = strings.TrimSpace(strings.ToLower(g))
		if g == "" {
			continue
		}
		h := fnv.New32a()
		_, _ = h.Write([]byte(g))
		idx := 9 + int(h.Sum32()%12)
		vec[idx] += 1
	}
	for i, s := range []string{track.Title, track.ArtistName, track.AlbumTitle} {
		h := fnv.New32a()
		_, _ = h.Write([]byte(strings.ToLower(s)))
		vec[21+i] = float32(h.Sum32()%1000) / 1000
		vec[24+i] = float32((h.Sum32()>>10)%1000) / 1000
	}
	vec[30] = float32(math.Log1p(float64(track.FileSize)) / 25)
	vec[31] = float32(track.FileMtime%86400) / 86400

	L2Normalize(vec)
	return &Result{Name: c.Name(), ModelVersion: c.Version, Features: features, Vector: vec}, nil
}

func formatOneHot(format string, slot int) float32 {
	f := strings.ToLower(format)
	order := []string{"mp3", "flac", "m4a", "ogg"}
	if slot >= len(order) {
		return 0
	}
	if f == order[slot] {
		return 1
	}
	return 0
}

func nullI64(n sql.NullInt64) int64 {
	if n.Valid {
		return n.Int64
	}
	return 0
}
