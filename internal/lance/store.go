// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

// Package lance wraps the on-disk LanceDB helper (Python subprocess) for ANN upsert/search.
package lance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/shirtbrotherlabs/LatentTone/internal/execprio"
)

// Store is a thin client around scripts/lance_helper.py.
type Store struct {
	DBPath     string
	Table      string
	HelperPath string // path to lance_helper.py
	Python     string // default: python3 from PATH, or LATENTTONE_PYTHON
}

// Enabled reports whether LanceDB is configured.
func (s *Store) Enabled() bool {
	return s != nil && s.DBPath != "" && s.HelperPath != ""
}

func (s *Store) pythonBin() string {
	if s.Python != "" {
		return s.Python
	}
	if v := os.Getenv("LATENTTONE_PYTHON"); v != "" {
		return v
	}
	return "python3"
}

// Upsert writes (track_id, vector) rows into the LanceDB table.
func (s *Store) Upsert(ctx context.Context, trackID int64, vec []float32) (string, error) {
	if !s.Enabled() {
		return "", nil
	}
	if err := os.MkdirAll(s.DBPath, 0o755); err != nil {
		return "", err
	}
	table := s.table()
	row, err := json.Marshal(map[string]any{
		"track_id": trackID,
		"vector":   float32To64(vec),
	})
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, s.pythonBin(), s.HelperPath, "upsert", s.DBPath, table)
	cmd.Stdin = bytes.NewReader(append(row, '\n'))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := execprio.Run(cmd); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("lancedb upsert: %s", msg)
	}
	return fmt.Sprintf("%s/%s#%d", s.DBPath, table, trackID), nil
}

// Neighbor is a search hit.
type Neighbor struct {
	TrackID int64   `json:"track_id"`
	Score   float64 `json:"score"`
}

// Search runs ANN (or brute) search for the query vector.
func (s *Store) Search(ctx context.Context, vec []float32, k int, excludeTrackID int64) ([]Neighbor, error) {
	if !s.Enabled() {
		return nil, fmt.Errorf("lancedb not configured")
	}
	if k <= 0 {
		k = 10
	}
	body, err := json.Marshal(map[string]any{
		"vector":           float32To64(vec),
		"exclude_track_id": excludeTrackID,
	})
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, s.pythonBin(), s.HelperPath, "search", s.DBPath, s.table(), fmt.Sprintf("%d", k))
	cmd.Stdin = bytes.NewReader(body)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := execprio.Run(cmd); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("lancedb search: %s", msg)
	}
	var out []Neighbor
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return nil, fmt.Errorf("lancedb search parse: %w", err)
	}
	return out, nil
}

// DumpRow is one LanceDB row with a truncated vector for UI display.
type DumpRow struct {
	TrackID       int64     `json:"track_id"`
	VectorDim     int       `json:"vector_dim"`
	VectorPreview []float64 `json:"vector_preview"`
	VectorTail    []float64 `json:"vector_tail"`
}

// DumpResult is a paginated LanceDB table dump.
type DumpResult struct {
	DBPath  string    `json:"db_path"`
	Table   string    `json:"table"`
	Tables  []string  `json:"tables"`
	Count   int       `json:"count"`
	Offset  int       `json:"offset"`
	Limit   int       `json:"limit"`
	Preview int       `json:"preview"`
	Rows    []DumpRow `json:"rows"`
	Error   string    `json:"error,omitempty"`
}

// Dump returns a paginated snapshot of the LanceDB table (vectors truncated).
func (s *Store) Dump(ctx context.Context, limit, offset, preview int) (*DumpResult, error) {
	if !s.Enabled() {
		return nil, fmt.Errorf("lancedb not configured")
	}
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	if preview <= 0 {
		preview = 8
	}
	cmd := exec.CommandContext(ctx, s.pythonBin(), s.HelperPath, "dump", s.DBPath, s.table(),
		fmt.Sprintf("%d", limit), fmt.Sprintf("%d", offset), fmt.Sprintf("%d", preview))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := execprio.Run(cmd); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("lancedb dump: %s", msg)
	}
	var out DumpResult
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return nil, fmt.Errorf("lancedb dump parse: %w", err)
	}
	return &out, nil
}

func (s *Store) table() string {
	if s.Table == "" {
		return "track_vectors"
	}
	return s.Table
}

func float32To64(v []float32) []float64 {
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = float64(x)
	}
	return out
}
