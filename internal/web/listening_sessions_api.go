// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-21

package web

import (
	"net/http"
	"strconv"
	"time"

	"github.com/shirtbrotherlabs/LatentTone/internal/auth"
	"github.com/shirtbrotherlabs/LatentTone/internal/session"
)

// handleMeListeningSessions serves GET /api/v1/me/listening-sessions.
func (s *Server) handleMeListeningSessions(w http.ResponseWriter, r *http.Request) {
	auth.RequireUser(s.listMeListeningSessions)(w, r)
}

func (s *Server) listMeListeningSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	u := auth.UserFrom(r.Context())
	if u == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}
	_ = s.Sessions.ReclaimIdle()
	s.writeListeningSessions(w, r, u.ID)
}

// handleAdminListeningSessions serves GET /api/v1/admin/listening-sessions.
func (s *Server) handleAdminListeningSessions(w http.ResponseWriter, r *http.Request) {
	auth.RequireAdmin(s.listAdminListeningSessions)(w, r)
}

func (s *Server) listAdminListeningSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	_ = s.Sessions.ReclaimIdle()
	s.writeListeningSessions(w, r, 0)
}

func (s *Server) writeListeningSessions(w http.ResponseWriter, r *http.Request, userID int64) {
	limit := 100
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 {
			limit = n
		}
	}
	rows, err := s.DB.ListActiveListeningSessions(userID, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	liveLast := map[string]string{}
	for _, live := range s.Sessions.ListLiveActive(userID) {
		if la := live.LastActiveAt(); !la.IsZero() {
			liveLast[live.ID] = la.UTC().Format(time.RFC3339)
		}
	}

	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		m := map[string]any{
			"id":         row.ID,
			"user_id":    row.UserID,
			"username":   row.Username,
			"status":     row.Status,
			"kind":       row.Kind,
			"created_at": row.CreatedAt,
			"updated_at": row.UpdatedAt,
		}
		if row.SeedTrackID.Valid {
			m["seed_track_id"] = row.SeedTrackID.Int64
		}
		if row.NowPlayingID.Valid {
			m["now_playing_id"] = row.NowPlayingID.Int64
			if t, err := s.DB.GetTrack(row.NowPlayingID.Int64); err == nil && t != nil {
				m["now_playing_title"] = t.Title
				m["now_playing_artist"] = t.ArtistName
			}
		}
		if la, ok := liveLast[row.ID]; ok {
			m["last_active_at"] = la
		} else {
			m["last_active_at"] = row.UpdatedAt
		}
		out = append(out, m)
	}

	idleSec := int(s.Sessions.IdleTTL.Seconds())
	if idleSec <= 0 {
		idleSec = int(session.DefaultSessionIdleTTL.Seconds())
	}
	maxPerUser := s.Sessions.MaxPerUser
	if maxPerUser <= 0 {
		maxPerUser = session.DefaultMaxSessionsPerUser
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sessions":         out,
		"idle_ttl_seconds": idleSec,
		"max_per_user":     maxPerUser,
		"max_concurrent":   s.Sessions.MaxConcurrent,
	})
}
