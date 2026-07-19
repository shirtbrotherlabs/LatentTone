// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-18

package web

import (
	"net/http"
	"sync"
	"time"

	"github.com/shirtbrotherlabs/LatentTone/internal/auth"
	"github.com/shirtbrotherlabs/LatentTone/internal/db"
)

// scheduleMu protects nextPeriodicAt (set by serve ticker / after scan).
var (
	scheduleMu     sync.Mutex
	nextPeriodicAt time.Time
)

// SetNextPeriodicScanAt records when the serve loop expects the next periodic scan.
func SetNextPeriodicScanAt(t time.Time) {
	scheduleMu.Lock()
	nextPeriodicAt = t
	scheduleMu.Unlock()
}

func nextPeriodicScanRFC3339() string {
	scheduleMu.Lock()
	t := nextPeriodicAt
	scheduleMu.Unlock()
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// handleAPIScanSchedule serves GET/PATCH /api/scan/schedule.
func (s *Server) handleAPIScanSchedule(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		auth.RequireUser(s.getScanSchedule)(w, r)
	case http.MethodPatch:
		auth.RequireAdmin(s.patchScanSchedule)(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (s *Server) getScanSchedule(w http.ResponseWriter, r *http.Request) {
	sched, err := s.DB.GetScanSchedule()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if sched.Enabled {
		sched.NextRunAt = nextPeriodicScanRFC3339()
	}
	writeJSON(w, http.StatusOK, sched)
}

type scanSchedulePatch struct {
	Enabled         *bool `json:"enabled"`
	IntervalSeconds *int  `json:"interval_seconds"`
}

func (s *Server) patchScanSchedule(w http.ResponseWriter, r *http.Request) {
	var body scanSchedulePatch
	if !decodeJSONBody(w, r, &body) {
		return
	}
	cur, err := s.DB.GetScanSchedule()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	enabled := cur.Enabled
	interval := cur.IntervalSeconds
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	if body.IntervalSeconds != nil {
		interval = *body.IntervalSeconds
	}
	if interval < db.MinScanIntervalSeconds {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "interval_seconds must be >= 60",
		})
		return
	}
	out, err := s.DB.UpsertScanSchedule(enabled, interval)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if out.Enabled {
		// Reset next-run hint to now+interval (ticker will refine).
		SetNextPeriodicScanAt(time.Now().UTC().Add(out.Duration()))
		out.NextRunAt = nextPeriodicScanRFC3339()
	} else {
		SetNextPeriodicScanAt(time.Time{})
		out.NextRunAt = ""
	}
	writeJSON(w, http.StatusOK, out)
}
