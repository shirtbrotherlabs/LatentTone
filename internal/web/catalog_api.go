// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-16

package web

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"github.com/shirtbrotherlabs/LatentTone/internal/db"
)

// Catalog JSON for the Phase 4 product SPA (exposes Phase 1–2 data; no scanner ownership).

func (s *Server) handleCatalogAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/catalog")
	path = strings.Trim(path, "/")
	if path == "" {
		artists, albums, tracks, _ := s.DB.Counts()
		writeJSON(w, http.StatusOK, map[string]any{
			"artists": artists,
			"albums":  albums,
			"tracks":  tracks,
		})
		return
	}
	parts := strings.Split(path, "/")
	switch parts[0] {
	case "artists":
		s.handleCatalogArtists(w, r, parts[1:])
	case "albums":
		s.handleCatalogAlbums(w, r, parts[1:])
	case "tracks":
		s.handleCatalogTracks(w, r, parts[1:])
	case "years":
		s.handleCatalogYears(w, r, parts[1:])
	case "genres":
		s.handleCatalogGenres(w, r, parts[1:])
	case "search":
		s.handleCatalogSearch(w, r, parts[1:])
	case "duplicates":
		s.handleCatalogDuplicates(w, r, parts[1:])
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleCatalogArtists(w http.ResponseWriter, r *http.Request, rest []string) {
	if len(rest) == 0 {
		artists, err := s.DB.ListArtists()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		out := make([]map[string]any, 0, len(artists))
		for _, a := range artists {
			row := map[string]any{"id": a.ID, "name": a.Name}
			if a.CoverPath.Valid && a.CoverPath.String != "" {
				row["cover_url"] = "/covers/" + a.CoverPath.String
			}
			out = append(out, row)
		}
		writeJSON(w, http.StatusOK, map[string]any{"artists": out})
		return
	}
	id, err := strconv.ParseInt(rest[0], 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid artist id"})
		return
	}
	a, err := s.DB.GetArtist(id)
	if err != nil || a == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	albums, err := s.DB.ListAlbumsByArtist(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":     a.ID,
		"name":   a.Name,
		"albums": albumJSONList(albums),
	})
}

func (s *Server) handleCatalogAlbums(w http.ResponseWriter, r *http.Request, rest []string) {
	if len(rest) == 0 {
		limit := queryInt(r, "limit", 500)
		albums, err := s.DB.ListAlbums(limit)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"albums": albumJSONList(albums)})
		return
	}
	id, err := strconv.ParseInt(rest[0], 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid album id"})
		return
	}
	al, err := s.DB.GetAlbum(id)
	if err != nil || al == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	tracks, err := s.DB.ListTracksByAlbum(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	trackRows := trackJSONList(tracks)
	marks := db.MarkAlbumDuplicates(tracks)
	for i := range trackRows {
		if i < len(marks) {
			if marks[i].IsDuplicate {
				trackRows[i]["is_duplicate"] = true
				trackRows[i]["preferred_track_id"] = marks[i].PreferredID
				trackRows[i]["duplicate_reason"] = marks[i].Reason
			} else {
				trackRows[i]["is_duplicate"] = false
			}
		}
	}
	s.enrichTrackMaps(r, trackRows)
	writeJSON(w, http.StatusOK, map[string]any{
		"album":  albumJSON(al),
		"tracks": trackRows,
	})
}

func (s *Server) handleCatalogTracks(w http.ResponseWriter, r *http.Request, rest []string) {
	if len(rest) == 0 {
		limit := queryInt(r, "limit", 200)
		if r.URL.Query().Get("suggest") == "seeds" {
			if limit <= 0 || limit > 100 {
				limit = 12
			}
			tracks, err := s.DB.ListSeedSuggestions(limit)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			trackRows := trackJSONList(tracks)
			s.enrichTrackMaps(r, trackRows)
			writeJSON(w, http.StatusOK, map[string]any{"tracks": trackRows})
			return
		}
		q := r.URL.Query().Get("q")
		year := queryInt(r, "year", 0)
		tracks, err := s.DB.ListTracksFiltered(limit, q, year)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		trackRows := trackJSONList(tracks)
		s.enrichTrackMaps(r, trackRows)
		writeJSON(w, http.StatusOK, map[string]any{"tracks": trackRows})
		return
	}
	id, err := strconv.ParseInt(rest[0], 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid track id"})
		return
	}
	t, err := s.DB.GetTrack(id)
	if err != nil || t == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	row := trackJSON(t)
	s.enrichTrackMaps(r, []map[string]any{row})
	writeJSON(w, http.StatusOK, row)
}

func (s *Server) handleCatalogYears(w http.ResponseWriter, r *http.Request, rest []string) {
	if len(rest) == 0 {
		years, err := s.DB.ListYears()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		out := make([]map[string]any, 0, len(years))
		for _, y := range years {
			out = append(out, map[string]any{"year": y.Year, "count": y.Count})
		}
		writeJSON(w, http.StatusOK, map[string]any{"years": out})
		return
	}
	year, err := strconv.Atoi(rest[0])
	if err != nil || year <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid year"})
		return
	}
	limit := queryInt(r, "limit", 500)
	tracks, err := s.DB.ListTracksByYear(year, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	trackRows := trackJSONList(tracks)
	s.enrichTrackMaps(r, trackRows)
	writeJSON(w, http.StatusOK, map[string]any{
		"year":   year,
		"tracks": trackRows,
	})
}

