// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package web

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/shirtbrotherlabs/LatentTone/internal/auth"
	"github.com/shirtbrotherlabs/LatentTone/internal/db"
)

// handleMePlaylists dispatches /api/v1/me/playlists and nested paths (auth required).
func (s *Server) handleMePlaylists(w http.ResponseWriter, r *http.Request) {
	auth.RequireUser(s.dispatchMePlaylists)(w, r)
}

func (s *Server) dispatchMePlaylists(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	if u == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/me/playlists")
	path = strings.Trim(path, "/")

	if path == "" {
		switch r.Method {
		case http.MethodGet:
			s.meListPlaylists(w, r, u.ID)
		case http.MethodPost:
			s.meCreatePlaylist(w, r, u.ID)
		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return
	}

	if path == "from-neighbor" {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		s.meFromNeighbor(w, r, u.ID)
		return
	}

	parts := strings.Split(path, "/")
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			s.meGetPlaylist(w, r, u.ID, id)
		case http.MethodPatch:
			s.meRenamePlaylist(w, r, u.ID, id)
		case http.MethodDelete:
			s.meDeletePlaylist(w, r, u.ID, id)
		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		return
	}

	if parts[1] == "tracks" {
		s.mePlaylistTracks(w, r, u.ID, id, parts[2:])
		return
	}

	writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}

func (s *Server) meListPlaylists(w http.ResponseWriter, r *http.Request, userID int64) {
	list, err := s.DB.ListUserPlaylists(userID, 200)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(list))
	for i := range list {
		out = append(out, playlistHeaderJSON(&list[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"playlists": out})
}

type createUserPlaylistBody struct {
	Name string `json:"name"`
}

func (s *Server) meCreatePlaylist(w http.ResponseWriter, r *http.Request, userID int64) {
	var body createUserPlaylistBody
	if !decodeJSONBody(w, r, &body) {
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "name required")
		return
	}
	id, err := s.DB.CreatePlaylist(db.CreatePlaylistOpts{
		Name:   name,
		UserID: sql.NullInt64{Int64: userID, Valid: true},
		Kind:   db.PlaylistKindUser,
	})
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	pl, err := s.DB.GetUserPlaylist(id, userID)
	if err != nil || pl == nil {
		writeJSONError(w, http.StatusInternalServerError, "created but not found")
		return
	}
	s.writePlaylistJSON(w, r, http.StatusCreated, pl, nil)
}

