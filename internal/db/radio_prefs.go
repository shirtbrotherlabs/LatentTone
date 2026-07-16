// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package db

import (
	"database/sql"
)

// RadioPrefs are per-user toggles for live Radio diversification (ADR-007).
// Defaults favor diversification (cooldown, jitter, penalty, bounded random, bridge ON).
type RadioPrefs struct {
	UserID         int64   `json:"user_id"`
	RadioBridge    bool    `json:"radio_bridge"`
	ArtistCooldown bool    `json:"artist_cooldown"`
	QueryJitter    bool    `json:"query_jitter"`
	ArtistPenalty  bool    `json:"artist_penalty"`
	BoundedRandom  bool    `json:"bounded_random"`
	JitterAlpha    float64 `json:"jitter_alpha"`
	UpdatedAt      string  `json:"updated_at,omitempty"`
}

// DefaultRadioPrefs returns product defaults (all diversification strategies ON).
func DefaultRadioPrefs(userID int64) RadioPrefs {
	return RadioPrefs{
		UserID:         userID,
		RadioBridge:    true,
		ArtistCooldown: true,
		QueryJitter:    true,
		ArtistPenalty:  true,
		BoundedRandom:  true,
		JitterAlpha:    0.05,
	}
}

// GetRadioPrefs loads prefs or returns defaults when no row exists.
func (d *DB) GetRadioPrefs(userID int64) (RadioPrefs, error) {
	def := DefaultRadioPrefs(userID)
	row := d.SQL.QueryRow(`
SELECT user_id, radio_bridge, artist_cooldown, query_jitter, artist_penalty, bounded_random, jitter_alpha, updated_at
FROM user_radio_prefs WHERE user_id = ?`, userID)
	var p RadioPrefs
	var bridge, cool, jitter, penalty, bounded int
	err := row.Scan(
		&p.UserID, &bridge, &cool, &jitter, &penalty, &bounded, &p.JitterAlpha, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return def, nil
	}
	if err != nil {
		return def, err
	}
	p.RadioBridge = bridge != 0
	p.ArtistCooldown = cool != 0
	p.QueryJitter = jitter != 0
	p.ArtistPenalty = penalty != 0
	p.BoundedRandom = bounded != 0
	if p.JitterAlpha <= 0 {
		p.JitterAlpha = def.JitterAlpha
	}
	return p, nil
}

// UpsertRadioPrefs persists prefs (partial merge: zero-value bools are written as given).
func (d *DB) UpsertRadioPrefs(p RadioPrefs) (RadioPrefs, error) {
	if p.JitterAlpha <= 0 {
		p.JitterAlpha = 0.05
	}
	if p.JitterAlpha > 0.5 {
		p.JitterAlpha = 0.5
	}
	now := Now()
	_, err := d.SQL.Exec(`
INSERT INTO user_radio_prefs (
  user_id, radio_bridge, artist_cooldown, query_jitter, artist_penalty, bounded_random, jitter_alpha, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  radio_bridge = VALUES(radio_bridge),
  artist_cooldown = VALUES(artist_cooldown),
  query_jitter = VALUES(query_jitter),
  artist_penalty = VALUES(artist_penalty),
  bounded_random = VALUES(bounded_random),
  jitter_alpha = VALUES(jitter_alpha),
  updated_at = VALUES(updated_at)`,
		p.UserID,
		boolInt(p.RadioBridge),
		boolInt(p.ArtistCooldown),
		boolInt(p.QueryJitter),
		boolInt(p.ArtistPenalty),
		boolInt(p.BoundedRandom),
		p.JitterAlpha,
		now,
	)
	if err != nil {
		return p, err
	}
	return d.GetRadioPrefs(p.UserID)
}

// AffinityHit is a high-affinity track for Radio Bridges.
type AffinityHit struct {
	TrackID int64
	Score   float64
}

// ListHighAffinityTracks returns liked / high-score tracks for the user (descending).
func (d *DB) ListHighAffinityTracks(userID int64, limit int) ([]AffinityHit, error) {
	if limit <= 0 {
		limit = 40
	}
	rows, err := d.SQL.Query(`
SELECT track_id, score FROM user_track_affinity
WHERE user_id = ? AND score >= 0.2
ORDER BY score DESC
LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AffinityHit
	for rows.Next() {
		var h AffinityHit
		if err := rows.Scan(&h.TrackID, &h.Score); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
