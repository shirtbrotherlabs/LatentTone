// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the scanner / browse / Phase 3 service configuration (scanner.yaml).
type Config struct {
	LibraryRoot  string        `yaml:"library_root"`
	DatabasePath string        `yaml:"database_path"`
	Extensions   []string      `yaml:"extensions"`
	Include      []string      `yaml:"include"`
	Exclude      []string      `yaml:"exclude"`
	Concurrency  int           `yaml:"concurrency"`
	CoverNames   []string      `yaml:"cover_names"`
	ScanInterval time.Duration `yaml:"scan_interval"`
	ListenAddr   string        `yaml:"listen_addr"`

	// Phase 3
	AuthMode          string        `yaml:"auth_mode"` // authenticated | open
	SessionTTL        time.Duration `yaml:"session_ttl"`
	SecureCookie      bool          `yaml:"secure_cookie"`
	EnableStreamProbe bool          `yaml:"enable_stream_probe"`
	EnableAPIDocs     bool          `yaml:"enable_api_docs"`
	HLSRoot           string        `yaml:"hls_root"`
	HLSTTL            time.Duration `yaml:"hls_ttl"`
	MaxSessions       int           `yaml:"max_concurrent_sessions"`
	QueuePrefetch     int           `yaml:"queue_prefetch"`
	FFmpegPath        string        `yaml:"ffmpeg_path"`
}

// Load reads and validates scanner configuration from path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	c.applyDefaults()
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) applyDefaults() {
	if c.LibraryRoot == "" {
		c.LibraryRoot = "/music"
	}
	if c.DatabasePath == "" {
		c.DatabasePath = "/data/latenttone.db"
	}
	if c.Concurrency <= 0 {
		c.Concurrency = 4
	}
	if len(c.Extensions) == 0 {
		c.Extensions = []string{"flac", "mp3", "m4a", "aac", "ogg", "opus", "wav", "aiff"}
	}
	if len(c.CoverNames) == 0 {
		c.CoverNames = []string{"cover.jpg", "cover.png", "folder.jpg", "folder.png"}
	}
	if c.ListenAddr == "" {
		c.ListenAddr = ":8080"
	}
	if c.AuthMode == "" {
		c.AuthMode = "authenticated"
	}
	if c.SessionTTL <= 0 {
		c.SessionTTL = 7 * 24 * time.Hour
	}
	if c.HLSRoot == "" {
		c.HLSRoot = "/data/hls"
	}
	if c.HLSTTL <= 0 {
		c.HLSTTL = 2 * time.Hour
	}
	if c.MaxSessions <= 0 {
		c.MaxSessions = 8
	}
	if c.QueuePrefetch <= 0 {
		c.QueuePrefetch = 2
	}
	if c.FFmpegPath == "" {
		c.FFmpegPath = "ffmpeg"
	}
}

func (c *Config) validate() error {
	if c.LibraryRoot == "" {
		return fmt.Errorf("library_root is required")
	}
	if c.DatabasePath == "" {
		return fmt.Errorf("database_path is required")
	}
	switch c.AuthMode {
	case "authenticated", "open":
	default:
		return fmt.Errorf("auth_mode must be authenticated or open")
	}
	return nil
}

// ExtSet returns a set of allowed extensions (lowercase, no dot).
func (c *Config) ExtSet() map[string]struct{} {
	out := make(map[string]struct{}, len(c.Extensions))
	for _, e := range c.Extensions {
		if e == "" {
			continue
		}
		out[normalizeExt(e)] = struct{}{}
	}
	return out
}

func normalizeExt(e string) string {
	if len(e) > 0 && e[0] == '.' {
		e = e[1:]
	}
	b := make([]byte, len(e))
	for i := 0; i < len(e); i++ {
		c := e[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}
