// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-20

package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/shirtbrotherlabs/LatentTone/internal/auth"
	"github.com/shirtbrotherlabs/LatentTone/internal/db"
	"github.com/shirtbrotherlabs/LatentTone/internal/stream"
)

// handleTrackDownload serves GET /api/v1/tracks/{id}/download.
// Defaults to the caller's stream prefs; optional query: format, bitrate_kbps.
func (s *Server) handleTrackDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	auth.RequireUser(func(w http.ResponseWriter, r *http.Request) {
		idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/tracks/")
		idStr = strings.TrimSuffix(idStr, "/download")
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
		enc := stream.EncodeOpts{Format: prefs.StreamFormat, BitrateKbps: prefs.BitrateKbps}
		if q := strings.TrimSpace(r.URL.Query().Get("format")); q != "" {
			norm := db.NormalizeStreamFormat(q)
			if norm == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": "format must be original, mp3, aac, or opus",
				})
				return
			}
			enc.Format = norm
		}
		if q := strings.TrimSpace(r.URL.Query().Get("bitrate_kbps")); q != "" {
			br, err := strconv.Atoi(q)
			if err != nil || br < 32 || br > 512 {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": "bitrate_kbps must be an integer between 32 and 512",
				})
				return
			}
			enc.BitrateKbps = br
		}

		ext := stream.FileExtensionForEncode(t.Path, enc)
		filename := downloadFilename(t, ext)

		if stream.DownloadNeedsTranscode(enc) {
			if s.HLS == nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{
					"error": "transcode unavailable (ffmpeg not configured)",
				})
				return
			}
			if err := s.HLS.ServeDownloadTranscode(r.Context(), w, abs, enc, filename); err != nil {
				s.Log.Printf("track %d download transcode: %v", id, err)
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
		st, err := f.Stat()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "stat failed"})
			return
		}
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
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, strings.ReplaceAll(filename, `"`, `'`)))
		http.ServeContent(w, r, filename, st.ModTime(), f)
	})(w, r)
}

// downloadFilename builds "Artist - Title.ext" with filesystem-safe characters.
func downloadFilename(t *db.Track, ext string) string {
	artist := sanitizeFilenamePart(t.ArtistName)
	title := sanitizeFilenamePart(t.Title)
	base := ""
	switch {
	case artist != "" && title != "":
		base = artist + " - " + title
	case title != "":
		base = title
	case artist != "":
		base = artist
	default:
		base = fmt.Sprintf("track_%d", t.ID)
	}
	ext = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ext)), ".")
	if ext == "" {
		ext = "bin"
	}
	return base + "." + ext
}

func sanitizeFilenamePart(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		switch {
		case r < 32 || r == 127 || strings.ContainsRune(`<>:"/\|?*`, r):
			continue
		case unicode.IsSpace(r):
			if !prevSpace && b.Len() > 0 {
				b.WriteByte(' ')
				prevSpace = true
			}
		default:
			b.WriteRune(r)
			prevSpace = false
		}
	}
	out := strings.TrimSpace(b.String())
	out = strings.Trim(out, ".")
	if len(out) > 120 {
		out = strings.TrimSpace(out[:120])
	}
	return out
}
