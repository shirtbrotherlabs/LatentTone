// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package extract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/shirtbrotherlabs/LatentTone/internal/db"
)

const mlBlockDim = 64

// MLHelperConfig is shared by YAMNet / MusiCNN subprocess extractors.
type MLHelperConfig struct {
	Python     string
	HelperPath string
}

func (c MLHelperConfig) pythonBin() string {
	if c.Python != "" {
		return c.Python
	}
	if v := os.Getenv("LATENTTONE_PYTHON"); v != "" {
		return v
	}
	return "python3"
}

func (c MLHelperConfig) helper() string {
	if c.HelperPath != "" {
		return c.HelperPath
	}
	return "/usr/local/lib/latenttone/ml_embed_helper.py"
}

type mlHelperOut struct {
	Features map[string]any `json:"features"`
	Vector   []float64      `json:"vector"`
}

func runMLHelper(ctx context.Context, cfg MLHelperConfig, args ...string) (*Result, error) {
	cmdArgs := append([]string{cfg.helper()}, args...)
	cmd := exec.CommandContext(ctx, cfg.pythonBin(), cmdArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("ml helper: %s", msg)
	}
	var out mlHelperOut
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return nil, fmt.Errorf("ml helper parse: %w", err)
	}
	if len(out.Vector) == 0 {
		return nil, fmt.Errorf("ml helper returned empty vector")
	}
	vec := make([]float32, len(out.Vector))
	for i, x := range out.Vector {
		vec[i] = float32(x)
	}
	if len(vec) != mlBlockDim {
		fixed := make([]float32, mlBlockDim)
		n := len(vec)
		if n > mlBlockDim {
			n = mlBlockDim
		}
		copy(fixed, vec[:n])
		L2Normalize(fixed)
		vec = fixed
	} else {
		L2Normalize(vec)
	}
	if out.Features == nil {
		out.Features = map[string]any{}
	}
	return &Result{Features: out.Features, Vector: vec}, nil
}

// YAMNet runs TFLite YAMNet via ml_embed_helper.py.
type YAMNet struct {
	Version  string
	Model    string
	ClassMap string
	Helper   MLHelperConfig
}

func (y *YAMNet) Name() string { return "yamnet" }

func (y *YAMNet) Extract(ctx context.Context, libraryRoot string, track *db.TrackEmbedBrief) (*Result, error) {
	audio := AbsPath(libraryRoot, track.Path)
	model := y.Model
	if model == "" {
		model = "/models/yamnet/yamnet.tflite"
	}
	cmap := y.ClassMap
	if cmap == "" {
		cmap = "/models/yamnet/yamnet_class_map.csv"
	}
	res, err := runMLHelper(ctx, y.Helper, "yamnet", audio, model, cmap)
	if err != nil {
		return nil, err
	}
	res.Name = y.Name()
	res.ModelVersion = y.Version
	if res.ModelVersion == "" {
		res.ModelVersion = "1"
	}
	return res, nil
}

// MusiCNN runs MSD MusiCNN ONNX via ml_embed_helper.py.
type MusiCNN struct {
	Version string
	Model   string
	Meta    string
	Helper  MLHelperConfig
}

func (m *MusiCNN) Name() string { return "musicnn" }

func (m *MusiCNN) Extract(ctx context.Context, libraryRoot string, track *db.TrackEmbedBrief) (*Result, error) {
	audio := AbsPath(libraryRoot, track.Path)
	model := m.Model
	if model == "" {
		model = "/models/musicnn/msd-musicnn-1.onnx"
	}
	meta := m.Meta
	if meta == "" {
		meta = "/models/musicnn/msd-musicnn-1.json"
	}
	res, err := runMLHelper(ctx, m.Helper, "musicnn", audio, model, meta)
	if err != nil {
		return nil, err
	}
	res.Name = m.Name()
	res.ModelVersion = m.Version
	if res.ModelVersion == "" {
		res.ModelVersion = "msd-1"
	}
	return res, nil
}
