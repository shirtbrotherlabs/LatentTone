// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

const (
	SignalLike    = "like"
	SignalDislike = "dislike"
	SignalSkip    = "skip"
	SignalBan     = "ban"

	SkipScopeLibrary = "library"
	SkipScopeSession = "session"

	SessionStatusCreated  = "created"
	SessionStatusPlaying  = "playing"
	SessionStatusStopped  = "stopped"
	SessionStatusError    = "error"
)

// InsertTrackFeedback records an explicit signal.
func (d *DB) InsertTrackFeedback(userID, trackID int64, signal, sessionID string) error {
	_, err := d.SQL.Exec(
		`INSERT INTO track_feedback (user_id, track_id, signal, session_id, created_at) VALUES (?, ?, ?, ?, ?)`,
		userID, trackID, signal, NullString(sessionID), Now(),
	)
	return err
}

// UpsertAffinity adds delta to affinity score, clamped to [-1, 1].
func (d *DB) UpsertAffinity(userID, trackID int64, delta float64) (float64, error) {
	now := Now()
	var cur float64
	err := d.SQL.QueryRow(
		`SELECT score FROM user_track_affinity WHERE user_id = ? AND track_id = ?`,
		userID, trackID,
	).Scan(&cur)
	if err != nil && err != sql.ErrNoRows {
		return 0, err
	}
	if err == sql.ErrNoRows {
		cur = 0
	}
	cur += delta
	if cur > 1 {
		cur = 1
	}
	if cur < -1 {
		cur = -1
	}
	_, err = d.SQL.Exec(`
INSERT INTO user_track_affinity (user_id, track_id, score, updated_at) VALUES (?, ?, ?, ?)
ON CONFLICT(user_id, track_id) DO UPDATE SET score = excluded.score, updated_at = excluded.updated_at`,
		userID, trackID, cur, now,
	)
	return cur, err
}

// GetAffinity returns score or 0 if missing.
func (d *DB) GetAffinity(userID, trackID int64) (float64, error) {
	var score float64
	err := d.SQL.QueryRow(
		`SELECT score FROM user_track_affinity WHERE user_id = ? AND track_id = ?`,
		userID, trackID,
	).Scan(&score)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return score, err
}

// AddSkip records a skip exclusion.
func (d *DB) AddSkip(userID, trackID int64, scope, sessionKey string) error {
	if scope == "" {
		scope = SkipScopeLibrary
	}
	_, err := d.SQL.Exec(`
INSERT OR IGNORE INTO user_track_skips (user_id, track_id, scope, session_key, created_at)
VALUES (?, ?, ?, ?, ?)`,
		userID, trackID, scope, sessionKey, Now(),
	)
	return err
}

// ListSkippedTrackIDs returns track ids skipped for this user (library + optional session).
func (d *DB) ListSkippedTrackIDs(userID int64, sessionKey string) (map[int64]struct{}, error) {
	rows, err := d.SQL.Query(`
SELECT track_id FROM user_track_skips
WHERE user_id = ? AND (scope = ? OR (scope = ? AND session_key = ?))`,
		userID, SkipScopeLibrary, SkipScopeSession, sessionKey,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]struct{})
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = struct{}{}
	}
	return out, rows.Err()
}

