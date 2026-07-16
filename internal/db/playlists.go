// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package db

import (
	"database/sql"
	"errors"
	"fmt"
)

// Playlist kinds (ADR-008).
const (
	PlaylistKindNeighbor = "neighbor"
	PlaylistKindUser     = "user"
)

// ErrPlaylistNotFound is returned when a playlist id is missing or not owned.
var ErrPlaylistNotFound = errors.New("playlist not found")

// ErrTrackNotFound is returned when a track id is missing from the catalog.
var ErrTrackNotFound = errors.New("track not found")

// ErrInvalidOrder is returned when a reorder payload does not match membership.
var ErrInvalidOrder = errors.New("invalid playlist order")

// Playlist is a persisted ordered track list.
type Playlist struct {
	ID          int64
	Name        string
	SeedTrackID sql.NullInt64
	UserID      sql.NullInt64
	Kind        string
	Length      int
	CreatedAt   string
	UpdatedAt   string
	CoverPath   sql.NullString // optional thumbnail (list queries may populate)
}

// PlaylistEntry is one position in a playlist.
type PlaylistEntry struct {
	Position int
	TrackID  int64
	Score    sql.NullFloat64
	Track    *Track // optional join
}

// CreatePlaylistOpts creates a playlist header and ordered entries.
type CreatePlaylistOpts struct {
	Name        string
	SeedTrackID sql.NullInt64
	UserID      sql.NullInt64
	Kind        string
	Entries     []PlaylistEntry
}

