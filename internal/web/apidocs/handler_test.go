// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package apidocs

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegisterServesDocsAndSpec(t *testing.T) {
	mux := http.NewServeMux()
	spec := []byte("openapi: 3.0.3\ninfo:\n  title: t\n  version: 0\npaths: {}\n")
	Register(mux, spec)

	t.Run("openapi yaml", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/openapi.yaml", nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status want 200 got %d", rec.Code)
		}
		ct := rec.Header().Get("Content-Type")
		if !strings.Contains(ct, "yaml") {
			t.Fatalf("content-type want yaml, got %q", ct)
		}
		body, _ := io.ReadAll(rec.Body)
		if !strings.Contains(string(body), "openapi: 3.0.3") {
			t.Fatalf("body missing openapi marker: %q", body)
		}
	})

	t.Run("docs redirect", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/docs", nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusFound {
			t.Fatalf("status want 302 got %d", rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/api/docs/" {
			t.Fatalf("location want /api/docs/ got %q", loc)
		}
	})

	t.Run("docs index", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/docs/", nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status want 200 got %d", rec.Code)
		}
		body, _ := io.ReadAll(rec.Body)
		if !strings.Contains(string(body), "/api/openapi.yaml") {
			t.Fatalf("index missing spec url: %s", body)
		}
		if !strings.Contains(string(body), "swagger-ui-bundle.js") {
			t.Fatalf("index missing swagger bundle: %s", body)
		}
	})

	t.Run("static css", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/docs/swagger-ui.css", nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status want 200 got %d body %s", rec.Code, rec.Body.String())
		}
		if rec.Body.Len() < 1000 {
			t.Fatalf("css unexpectedly small: %d bytes", rec.Body.Len())
		}
	})
}

func TestDocsNotRegisteredWhenOmitted(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/docs/", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status want 404 got %d", rec.Code)
	}
}
