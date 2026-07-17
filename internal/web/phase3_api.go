// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package web

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/shirtbrotherlabs/LatentTone/internal/auth"
	"github.com/shirtbrotherlabs/LatentTone/internal/db"
	"github.com/shirtbrotherlabs/LatentTone/internal/session"
	"github.com/shirtbrotherlabs/LatentTone/internal/stream"
)

type credBody struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleAuthRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var body credBody
	if !decodeJSONBody(w, r, &body) {
		return
	}
	u, sess, err := s.Auth.Register(body.Username, body.Password)
	if err != nil {
		code := http.StatusBadRequest
		if err == db.ErrUserExists {
			code = http.StatusConflict
		}
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	s.Auth.SetSessionCookie(w, sess.ID)
	writeJSON(w, http.StatusCreated, map[string]any{
		"user":  userPublic(u),
		"token": sess.ID,
	})
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var body credBody
	if !decodeJSONBody(w, r, &body) {
		return
	}
	u, sess, err := s.Auth.Login(body.Username, body.Password, r.RemoteAddr)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	s.Auth.SetSessionCookie(w, sess.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"user":  userPublic(u),
		"token": sess.ID,
	})
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	token := auth.ExtractToken(r)
	_ = s.Auth.Logout(token)
	s.Auth.ClearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	u := auth.UserFrom(r.Context())
	if u == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}
	writeJSON(w, http.StatusOK, userPublic(u))
}

type passwordBody struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (s *Server) handleAuthPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	u := auth.UserFrom(r.Context())
	if u == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}
	var body passwordBody
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if err := s.Auth.ChangePassword(u.ID, body.CurrentPassword, body.NewPassword); err != nil {
		code := http.StatusBadRequest
		if strings.Contains(err.Error(), "incorrect") {
			code = http.StatusUnauthorized
		}
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func userPublic(u *db.User) map[string]any {
	return map[string]any{
		"id":       u.ID,
		"username": u.Username,
		"is_admin": u.IsAdmin,
	}
}

type createSessionBody struct {
	SeedTrackID int64 `json:"seed_track_id"`
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/sessions")
	path = strings.Trim(path, "/")
	if path == "" {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		auth.RequireUser(s.createSession)(w, r)
		return
	}

	parts := strings.Split(path, "/")
	sessionID := parts[0]
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			auth.RequireUser(func(w http.ResponseWriter, r *http.Request) {
				s.getSession(w, r, sessionID)
			})(w, r)
		case http.MethodDelete:
			auth.RequireUser(func(w http.ResponseWriter, r *http.Request) {
				s.stopSession(w, r, sessionID)
			})(w, r)
		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return
	}

	if parts[1] == "feedback" && r.Method == http.MethodPost {
		auth.RequireUser(func(w http.ResponseWriter, r *http.Request) {
			s.postFeedback(w, r, sessionID)
		})(w, r)
		return
	}

	if parts[1] == "queue" && r.Method == http.MethodPost {
		auth.RequireUser(func(w http.ResponseWriter, r *http.Request) {
			s.postQueueInject(w, r, sessionID)
		})(w, r)
		return
	}

	if parts[1] == "queue" && r.Method == http.MethodDelete {
		auth.RequireUser(func(w http.ResponseWriter, r *http.Request) {
			s.deleteQueueTrack(w, r, sessionID)
		})(w, r)
		return
	}

	if parts[1] == "back" && r.Method == http.MethodPost {
		auth.RequireUser(func(w http.ResponseWriter, r *http.Request) {
			s.postSessionBack(w, r, sessionID)
		})(w, r)
		return
	}

	if parts[1] == "hls" {
		auth.RequireUser(func(w http.ResponseWriter, r *http.Request) {
			s.serveHLS(w, r, sessionID, parts[2:])
		})(w, r)
		return
	}

	http.NotFound(w, r)
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	var body createSessionBody
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if body.SeedTrackID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "seed_track_id required"})
		return
	}
	live, err := s.Sessions.Create(r.Context(), u.ID, body.SeedTrackID)
	if err != nil {
		msg := err.Error()
		code := http.StatusBadRequest
		if strings.Contains(msg, "too many") {
			code = http.StatusServiceUnavailable
		}
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	writeJSON(w, http.StatusCreated, s.Sessions.ToStatus(live))
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	u := auth.UserFrom(r.Context())
	live, err := s.Sessions.Get(sessionID, u.ID)
	if err != nil {
		if err.Error() == "forbidden" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if live == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, s.Sessions.ToStatus(live))
}

