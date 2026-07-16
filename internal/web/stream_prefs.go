// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package web

import (
	"net/http"

	"github.com/shirtbrotherlabs/LatentTone/internal/auth"
	"github.com/shirtbrotherlabs/LatentTone/internal/db"
)

// handleMeStreamPrefs serves GET/PATCH /api/v1/me/stream-prefs.
func (s *Server) handleMeStreamPrefs(w http.ResponseWriter, r *http.Request) {
	auth.RequireUser(s.dispatchMeStreamPrefs)(w, r)
}

func (s *Server) dispatchMeStreamPrefs(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		prefs, err := s.DB.GetStreamPrefs(u.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, prefs)
	case http.MethodPatch:
		s.patchStreamPrefs(w, r, u.ID)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

type streamPrefsPatch struct {
	StreamFormat *string `json:"stream_format"`
	BitrateKbps  *int    `json:"bitrate_kbps"`
}

func (s *Server) patchStreamPrefs(w http.ResponseWriter, r *http.Request, userID int64) {
	var body streamPrefsPatch
	if !decodeJSONBody(w, r, &body) {
		return
	}
	cur, err := s.DB.GetStreamPrefs(userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if body.StreamFormat != nil {
		norm := db.NormalizeStreamFormat(*body.StreamFormat)
		if norm == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "stream_format must be original, mp3, aac, or opus",
			})
			return
		}
		cur.StreamFormat = norm
	}
	if body.BitrateKbps != nil {
		cur.BitrateKbps = *body.BitrateKbps
	}
	cur.UserID = userID
	out, err := s.DB.UpsertStreamPrefs(cur)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}
