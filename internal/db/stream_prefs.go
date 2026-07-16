// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package db

import (
	"database/sql"
	"strings"
)

const (
	StreamFormatOriginal = "original"
	StreamFormatMP3      = "mp3"
	StreamFormatAAC      = "aac"

	DefaultStreamBitrateKbps = 192
)

// StreamPrefs are per-user progressive/HLS encode defaults.
// Default is original (no transcode) unless the container is browser-unsafe.
type StreamPrefs struct {
	UserID       int64  `json:"user_id"`
	StreamFormat string `json:"stream_format"`
	BitrateKbps  int    `json:"bitrate_kbps"`
	UpdatedAt    string `json:"updated_at,omitempty"`
}

// DefaultStreamPrefs returns product defaults (original / 192 kbps target).
func DefaultStreamPrefs(userID int64) StreamPrefs {
	return StreamPrefs{
		UserID:       userID,
		StreamFormat: StreamFormatOriginal,
		BitrateKbps:  DefaultStreamBitrateKbps,
	}
}

// NormalizeStreamFormat returns a known format or empty when invalid.
func NormalizeStreamFormat(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case StreamFormatOriginal, StreamFormatMP3, StreamFormatAAC:
		return strings.ToLower(strings.TrimSpace(s))
	default:
		return ""
	}
}

// GetStreamPrefs loads prefs or returns defaults when no row exists.
func (d *DB) GetStreamPrefs(userID int64) (StreamPrefs, error) {
	def := DefaultStreamPrefs(userID)
	row := d.SQL.QueryRow(`
SELECT user_id, stream_format, bitrate_kbps, updated_at
FROM user_stream_prefs WHERE user_id = ?`, userID)
	var p StreamPrefs
	err := row.Scan(&p.UserID, &p.StreamFormat, &p.BitrateKbps, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return def, nil
	}
	if err != nil {
		return def, err
	}
	if NormalizeStreamFormat(p.StreamFormat) == "" {
		p.StreamFormat = def.StreamFormat
	}
	if p.BitrateKbps <= 0 {
		p.BitrateKbps = def.BitrateKbps
	}
	return p, nil
}

// UpsertStreamPrefs persists stream encode preferences.
func (d *DB) UpsertStreamPrefs(p StreamPrefs) (StreamPrefs, error) {
	if NormalizeStreamFormat(p.StreamFormat) == "" {
		p.StreamFormat = StreamFormatOriginal
	}
	if p.BitrateKbps < 64 {
		p.BitrateKbps = 64
	}
	if p.BitrateKbps > 320 {
		p.BitrateKbps = 320
	}
	now := Now()
	_, err := d.SQL.Exec(`
INSERT INTO user_stream_prefs (user_id, stream_format, bitrate_kbps, updated_at)
VALUES (?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  stream_format = VALUES(stream_format),
  bitrate_kbps = VALUES(bitrate_kbps),
  updated_at = VALUES(updated_at)`,
		p.UserID, p.StreamFormat, p.BitrateKbps, now,
	)
	if err != nil {
		return p, err
	}
	return d.GetStreamPrefs(p.UserID)
}