func (s *Server) meGetPlaylist(w http.ResponseWriter, r *http.Request, userID, id int64) {
	pl, err := s.DB.GetUserPlaylist(id, userID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if pl == nil {
		writeJSONError(w, http.StatusNotFound, "playlist not found")
		return
	}
	entries, err := s.DB.ListPlaylistEntries(id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writePlaylistJSON(w, r, http.StatusOK, pl, entries)
}

type patchPlaylistBody struct {
	Name string `json:"name"`
}

func (s *Server) meRenamePlaylist(w http.ResponseWriter, r *http.Request, userID, id int64) {
	var body patchPlaylistBody
	if !decodeJSONBody(w, r, &body) {
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "name required")
		return
	}
	pl, err := s.DB.RenameUserPlaylist(id, userID, name)
	if err != nil {
		if errors.Is(err, db.ErrPlaylistNotFound) {
			writeJSONError(w, http.StatusNotFound, "playlist not found")
			return
		}
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	entries, err := s.DB.ListPlaylistEntries(id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writePlaylistJSON(w, r, http.StatusOK, pl, entries)
}

func (s *Server) meDeletePlaylist(w http.ResponseWriter, r *http.Request, userID, id int64) {
	if err := s.DB.DeleteUserPlaylist(id, userID); err != nil {
		if errors.Is(err, db.ErrPlaylistNotFound) {
			writeJSONError(w, http.StatusNotFound, "playlist not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type addTracksBody struct {
	TrackID  int64   `json:"track_id"`
	TrackIDs []int64 `json:"track_ids"`
}

type reorderBody struct {
	TrackIDs []int64 `json:"track_ids"`
}

type fromNeighborBody struct {
	PlaylistID int64  `json:"playlist_id"`
	Name       string `json:"name"`
}

func (s *Server) mePlaylistTracks(w http.ResponseWriter, r *http.Request, userID, playlistID int64, rest []string) {
	if len(rest) == 0 {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		s.meAddTracks(w, r, userID, playlistID)
		return
	}
	if len(rest) == 1 && rest[0] == "order" {
		if r.Method != http.MethodPut {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		s.meReorderTracks(w, r, userID, playlistID)
		return
	}
	if len(rest) == 1 {
		trackID, err := strconv.ParseInt(rest[0], 10, 64)
		if err != nil || trackID <= 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		if r.Method != http.MethodDelete {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		s.meRemoveTrack(w, r, userID, playlistID, trackID)
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}

func (s *Server) meAddTracks(w http.ResponseWriter, r *http.Request, userID, playlistID int64) {
	var body addTracksBody
	if !decodeJSONBody(w, r, &body) {
		return
	}
	ids := body.TrackIDs
	if body.TrackID > 0 {
		ids = append([]int64{body.TrackID}, ids...)
	}
	pl, err := s.DB.AddTracksToUserPlaylist(playlistID, userID, ids)
	if err != nil {
		if errors.Is(err, db.ErrPlaylistNotFound) {
			writeJSONError(w, http.StatusNotFound, "playlist not found")
			return
		}
		if errors.Is(err, db.ErrTrackNotFound) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	entries, err := s.DB.ListPlaylistEntries(playlistID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writePlaylistJSON(w, r, http.StatusOK, pl, entries)
}

func (s *Server) meRemoveTrack(w http.ResponseWriter, r *http.Request, userID, playlistID, trackID int64) {
	pl, err := s.DB.RemoveTrackFromUserPlaylist(playlistID, userID, trackID)
	if err != nil {
		if errors.Is(err, db.ErrPlaylistNotFound) {
			writeJSONError(w, http.StatusNotFound, "playlist not found")
			return
		}
		if errors.Is(err, db.ErrTrackNotFound) {
			writeJSONError(w, http.StatusNotFound, "track not in playlist")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	entries, err := s.DB.ListPlaylistEntries(playlistID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writePlaylistJSON(w, r, http.StatusOK, pl, entries)
}

func (s *Server) meReorderTracks(w http.ResponseWriter, r *http.Request, userID, playlistID int64) {
	var body reorderBody
	if !decodeJSONBody(w, r, &body) {
		return
	}
	pl, err := s.DB.ReorderUserPlaylist(playlistID, userID, body.TrackIDs)
	if err != nil {
		if errors.Is(err, db.ErrPlaylistNotFound) {
			writeJSONError(w, http.StatusNotFound, "playlist not found")
			return
		}
		if errors.Is(err, db.ErrInvalidOrder) {
			writeJSONError(w, http.StatusBadRequest, "track_ids must be a permutation of current membership")
			return
		}
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	entries, err := s.DB.ListPlaylistEntries(playlistID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writePlaylistJSON(w, r, http.StatusOK, pl, entries)
}

func (s *Server) meFromNeighbor(w http.ResponseWriter, r *http.Request, userID int64) {
	var body fromNeighborBody
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if body.PlaylistID <= 0 {
		writeJSONError(w, http.StatusBadRequest, "playlist_id required")
		return
	}
	id, err := s.DB.CopyNeighborToUserPlaylist(body.PlaylistID, userID, strings.TrimSpace(body.Name))
	if err != nil {
		if errors.Is(err, db.ErrPlaylistNotFound) {
			writeJSONError(w, http.StatusNotFound, "neighbor playlist not found")
			return
		}
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	pl, err := s.DB.GetUserPlaylist(id, userID)
	if err != nil || pl == nil {
		writeJSONError(w, http.StatusInternalServerError, "created but not found")
		return
	}
	entries, err := s.DB.ListPlaylistEntries(id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writePlaylistJSON(w, r, http.StatusCreated, pl, entries)
}
