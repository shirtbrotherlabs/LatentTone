// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
)

// TrackInput is catalog upsert payload from the scanner.
type TrackInput struct {
	Path         string
	FileMtime    int64
	FileSize     int64
	Title        string
	Album        string
	AlbumArtist  string
	Artists      []string
	Genres       []string
	TrackNumber  *int
	DiscNumber   *int
	DurationMS   *int64
	BitrateKbps  *int
	SampleRateHz *int
	Channels     *int
	Format       string
	Year         *int
	Comment      string
	MBID         string
	CoverPath    string
	AlbumYear    *int
}

// UpsertTrack inserts or updates a track and related artist/album/genre rows.
func (d *DB) UpsertTrack(in TrackInput) (trackID int64, err error) {
	now := Now()
	tx, err := d.SQL.Begin()
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	albumArtist := strings.TrimSpace(in.AlbumArtist)
	if albumArtist == "" && len(in.Artists) > 0 {
		albumArtist = strings.TrimSpace(in.Artists[0])
	}
	if albumArtist == "" {
		albumArtist = "Unknown Artist"
	}
	albumTitle := strings.TrimSpace(in.Album)
	if albumTitle == "" {
		albumTitle = "Unknown Album"
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = "Unknown Track"
	}

	artistID, err := upsertArtistTx(tx, albumArtist, now)
	if err != nil {
		return 0, err
	}

	albumID, err := upsertAlbumTx(tx, artistID, albumTitle, in.AlbumYear, in.CoverPath, now)
	if err != nil {
		return 0, err
	}

	pathHash := hashPath(in.Path)
	disc := 1
	if in.DiscNumber != nil && *in.DiscNumber > 0 {
		disc = *in.DiscNumber
	}

	var existingID int64
	err = tx.QueryRow(`SELECT id FROM tracks WHERE path = ?`, in.Path).Scan(&existingID)
	switch {
	case err == sql.ErrNoRows:
		res, e := tx.Exec(`
INSERT INTO tracks (
  album_id, path, path_hash, file_mtime, file_size, title, track_number, disc_number,
  duration_ms, bitrate_kbps, sample_rate_hz, channels, format, year, comment, mbid,
  catalogued_at, updated_at, missing_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
			albumID, in.Path, pathHash, in.FileMtime, in.FileSize, title,
			NullInt(in.TrackNumber), disc,
			NullInt64(in.DurationMS), NullInt(in.BitrateKbps), NullInt(in.SampleRateHz), NullInt(in.Channels),
			NullString(in.Format), NullInt(in.Year), NullString(in.Comment), NullString(in.MBID),
			now, now,
		)
		if e != nil {
			err = e
			return 0, err
		}
		trackID, err = res.LastInsertId()
		if err != nil {
			return 0, err
		}
	case err != nil:
		return 0, err
	default:
		trackID = existingID
		_, err = tx.Exec(`
UPDATE tracks SET
  album_id = ?, path_hash = ?, file_mtime = ?, file_size = ?, title = ?,
  track_number = ?, disc_number = ?, duration_ms = ?, bitrate_kbps = ?,
  sample_rate_hz = ?, channels = ?, format = ?, year = ?, comment = ?, mbid = ?,
  updated_at = ?, missing_at = NULL
WHERE id = ?`,
			albumID, pathHash, in.FileMtime, in.FileSize, title,
			NullInt(in.TrackNumber), disc,
			NullInt64(in.DurationMS), NullInt(in.BitrateKbps), NullInt(in.SampleRateHz), NullInt(in.Channels),
			NullString(in.Format), NullInt(in.Year), NullString(in.Comment), NullString(in.MBID),
			now, trackID,
		)
		if err != nil {
			return 0, err
		}
		_, err = tx.Exec(`DELETE FROM track_artists WHERE track_id = ?`, trackID)
		if err != nil {
			return 0, err
		}
		_, err = tx.Exec(`DELETE FROM track_genres WHERE track_id = ?`, trackID)
		if err != nil {
			return 0, err
		}
	}

	artists := in.Artists
	if len(artists) == 0 {
		artists = []string{albumArtist}
	}
	for i, name := range artists {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		aid, e := upsertArtistTx(tx, name, now)
		if e != nil {
			err = e
			return 0, err
		}
		role := "primary"
		if i > 0 {
			role = "featured"
		}
		_, err = tx.Exec(
			`INSERT OR REPLACE INTO track_artists (track_id, artist_id, role, position) VALUES (?, ?, ?, ?)`,
			trackID, aid, role, i,
		)
		if err != nil {
			return 0, err
		}
	}

	for _, g := range in.Genres {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		gid, e := upsertGenreTx(tx, g)
		if e != nil {
			err = e
			return 0, err
		}
		_, err = tx.Exec(
			`INSERT OR IGNORE INTO track_genres (track_id, genre_id, source) VALUES (?, ?, 'tag')`,
			trackID, gid,
		)
		if err != nil {
			return 0, err
		}
	}

	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return trackID, nil
}

func upsertArtistTx(tx *sql.Tx, name, now string) (int64, error) {
	var id int64
	err := tx.QueryRow(`SELECT id FROM artists WHERE name = ? COLLATE NOCASE`, name).Scan(&id)
	if err == nil {
		_, err = tx.Exec(`UPDATE artists SET updated_at = ? WHERE id = ?`, now, id)
		return id, err
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	res, err := tx.Exec(
		`INSERT INTO artists (name, name_sort, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		name, nameSort(name), now, now,
	)
	if err != nil {
		// concurrent insert race
		err2 := tx.QueryRow(`SELECT id FROM artists WHERE name = ? COLLATE NOCASE`, name).Scan(&id)
		return id, err2
	}
	return res.LastInsertId()
}

func upsertAlbumTx(tx *sql.Tx, artistID int64, title string, year *int, coverPath, now string) (int64, error) {
	var id int64
	err := tx.QueryRow(
		`SELECT id FROM albums WHERE artist_id = ? AND title = ? COLLATE NOCASE`,
		artistID, title,
	).Scan(&id)
	if err == nil {
		_, err = tx.Exec(`
UPDATE albums SET year = COALESCE(?, year), cover_path = CASE
  WHEN ? != '' THEN ? ELSE cover_path END, updated_at = ?
WHERE id = ?`,
			NullInt(year), coverPath, coverPath, now, id,
		)
		return id, err
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	res, err := tx.Exec(`
INSERT INTO albums (artist_id, title, title_sort, year, cover_path, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		artistID, title, title, NullInt(year), NullString(coverPath), now, now,
	)
	if err != nil {
		err2 := tx.QueryRow(
			`SELECT id FROM albums WHERE artist_id = ? AND title = ? COLLATE NOCASE`,
			artistID, title,
		).Scan(&id)
		return id, err2
	}
	return res.LastInsertId()
}

func upsertGenreTx(tx *sql.Tx, name string) (int64, error) {
	var id int64
	err := tx.QueryRow(`SELECT id FROM genres WHERE name = ? COLLATE NOCASE`, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	res, err := tx.Exec(`INSERT INTO genres (name) VALUES (?)`, name)
	if err != nil {
		err2 := tx.QueryRow(`SELECT id FROM genres WHERE name = ? COLLATE NOCASE`, name).Scan(&id)
		return id, err2
	}
	return res.LastInsertId()
}

func hashPath(path string) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:])
}

func nameSort(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	for _, p := range []string{"the ", "a ", "an "} {
		if strings.HasPrefix(lower, p) {
			return strings.TrimSpace(name[len(p):]) + ", " + strings.TrimSpace(name[:len(p)])
		}
	}
	return name
}

// MarkMissingExcept marks tracks not in seenPaths as missing.
func (d *DB) MarkMissingExcept(seenPaths map[string]struct{}) (int64, error) {
	now := Now()
	rows, err := d.SQL.Query(`SELECT id, path FROM tracks WHERE missing_at IS NULL`)
	if err != nil {
		return 0, err
	}
	var toMark []int64
	for rows.Next() {
		var id int64
		var path string
		if err := rows.Scan(&id, &path); err != nil {
			_ = rows.Close()
			return 0, err
		}
		if _, ok := seenPaths[path]; ok {
			continue
		}
		toMark = append(toMark, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}

	var missing int64
	for _, id := range toMark {
		if _, err := d.SQL.Exec(`UPDATE tracks SET missing_at = ?, updated_at = ? WHERE id = ?`, now, now, id); err != nil {
			return missing, err
		}
		missing++
	}
	return missing, nil
}

// TrackFileInfo is stored file identity for incremental scans.
type TrackFileInfo struct {
	ID        int64
	Path      string
	FileMtime int64
	FileSize  int64
	Missing   bool
}

// ListTrackFiles returns path identity for all catalogued tracks.
func (d *DB) ListTrackFiles() ([]TrackFileInfo, error) {
	rows, err := d.SQL.Query(`SELECT id, path, COALESCE(file_mtime,0), COALESCE(file_size,0), missing_at FROM tracks`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrackFileInfo
	for rows.Next() {
		var t TrackFileInfo
		var missing sql.NullString
		if err := rows.Scan(&t.ID, &t.Path, &t.FileMtime, &t.FileSize, &missing); err != nil {
			return nil, err
		}
		t.Missing = missing.Valid
		out = append(out, t)
	}
	return out, rows.Err()
}

// BeginScanRun records a new scan run; returns id.
func (d *DB) BeginScanRun(trigger string) (int64, error) {
	res, err := d.SQL.Exec(
		`INSERT INTO scan_runs (started_at, trigger, status) VALUES (?, ?, 'running')`,
		Now(), trigger,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FinishScanRun completes a scan run row.
func (d *DB) FinishScanRun(id int64, seen, upserted, missing int, status, errMsg string) error {
	_, err := d.SQL.Exec(`
UPDATE scan_runs SET finished_at = ?, files_seen = ?, files_upserted = ?, files_missing = ?, status = ?, error_message = ?
WHERE id = ?`,
		Now(), seen, upserted, missing, status, NullString(errMsg), id,
	)
	return err
}

// Artist is a browse row.
type Artist struct {
	ID   int64
	Name string
}

// Album is a browse row.
type Album struct {
	ID        int64
	ArtistID  sql.NullInt64
	Title     string
	Year      sql.NullInt64
	CoverPath sql.NullString
	Artist    string
}

// Track is a browse row.
type Track struct {
	ID           int64
	AlbumID      sql.NullInt64
	Path         string
	Title        string
	TrackNumber  sql.NullInt64
	DiscNumber   sql.NullInt64
	DurationMS   sql.NullInt64
	BitrateKbps  sql.NullInt64
	SampleRateHz sql.NullInt64
	Channels     sql.NullInt64
	Format       sql.NullString
	Year         sql.NullInt64
	Comment      sql.NullString
	MissingAt    sql.NullString
	AlbumTitle   string
	ArtistName   string
	CoverPath    sql.NullString
	Genres       string
}

// ListArtists returns artists with at least one non-missing track, sorted.
func (d *DB) ListArtists() ([]Artist, error) {
	rows, err := d.SQL.Query(`
SELECT DISTINCT a.id, a.name
FROM artists a
JOIN albums al ON al.artist_id = a.id
JOIN tracks t ON t.album_id = al.id AND t.missing_at IS NULL
ORDER BY COALESCE(a.name_sort, a.name) COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Artist
	for rows.Next() {
		var a Artist
		if err := rows.Scan(&a.ID, &a.Name); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetArtist returns an artist by id.
func (d *DB) GetArtist(id int64) (*Artist, error) {
	var a Artist
	err := d.SQL.QueryRow(`SELECT id, name FROM artists WHERE id = ?`, id).Scan(&a.ID, &a.Name)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// ListAlbumsByArtist lists albums for an artist.
func (d *DB) ListAlbumsByArtist(artistID int64) ([]Album, error) {
	rows, err := d.SQL.Query(`
SELECT al.id, al.artist_id, al.title, al.year, al.cover_path, COALESCE(a.name, '')
FROM albums al
LEFT JOIN artists a ON a.id = al.artist_id
WHERE al.artist_id = ?
  AND EXISTS (SELECT 1 FROM tracks t WHERE t.album_id = al.id AND t.missing_at IS NULL)
ORDER BY al.year, al.title COLLATE NOCASE`, artistID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAlbums(rows)
}

// GetAlbum returns album with artist name.
func (d *DB) GetAlbum(id int64) (*Album, error) {
	row := d.SQL.QueryRow(`
SELECT al.id, al.artist_id, al.title, al.year, al.cover_path, COALESCE(a.name, '')
FROM albums al
LEFT JOIN artists a ON a.id = al.artist_id
WHERE al.id = ?`, id)
	var al Album
	if err := row.Scan(&al.ID, &al.ArtistID, &al.Title, &al.Year, &al.CoverPath, &al.Artist); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &al, nil
}

// ListTracksByAlbum lists tracks on an album.
func (d *DB) ListTracksByAlbum(albumID int64) ([]Track, error) {
	rows, err := d.SQL.Query(`
SELECT t.id, t.album_id, t.path, t.title, t.track_number, t.disc_number, t.duration_ms,
       t.bitrate_kbps, t.sample_rate_hz, t.channels, t.format, t.year, t.comment, t.missing_at,
       COALESCE(al.title, ''), COALESCE(a.name, ''), al.cover_path,
       COALESCE((SELECT GROUP_CONCAT(g.name, ', ') FROM track_genres tg JOIN genres g ON g.id = tg.genre_id WHERE tg.track_id = t.id), '')
FROM tracks t
LEFT JOIN albums al ON al.id = t.album_id
LEFT JOIN artists a ON a.id = al.artist_id
WHERE t.album_id = ? AND t.missing_at IS NULL
ORDER BY COALESCE(t.disc_number,1), COALESCE(t.track_number, 9999), t.title COLLATE NOCASE`, albumID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTracks(rows)
}

// GetTrack returns a track by id.
func (d *DB) GetTrack(id int64) (*Track, error) {
	row := d.SQL.QueryRow(`
SELECT t.id, t.album_id, t.path, t.title, t.track_number, t.disc_number, t.duration_ms,
       t.bitrate_kbps, t.sample_rate_hz, t.channels, t.format, t.year, t.comment, t.missing_at,
       COALESCE(al.title, ''), COALESCE(a.name, ''), al.cover_path,
       COALESCE((SELECT GROUP_CONCAT(g.name, ', ') FROM track_genres tg JOIN genres g ON g.id = tg.genre_id WHERE tg.track_id = t.id), '')
FROM tracks t
LEFT JOIN albums al ON al.id = t.album_id
LEFT JOIN artists a ON a.id = al.artist_id
WHERE t.id = ?`, id)
	t, err := scanTrack(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ListTracks returns recent non-missing tracks (browse index).
func (d *DB) ListTracks(limit int) ([]Track, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := d.SQL.Query(`
SELECT t.id, t.album_id, t.path, t.title, t.track_number, t.disc_number, t.duration_ms,
       t.bitrate_kbps, t.sample_rate_hz, t.channels, t.format, t.year, t.comment, t.missing_at,
       COALESCE(al.title, ''), COALESCE(a.name, ''), al.cover_path,
       COALESCE((SELECT GROUP_CONCAT(g.name, ', ') FROM track_genres tg JOIN genres g ON g.id = tg.genre_id WHERE tg.track_id = t.id), '')
FROM tracks t
LEFT JOIN albums al ON al.id = t.album_id
LEFT JOIN artists a ON a.id = al.artist_id
WHERE t.missing_at IS NULL
ORDER BY t.title COLLATE NOCASE
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTracks(rows)
}

// Counts returns basic catalog counts.
func (d *DB) Counts() (artists, albums, tracks int, err error) {
	err = d.SQL.QueryRow(`SELECT COUNT(*) FROM artists`).Scan(&artists)
	if err != nil {
		return
	}
	err = d.SQL.QueryRow(`SELECT COUNT(*) FROM albums`).Scan(&albums)
	if err != nil {
		return
	}
	err = d.SQL.QueryRow(`SELECT COUNT(*) FROM tracks WHERE missing_at IS NULL`).Scan(&tracks)
	return
}

func scanAlbums(rows *sql.Rows) ([]Album, error) {
	var out []Album
	for rows.Next() {
		var al Album
		if err := rows.Scan(&al.ID, &al.ArtistID, &al.Title, &al.Year, &al.CoverPath, &al.Artist); err != nil {
			return nil, err
		}
		out = append(out, al)
	}
	return out, rows.Err()
}

type scannable interface {
	Scan(dest ...any) error
}

func scanTrack(row scannable) (Track, error) {
	var t Track
	err := row.Scan(
		&t.ID, &t.AlbumID, &t.Path, &t.Title, &t.TrackNumber, &t.DiscNumber, &t.DurationMS,
		&t.BitrateKbps, &t.SampleRateHz, &t.Channels, &t.Format, &t.Year, &t.Comment, &t.MissingAt,
		&t.AlbumTitle, &t.ArtistName, &t.CoverPath, &t.Genres,
	)
	return t, err
}

func scanTracks(rows *sql.Rows) ([]Track, error) {
	var out []Track
	for rows.Next() {
		t, err := scanTrack(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// FormatDuration formats milliseconds as m:ss.
func FormatDuration(ms sql.NullInt64) string {
	if !ms.Valid {
		return "—"
	}
	sec := ms.Int64 / 1000
	return fmt.Sprintf("%d:%02d", sec/60, sec%60)
}
