// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-20

package db

import (
	"strings"
)

// SearchHit is one typeahead suggestion row.
type SearchHit struct {
	Kind       string // track | artist | album
	ID         int64
	Label      string
	SubLabel   string
	CoverPath  string
	TrackID    int64 // for track hits (= ID); else 0
	DurationMS int64
}

// SearchSuggest returns prefix/substring hits for tracks, artists, and albums.
func (d *DB) SearchSuggest(q string, limit int) ([]SearchHit, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 12
	}
	if limit > 40 {
		limit = 40
	}
	per := limit / 3
	if per < 3 {
		per = 3
	}
	like := "%" + q + "%"
	prefix := q + "%"

	var out []SearchHit

	tRows, err := d.SQL.Query(`
SELECT t.id, t.title, COALESCE(a.name, ''), COALESCE(al.title, ''),
       COALESCE(al.cover_path, ''), COALESCE(t.duration_ms, 0),
       CASE WHEN LOWER(t.title) LIKE LOWER(?) THEN 0
            WHEN LOWER(COALESCE(a.name,'')) LIKE LOWER(?) THEN 1
            ELSE 2 END AS rank
FROM tracks t
LEFT JOIN albums al ON al.id = t.album_id
LEFT JOIN artists a ON a.id = al.artist_id
WHERE t.missing_at IS NULL
  AND (t.title LIKE ? OR COALESCE(a.name, '') LIKE ? OR COALESCE(al.title, '') LIKE ?)
ORDER BY rank, t.title
LIMIT ?`, prefix, prefix, like, like, like, per)
	if err != nil {
		return nil, err
	}
	for tRows.Next() {
		var id, dur int64
		var title, artist, album, cover string
		var rank int
		if err := tRows.Scan(&id, &title, &artist, &album, &cover, &dur, &rank); err != nil {
			tRows.Close()
			return nil, err
		}
		sub := artist
		if album != "" {
			if sub != "" {
				sub += " · "
			}
			sub += album
		}
		out = append(out, SearchHit{
			Kind: "track", ID: id, TrackID: id, Label: title, SubLabel: sub,
			CoverPath: cover, DurationMS: dur,
		})
	}
	err = tRows.Err()
	tRows.Close()
	if err != nil {
		return nil, err
	}

	aRows, err := d.SQL.Query(`
SELECT a.id, a.name, COALESCE((
  SELECT al.cover_path FROM albums al
  JOIN tracks t ON t.album_id = al.id AND t.missing_at IS NULL
  WHERE al.artist_id = a.id AND al.cover_path IS NOT NULL AND al.cover_path != ''
  LIMIT 1
), '')
FROM artists a
WHERE a.name LIKE ?
  AND EXISTS (SELECT 1 FROM albums al JOIN tracks t ON t.album_id = al.id AND t.missing_at IS NULL WHERE al.artist_id = a.id)
ORDER BY CASE WHEN LOWER(a.name) LIKE LOWER(?) THEN 0 ELSE 1 END, a.name
LIMIT ?`, like, prefix, per)
	if err != nil {
		return nil, err
	}
	for aRows.Next() {
		var id int64
		var name, cover string
		if err := aRows.Scan(&id, &name, &cover); err != nil {
			aRows.Close()
			return nil, err
		}
		out = append(out, SearchHit{Kind: "artist", ID: id, Label: name, SubLabel: "Artist", CoverPath: cover})
	}
	err = aRows.Err()
	aRows.Close()
	if err != nil {
		return nil, err
	}

	alRows, err := d.SQL.Query(`
SELECT al.id, al.title, COALESCE(a.name, ''), COALESCE(al.cover_path, '')
FROM albums al
LEFT JOIN artists a ON a.id = al.artist_id
WHERE EXISTS (SELECT 1 FROM tracks t WHERE t.album_id = al.id AND t.missing_at IS NULL)
  AND (al.title LIKE ? OR COALESCE(a.name, '') LIKE ?)
ORDER BY CASE WHEN LOWER(al.title) LIKE LOWER(?) THEN 0 ELSE 1 END, al.title
LIMIT ?`, like, like, prefix, per)
	if err != nil {
		return nil, err
	}
	for alRows.Next() {
		var id int64
		var title, artist, cover string
		if err := alRows.Scan(&id, &title, &artist, &cover); err != nil {
			alRows.Close()
			return nil, err
		}
		out = append(out, SearchHit{
			Kind: "album", ID: id, Label: title, SubLabel: artist, CoverPath: cover,
		})
	}
	err = alRows.Err()
	alRows.Close()
	if err != nil {
		return nil, err
	}

	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
