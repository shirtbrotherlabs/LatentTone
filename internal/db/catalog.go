// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15
// Last-Modified: 2026-07-18

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

// ReuseScanMetadata copies previously probed technical fields when the source
// file signature is unchanged. It returns true when a matching catalog row
// exists. This avoids launching ffprobe for every MP3 on every full scan.
func (d *DB) ReuseScanMetadata(in *TrackInput) (bool, error) {
	if d == nil || d.SQL == nil || in == nil || in.Path == "" {
		return false, nil
	}
	var duration, year sql.NullInt64
	err := d.SQL.QueryRow(`
SELECT duration_ms, year
FROM tracks
WHERE path = ? AND file_mtime = ? AND file_size = ? AND missing_at IS NULL`,
		in.Path, in.FileMtime, in.FileSize,
	).Scan(&duration, &year)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if in.DurationMS == nil && duration.Valid && duration.Int64 > 0 {
		value := duration.Int64
		in.DurationMS = &value
	}
	if in.Year == nil && year.Valid && year.Int64 > 0 {
		value := int(year.Int64)
		in.Year = &value
		in.AlbumYear = &value
	}
	return true, nil
}

// TrackUnchanged reports whether path exists with matching mtime+size and is not missing.
// Used by incremental scans and the filesystem watcher to skip tag extract/upsert.
func (d *DB) TrackUnchanged(path string, mtime, size int64) (bool, error) {
	if d == nil || d.SQL == nil || path == "" {
		return false, nil
	}
	var id int64
	err := d.SQL.QueryRow(`
SELECT id FROM tracks
WHERE path = ? AND file_mtime = ? AND file_size = ? AND missing_at IS NULL`,
		path, mtime, size,
	).Scan(&id)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// UpsertTrackResult is one row outcome from a batched catalog write.
type UpsertTrackResult struct {
	Path    string
	TrackID int64
	Err     error
}

// UpsertTrack inserts or updates a track and related artist/album/genre rows.
func (d *DB) UpsertTrack(in TrackInput) (trackID int64, err error) {
	results, err := d.UpsertTracks([]TrackInput{in})
	if err != nil {
		return 0, err
	}
	if len(results) == 0 {
		return 0, fmt.Errorf("upsert produced no result")
	}
	return results[0].TrackID, results[0].Err
}

// UpsertTracks writes many tracks in a single transaction.
// Concurrent scanner batches feed this; the batch is serialized under
// writeMu to avoid InnoDB deadlocks between overlapping batches sharing
// artist/album rows within this process. Per-track SAVEPOINTs keep one bad
// file from rolling back the whole batch.
func (d *DB) UpsertTracks(batch []TrackInput) ([]UpsertTrackResult, error) {
	out := make([]UpsertTrackResult, 0, len(batch))
	if len(batch) == 0 {
		return out, nil
	}

	d.writeMu.Lock()
	defer d.writeMu.Unlock()

	return d.upsertTracksLocked(batch)
}

func (d *DB) upsertTracksLocked(batch []TrackInput) ([]UpsertTrackResult, error) {
	out := make([]UpsertTrackResult, 0, len(batch))
	now := Now()
	tx, err := d.SQL.Begin()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	for i, in := range batch {
		res := UpsertTrackResult{Path: in.Path}
		sp := fmt.Sprintf("sp_%d", i)
		if _, e := tx.Exec("SAVEPOINT " + sp); e != nil {
			res.Err = e
			out = append(out, res)
			continue
		}
		id, e := upsertTrackTx(tx, in, now)
		if e != nil {
			_, _ = tx.Exec("ROLLBACK TO SAVEPOINT " + sp)
			_, _ = tx.Exec("RELEASE SAVEPOINT " + sp)
			res.Err = e
			out = append(out, res)
			continue
		}
		if _, e := tx.Exec("RELEASE SAVEPOINT " + sp); e != nil {
			res.Err = e
			out = append(out, res)
			continue
		}
		res.TrackID = id
		out = append(out, res)
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

func upsertTrackTx(tx *sql.Tx, in TrackInput, now string) (trackID int64, err error) {
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
			return 0, e
		}
		trackID, e = res.LastInsertId()
		if e != nil {
			return 0, e
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
			return 0, e
		}
		role := "primary"
		if i > 0 {
			role = "featured"
		}
		_, err = tx.Exec(
			`INSERT INTO track_artists (track_id, artist_id, role, position) VALUES (?, ?, ?, ?)
ON DUPLICATE KEY UPDATE position = VALUES(position)`,
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
			return 0, e
		}
		_, err = tx.Exec(
			`INSERT IGNORE INTO track_genres (track_id, genre_id, source) VALUES (?, ?, 'tag')`,
			trackID, gid,
		)
		if err != nil {
			return 0, err
		}
	}
	return trackID, nil
}

// upsertArtistTx matches by name case-insensitively via the artists table's
// default collation (utf8mb4_unicode_ci).
func upsertArtistTx(tx *sql.Tx, name, now string) (int64, error) {
	var id int64
	err := tx.QueryRow(`SELECT id FROM artists WHERE name = ?`, name).Scan(&id)
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
		err2 := tx.QueryRow(`SELECT id FROM artists WHERE name = ?`, name).Scan(&id)
		return id, err2
	}
	return res.LastInsertId()
}

