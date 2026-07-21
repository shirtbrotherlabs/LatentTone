// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-16

package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultPublicBaseURL is the canonical reverse-proxied origin when unset.
const DefaultPublicBaseURL = "https://latent.lt.lkeng.org"

// DefaultDatabaseDSN points at the Compose mariadb service hostname; override
// via database_dsn (YAML) or DATABASE_DSN / LATENTTONE_DATABASE_DSN (env).
const DefaultDatabaseDSN = "latenttone:latenttone@tcp(mariadb:3306)/latenttone?parseTime=true&charset=utf8mb4&collation=utf8mb4_unicode_ci"

// Config is the scanner / browse / Phase 3 service configuration (scanner.yaml).
type Config struct {
	LibraryRoot   string        `yaml:"library_root"`
	DatabaseDSN   string        `yaml:"database_dsn"`
	Extensions    []string      `yaml:"extensions"`
	Include       []string      `yaml:"include"`
	Exclude       []string      `yaml:"exclude"`
	Concurrency   int           `yaml:"concurrency"`
	CoverNames    []string      `yaml:"cover_names"`
	CoverCacheDir string        `yaml:"cover_cache_dir"`
	ScanInterval  time.Duration `yaml:"scan_interval"`
	ListenAddr    string        `yaml:"listen_addr"`

	// PublicBaseURL is the self-referenced / reverse-proxied canonical origin
	// (no trailing slash). Overridden by PUBLIC_BASE_URL or LATENTTONE_PUBLIC_URL.
	PublicBaseURL string `yaml:"public_base_url"`

	// Phase 3
	AuthMode          string        `yaml:"auth_mode"` // authenticated | open
	SessionTTL        time.Duration `yaml:"session_ttl"`
	EnableStreamProbe bool          `yaml:"enable_stream_probe"`
	EnableAPIDocs     bool          `yaml:"enable_api_docs"`
	HLSRoot           string        `yaml:"hls_root"`
	HLSTTL            time.Duration `yaml:"hls_ttl"`
	MaxSessions         int           `yaml:"max_concurrent_sessions"`
	MaxSessionsPerUser  int           `yaml:"max_sessions_per_user"`
	SessionIdleTTL      time.Duration `yaml:"session_idle_ttl"`
	QueuePrefetch       int           `yaml:"queue_prefetch"`
	FFmpegPath          string        `yaml:"ffmpeg_path"`
	SPARoot             string        `yaml:"spa_root"` // Phase 4 product SPA static root

	// SecureCookieYAML is the optional YAML override. Nil means "unset" so
	// resolveSecureCookie can infer from https PublicBaseURL.
	SecureCookieYAML *bool `yaml:"secure_cookie"`
	// SecureCookie is the resolved flag passed to auth (HttpOnly cookie Secure).
	SecureCookie bool `yaml:"-"`

	// Admin bootstrap (env only — never commit real passwords).
	AdminUsername string `yaml:"-"`
	AdminPassword string `yaml:"-"`
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
	c.applyEnv()
	c.resolveSecureCookie()
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) applyDefaults() {
	if c.LibraryRoot == "" {
		c.LibraryRoot = "/music"
	}
	if c.DatabaseDSN == "" {
		c.DatabaseDSN = DefaultDatabaseDSN
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
	if c.CoverCacheDir == "" {
		c.CoverCacheDir = "/data/covers"
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
		c.MaxSessions = 64
	}
	if c.MaxSessionsPerUser <= 0 {
		c.MaxSessionsPerUser = 16
	}
	if c.SessionIdleTTL <= 0 {
		c.SessionIdleTTL = 45 * time.Minute
	}
	if c.QueuePrefetch <= 0 {
		c.QueuePrefetch = 12
	}
	if c.FFmpegPath == "" {
		c.FFmpegPath = "ffmpeg"
	}
	if c.SPARoot == "" {
		c.SPARoot = "/usr/share/latenttone/app"
	}
	c.PublicBaseURL = NormalizePublicBaseURL(c.PublicBaseURL)
}

// applyEnv overlays PUBLIC_BASE_URL / LATENTTONE_PUBLIC_URL, DATABASE_DSN, and
// admin bootstrap vars. Secure-cookie env is applied in resolveSecureCookie.
func (c *Config) applyEnv() {
	if v := strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL")); v != "" {
		c.PublicBaseURL = NormalizePublicBaseURL(v)
	} else if v := strings.TrimSpace(os.Getenv("LATENTTONE_PUBLIC_URL")); v != "" {
		c.PublicBaseURL = NormalizePublicBaseURL(v)
	}
	if v := strings.TrimSpace(os.Getenv("DATABASE_DSN")); v != "" {
		c.DatabaseDSN = v
	} else if v := strings.TrimSpace(os.Getenv("LATENTTONE_DATABASE_DSN")); v != "" {
		c.DatabaseDSN = v
	}
	if v := strings.TrimSpace(os.Getenv("ADMIN_USERNAME")); v != "" {
		c.AdminUsername = v
	}
	if v := os.Getenv("ADMIN_PASSWORD"); strings.TrimSpace(v) != "" {
		c.AdminPassword = v
	}
}

// resolveSecureCookie sets SecureCookie from (in order):
//  1. SECURE_COOKIE / LATENTTONE_SECURE_COOKIE env (true/false/1/0)
//  2. Explicit YAML secure_cookie
//  3. Inference: https PublicBaseURL → true, otherwise false
func (c *Config) resolveSecureCookie() {
	if v, ok := parseBoolEnv("SECURE_COOKIE"); ok {
		c.SecureCookie = v
		return
	}
	if v, ok := parseBoolEnv("LATENTTONE_SECURE_COOKIE"); ok {
		c.SecureCookie = v
		return
	}
	if c.SecureCookieYAML != nil {
		c.SecureCookie = *c.SecureCookieYAML
		return
	}
	c.SecureCookie = strings.HasPrefix(strings.ToLower(c.PublicBaseURL), "https://")
}

func parseBoolEnv(key string) (bool, bool) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return false, false
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
	}
}

// NormalizePublicBaseURL trims whitespace/trailing slash; empty → DefaultPublicBaseURL.
func NormalizePublicBaseURL(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimRight(s, "/")
	if s == "" {
		return DefaultPublicBaseURL
	}
	return s
}

// AbsoluteURL joins pathOrURL onto PublicBaseURL when relative.
func (c *Config) AbsoluteURL(pathOrURL string) string {
	base := DefaultPublicBaseURL
	if c != nil && c.PublicBaseURL != "" {
		base = c.PublicBaseURL
	}
	p := strings.TrimSpace(pathOrURL)
	if p == "" {
		return base
	}
	if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
		return p
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return base + p
}

// PublicOrigin parses PublicBaseURL for host checks (nil if invalid).
func (c *Config) PublicOrigin() *url.URL {
	base := DefaultPublicBaseURL
	if c != nil && c.PublicBaseURL != "" {
		base = c.PublicBaseURL
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil
	}
	return u
}

func (c *Config) validate() error {
	if c.LibraryRoot == "" {
		return fmt.Errorf("library_root is required")
	}
	if c.DatabaseDSN == "" {
		return fmt.Errorf("database_dsn is required")
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
