// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-20

package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// GenreCount is a browseable genre with track count.
type GenreCount struct {
	ID    int64
	Name  string
	Count int
}

// ListGenres returns genres that have at least one non-missing track.
func (d *DB) ListGenres(limit int) ([]GenreCount, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := d.SQL.Query(`
SELECT g.id, g.name, COUNT(*) AS n
FROM genres g
JOIN track_genres tg ON tg.genre_id = g.id
JOIN tracks t ON t.id = tg.track_id AND t.missing_at IS NULL
GROUP BY g.id, g.name
ORDER BY n DESC, g.name
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GenreCount
	for rows.Next() {
		var g GenreCount
		if err := rows.Scan(&g.ID, &g.Name, &g.Count); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// PickSeedTrackID resolves a Radio seed for artist / genre / playlist.
// Prefers tracks with ready embeddings when available.
func (d *DB) PickSeedTrackID(artistID, genreID, playlistID int64, genreName string) (int64, error) {
	n := 0
	if artistID > 0 {
		n++
	}
	if genreID > 0 || strings.TrimSpace(genreName) != "" {
		n++
	}
	if playlistID > 0 {
		n++
	}
	if n != 1 {
		return 0, fmt.Errorf("specify exactly one of seed_artist_id, seed_genre_id/seed_genre, seed_playlist_id")
	}

	if artistID > 0 {
		return d.pickSeedSQL(`
SELECT t.id FROM tracks t
JOIN albums al ON al.id = t.album_id
LEFT JOIN track_vectors tv ON tv.track_id = t.id AND tv.status = 'ready'
WHERE t.missing_at IS NULL AND al.artist_id = ?
ORDER BY CASE WHEN tv.track_id IS NOT NULL THEN 0 ELSE 1 END, t.id
LIMIT 1`, artistID)
	}

	if playlistID > 0 {
		return d.pickSeedSQL(`
SELECT t.id FROM playlist_tracks pt
JOIN tracks t ON t.id = pt.track_id AND t.missing_at IS NULL
LEFT JOIN track_vectors tv ON tv.track_id = t.id AND tv.status = 'ready'
WHERE pt.playlist_id = ?
ORDER BY CASE WHEN tv.track_id IS NOT NULL THEN 0 ELSE 1 END, pt.position, t.id
LIMIT 1`, playlistID)
	}

	if genreID > 0 {
		return d.pickSeedSQL(`
SELECT t.id FROM tracks t
JOIN track_genres tg ON tg.track_id = t.id AND tg.genre_id = ?
LEFT JOIN track_vectors tv ON tv.track_id = t.id AND tv.status = 'ready'
WHERE t.missing_at IS NULL
ORDER BY CASE WHEN tv.track_id IS NOT NULL THEN 0 ELSE 1 END, t.id
LIMIT 1`, genreID)
	}

	name := strings.TrimSpace(genreName)
	return d.pickSeedSQL(`
SELECT t.id FROM tracks t
JOIN track_genres tg ON tg.track_id = t.id
JOIN genres g ON g.id = tg.genre_id AND g.name = ?
LEFT JOIN track_vectors tv ON tv.track_id = t.id AND tv.status = 'ready'
WHERE t.missing_at IS NULL
ORDER BY CASE WHEN tv.track_id IS NOT NULL THEN 0 ELSE 1 END, t.id
LIMIT 1`, name)
}

func (d *DB) pickSeedSQL(q string, arg any) (int64, error) {
	var id int64
	err := d.SQL.QueryRow(q, arg).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("no playable seed track found")
	}
	if err != nil {
		return 0, err
	}
	return id, nil
}

// FirstTrackIDForAlbum returns the first non-missing track on an album (disc/track order).
func (d *DB) FirstTrackIDForAlbum(albumID int64) (int64, error) {
	return d.pickSeedSQL(`
SELECT t.id FROM tracks t
WHERE t.album_id = ? AND t.missing_at IS NULL
ORDER BY COALESCE(t.disc_number, 1), COALESCE(t.track_number, 0), t.id
LIMIT 1`, albumID)
}
