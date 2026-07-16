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

func TestStreamPrefsAPI(t *testing.T) {
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
	token := register(t, h, "streamprefs")

	rr := doJSON(t, h, http.MethodGet, "/api/v1/me/stream-prefs", token, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET %d %s", rr.Code, rr.Body.String())
	}
	var prefs map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &prefs); err != nil {
		t.Fatal(err)
	}
	if prefs["stream_format"] != "original" {
		t.Fatalf("default format %#v", prefs["stream_format"])
	}

	rr = doJSON(t, h, http.MethodPatch, "/api/v1/me/stream-prefs", token,
		`{"stream_format":"mp3","bitrate_kbps":256}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("PATCH %d %s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &prefs); err != nil {
		t.Fatal(err)
	}
	if prefs["stream_format"] != "mp3" {
		t.Fatalf("patched format %#v", prefs["stream_format"])
	}
	if int(prefs["bitrate_kbps"].(float64)) != 256 {
		t.Fatalf("patched bitrate %#v", prefs["bitrate_kbps"])
	}

	rr = doJSON(t, h, http.MethodPatch, "/api/v1/me/stream-prefs", token,
		`{"stream_format":"flac"}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad format want 400 got %d", rr.Code)
	}

	rr = doJSON(t, h, http.MethodGet, "/api/v1/me/stream-prefs", "", "")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauth want 401 got %d", rr.Code)
	}
}
