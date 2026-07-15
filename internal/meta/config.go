// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package meta

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is metadata.yaml for the embedding tool.
type Config struct {
	LibraryRoot     string            `yaml:"library_root"`
	DatabasePath    string            `yaml:"database_path"`
	Extractors      []string          `yaml:"extractors"`
	ModelVersions   map[string]string `yaml:"model_versions"`
	Concurrency     int               `yaml:"concurrency"`
	BatchSize       int               `yaml:"batch_size"`
	SampleMode      string            `yaml:"sample_mode"`
	MaxTracks       int               `yaml:"max_tracks"`
	SampleSeed      int64             `yaml:"sample_seed"`
	EmbedInterval   time.Duration     `yaml:"embed_interval"`
	JobControlPath  string            `yaml:"job_control_path"`
	LanceDBPath     string            `yaml:"lancedb_path"`
	LanceDBTable    string            `yaml:"lancedb_table"`
	LanceHelperPath string            `yaml:"lance_helper_path"`
	EssentiaBinary   string            `yaml:"essentia_binary"`
	EssentiaProfile  string            `yaml:"essentia_profile"`
	MLHelperPath    string            `yaml:"ml_helper_path"`
	YAMNetModel     string            `yaml:"yamnet_model"`
	YAMNetClassMap  string            `yaml:"yamnet_class_map"`
	MusiCNNModel    string            `yaml:"musicnn_model"`
	MusiCNNMeta     string            `yaml:"musicnn_meta"`
}

// Load reads metadata.yaml.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read metadata config: %w", err)
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse metadata config: %w", err)
	}
	c.applyDefaults()
	return &c, nil
}

func (c *Config) applyDefaults() {
	if c.LibraryRoot == "" {
		c.LibraryRoot = "/music"
	}
	if c.DatabasePath == "" {
		c.DatabasePath = "/data/latenttone.db"
	}
	if len(c.Extractors) == 0 {
		c.Extractors = []string{"catalog", "filesig"}
	}
	if c.Concurrency <= 0 {
		c.Concurrency = 2
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 32
	}
	if c.SampleMode == "" {
		c.SampleMode = "random"
	}
	if c.MaxTracks <= 0 {
		c.MaxTracks = 64
	}
	if c.JobControlPath == "" {
		c.JobControlPath = "/data/embed.job"
	}
	if c.ModelVersions == nil {
		c.ModelVersions = map[string]string{}
	}
	if c.LanceDBPath == "" {
		c.LanceDBPath = "/data/lancedb"
	}
	if c.LanceDBTable == "" {
		c.LanceDBTable = "track_vectors"
	}
	if c.LanceHelperPath == "" {
		c.LanceHelperPath = "/usr/local/lib/latenttone/lance_helper.py"
	}
	if c.EssentiaBinary == "" {
		c.EssentiaBinary = "essentia_streaming_extractor_music"
	}
	if c.EssentiaProfile == "" {
		c.EssentiaProfile = "/config/essentia_profile.yaml"
	}
	if c.MLHelperPath == "" {
		c.MLHelperPath = "/usr/local/lib/latenttone/ml_embed_helper.py"
	}
	if c.YAMNetModel == "" {
		c.YAMNetModel = "/models/yamnet/yamnet.tflite"
	}
	if c.YAMNetClassMap == "" {
		c.YAMNetClassMap = "/models/yamnet/yamnet_class_map.csv"
	}
	if c.MusiCNNModel == "" {
		c.MusiCNNModel = "/models/musicnn/msd-musicnn-1.onnx"
	}
	if c.MusiCNNMeta == "" {
		c.MusiCNNMeta = "/models/musicnn/msd-musicnn-1.json"
	}
}

// Clone returns a shallow copy (safe for per-request overrides).
func (c *Config) Clone() *Config {
	if c == nil {
		return nil
	}
	out := *c
	if c.Extractors != nil {
		out.Extractors = append([]string(nil), c.Extractors...)
	}
	if c.ModelVersions != nil {
		out.ModelVersions = make(map[string]string, len(c.ModelVersions))
		for k, v := range c.ModelVersions {
			out.ModelVersions[k] = v
		}
	}
	return &out
}

// ForWebStart returns config for the browse UI identity scan: all pending tracks,
// with parallel Essentia workers (at least 4, or configured concurrency if higher).
func (c *Config) ForWebStart() *Config {
	out := c.Clone()
	out.SampleMode = "all"
	ml := false
	for _, e := range out.Extractors {
		if e == "yamnet" || e == "musicnn" {
			ml = true
			break
		}
	}
	if ml {
		// ML extractors are heavier; keep web concurrency modest to avoid host OOM.
		if out.Concurrency < 1 {
			out.Concurrency = 1
		}
		if out.Concurrency > 2 {
			out.Concurrency = 2
		}
	} else if out.Concurrency < 4 {
		out.Concurrency = 4
	}
	return out
}

// ExtractorSetString joins enabled extractors.
func (c *Config) ExtractorSetString() string {
	out := ""
	for i, e := range c.Extractors {
		if i > 0 {
			out += "+"
		}
		out += e
	}
	return out
}