// InsertPlaybackEvent starts a playback history row; returns id.
func (d *DB) InsertPlaybackEvent(userID, trackID int64, sessionID string) (int64, error) {
	res, err := d.SQL.Exec(`
INSERT INTO playback_events (user_id, track_id, session_id, started_at, completed, skipped)
VALUES (?, ?, ?, ?, 0, 0)`,
		userID, trackID, NullString(sessionID), Now(),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FinishPlaybackEvent marks end/skip fields.
func (d *DB) FinishPlaybackEvent(id int64, listenedMS int64, completed, skipped bool, skipWithinMS *int64) error {
	c, s := 0, 0
	if completed {
		c = 1
	}
	if skipped {
		s = 1
	}
	_, err := d.SQL.Exec(`
UPDATE playback_events SET ended_at = ?, listened_ms = ?, completed = ?, skipped = ?, skip_within_ms = ?
WHERE id = ?`,
		Now(), listenedMS, c, s, NullInt64(skipWithinMS), id,
	)
	return err
}

// ListeningSession is a persisted station session.
type ListeningSession struct {
	ID            string
	UserID        int64
	SeedTrackID   sql.NullInt64
	Status        string
	NowPlayingID  sql.NullInt64
	QueueJSON     sql.NullString
	LastFeedback  sql.NullString
	ErrorMessage  sql.NullString
	CreatedAt     string
	UpdatedAt     string
}

// CreateListeningSession inserts a new listening session row.
func (d *DB) CreateListeningSession(id string, userID, seedTrackID int64) (*ListeningSession, error) {
	now := Now()
	_, err := d.SQL.Exec(`
INSERT INTO listening_sessions (id, user_id, seed_track_id, status, now_playing_id, queue_json, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, '[]', ?, ?)`,
		id, userID, seedTrackID, SessionStatusCreated, seedTrackID, now, now,
	)
	if err != nil {
		return nil, err
	}
	return d.GetListeningSession(id)
}

// GetListeningSession loads a session by id.
func (d *DB) GetListeningSession(id string) (*ListeningSession, error) {
	row := d.SQL.QueryRow(`
SELECT id, user_id, seed_track_id, status, now_playing_id, queue_json, last_feedback, error_message, created_at, updated_at
FROM listening_sessions WHERE id = ?`, id)
	var s ListeningSession
	err := row.Scan(
		&s.ID, &s.UserID, &s.SeedTrackID, &s.Status, &s.NowPlayingID,
		&s.QueueJSON, &s.LastFeedback, &s.ErrorMessage, &s.CreatedAt, &s.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// UpdateListeningSessionState persists status, now-playing, queue, and optional feedback ack.
func (d *DB) UpdateListeningSessionState(id, status string, nowPlayingID int64, queue []int64, lastFeedbackJSON string, errMsg string) error {
	qj, err := json.Marshal(queue)
	if err != nil {
		return err
	}
	var errNS sql.NullString
	if errMsg != "" {
		errNS = sql.NullString{String: errMsg, Valid: true}
	}
	var fb sql.NullString
	if lastFeedbackJSON != "" {
		fb = sql.NullString{String: lastFeedbackJSON, Valid: true}
	}
	var np sql.NullInt64
	if nowPlayingID > 0 {
		np = sql.NullInt64{Int64: nowPlayingID, Valid: true}
	}
	_, err = d.SQL.Exec(`
UPDATE listening_sessions SET status = ?, now_playing_id = ?, queue_json = ?, last_feedback = COALESCE(?, last_feedback),
  error_message = ?, updated_at = ?
WHERE id = ?`,
		status, np, string(qj), fb, errNS, Now(), id,
	)
	return err
}

// CountActiveListeningSessions returns non-stopped sessions for concurrency budget.
func (d *DB) CountActiveListeningSessions() (int64, error) {
	var n int64
	err := d.SQL.QueryRow(`
SELECT COUNT(1) FROM listening_sessions WHERE status IN (?, ?)`,
		SessionStatusCreated, SessionStatusPlaying,
	).Scan(&n)
	return n, err
}

// ListTrackIDsNotMissing returns up to limit track ids that are not soft-deleted.
func (d *DB) ListTrackIDsNotMissing(limit int) ([]int64, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := d.SQL.Query(`SELECT id FROM tracks WHERE missing_at IS NULL ORDER BY id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ParseQueueJSON decodes queue_json into track ids.
func ParseQueueJSON(s string) ([]int64, error) {
	if s == "" {
		return nil, nil
	}
	var ids []int64
	if err := json.Unmarshal([]byte(s), &ids); err != nil {
		return nil, fmt.Errorf("queue_json: %w", err)
	}
	return ids, nil
}
