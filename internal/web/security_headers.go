// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-16

package web

import (
	"net/http"
	"strings"
)

// defaultCSP is tuned for the same-origin /app SPA: self-hosted assets,
// Google Fonts, covers/streams, and Media/Web Audio blobs. HSTS stays at the
// reverse proxy — do not set Strict-Transport-Security here.
const defaultCSP = "" +
	"default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
	"font-src 'self' https://fonts.gstatic.com; " +
	"img-src 'self' data: blob:; " +
	"media-src 'self' blob:; " +
	"connect-src 'self'; " +
	"worker-src 'self' blob:; " +
	"frame-ancestors 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'"

// apiResponseWriter strips WWW-Authenticate on JSON API responses so a missing
// app session (403) or invalid login (401) does not re-trigger the browser's
// HTTP Basic Auth dialog when nginx also protects the origin.
type apiResponseWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (w *apiResponseWriter) WriteHeader(statusCode int) {
	if !w.wroteHeader {
		w.ResponseWriter.Header().Del("WWW-Authenticate")
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *apiResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

// withSecurityHeaders adds baseline browser security headers to every response.
func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Content-Security-Policy", defaultCSP)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w = &apiResponseWriter{ResponseWriter: w}
		}
		next.ServeHTTP(w, r)
	})
}
