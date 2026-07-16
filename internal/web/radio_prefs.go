// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package web

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/shirtbrotherlabs/LatentTone/internal/auth"
)

// handleMeRadioPrefs serves GET/PATCH /api/v1/me/radio-prefs.
func (s *Server) handleMeRadioPrefs(w http.ResponseWriter, r *http.Request) {
	auth.RequireUser(s.dispatchMeRadioPrefs)(w, r)
}

func (s *Server) dispatchMeRadioPrefs(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		prefs, err := s.DB.GetRadioPrefs(u.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, prefs)
	case http.MethodPatch:
		s.patchRadioPrefs(w, r, u.ID)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

type radioPrefsPatch struct {
	RadioBridge    *bool    `json:"radio_bridge"`
	ArtistCooldown *bool    `json:"artist_cooldown"`
	QueryJitter    *bool    `json:"query_jitter"`
	ArtistPenalty  *bool    `json:"artist_penalty"`
	BoundedRandom  *bool    `json:"bounded_random"`
	JitterAlpha    *float64 `json:"jitter_alpha"`
}

func (s *Server) patchRadioPrefs(w http.ResponseWriter, r *http.Request, userID int64) {
	var body radioPrefsPatch
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	cur, err := s.DB.GetRadioPrefs(userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if body.RadioBridge != nil {
		cur.RadioBridge = *body.RadioBridge
	}
	if body.ArtistCooldown != nil {
		cur.ArtistCooldown = *body.ArtistCooldown
	}
	if body.QueryJitter != nil {
		cur.QueryJitter = *body.QueryJitter
	}
	if body.ArtistPenalty != nil {
		cur.ArtistPenalty = *body.ArtistPenalty
	}
	if body.BoundedRandom != nil {
		cur.BoundedRandom = *body.BoundedRandom
	}
	if body.JitterAlpha != nil {
		cur.JitterAlpha = *body.JitterAlpha
	}
	cur.UserID = userID
	out, err := s.DB.UpsertRadioPrefs(cur)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}
