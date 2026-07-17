// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package web

import (
	"net/http"
	"strconv"

	"github.com/shirtbrotherlabs/LatentTone/internal/auth"
	"github.com/shirtbrotherlabs/LatentTone/internal/db"
)

// handleMeStations serves GET /api/v1/me/stations — recent continuous-play radio stations.
func (s *Server) handleMeStations(w http.ResponseWriter, r *http.Request) {
	auth.RequireUser(s.listMeStations)(w, r)
}

func (s *Server) listMeStations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	u := auth.UserFrom(r.Context())
	if u == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}
	limit := 12
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil {
			limit = n
		}
	}
	rows, err := s.DB.ListRecentListeningSessions(u.ID, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	stations := make([]map[string]any, 0, len(rows))
	for i := range rows {
		stations = append(stations, s.stationJSON(&rows[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"stations": stations})
}

func (s *Server) stationJSON(row *db.ListeningSession) map[string]any {
	m := map[string]any{
		"id":         row.ID,
		"status":     row.Status,
		"started_at": row.CreatedAt,
		"updated_at": row.UpdatedAt,
	}
	if row.SeedTrackID.Valid {
		m["seed_track_id"] = row.SeedTrackID.Int64
		if t, err := s.DB.GetTrack(row.SeedTrackID.Int64); err == nil && t != nil {
			m["seed_track"] = trackJSON(t)
		}
	}
	if row.NowPlayingID.Valid {
		m["now_playing_id"] = row.NowPlayingID.Int64
		if t, err := s.DB.GetTrack(row.NowPlayingID.Int64); err == nil && t != nil {
			m["now_playing"] = trackJSON(t)
		}
	}
	if row.Status == db.SessionStatusStopped || row.Status == db.SessionStatusError {
		m["stopped_at"] = row.UpdatedAt
	}
	return m
}
