// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package web_test

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/shirtbrotherlabs/LatentTone/internal/config"
	"github.com/shirtbrotherlabs/LatentTone/internal/dbtest"
	"github.com/shirtbrotherlabs/LatentTone/internal/meta"
	"github.com/shirtbrotherlabs/LatentTone/internal/web"
)

func TestRadioPrefsAPI(t *testing.T) {
	dir := t.TempDir()
	catalog, dsn := dbtest.Open(t)
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
	token := register(t, h, "radiouser")

	rr := doJSON(t, h, http.MethodGet, "/api/v1/me/radio-prefs", token, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET %d %s", rr.Code, rr.Body.String())
	}
	var prefs map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &prefs); err != nil {
		t.Fatal(err)
	}
	if prefs["radio_bridge"] != true {
		t.Fatalf("default bridge %#v", prefs["radio_bridge"])
	}

	rr = doJSON(t, h, http.MethodPatch, "/api/v1/me/radio-prefs", token,
		`{"radio_bridge":false,"jitter_alpha":0.2}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("PATCH %d %s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &prefs); err != nil {
		t.Fatal(err)
	}
	if prefs["radio_bridge"] != false {
		t.Fatalf("patched bridge %#v", prefs["radio_bridge"])
	}
	if prefs["jitter_alpha"] != 0.2 {
		t.Fatalf("patched alpha %#v", prefs["jitter_alpha"])
	}

	rr = doJSON(t, h, http.MethodGet, "/api/v1/me/radio-prefs", "", "")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("unauth want 403 got %d", rr.Code)
	}
}