// upsertAlbumTx matches by (artist_id, title) case-insensitively via the
// albums table's default collation (utf8mb4_unicode_ci).
func upsertAlbumTx(tx *sql.Tx, artistID int64, title string, year *int, coverPath, now string) (int64, error) {
	var id int64
	err := tx.QueryRow(
		`SELECT id FROM albums WHERE artist_id = ? AND title = ?`,
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
			`SELECT id FROM albums WHERE artist_id = ? AND title = ?`,
			artistID, title,
		).Scan(&id)
		return id, err2
	}
	return res.LastInsertId()
}

// upsertGenreTx matches by name case-insensitively via the genres table's
// default collation (utf8mb4_unicode_ci).
func upsertGenreTx(tx *sql.Tx, name string) (int64, error) {
	var id int64
	err := tx.QueryRow(`SELECT id FROM genres WHERE name = ?`, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	res, err := tx.Exec(`INSERT INTO genres (name) VALUES (?)`, name)
	if err != nil {
		err2 := tx.QueryRow(`SELECT id FROM genres WHERE name = ?`, name).Scan(&id)
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
		"INSERT INTO scan_runs (started_at, `trigger`, status) VALUES (?, ?, 'running')",
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

// ScanRunSummary is the latest (or current) library scan run for the UI.
type ScanRunSummary struct {
	ID        int64
	Trigger   string
	StartedAt string
	Finished  string // empty while running
	Seen      int
	Upserted  int
	Missing   int
	Status    string
	Error     string
}

// LatestScanRun returns the most recent scan_runs row, if any.
func (d *DB) LatestScanRun() (*ScanRunSummary, error) {
	if d == nil || d.SQL == nil {
		return nil, nil
	}
	row := d.SQL.QueryRow(
		"SELECT id, started_at, COALESCE(finished_at,''), `trigger`, " +
			"COALESCE(files_seen,0), COALESCE(files_upserted,0), COALESCE(files_missing,0), " +
			"status, COALESCE(error_message,'') " +
			"FROM scan_runs ORDER BY id DESC LIMIT 1",
	)
	var s ScanRunSummary
	err := row.Scan(
		&s.ID, &s.StartedAt, &s.Finished, &s.Trigger,
		&s.Seen, &s.Upserted, &s.Missing, &s.Status, &s.Error,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// FormatScanRunLast builds the Settings "Last:" line from a scan_runs row.
func FormatScanRunLast(s *ScanRunSummary) string {
	if s == nil {
		return ""
	}
	when := s.Finished
	if when == "" {
		when = s.StartedAt
	}
	msg := fmt.Sprintf("%s trigger=%s seen=%d upserted=%d missing=%d status=%s",
		when, s.Trigger, s.Seen, s.Upserted, s.Missing, s.Status)
	if s.Error != "" {
		msg += " error=" + s.Error
	}
	return msg
}

// Artist is a browse row.
type Artist struct {
	ID        int64
	Name      string
	CoverPath sql.NullString // representative album cover when available
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
	ArtistID     sql.NullInt64
	CoverPath    sql.NullString
	Genres       string
}

// ListArtists returns artists with at least one non-missing track, sorted.
// CoverPath is a representative album cover (first album with a cover, by year/title).
// Uses EXISTS (not JOIN) so the planner can use idx_tracks_album_missing per album.
func (d *DB) ListArtists() ([]Artist, error) {
	rows, err := d.SQL.Query(`
SELECT a.id, a.name,
  (SELECT al.cover_path
   FROM albums al
   WHERE al.artist_id = a.id
     AND al.cover_path IS NOT NULL AND TRIM(al.cover_path) != ''
     AND EXISTS (
       SELECT 1 FROM tracks t
       WHERE t.album_id = al.id AND t.missing_at IS NULL
     )
   ORDER BY al.year, al.title
   LIMIT 1) AS cover_path
FROM artists a
WHERE EXISTS (
  SELECT 1 FROM albums al
  WHERE al.artist_id = a.id
    AND EXISTS (
      SELECT 1 FROM tracks t
      WHERE t.album_id = al.id AND t.missing_at IS NULL
    )
)
ORDER BY COALESCE(a.name_sort, a.name)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Artist
	for rows.Next() {
		var a Artist
		if err := rows.Scan(&a.ID, &a.Name, &a.CoverPath); err != nil {
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
ORDER BY al.year, al.title`, artistID)
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
       t.bitrate_kbps, t.sample_rate_hz, t.channels, t.format, COALESCE(t.year, al.year), t.comment, t.missing_at,
       COALESCE(al.title, ''), COALESCE(a.name, ''), al.cover_path,
       COALESCE((SELECT GROUP_CONCAT(g.name SEPARATOR ', ') FROM track_genres tg JOIN genres g ON g.id = tg.genre_id WHERE tg.track_id = t.id), ''),
       al.artist_id
FROM tracks t
LEFT JOIN albums al ON al.id = t.album_id
LEFT JOIN artists a ON a.id = al.artist_id
WHERE t.album_id = ? AND t.missing_at IS NULL
ORDER BY COALESCE(t.disc_number,1), COALESCE(t.track_number, 9999), t.title`, albumID)
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
       t.bitrate_kbps, t.sample_rate_hz, t.channels, t.format, COALESCE(t.year, al.year), t.comment, t.missing_at,
       COALESCE(al.title, ''), COALESCE(a.name, ''), al.cover_path,
       COALESCE((SELECT GROUP_CONCAT(g.name SEPARATOR ', ') FROM track_genres tg JOIN genres g ON g.id = tg.genre_id WHERE tg.track_id = t.id), ''),
       al.artist_id
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
	return d.ListTracksFiltered(limit, "", 0)
}

// ListSeedSuggestions returns up to limit random non-missing tracks for Now Playing
// quick seeds. Prefer tracks with at least one playback_events row (global play count
// proxy); if that pool is short, fill the rest from unplayed tracks at random.
func (d *DB) ListSeedSuggestions(limit int) ([]Track, error) {
	if limit <= 0 {
		limit = 12
	}
	if limit > 100 {
		limit = 100
	}
	const trackCols = `
SELECT t.id, t.album_id, t.path, t.title, t.track_number, t.disc_number, t.duration_ms,
       t.bitrate_kbps, t.sample_rate_hz, t.channels, t.format, COALESCE(t.year, al.year), t.comment, t.missing_at,
       COALESCE(al.title, ''), COALESCE(a.name, ''), al.cover_path,
       COALESCE((SELECT GROUP_CONCAT(g.name SEPARATOR ', ') FROM track_genres tg JOIN genres g ON g.id = tg.genre_id WHERE tg.track_id = t.id), ''),
       al.artist_id
FROM tracks t
LEFT JOIN albums al ON al.id = t.album_id
LEFT JOIN artists a ON a.id = al.artist_id`

	played, err := d.SQL.Query(trackCols+`
WHERE t.missing_at IS NULL
  AND EXISTS (SELECT 1 FROM playback_events pe WHERE pe.track_id = t.id)
ORDER BY RAND()
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	out, err := scanTracks(played)
	_ = played.Close()
	if err != nil {
		return nil, err
	}
	if len(out) >= limit {
		return out[:limit], nil
	}

	need := limit - len(out)
	exclude := make([]string, 0, len(out))
	args := make([]any, 0, len(out)+1)
	for _, t := range out {
		exclude = append(exclude, "?")
		args = append(args, t.ID)
	}
	args = append(args, need)
	sqlStr := trackCols + `
WHERE t.missing_at IS NULL`
	if len(exclude) > 0 {
		sqlStr += `
  AND t.id NOT IN (` + strings.Join(exclude, ",") + `)`
	}
	sqlStr += `
ORDER BY RAND()
LIMIT ?`
	fill, err := d.SQL.Query(sqlStr, args...)
	if err != nil {
		return nil, err
	}
	defer fill.Close()
	extra, err := scanTracks(fill)
	if err != nil {
		return nil, err
	}
	return append(out, extra...), nil
}

// ListTracksFiltered lists non-missing tracks with optional title substring and year.
// year == 0 means any year. q is matched case-insensitively against title/artist/album.
func (d *DB) ListTracksFiltered(limit int, q string, year int) ([]Track, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 2000 {
		limit = 2000
	}
	q = strings.TrimSpace(q)
	args := []any{}
	where := []string{"t.missing_at IS NULL"}
	if year > 0 {
		where = append(where, "COALESCE(t.year, al.year) = ?")
		args = append(args, year)
	}
	if q != "" {
		where = append(where, `(t.title LIKE ?
			OR COALESCE(a.name, '') LIKE ?
			OR COALESCE(al.title, '') LIKE ?)`)
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	args = append(args, limit)
	sqlStr := `
SELECT t.id, t.album_id, t.path, t.title, t.track_number, t.disc_number, t.duration_ms,
       t.bitrate_kbps, t.sample_rate_hz, t.channels, t.format, COALESCE(t.year, al.year), t.comment, t.missing_at,
       COALESCE(al.title, ''), COALESCE(a.name, ''), al.cover_path,
       COALESCE((SELECT GROUP_CONCAT(g.name SEPARATOR ', ') FROM track_genres tg JOIN genres g ON g.id = tg.genre_id WHERE tg.track_id = t.id), ''),
       al.artist_id
FROM tracks t
LEFT JOIN albums al ON al.id = t.album_id
LEFT JOIN artists a ON a.id = al.artist_id
WHERE ` + strings.Join(where, " AND ") + `
ORDER BY t.title
LIMIT ?`
	rows, err := d.SQL.Query(sqlStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTracks(rows)
}

// ListAlbums returns albums with at least one non-missing track.
func (d *DB) ListAlbums(limit int) ([]Album, error) {
	if limit <= 0 {
		limit = 500
	}
	if limit > 2000 {
		limit = 2000
	}
	rows, err := d.SQL.Query(`
SELECT al.id, al.artist_id, al.title, al.year, al.cover_path, COALESCE(a.name, '')
FROM albums al
LEFT JOIN artists a ON a.id = al.artist_id
WHERE EXISTS (SELECT 1 FROM tracks t WHERE t.album_id = al.id AND t.missing_at IS NULL)
ORDER BY al.title
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAlbums(rows)
}

// YearCount is a release-year browse bucket.
type YearCount struct {
	Year  int
	Count int
}

// ListYears returns distinct release years (track year, falling back to album year) with track counts.
func (d *DB) ListYears() ([]YearCount, error) {
	rows, err := d.SQL.Query(`
SELECT y.year, COUNT(*) AS n
FROM (
  SELECT COALESCE(t.year, al.year) AS year
  FROM tracks t
  LEFT JOIN albums al ON al.id = t.album_id
  WHERE t.missing_at IS NULL AND COALESCE(t.year, al.year) IS NOT NULL
) y
GROUP BY y.year
ORDER BY y.year DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []YearCount
	for rows.Next() {
		var yc YearCount
		if err := rows.Scan(&yc.Year, &yc.Count); err != nil {
			return nil, err
		}
		out = append(out, yc)
	}
	return out, rows.Err()
}

// ListTracksByYear lists tracks whose release year matches (track year, else album year).
func (d *DB) ListTracksByYear(year, limit int) ([]Track, error) {
	return d.ListTracksFiltered(limit, "", year)
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
		&t.AlbumTitle, &t.ArtistName, &t.CoverPath, &t.Genres, &t.ArtistID,
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
