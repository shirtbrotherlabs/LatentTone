// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package auth_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/shirtbrotherlabs/LatentTone/internal/auth"
	"github.com/shirtbrotherlabs/LatentTone/internal/config"
	"github.com/shirtbrotherlabs/LatentTone/internal/dbtest"
	"github.com/shirtbrotherlabs/LatentTone/internal/meta"
	"github.com/shirtbrotherlabs/LatentTone/internal/web"
)

func TestArgon2RoundTrip(t *testing.T) {
	h, err := auth.HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	ok, err := auth.VerifyPassword(h, "correct horse battery")
	if err != nil || !ok {
		t.Fatalf("verify want true: ok=%v err=%v", ok, err)
	}
	ok, err = auth.VerifyPassword(h, "wrong")
	if err != nil || ok {
		t.Fatalf("verify want false: ok=%v err=%v", ok, err)
	}
}

func TestAuthAPIAndBearer(t *testing.T) {
	dir := t.TempDir()
	catalog, dsn := dbtest.Open(t)

	cfg := &config.Config{
		LibraryRoot: dir,
		DatabaseDSN: dsn,
		ListenAddr:  ":0",
		AuthMode:    auth.AuthModeAuth,
		HLSRoot:     filepath.Join(dir, "hls"),
	}
	cfg.AuthMode = "authenticated"
	_ = cfg
	// apply defaults via Load-like fields
	mcfg := &meta.Config{LibraryRoot: dir, DatabaseDSN: cfg.DatabaseDSN}
	srv, err := web.New(cfg, mcfg, catalog, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	regBody := bytes.NewBufferString(`{"username":"alice","password":"secretpass"}`)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", regBody)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("register status %d body %s", rr.Code, rr.Body.String())
	}
	var reg map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &reg); err != nil {
		t.Fatal(err)
	}
	token, _ := reg["token"].(string)
	if token == "" {
		t.Fatal("missing token")
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("me status %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/sessions", bytes.NewBufferString(`{"seed_track_id":1}`))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("unauth session want 403 got %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/tracks/1/stream", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("unauth stream want 403 got %d", rr.Code)
	}
}
