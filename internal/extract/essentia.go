// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shirtbrotherlabs/LatentTone/internal/db"
	"github.com/shirtbrotherlabs/LatentTone/internal/execprio"
)

const essentiaDim = 64

// Essentia runs essentia_streaming_extractor_music as a subprocess (AGPL binary).
type Essentia struct {
	Version string
	Binary  string // default: essentia_streaming_extractor_music
	Profile string // optional YAML/JSON profile path
}

func (e *Essentia) Name() string { return "essentia" }

func (e *Essentia) Extract(ctx context.Context, libraryRoot string, track *db.TrackEmbedBrief, _ *SharedAudio) (*Result, error) {
	bin := e.Binary
	if bin == "" {
		bin = "essentia_streaming_extractor_music"
	}
	audio := AbsPath(libraryRoot, track.Path)
	if _, err := os.Stat(audio); err != nil {
		return nil, err
	}

	tmpDir, err := os.MkdirTemp("", "lt-essentia-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)
	outJSON := filepath.Join(tmpDir, "features.json")

	args := []string{audio, outJSON}
	if e.Profile != "" {
		args = append(args, e.Profile)
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := execprio.Run(cmd); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("essentia: %s", msg)
	}

	raw, err := os.ReadFile(outJSON)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse essentia json: %w", err)
	}

	features, vec := mapEssentia(doc)
	L2Normalize(vec)
	return &Result{
		Name:         e.Name(),
		ModelVersion: e.Version,
		Features:     features,
		Vector:       vec,
	}, nil
}

func mapEssentia(doc map[string]any) (map[string]any, []float32) {
	low := asMap(doc["lowlevel"])
	rhythm := asMap(doc["rhythm"])
	tonal := asMap(doc["tonal"])

	mfccMean := floatSlice(asMap(low["mfcc"])["mean"])
	hpcpMean := floatSlice(asMap(tonal["hpcp"])["mean"])

	bpm := asFloat(rhythm["bpm"])
	dance := asFloat(rhythm["danceability"])
	onset := asFloat(rhythm["onset_rate"])
	loud := asFloat(low["average_loudness"])
	dyn := asFloat(low["dynamic_complexity"])
	dissonance := asFloat(asMap(low["dissonance"])["mean"])
	centroid := asFloat(asMap(low["spectral_centroid"])["mean"])
	complexity := asFloat(asMap(low["spectral_complexity"])["mean"])
	zcr := asFloat(asMap(low["zerocrossingrate"])["mean"])
	keyStrength := asFloat(asMap(tonal["key_edma"])["strength"])
	chordsStrength := asFloat(asMap(tonal["chords_strength"])["mean"])
	tuning := asFloat(tonal["tuning_diatonic_strength"])

	features := map[string]any{
		"bpm":                 bpm,
		"danceability":        dance,
		"onset_rate":          onset,
		"average_loudness":    loud,
		"dynamic_complexity":  dyn,
		"dissonance_mean":     dissonance,
		"spectral_centroid":   centroid,
		"spectral_complexity": complexity,
		"zerocrossingrate":    zcr,
		"key_strength":        keyStrength,
		"chords_strength":     chordsStrength,
		"tuning_diatonic":     tuning,
		"mfcc_mean":           mfccMean,
		"hpcp_mean_len":       len(hpcpMean),
	}

	vec := make([]float32, essentiaDim)
	i := 0
	put := func(v float64) {
		if i >= essentiaDim {
			return
		}
		vec[i] = float32(v)
		i++
	}
	// Scalars (12) — soft scale into roughly [0,1]-ish ranges
	put(clamp01(loud))
	put(clamp01(dyn / 10))
	put(clamp01(dissonance))
	put(clamp01(centroid / 8000))
	put(clamp01(complexity / 20))
	put(clamp01(zcr))
	put(clamp01(bpm / 200))
	put(clamp01(dance / 3))
	put(clamp01(onset / 10))
	put(clamp01(keyStrength))
	put(clamp01(chordsStrength))
	put(clamp01(tuning))

	for _, v := range mfccMean {
		if i >= essentiaDim {
			break
		}
		// MFCC roughly [-100,100]
		put(math.Tanh(v / 50))
	}
	for _, v := range hpcpMean {
		if i >= essentiaDim {
			break
		}
		put(clamp01(v))
	}
	return features, vec
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func asFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	default:
		return 0
	}
}

func floatSlice(v any) []float64 {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]float64, 0, len(arr))
	for _, x := range arr {
		out = append(out, asFloat(x))
	}
	return out
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}
