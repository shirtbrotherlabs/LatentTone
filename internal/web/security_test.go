// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-16

package web_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shirtbrotherlabs/LatentTone/internal/auth"
	"github.com/shirtbrotherlabs/LatentTone/internal/config"
	"github.com/shirtbrotherlabs/LatentTone/internal/dbtest"
	"github.com/shirtbrotherlabs/LatentTone/internal/meta"
	"github.com/shirtbrotherlabs/LatentTone/internal/web"
)

func testHandler(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	catalog, dsn := dbtest.Open(t)
	cfg := &config.Config{
		LibraryRoot: dir,
		DatabaseDSN: dsn,
		ListenAddr:  ":0",
		AuthMode:    auth.AuthModeAuth,
		HLSRoot:     filepath.Join(dir, "hls"),
	}
	mcfg := &meta.Config{LibraryRoot: dir, DatabaseDSN: cfg.DatabaseDSN}
	srv, err := web.New(cfg, mcfg, catalog, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return srv.Handler()
}

func TestSecurityHeadersOnConfig(t *testing.T) {
	h := testHandler(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	if got := rr.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options: %q", got)
	}
	if got := rr.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options: %q", got)
	}
	if got := rr.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Fatalf("Referrer-Policy: %q", got)
	}
	csp := rr.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'self'") {
		t.Fatalf("CSP missing default-src: %q", csp)
	}
	if rr.Header().Get("Strict-Transport-Security") != "" {
		t.Fatal("app must not set HSTS (proxy owns TLS)")
	}
}

func TestDisallowUnknownJSONFields(t *testing.T) {
	h := testHandler(t)
	rr := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"username":"bob","password":"secretpass","extra":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", body)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["error"] != "invalid json" {
		t.Fatalf("error: %#v", out["error"])
	}
}

func TestCookieAuthWithoutBearer(t *testing.T) {
	h := testHandler(t)
	rr := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"username":"carol","password":"secretpass"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", body)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("register %d %s", rr.Code, rr.Body.String())
	}
	cookies := rr.Result().Cookies()
	var session *http.Cookie
	for _, c := range cookies {
		if c.Name == auth.CookieName {
			session = c
			break
		}
	}
	if session == nil {
		t.Fatal("missing lt_session cookie")
	}
	if !session.HttpOnly {
		t.Fatal("cookie should be HttpOnly")
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.AddCookie(session)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("me via cookie %d %s", rr.Code, rr.Body.String())
	}
}
