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
	"path/filepath"

	"github.com/shirtbrotherlabs/LatentTone/internal/db"
)

// Result is one extractor's output.
type Result struct {
	Name         string
	ModelVersion string
	Features     map[string]any
	Vector       []float32
}

// Extractor produces features + a float vector slice for one track.
type Extractor interface {
	Name() string
	Extract(ctx context.Context, libraryRoot string, track *db.TrackEmbedBrief) (*Result, error)
}

// Registry maps names to extractors.
func Registry(modelVersions map[string]string) map[string]Extractor {
	ver := func(name, def string) string {
		if modelVersions != nil {
			if v, ok := modelVersions[name]; ok && v != "" {
				return v
			}
		}
		return def
	}
	helper := MLHelperConfig{
		Python:     os.Getenv("LATENTTONE_PYTHON"),
		HelperPath: os.Getenv("ML_EMBED_HELPER"),
	}
	return map[string]Extractor{
		"catalog": &Catalog{Version: ver("catalog", "1")},
		"filesig": &FileSig{Version: ver("filesig", "1")},
		"essentia": &Essentia{
			Version: ver("essentia", "music-2.0"),
			Binary:  os.Getenv("ESSENTIA_EXTRACTOR"),
			Profile: os.Getenv("ESSENTIA_PROFILE"),
		},
		"yamnet": &YAMNet{
			Version:  ver("yamnet", "1"),
			Model:    os.Getenv("YAMNET_MODEL"),
			ClassMap: os.Getenv("YAMNET_CLASS_MAP"),
			Helper:   helper,
		},
		"musicnn": &MusiCNN{
			Version: ver("musicnn", "msd-1"),
			Model:   os.Getenv("MUSICNN_MODEL"),
			Meta:    os.Getenv("MUSICNN_META"),
			Helper:  helper,
		},
	}
}

// Stub is an unimplemented optional extractor.
type Stub struct {
	ExtractorName string
	Version       string
}

func (s *Stub) Name() string { return s.ExtractorName }
func (s *Stub) Extract(context.Context, string, *db.TrackEmbedBrief) (*Result, error) {
	return nil, fmt.Errorf("extractor %q not implemented in this build (see ADR-002)", s.ExtractorName)
}

// L2Normalize normalizes v in place; zero vector becomes uniform tiny eps.
func L2Normalize(v []float32) {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return
	}
	inv := float32(1 / math.Sqrt(sum))
	for i := range v {
		v[i] *= inv
	}
}

// FeaturesJSON marshals feature map.
func FeaturesJSON(m map[string]any) (string, error) {
	b, err := json.Marshal(m)
	return string(b), err
}

// AbsPath joins library root and relative path safely.
func AbsPath(libraryRoot, rel string) string {
	return filepath.Join(libraryRoot, filepath.FromSlash(rel))
}
