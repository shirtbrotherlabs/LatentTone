// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package web

import (
	"net/http"

	"github.com/shirtbrotherlabs/LatentTone/internal/config"
)

// handlePublicConfig serves GET /api/v1/config — runtime SPA settings (no auth).
func (s *Server) handlePublicConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	base := config.DefaultPublicBaseURL
	if s.Cfg != nil && s.Cfg.PublicBaseURL != "" {
		base = s.Cfg.PublicBaseURL
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"public_base_url": base,
	})
}
