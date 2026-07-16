// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-16

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

func TestBootstrapAdminAndOpsACL(t *testing.T) {
	dir := t.TempDir()
	catalog, dsn := dbtest.Open(t)

	if err := auth.BootstrapAdmin(catalog, "admin", "adminpass1"); err != nil {
		t.Fatal(err)
	}
	// Idempotent — does not reset password.
	if err := auth.BootstrapAdmin(catalog, "admin", "otherpass999"); err != nil {
		t.Fatal(err)
	}
	u, err := catalog.GetUserByUsername("admin")
	if err != nil || u == nil || !u.IsAdmin {
		t.Fatalf("admin missing: %#v err=%v", u, err)
	}
	ok, _ := auth.VerifyPassword(u.PasswordHash, "adminpass1")
	if !ok {
		t.Fatal("password should not be reset on second bootstrap")
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

	// Non-admin register
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		bytes.NewBufferString(`{"username":"bob","password":"secretpass"}`))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("register %d %s", rr.Code, rr.Body.String())
	}
	var bobReg map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &bobReg)
	bobTok, _ := bobReg["token"].(string)
	bobUser, _ := bobReg["user"].(map[string]any)
	if bobUser["is_admin"] == true {
		t.Fatal("registered user must not be admin")
	}

	// Bob can read status, cannot start scan
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/scan/status", nil)
	req.Header.Set("Authorization", "Bearer "+bobTok)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("bob status %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/scan/start", nil)
	req.Header.Set("Authorization", "Bearer "+bobTok)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("bob start want 403 got %d", rr.Code)
	}

	// Admin login
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		bytes.NewBufferString(`{"username":"admin","password":"adminpass1"}`))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("admin login %d %s", rr.Code, rr.Body.String())
	}
	var adminReg map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &adminReg)
	adminTok, _ := adminReg["token"].(string)
	adminUser, _ := adminReg["user"].(map[string]any)
	if adminUser["is_admin"] != true {
		t.Fatal("admin is_admin flag missing")
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/scan/start", nil)
	req.Header.Set("Authorization", "Bearer "+adminTok)
	h.ServeHTTP(rr, req)
	// No scanner configured → 503, but not 403
	if rr.Code == http.StatusForbidden || rr.Code == http.StatusUnauthorized {
		t.Fatalf("admin start ACL failed: %d %s", rr.Code, rr.Body.String())
	}

	// Password change
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/password",
		bytes.NewBufferString(`{"current_password":"adminpass1","new_password":"newadminpass"}`))
	req.Header.Set("Authorization", "Bearer "+adminTok)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("password change %d %s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		bytes.NewBufferString(`{"username":"admin","password":"newadminpass"}`))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("login after change %d", rr.Code)
	}
}
