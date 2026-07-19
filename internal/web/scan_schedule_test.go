// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-18

package web_test

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/shirtbrotherlabs/LatentTone/internal/auth"
	"github.com/shirtbrotherlabs/LatentTone/internal/config"
	"github.com/shirtbrotherlabs/LatentTone/internal/db"
	"github.com/shirtbrotherlabs/LatentTone/internal/dbtest"
	"github.com/shirtbrotherlabs/LatentTone/internal/meta"
	"github.com/shirtbrotherlabs/LatentTone/internal/web"
)

func TestScanScheduleAPI(t *testing.T) {
	dir := t.TempDir()
	catalog, dsn := dbtest.Open(t)
	if err := catalog.EnsureScanScheduleRow(db.DefaultScanIntervalSeconds, false); err != nil {
		t.Fatal(err)
	}
	if err := auth.BootstrapAdmin(catalog, "admin", "adminpass1"); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		LibraryRoot: dir,
		DatabaseDSN: dsn,
		ListenAddr:  ":0",
		AuthMode:    "authenticated",
		HLSRoot:     filepath.Join(dir, "hls"),
	}
	mcfg := &meta.Config{LibraryRoot: dir, DatabaseDSN: cfg.DatabaseDSN}
	srv, err := web.New(cfg, mcfg, catalog, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	bob := register(t, h, "bob")
	rr := doJSON(t, h, http.MethodGet, "/api/scan/schedule", bob, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("bob GET %d %s", rr.Code, rr.Body.String())
	}
	var sched map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &sched); err != nil {
		t.Fatal(err)
	}
	if sched["enabled"] != true {
		t.Fatalf("default enabled %#v", sched)
	}
	if int(sched["interval_seconds"].(float64)) != db.DefaultScanIntervalSeconds {
		t.Fatalf("default interval %#v", sched["interval_seconds"])
	}

	rr = doJSON(t, h, http.MethodPatch, "/api/scan/schedule", bob,
		`{"enabled":false}`)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("bob PATCH want 403 got %d", rr.Code)
	}

	// Admin login
	rr = doJSON(t, h, http.MethodPost, "/api/v1/auth/login", "",
		`{"username":"admin","password":"adminpass1"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("admin login %d %s", rr.Code, rr.Body.String())
	}
	var login map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &login)
	adminTok, _ := login["token"].(string)

	rr = doJSON(t, h, http.MethodPatch, "/api/scan/schedule", adminTok,
		`{"enabled":false,"interval_seconds":7200}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("admin PATCH %d %s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &sched); err != nil {
		t.Fatal(err)
	}
	if sched["enabled"] != false {
		t.Fatalf("patched enabled %#v", sched)
	}
	if int(sched["interval_seconds"].(float64)) != 7200 {
		t.Fatalf("patched interval %#v", sched["interval_seconds"])
	}

	rr = doJSON(t, h, http.MethodPatch, "/api/scan/schedule", adminTok,
		`{"interval_seconds":30}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("short interval want 400 got %d", rr.Code)
	}
}