func nullIntJSON(n sql.NullInt64) any {
	if !n.Valid {
		return nil
	}
	return n.Int64
}

func nullStrJSON(n sql.NullString) any {
	if !n.Valid {
		return nil
	}
	return n.String
}

func albumJSON(al *db.Album) map[string]any {
	if al == nil {
		return nil
	}
	m := map[string]any{
		"id":     al.ID,
		"title":  al.Title,
		"artist": al.Artist,
		"year":   nullIntJSON(al.Year),
	}
	if al.ArtistID.Valid {
		m["artist_id"] = al.ArtistID.Int64
	}
	if al.CoverPath.Valid && al.CoverPath.String != "" {
		m["cover_url"] = "/covers/" + al.CoverPath.String
	}
	return m
}

func albumJSONList(albums []db.Album) []map[string]any {
	out := make([]map[string]any, 0, len(albums))
	for i := range albums {
		out = append(out, albumJSON(&albums[i]))
	}
	return out
}

func trackJSON(t *db.Track) map[string]any {
	if t == nil {
		return nil
	}
	m := map[string]any{
		"id":           t.ID,
		"title":        t.Title,
		"artist":       t.ArtistName,
		"album":        t.AlbumTitle,
		"track_number": nullIntJSON(t.TrackNumber),
		"disc_number":  nullIntJSON(t.DiscNumber),
		"duration_ms":  nullIntJSON(t.DurationMS),
		"bitrate_kbps": nullIntJSON(t.BitrateKbps),
		"format":       nullStrJSON(t.Format),
		"year":         nullIntJSON(t.Year),
		"genres":       t.Genres,
	}
	if t.AlbumID.Valid {
		m["album_id"] = t.AlbumID.Int64
	}
	if t.ArtistID.Valid {
		m["artist_id"] = t.ArtistID.Int64
	}
	if t.CoverPath.Valid && t.CoverPath.String != "" {
		m["cover_url"] = "/covers/" + t.CoverPath.String
	}
	return m
}

func trackJSONList(tracks []db.Track) []map[string]any {
	out := make([]map[string]any, 0, len(tracks))
	for i := range tracks {
		out = append(out, trackJSON(&tracks[i]))
	}
	return out
}

func (s *Server) handleCatalogGenres(w http.ResponseWriter, r *http.Request, rest []string) {
	if len(rest) != 0 {
		http.NotFound(w, r)
		return
	}
	limit := queryInt(r, "limit", 200)
	genres, err := s.DB.ListGenres(limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := make([]map[string]any, 0, len(genres))
	for _, g := range genres {
		out = append(out, map[string]any{"id": g.ID, "name": g.Name, "count": g.Count})
	}
	writeJSON(w, http.StatusOK, map[string]any{"genres": out})
}

func (s *Server) handleCatalogSearch(w http.ResponseWriter, r *http.Request, rest []string) {
	if len(rest) == 1 && rest[0] == "suggest" {
		q := r.URL.Query().Get("q")
		limit := queryInt(r, "limit", 12)
		hits, err := s.DB.SearchSuggest(q, limit)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		out := make([]map[string]any, 0, len(hits))
		for _, h := range hits {
			row := map[string]any{
				"kind":     h.Kind,
				"id":       h.ID,
				"label":    h.Label,
				"sublabel": h.SubLabel,
			}
			if h.TrackID > 0 {
				row["track_id"] = h.TrackID
			}
			if h.DurationMS > 0 {
				row["duration_ms"] = h.DurationMS
			}
			if h.CoverPath != "" {
				row["cover_url"] = "/covers/" + h.CoverPath
			}
			out = append(out, row)
		}
		writeJSON(w, http.StatusOK, map[string]any{"suggestions": out, "q": q})
		return
	}
	http.NotFound(w, r)
}

func (s *Server) handleCatalogDuplicates(w http.ResponseWriter, r *http.Request, rest []string) {
	if len(rest) != 0 {
		http.NotFound(w, r)
		return
	}
	// Authenticated library owners; RequireUser via wrapper in mux if needed — catalog is public today.
	limit := queryInt(r, "limit", 200)
	groups, err := s.DB.ListDuplicateGroups(limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := make([]map[string]any, 0, len(groups))
	for _, g := range groups {
		tracks := make([]map[string]any, 0, len(g.Tracks))
		for _, tr := range g.Tracks {
			row := map[string]any{
				"id":          tr.TrackID,
				"title":       tr.Title,
				"album":       tr.Album,
				"artist":      tr.Artist,
				"path":        tr.Path,
				"duration_ms": tr.DurationMS,
			}
			if tr.CoverPath != "" {
				row["cover_url"] = "/covers/" + tr.CoverPath
			}
			tracks = append(tracks, row)
		}
		out = append(out, map[string]any{
			"title":       g.Title,
			"album":       g.Album,
			"artist":      g.Artist,
			"duration_ms": g.DurationMS,
			"count":       len(g.Tracks),
			"tracks":      tracks,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"groups": out,
		"rule":   "normalized title+album+artist; |duration| ≤ 1s",
	})
}
