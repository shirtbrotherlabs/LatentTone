// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-16

package web

import (
	"net/http"

	"github.com/shirtbrotherlabs/LatentTone/internal/auth"
	"github.com/shirtbrotherlabs/LatentTone/internal/db"
)

// enrichTrackMaps attaches per-user feedback + play_count onto track JSON maps.
// Accepts either "id" or "track_id" keys.
func (s *Server) enrichTrackMaps(r *http.Request, tracks []map[string]any) {
	if s == nil || s.DB == nil || len(tracks) == 0 {
		return
	}
	u := auth.UserFrom(r.Context())
	if u == nil {
		// Global play counts still useful without auth.
		ids := trackIDsFromMaps(tracks)
		plays, _ := s.DB.PlayCountsForTracks(ids)
		applyPlaysOnly(tracks, plays)
		return
	}
	ids := trackIDsFromMaps(tracks)
	signals, _ := s.DB.LatestLikeDislikeSignals(u.ID, ids)
	plays, _ := s.DB.PlayCountsForTracks(ids)
	for _, m := range tracks {
		id := trackIDFromMap(m)
		if id <= 0 {
			continue
		}
		if sig, ok := signals[id]; ok {
			m["feedback"] = sig
		}
		if n, ok := plays[id]; ok {
			m["play_count"] = n
		} else {
			m["play_count"] = int64(0)
		}
	}
}

func trackIDsFromMaps(tracks []map[string]any) []int64 {
	ids := make([]int64, 0, len(tracks))
	seen := make(map[int64]struct{}, len(tracks))
	for _, m := range tracks {
		id := trackIDFromMap(m)
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func trackIDFromMap(m map[string]any) int64 {
	if m == nil {
		return 0
	}
	switch v := m["id"].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	}
	switch v := m["track_id"].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	}
	return 0
}

// writePlaylistJSON returns a playlist payload with per-user feedback and play counts on track rows.
func (s *Server) writePlaylistJSON(w http.ResponseWriter, r *http.Request, status int, pl *db.Playlist, entries []db.PlaylistEntry) {
	out := playlistJSON(pl, entries)
	if tracks, ok := out["tracks"].([]map[string]any); ok && len(tracks) > 0 {
		s.enrichTrackMaps(r, tracks)
	}
	writeJSON(w, status, out)
}

func applyPlaysOnly(tracks []map[string]any, plays map[int64]int64) {
	for _, m := range tracks {
		id := trackIDFromMap(m)
		if id <= 0 {
			continue
		}
		if n, ok := plays[id]; ok {
			m["play_count"] = n
		} else {
			m["play_count"] = int64(0)
		}
	}
}
