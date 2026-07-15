// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shirtbrotherlabs/LatentTone/internal/auth"
	"github.com/shirtbrotherlabs/LatentTone/internal/config"
)

func TestAPIDocsFlagGating(t *testing.T) {
	authMgr := &auth.Manager{AuthMode: auth.AuthModeAuth}

	t.Run("off returns 404", func(t *testing.T) {
		s := &Server{
			Cfg:  &config.Config{EnableAPIDocs: false},
			Auth: authMgr,
		}
		h := s.Handler()
		for _, path := range []string{"/api/docs", "/api/docs/", "/api/openapi.yaml"} {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("%s: want 404 got %d", path, rec.Code)
			}
		}
	})

	t.Run("on serves docs and spec", func(t *testing.T) {
		s := &Server{
			Cfg:  &config.Config{EnableAPIDocs: true},
			Auth: authMgr,
		}
		h := s.Handler()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/openapi.yaml", nil)
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("openapi: want 200 got %d", rec.Code)
		}
		body, _ := io.ReadAll(rec.Body)
		if !strings.Contains(string(body), "openapi: 3.") {
			t.Fatalf("openapi body missing version: %s", truncate(body, 120))
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "/api/docs/", nil)
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("docs: want 200 got %d", rec.Code)
		}
	})
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
