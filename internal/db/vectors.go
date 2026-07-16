// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package db

import (
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// Vector statuses.
const (
	VecPending    = "pending"
	VecProcessing = "processing"
	VecReady      = "ready"
	VecError      = "error"
	VecStale      = "stale"
)

// TrackVector is the queue / compiled embedding row.
type TrackVector struct {
	TrackID         int64
	Status          string
	ExtractorSet    string
	ModelVersions   string
	LanceDBID       sql.NullString
	VectorDim       sql.NullInt64
	Embedding       []float32
	ErrorMessage    sql.NullString
	AudioMtimeAtRun sql.NullInt64
	CreatedAt       string
	UpdatedAt       string
}

// TrackFeature is one extractor's JSON payload for GUI/display.
type TrackFeature struct {
	TrackID      int64
	Extractor    string
	ModelVersion string
	FeaturesJSON string
	VectorDim    sql.NullInt64
	CreatedAt    string
	UpdatedAt    string
}

// ResetStuckProcessing moves abandoned processing rows back to pending.
func (d *DB) ResetStuckProcessing() (int64, error) {
	res, err := d.SQL.Exec(`UPDATE track_vectors SET status = ?, updated_at = ? WHERE status = ?`,
		VecPending, Now(), VecProcessing)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// EnsureVectorRows inserts pending rows for catalogued tracks lacking vectors.
func (d *DB) EnsureVectorRows(extractorSet, modelVersions string) (int64, error) {
	now := Now()
	res, err := d.SQL.Exec(`
INSERT INTO track_vectors (track_id, status, extractor_set, model_versions, created_at, updated_at)
SELECT t.id, ?, ?, ?, ?, ?
FROM tracks t
WHERE t.missing_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM track_vectors v WHERE v.track_id = t.id)`,
		VecPending, extractorSet, modelVersions, now, now)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// MarkStaleByConfig marks ready/error rows stale when config/mtime drift, then requeues stale → pending.
func (d *DB) MarkStaleByConfig(extractorSet, modelVersions string) (int64, error) {
	now := Now()
	res, err := d.SQL.Exec(`
UPDATE track_vectors
SET status = ?, updated_at = ?
WHERE status IN (?, ?)
  AND (
    extractor_set != ?
    OR IFNULL(model_versions, '') != ?
    OR audio_mtime_at_run IS NULL
    OR audio_mtime_at_run != (SELECT file_mtime FROM tracks WHERE tracks.id = track_vectors.track_id)
  )`,
		VecStale, now, VecReady, VecError, extractorSet, modelVersions)
	if err != nil {
		return 0, err
	}
	res2, err := d.SQL.Exec(`UPDATE track_vectors SET status = ?, updated_at = ? WHERE status = ?`,
		VecPending, now, VecStale)
	if err != nil {
		return 0, err
	}
	n1, _ := res.RowsAffected()
	n2, _ := res2.RowsAffected()
	return n1 + n2, nil
}

// ClaimVectorWork selects up to limit pending tracks (optionally random sample).
func (d *DB) ClaimVectorWork(limit int, sampleMode string, seed int64) ([]int64, error) {
	if limit <= 0 {
		limit = 64
	}
	order := "t.id ASC"
	if strings.EqualFold(sampleMode, "random") {
		// SQLite RANDOM() is fine; seed is advisory (documented in metadata.yaml).
		order = "RANDOM()"
		_ = seed
	}
	q := fmt.Sprintf(`
SELECT v.track_id
FROM track_vectors v
JOIN tracks t ON t.id = v.track_id AND t.missing_at IS NULL
WHERE v.status = ?
ORDER BY %s
LIMIT ?`, order)

	rows, err := d.SQL.Query(q, VecPending, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	now := Now()
	for _, id := range ids {
		if _, err := d.SQL.Exec(
			`UPDATE track_vectors SET status = ?, updated_at = ?, error_message = NULL WHERE track_id = ? AND status = ?`,
			VecProcessing, now, id, VecPending,
		); err != nil {
			return ids, err
		}
	}
	return ids, nil
}

// SaveTrackFeatures upserts one extractor feature JSON row.
func (d *DB) SaveTrackFeatures(trackID int64, extractor, modelVersion, featuresJSON string, dim int) error {
	now := Now()
	_, err := d.SQL.Exec(`
INSERT INTO track_features (track_id, extractor, model_version, features_json, vector_dim, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(track_id, extractor) DO UPDATE SET
  model_version = excluded.model_version,
  features_json = excluded.features_json,
  vector_dim = excluded.vector_dim,
  updated_at = excluded.updated_at`,
		trackID, extractor, modelVersion, featuresJSON, dim, now, now)
	return err
}

// MarkVectorReady stores compiled embedding and marks ready.
// lanceID may be empty (SQLite-only) or a LanceDB row/path identifier.
func (d *DB) MarkVectorReady(trackID int64, extractorSet, modelVersions string, vec []float32, audioMtime int64, lanceID string) error {
	now := Now()
	if lanceID == "" {
		lanceID = fmt.Sprintf("sqlite:%d", trackID)
	}
	blob := Float32SliceToBytes(vec)
	_, err := d.SQL.Exec(`
UPDATE track_vectors SET
  status = ?, extractor_set = ?, model_versions = ?, vector_dim = ?, embedding_blob = ?,
  audio_mtime_at_run = ?, error_message = NULL, updated_at = ?, lancedb_id = ?
WHERE track_id = ?`,
		VecReady, extractorSet, modelVersions, len(vec), blob, audioMtime, now,
		lanceID, trackID)
	return err
}

// MarkVectorError sets error status.
func (d *DB) MarkVectorError(trackID int64, msg string) error {
	_, err := d.SQL.Exec(`
UPDATE track_vectors SET status = ?, error_message = ?, updated_at = ? WHERE track_id = ?`,
		VecError, msg, Now(), trackID)
	return err
}

// ReleaseProcessingToPending resets processing track to pending (cancel).
func (d *DB) ReleaseProcessingToPending(trackID int64) error {
	_, err := d.SQL.Exec(`
UPDATE track_vectors SET status = ?, updated_at = ? WHERE track_id = ? AND status = ?`,
		VecPending, Now(), trackID, VecProcessing)
	return err
}

// GetTrackVector returns vector row for a track (nil if none).
func (d *DB) GetTrackVector(trackID int64) (*TrackVector, error) {
	row := d.SQL.QueryRow(`
SELECT track_id, status, extractor_set, IFNULL(model_versions,''), lancedb_id, vector_dim,
       embedding_blob, error_message, audio_mtime_at_run, created_at, updated_at
FROM track_vectors WHERE track_id = ?`, trackID)
	var v TrackVector
	var blob []byte
	err := row.Scan(&v.TrackID, &v.Status, &v.ExtractorSet, &v.ModelVersions, &v.LanceDBID, &v.VectorDim,
		&blob, &v.ErrorMessage, &v.AudioMtimeAtRun, &v.CreatedAt, &v.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	v.Embedding = BytesToFloat32Slice(blob)
	return &v, nil
}

// ListTrackFeatures returns feature rows for GUI.
func (d *DB) ListTrackFeatures(trackID int64) ([]TrackFeature, error) {
	rows, err := d.SQL.Query(`
SELECT track_id, extractor, model_version, features_json, vector_dim, created_at, updated_at
FROM track_features WHERE track_id = ? ORDER BY extractor`, trackID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrackFeature
	for rows.Next() {
		var f TrackFeature
		if err := rows.Scan(&f.TrackID, &f.Extractor, &f.ModelVersion, &f.FeaturesJSON, &f.VectorDim, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ReadyEmbedding is a track id + vector for affinity search.
type ReadyEmbedding struct {
	TrackID int64
	Vector  []float32
}

// ListReadyEmbeddings loads all ready embeddings (flat index).
func (d *DB) ListReadyEmbeddings() ([]ReadyEmbedding, error) {
	rows, err := d.SQL.Query(`
SELECT track_id, embedding_blob FROM track_vectors
WHERE status = ? AND embedding_blob IS NOT NULL`, VecReady)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReadyEmbedding
	for rows.Next() {
		var id int64
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			return nil, err
		}
		out = append(out, ReadyEmbedding{TrackID: id, Vector: BytesToFloat32Slice(blob)})
	}
	return out, rows.Err()
}

// AcousticExtractors are the identity scanners shown in the web UI.
var AcousticExtractors = []string{"essentia", "yamnet", "musicnn"}

// ExtractorFeatureCounts returns how many distinct tracks have a features row per extractor.
func (d *DB) ExtractorFeatureCounts(extractors []string) (map[string]int, error) {
	out := make(map[string]int, len(extractors))
	for _, name := range extractors {
		out[name] = 0
	}
	if len(extractors) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(extractors))
	args := make([]any, len(extractors))
	for i, name := range extractors {
		placeholders[i] = "?"
		args[i] = name
	}
	q := fmt.Sprintf(`
SELECT extractor, COUNT(*) FROM track_features
WHERE extractor IN (%s)
GROUP BY extractor`, strings.Join(placeholders, ","))
	rows, err := d.SQL.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var n int
		if err := rows.Scan(&name, &n); err != nil {
			return nil, err
		}
		out[name] = n
	}
	return out, rows.Err()
}

// VectorStatusCounts returns counts of track_vectors by status plus catalog track total.
func (d *DB) VectorStatusCounts() (ready, pending, processing, errorN, stale, catalogTracks int, err error) {
	_, _, catalogTracks, err = d.Counts()
	if err != nil {
		return
	}
	rows, err := d.SQL.Query(`SELECT status, COUNT(*) FROM track_vectors GROUP BY status`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var n int
		if err = rows.Scan(&status, &n); err != nil {
			return
		}
		switch status {
		case VecReady:
			ready = n
		case VecPending:
			pending = n
		case VecProcessing:
			processing = n
		case VecError:
			errorN = n
		case VecStale:
			stale = n
		}
	}
	err = rows.Err()
	return
}

// BeginEmbedRun records an embed job.
func (d *DB) BeginEmbedRun(trigger, sampleMode string, maxTracks int) (int64, error) {
	res, err := d.SQL.Exec(`
INSERT INTO embed_runs (started_at, trigger, sample_mode, max_tracks, status)
VALUES (?, ?, ?, ?, 'running')`, Now(), trigger, sampleMode, maxTracks)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FinishEmbedRun completes an embed run.
func (d *DB) FinishEmbedRun(id int64, claimed, ok, errs int, status, errMsg string) error {
	_, err := d.SQL.Exec(`
UPDATE embed_runs SET finished_at = ?, tracks_claimed = ?, tracks_ok = ?, tracks_error = ?, status = ?, error_message = ?
WHERE id = ?`, Now(), claimed, ok, errs, status, NullString(errMsg), id)
	return err
}

// TrackEmbedBrief is catalog fields needed by extractors.
type TrackEmbedBrief struct {
	ID           int64
	Path         string
	FileMtime    int64
	FileSize     int64
	Title        string
	Format       string
	DurationMS   sql.NullInt64
	BitrateKbps  sql.NullInt64
	SampleRateHz sql.NullInt64
	Channels     sql.NullInt64
	Year         sql.NullInt64
	Genres       string
	AlbumTitle   string
	ArtistName   string
}

// GetTrackEmbedBrief loads catalog fields for embedding.
func (d *DB) GetTrackEmbedBrief(trackID int64) (*TrackEmbedBrief, error) {
	row := d.SQL.QueryRow(`
SELECT t.id, t.path, COALESCE(t.file_mtime,0), COALESCE(t.file_size,0), t.title, COALESCE(t.format,''),
       t.duration_ms, t.bitrate_kbps, t.sample_rate_hz, t.channels, COALESCE(t.year, al.year),
       COALESCE((SELECT GROUP_CONCAT(g.name, '|') FROM track_genres tg JOIN genres g ON g.id = tg.genre_id WHERE tg.track_id = t.id), ''),
       COALESCE(al.title, ''), COALESCE(a.name, '')
FROM tracks t
LEFT JOIN albums al ON al.id = t.album_id
LEFT JOIN artists a ON a.id = al.artist_id
WHERE t.id = ?`, trackID)
	var t TrackEmbedBrief
	err := row.Scan(&t.ID, &t.Path, &t.FileMtime, &t.FileSize, &t.Title, &t.Format,
		&t.DurationMS, &t.BitrateKbps, &t.SampleRateHz, &t.Channels, &t.Year,
		&t.Genres, &t.AlbumTitle, &t.ArtistName)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// Float32SliceToBytes encodes float32 LE.
func Float32SliceToBytes(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

// BytesToFloat32Slice decodes float32 LE.
func BytesToFloat32Slice(b []byte) []float32 {
	if len(b) < 4 {
		return nil
	}
	n := len(b) / 4
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out
}

// FeaturesToPrettyJSON returns indented JSON if valid.
func FeaturesToPrettyJSON(raw string) string {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return raw
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return raw
	}
	return string(b)
}
