// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-18

package db

import (
	"database/sql"
	"fmt"
	"time"
)

// DefaultScanIntervalSeconds is the product default (24h) for periodic library scans.
const DefaultScanIntervalSeconds = 86400

// MinScanIntervalSeconds rejects overly aggressive admin schedules.
const MinScanIntervalSeconds = 60

// ScanSchedule is the global admin-controlled periodic scan config.
type ScanSchedule struct {
	Enabled         bool   `json:"enabled"`
	IntervalSeconds int    `json:"interval_seconds"`
	UpdatedAt       string `json:"updated_at,omitempty"`
	// NextRunAt is filled by the HTTP layer when a schedule is active (RFC3339 UTC).
	NextRunAt string `json:"next_run_at,omitempty"`
	// Source describes where the effective values came from: "db" | "default" | "yaml_bootstrap".
	Source string `json:"source,omitempty"`
}

// DefaultScanSchedule returns product defaults (enabled, 24h).
func DefaultScanSchedule() ScanSchedule {
	return ScanSchedule{
		Enabled:         true,
		IntervalSeconds: DefaultScanIntervalSeconds,
		Source:          "default",
	}
}

// GetScanSchedule loads the persisted schedule or returns defaults when no row exists.
func (d *DB) GetScanSchedule() (ScanSchedule, error) {
	def := DefaultScanSchedule()
	if d == nil || d.SQL == nil {
		return def, nil
	}
	var enabled int
	var interval int
	var updated string
	err := d.SQL.QueryRow(`
SELECT enabled, interval_seconds, updated_at FROM scan_schedule WHERE id = 1`).Scan(&enabled, &interval, &updated)
	if err == sql.ErrNoRows {
		return def, nil
	}
	if err != nil {
		return def, err
	}
	out := ScanSchedule{
		Enabled:         enabled != 0,
		IntervalSeconds: interval,
		UpdatedAt:       updated,
		Source:          "db",
	}
	if out.IntervalSeconds < MinScanIntervalSeconds {
		out.IntervalSeconds = DefaultScanIntervalSeconds
	}
	return out, nil
}

// UpsertScanSchedule persists the global schedule (singleton id=1).
func (d *DB) UpsertScanSchedule(enabled bool, intervalSeconds int) (ScanSchedule, error) {
	if intervalSeconds < MinScanIntervalSeconds {
		return ScanSchedule{}, fmt.Errorf("interval_seconds must be >= %d", MinScanIntervalSeconds)
	}
	now := Now()
	_, err := d.SQL.Exec(`
INSERT INTO scan_schedule (id, enabled, interval_seconds, updated_at)
VALUES (1, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  enabled = VALUES(enabled),
  interval_seconds = VALUES(interval_seconds),
  updated_at = VALUES(updated_at)`,
		boolInt(enabled), intervalSeconds, now,
	)
	if err != nil {
		return ScanSchedule{}, err
	}
	return d.GetScanSchedule()
}

// EnsureScanScheduleRow seeds the singleton from bootstrapSeconds when missing.
// bootstrapSeconds ≤ 0 means use the product default (24h, enabled).
// yamlInterval of 0 with seedDisabled=true seeds enabled=false (stream-smoke).
func (d *DB) EnsureScanScheduleRow(bootstrapSeconds int, seedDisabled bool) error {
	var id int
	err := d.SQL.QueryRow(`SELECT id FROM scan_schedule WHERE id = 1`).Scan(&id)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	enabled := !seedDisabled
	interval := bootstrapSeconds
	if interval <= 0 {
		interval = DefaultScanIntervalSeconds
	}
	if interval < MinScanIntervalSeconds {
		interval = DefaultScanIntervalSeconds
	}
	_, err = d.SQL.Exec(`
INSERT INTO scan_schedule (id, enabled, interval_seconds, updated_at)
VALUES (1, ?, ?, ?)`,
		boolInt(enabled), interval, Now(),
	)
	return err
}

// Duration converts IntervalSeconds to a time.Duration.
func (s ScanSchedule) Duration() time.Duration {
	if s.IntervalSeconds < MinScanIntervalSeconds {
		return time.Duration(DefaultScanIntervalSeconds) * time.Second
	}
	return time.Duration(s.IntervalSeconds) * time.Second
}