func (s *Server) stopSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	u := auth.UserFrom(r.Context())
	live, err := s.Sessions.Get(sessionID, u.ID)
	if err != nil {
		if err.Error() == "forbidden" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if live == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	_ = s.Sessions.Stop(live)
	if s.HLS != nil {
		s.HLS.Stop(sessionID, true)
	}
	writeJSON(w, http.StatusOK, s.Sessions.ToStatus(live))
}

type feedbackBody struct {
	Signal  string `json:"signal"`
	TrackID int64  `json:"track_id"`
}

func (s *Server) postFeedback(w http.ResponseWriter, r *http.Request, sessionID string) {
	u := auth.UserFrom(r.Context())
	live, err := s.Sessions.Get(sessionID, u.ID)
	if err != nil {
		if err.Error() == "forbidden" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if live == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	var body feedbackBody
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if body.Signal == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "signal required"})
		return
	}
	if err := s.Sessions.ApplyFeedback(r.Context(), live, body.Signal, body.TrackID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.Sessions.ToStatus(live))
}

func (s *Server) postSessionBack(w http.ResponseWriter, r *http.Request, sessionID string) {
	u := auth.UserFrom(r.Context())
	live, err := s.Sessions.Get(sessionID, u.ID)
	if err != nil {
		if err.Error() == "forbidden" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if live == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	err = s.Sessions.Back(r.Context(), live)
	if err != nil {
		if errors.Is(err, session.ErrNoHistory) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "no history"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.Sessions.ToStatus(live))
}

type queueInjectBody struct {
	TrackID  int64  `json:"track_id"`
	Position string `json:"position"`
}

func (s *Server) postQueueInject(w http.ResponseWriter, r *http.Request, sessionID string) {
	u := auth.UserFrom(r.Context())
	live, err := s.Sessions.Get(sessionID, u.ID)
	if err != nil {
		if err.Error() == "forbidden" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if live == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	var body queueInjectBody
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if body.TrackID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "track_id required"})
		return
	}
	if err := s.Sessions.InjectQueue(r.Context(), live, body.TrackID, body.Position); err != nil {
		if session.IsQueueConflict(err) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.Sessions.ToStatus(live))
}

type queueRemoveBody struct {
	TrackID int64 `json:"track_id"`
}

func (s *Server) deleteQueueTrack(w http.ResponseWriter, r *http.Request, sessionID string) {
	u := auth.UserFrom(r.Context())
	live, err := s.Sessions.Get(sessionID, u.ID)
	if err != nil {
		if err.Error() == "forbidden" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if live == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	var body queueRemoveBody
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if body.TrackID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "track_id required"})
		return
	}
	if err := s.Sessions.RemoveFromQueue(r.Context(), live, body.TrackID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.Sessions.ToStatus(live))
}

