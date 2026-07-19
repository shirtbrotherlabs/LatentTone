// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15
// Last-Modified: 2026-07-18

package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/shirtbrotherlabs/LatentTone/internal/db"
	"github.com/shirtbrotherlabs/LatentTone/internal/meta"
	"github.com/shirtbrotherlabs/LatentTone/internal/scan"
)

// handleAPIScanStatus returns catalog-scan job state and library counts.
// GET /api/scan/status
func (s *Server) handleAPIScanStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	running := s.Scanner != nil && s.Scanner.Running()
	s.scanMu.Lock()
	last := s.lastScan
	s.scanMu.Unlock()
	// Prefer persisted scan_runs so startup/periodic scans show in Settings
	// (in-memory lastScan is only set for API-triggered jobs).
	if run, err := s.DB.LatestScanRun(); err == nil && run != nil {
		last = db.FormatScanRunLast(run)
		if run.Status == "running" {
			running = true
		}
	}
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
// Optional query force=1 (or JSON {"force":true}) re-extracts unchanged files.
// Note: catalog scan is not cancellable once started (no stop endpoint).
func (s *Server) handleAPIScanStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	force := false
	if v := strings.TrimSpace(r.URL.Query().Get("force")); v != "" {
		force, _ = strconv.ParseBool(v)
	}
	if r.Body != nil && r.ContentLength != 0 {
		var body struct {
			Force *bool `json:"force"`
		}
		data, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if len(data) > 0 && json.Unmarshal(data, &body) == nil && body.Force != nil {
			force = *body.Force
		}
	}
	if err := s.startScanJob(force); err != nil {
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
		"force":     force,
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

func (s *Server) startScanJob(force bool) error {
	if s.Scanner == nil {
		return fmt.Errorf("scanner not configured")
	}
	if s.Scanner.Running() {
		return fmt.Errorf("scan already running")
	}

	go func() {
		res, err := s.Scanner.FullOpts("api", scan.Options{Force: force})
		s.scanMu.Lock()
		if errors.Is(err, scan.ErrAlreadyRunning) {
			s.scanMu.Unlock()
			return
		}
		if err != nil {
			s.lastScan = fmt.Sprintf("error: %v", err)
			s.scanMu.Unlock()
			return
		}
		s.lastScan = fmt.Sprintf("%s seen=%d upserted=%d skipped=%d missing=%d",
			time.Now().UTC().Format(time.RFC3339), res.Seen, res.Upserted, res.Skipped, res.Missing)
		s.scanMu.Unlock()

		ctx := s.ServeCtx
		if ctx == nil {
			ctx = context.Background()
		}
		meta.StartIfIncomplete(ctx, s.Embed, s.MetaCfg, s.DB, "post-scan")
	}()
	return nil
}