// CreatePlaylist inserts a playlist header and ordered entries.
func (d *DB) CreatePlaylist(opts CreatePlaylistOpts) (int64, error) {
	kind := opts.Kind
	if kind == "" {
		kind = PlaylistKindNeighbor
	}
	if kind == PlaylistKindUser && !opts.UserID.Valid {
		return 0, fmt.Errorf("user_id required for kind=user")
	}
	name := opts.Name
	if name == "" {
		if opts.SeedTrackID.Valid {
			name = fmt.Sprintf("Neighbor playlist from track %d", opts.SeedTrackID.Int64)
		} else {
			name = "Playlist"
		}
	}
	now := Now()
	tx, err := d.SQL.Begin()
	if err != nil {
		return 0, err
	}
	res, err := tx.Exec(`
INSERT INTO playlists (name, seed_track_id, user_id, kind, length, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		name, nullInt64Arg(opts.SeedTrackID), nullInt64Arg(opts.UserID), kind, len(opts.Entries), now, now)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	for i, e := range opts.Entries {
		_, err := tx.Exec(`
INSERT INTO playlist_tracks (playlist_id, position, track_id, score)
VALUES (?, ?, ?, ?)`, id, i, e.TrackID, e.Score)
		if err != nil {
			_ = tx.Rollback()
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func nullInt64Arg(v sql.NullInt64) any {
	if !v.Valid {
		return nil
	}
	return v.Int64
}

// GetPlaylist returns playlist header or nil.
func (d *DB) GetPlaylist(id int64) (*Playlist, error) {
	row := d.SQL.QueryRow(`
SELECT id, name, seed_track_id, user_id, kind, length, created_at, updated_at
FROM playlists WHERE id = ?`, id)
	return scanPlaylist(row)
}

// GetUserPlaylist returns a kind=user playlist owned by userID, or nil.
func (d *DB) GetUserPlaylist(id, userID int64) (*Playlist, error) {
	row := d.SQL.QueryRow(`
SELECT id, name, seed_track_id, user_id, kind, length, created_at, updated_at
FROM playlists
WHERE id = ? AND kind = ? AND user_id = ?`, id, PlaylistKindUser, userID)
	return scanPlaylist(row)
}

func scanPlaylist(row *sql.Row) (*Playlist, error) {
	var p Playlist
	err := row.Scan(&p.ID, &p.Name, &p.SeedTrackID, &p.UserID, &p.Kind, &p.Length, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ListUserPlaylists returns the caller's user-owned playlists, newest first.
func (d *DB) ListUserPlaylists(userID int64, limit int) ([]Playlist, error) {
	if limit <= 0 {
		limit = 100
	}
	// CoverPath: album art from a track in the playlist (stable pick via
	// (track_id + playlist_id) ordering so the thumbnail does not flicker).
	rows, err := d.SQL.Query(`
SELECT p.id, p.name, p.seed_track_id, p.user_id, p.kind, p.length, p.created_at, p.updated_at,
  (SELECT al.cover_path
   FROM playlist_tracks pt
   JOIN tracks t ON t.id = pt.track_id
   JOIN albums al ON al.id = t.album_id
   WHERE pt.playlist_id = p.id
     AND al.cover_path IS NOT NULL
     AND al.cover_path != ''
   ORDER BY ((pt.track_id + pt.playlist_id) % 9973), pt.position
   LIMIT 1) AS cover_path
FROM playlists p
WHERE p.kind = ? AND p.user_id = ?
ORDER BY p.updated_at DESC, p.id DESC
LIMIT ?`, PlaylistKindUser, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Playlist
	for rows.Next() {
		var p Playlist
		if err := rows.Scan(&p.ID, &p.Name, &p.SeedTrackID, &p.UserID, &p.Kind, &p.Length, &p.CreatedAt, &p.UpdatedAt, &p.CoverPath); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// RenameUserPlaylist updates name for an owned user playlist.
func (d *DB) RenameUserPlaylist(id, userID int64, name string) (*Playlist, error) {
	if name == "" {
		return nil, fmt.Errorf("name required")
	}
	now := Now()
	res, err := d.SQL.Exec(`
UPDATE playlists SET name = ?, updated_at = ?
WHERE id = ? AND kind = ? AND user_id = ?`, name, now, id, PlaylistKindUser, userID)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrPlaylistNotFound
	}
	return d.GetUserPlaylist(id, userID)
}

// DeleteUserPlaylist deletes an owned user playlist (cascades tracks).
func (d *DB) DeleteUserPlaylist(id, userID int64) error {
	res, err := d.SQL.Exec(`
DELETE FROM playlists WHERE id = ? AND kind = ? AND user_id = ?`, id, PlaylistKindUser, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrPlaylistNotFound
	}
	return nil
}

// ListPlaylistEntries returns ordered entries with catalog track rows joined.
func (d *DB) ListPlaylistEntries(playlistID int64) ([]PlaylistEntry, error) {
	rows, err := d.SQL.Query(`
SELECT pt.position, pt.track_id, pt.score,
       t.id, t.album_id, t.path, t.title, t.track_number, t.disc_number,
       t.duration_ms, t.bitrate_kbps, t.sample_rate_hz, t.channels, t.format, t.year,
       t.comment, t.missing_at,
       COALESCE(al.title, ''), COALESCE(a.name, ''), al.cover_path,
       COALESCE((SELECT GROUP_CONCAT(g.name, '|') FROM track_genres tg JOIN genres g ON g.id = tg.genre_id WHERE tg.track_id = t.id), '')
FROM playlist_tracks pt
JOIN tracks t ON t.id = pt.track_id
LEFT JOIN albums al ON al.id = t.album_id
LEFT JOIN artists a ON a.id = al.artist_id
WHERE pt.playlist_id = ?
ORDER BY pt.position ASC`, playlistID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlaylistEntry
	for rows.Next() {
		var e PlaylistEntry
		var tr Track
		if err := rows.Scan(
			&e.Position, &e.TrackID, &e.Score,
			&tr.ID, &tr.AlbumID, &tr.Path, &tr.Title, &tr.TrackNumber, &tr.DiscNumber,
			&tr.DurationMS, &tr.BitrateKbps, &tr.SampleRateHz, &tr.Channels, &tr.Format, &tr.Year,
			&tr.Comment, &tr.MissingAt,
			&tr.AlbumTitle, &tr.ArtistName, &tr.CoverPath, &tr.Genres,
		); err != nil {
			return nil, err
		}
		e.Track = &tr
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListPlaylistsForSeed returns recent playlists spawned from a seed track.
func (d *DB) ListPlaylistsForSeed(seedTrackID int64, limit int) ([]Playlist, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := d.SQL.Query(`
SELECT id, name, seed_track_id, user_id, kind, length, created_at, updated_at
FROM playlists WHERE seed_track_id = ?
ORDER BY created_at DESC, id DESC LIMIT ?`, seedTrackID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Playlist
	for rows.Next() {
		var p Playlist
		if err := rows.Scan(&p.ID, &p.Name, &p.SeedTrackID, &p.UserID, &p.Kind, &p.Length, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// AddTracksToUserPlaylist appends track ids (in order) to an owned playlist.
func (d *DB) AddTracksToUserPlaylist(playlistID, userID int64, trackIDs []int64) (*Playlist, error) {
	if len(trackIDs) == 0 {
		return nil, fmt.Errorf("track_id or track_ids required")
	}
	pl, err := d.GetUserPlaylist(playlistID, userID)
	if err != nil {
		return nil, err
	}
	if pl == nil {
		return nil, ErrPlaylistNotFound
	}
	for _, tid := range trackIDs {
		t, err := d.GetTrack(tid)
		if err != nil {
			return nil, err
		}
		if t == nil {
			return nil, fmt.Errorf("%w: %d", ErrTrackNotFound, tid)
		}
	}
	tx, err := d.SQL.Begin()
	if err != nil {
		return nil, err
	}
	pos := pl.Length
	for _, tid := range trackIDs {
		_, err := tx.Exec(`
INSERT INTO playlist_tracks (playlist_id, position, track_id, score)
VALUES (?, ?, ?, NULL)`, playlistID, pos, tid)
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		pos++
	}
	now := Now()
	_, err = tx.Exec(`UPDATE playlists SET length = ?, updated_at = ? WHERE id = ?`, pos, now, playlistID)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return d.GetUserPlaylist(playlistID, userID)
}

// RemoveTrackFromUserPlaylist removes the first occurrence of trackID and renumbers.
func (d *DB) RemoveTrackFromUserPlaylist(playlistID, userID, trackID int64) (*Playlist, error) {
	pl, err := d.GetUserPlaylist(playlistID, userID)
	if err != nil {
		return nil, err
	}
	if pl == nil {
		return nil, ErrPlaylistNotFound
	}
	entries, err := d.ListPlaylistEntries(playlistID)
	if err != nil {
		return nil, err
	}
	kept := make([]PlaylistEntry, 0, len(entries))
	removed := false
	for _, e := range entries {
		if !removed && e.TrackID == trackID {
			removed = true
			continue
		}
		kept = append(kept, e)
	}
	if !removed {
		return nil, ErrTrackNotFound
	}
	if err := d.rewritePlaylistTracks(playlistID, kept); err != nil {
		return nil, err
	}
	return d.GetUserPlaylist(playlistID, userID)
}

// ReorderUserPlaylist sets the full track order (must be a permutation of current membership).
func (d *DB) ReorderUserPlaylist(playlistID, userID int64, trackIDs []int64) (*Playlist, error) {
	pl, err := d.GetUserPlaylist(playlistID, userID)
	if err != nil {
		return nil, err
	}
	if pl == nil {
		return nil, ErrPlaylistNotFound
	}
	entries, err := d.ListPlaylistEntries(playlistID)
	if err != nil {
		return nil, err
	}
	if len(trackIDs) != len(entries) {
		return nil, ErrInvalidOrder
	}
	counts := make(map[int64]int, len(entries))
	for _, e := range entries {
		counts[e.TrackID]++
	}
	want := make(map[int64]int, len(trackIDs))
	for _, tid := range trackIDs {
		want[tid]++
	}
	for tid, n := range counts {
		if want[tid] != n {
			return nil, ErrInvalidOrder
		}
	}
	scoreByOcc := make(map[int64][]sql.NullFloat64, len(entries))
	for _, e := range entries {
		scoreByOcc[e.TrackID] = append(scoreByOcc[e.TrackID], e.Score)
	}
	rewritten := make([]PlaylistEntry, 0, len(trackIDs))
	for _, tid := range trackIDs {
		scores := scoreByOcc[tid]
		var score sql.NullFloat64
		if len(scores) > 0 {
			score = scores[0]
			scoreByOcc[tid] = scores[1:]
		}
		rewritten = append(rewritten, PlaylistEntry{TrackID: tid, Score: score})
	}
	if err := d.rewritePlaylistTracks(playlistID, rewritten); err != nil {
		return nil, err
	}
	return d.GetUserPlaylist(playlistID, userID)
}

func (d *DB) rewritePlaylistTracks(playlistID int64, entries []PlaylistEntry) error {
	tx, err := d.SQL.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM playlist_tracks WHERE playlist_id = ?`, playlistID); err != nil {
		_ = tx.Rollback()
		return err
	}
	for i, e := range entries {
		_, err := tx.Exec(`
INSERT INTO playlist_tracks (playlist_id, position, track_id, score)
VALUES (?, ?, ?, ?)`, playlistID, i, e.TrackID, e.Score)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	now := Now()
	if _, err := tx.Exec(`UPDATE playlists SET length = ?, updated_at = ? WHERE id = ?`, len(entries), now, playlistID); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// CopyNeighborToUserPlaylist clones a neighbor playlist into a new kind=user playlist.
func (d *DB) CopyNeighborToUserPlaylist(neighborID, userID int64, name string) (int64, error) {
	src, err := d.GetPlaylist(neighborID)
	if err != nil {
		return 0, err
	}
	if src == nil || src.Kind != PlaylistKindNeighbor {
		return 0, ErrPlaylistNotFound
	}
	entries, err := d.ListPlaylistEntries(neighborID)
	if err != nil {
		return 0, err
	}
	if name == "" {
		name = src.Name
		if name == "" {
			name = fmt.Sprintf("From neighbor %d", neighborID)
		}
	}
	return d.CreatePlaylist(CreatePlaylistOpts{
		Name:        name,
		SeedTrackID: src.SeedTrackID,
		UserID:      sql.NullInt64{Int64: userID, Valid: true},
		Kind:        PlaylistKindUser,
		Entries:     entries,
	})
}
