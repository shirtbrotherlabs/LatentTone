// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package web

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// handleAPIScanStatus returns catalog-scan job state and library counts.
// GET /api/scan/status
func (s *Server) handleAPIScanStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	s.scanMu.Lock()
	running := s.scanning
	last := s.lastScan
	s.scanMu.Unlock()
	artists, albums, tracks, _ := s.DB.Counts()
	writeJSON(w, http.StatusOK, map[string]any{
		"running":   running,
		"last":      last,
		"artists":   artists,
		"albums":    albums,
		"tracks":    tracks,
		"stoppable": false,
	})
}

// handleAPIScanStart starts a full catalog/metadata library scan.
// POST /api/scan/start
// Note: catalog scan is not cancellable once started (no stop endpoint).
func (s *Server) handleAPIScanStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	if err := s.startScanJob(); err != nil {
		status := http.StatusConflict
		if s.Scanner == nil {
			status = http.StatusServiceUnavailable
		}
		writeJSONError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"running":   true,
		"stoppable": false,
	})
}

// handleAPIEmbedStart starts an acoustic-identity / embed job (JSON twin of /embed/start).
// POST /api/embed/start
func (s *Server) handleAPIEmbedStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	if s.Embed == nil || s.MetaCfg == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "embed not configured")
		return
	}
	ctx := s.ServeCtx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.Embed.Start(ctx, s.MetaCfg.ForWebStart(), s.DB, "api"); err != nil {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "running": true})
}

// handleAPIEmbedStop requests cancellation of a running embed job (JSON twin of /embed/stop).
// POST /api/embed/stop
func (s *Server) handleAPIEmbedStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	if s.Embed == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "embed not configured")
		return
	}
	_ = s.Embed.Stop()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "running": s.Embed.Running()})
}

func (s *Server) startScanJob() error {
	if s.Scanner == nil {
		return fmt.Errorf("scanner not configured")
	}
	s.scanMu.Lock()
	if s.scanning {
		s.scanMu.Unlock()
		return fmt.Errorf("scan already running")
	}
	s.scanning = true
	s.scanMu.Unlock()

	go func() {
		defer func() {
			s.scanMu.Lock()
			s.scanning = false
			s.scanMu.Unlock()
		}()
		res, err := s.Scanner.Full("api")
		s.scanMu.Lock()
		if err != nil {
			s.lastScan = fmt.Sprintf("error: %v", err)
		} else {
			s.lastScan = fmt.Sprintf("%s seen=%d upserted=%d missing=%d",
				time.Now().UTC().Format(time.RFC3339), res.Seen, res.Upserted, res.Missing)
		}
		s.scanMu.Unlock()
	}()
	return nil
}