func (s *Server) serveHLS(w http.ResponseWriter, r *http.Request, sessionID string, rest []string) {
	u := auth.UserFrom(r.Context())
	live, err := s.Sessions.Get(sessionID, u.ID)
	if err != nil {
		if err.Error() == "forbidden" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if live == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if s.HLS == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "hls unavailable"})
		return
	}
	name := "index.m3u8"
	if len(rest) > 0 && rest[0] != "" {
		name = filepath.Base(rest[0])
	}
	path := filepath.Join(s.HLS.SessionDir(sessionID), name)
	if _, err := os.Stat(path); err != nil {
		// Kick transcoding for current track if playlist missing.
		if name == "index.m3u8" {
			t, _ := s.DB.GetTrack(live.NowPlayingID)
			if t != nil {
				enc := stream.EncodeOpts{}
				if prefs, err := s.DB.GetStreamPrefs(live.UserID); err == nil {
					enc = stream.EncodeOpts{Format: prefs.StreamFormat, BitrateKbps: prefs.BitrateKbps}
				}
				_ = s.HLS.EnsureHLS(sessionID, t.Path, enc)
			}
		}
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(path); err == nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	if _, err := os.Stat(path); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "segment not ready"})
		return
	}
	if strings.HasSuffix(name, ".m3u8") {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	} else if strings.HasSuffix(name, ".ts") {
		w.Header().Set("Content-Type", "video/mp2t")
	}
	http.ServeFile(w, r, path)
}

func (s *Server) handleTrackStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	auth.RequireUser(func(w http.ResponseWriter, r *http.Request) {
		idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/tracks/")
		idStr = strings.TrimSuffix(idStr, "/stream")
		idStr = strings.Trim(idStr, "/")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid track id"})
			return
		}
		t, err := s.DB.GetTrack(id)
		if err != nil || t == nil || t.MissingAt.Valid {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "track not found"})
			return
		}
		abs, err := stream.ResolveMediaPath(s.Cfg.LibraryRoot, t.Path)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid media path"})
			return
		}
		u := auth.UserFrom(r.Context())
		prefs := db.DefaultStreamPrefs(0)
		if u != nil {
			if p, err := s.DB.GetStreamPrefs(u.ID); err == nil {
				prefs = p
			}
		}
		catalogFmt := ""
		if t.Format.Valid {
			catalogFmt = t.Format.String
		}
		enc := stream.EncodeOpts{Format: prefs.StreamFormat, BitrateKbps: prefs.BitrateKbps}
		if stream.NeedsTranscode(t.Path, catalogFmt, enc) {
			if s.HLS == nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{
					"error": "transcode unavailable (ffmpeg not configured)",
				})
				return
			}
			// Chrome's <audio> always sends open-ended "Range: bytes=0-" on load —
			// stream the full encode (Accept-Ranges: none). Closed probes like the
			// SPA's former "bytes=0-2047" warm-up must NOT start FFmpeg or rapid
			// skip piles up encodes and freezes the box.
			if stream.IsTranscodeRangeProbe(r.Header.Get("Range")) {
				_, _, ctype, _ := stream.ResolveEncodeTarget(enc)
				w.Header().Set("Content-Type", ctype)
				w.Header().Set("Accept-Ranges", "none")
				w.Header().Set("Cache-Control", "private, no-store")
				w.WriteHeader(http.StatusOK)
				return
			}
			if err := s.HLS.ServeProgressiveTranscode(r.Context(), w, abs, enc); err != nil {
				// Headers may already be sent; best-effort log via JSON only when possible.
				s.Log.Printf("track %d stream transcode: %v", id, err)
			}
			return
		}
		f, err := os.Open(abs)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error": "media file missing on disk",
			})
			return
		}
		defer f.Close()
		st, _ := f.Stat()
		ctype := "application/octet-stream"
		switch strings.ToLower(filepath.Ext(t.Path)) {
		case ".mp3":
			ctype = "audio/mpeg"
		case ".flac":
			ctype = "audio/flac"
		case ".ogg", ".opus":
			ctype = "audio/ogg"
		case ".m4a", ".aac":
			ctype = "audio/mp4"
		case ".wav":
			ctype = "audio/wav"
		}
		w.Header().Set("Content-Type", ctype)
		http.ServeContent(w, r, filepath.Base(t.Path), st.ModTime(), f)
	})(w, r)
}
