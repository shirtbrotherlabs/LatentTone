// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

// Package apidocs serves flag-gated Swagger UI and the OpenAPI artifact.
//
// Static Swagger UI assets are vendored from swagger-api/swagger-ui v5.32.8
// (Apache-2.0); see LICENSE/NOTICE in this directory and docs/DEPENDENCIES.md.
package apidocs

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed index.html
var indexHTML []byte

//go:embed static/*
var staticRoot embed.FS

// Register mounts Swagger UI at /api/docs and the OpenAPI YAML at /api/openapi.yaml.
// Call only when enable_api_docs is true; when omitted, those paths 404.
func Register(mux *http.ServeMux, openAPIYAML []byte) {
	sub, err := fs.Sub(staticRoot, "static")
	if err != nil {
		panic("apidocs: static subfs: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(sub))

	mux.HandleFunc("/api/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write(openAPIYAML)
	})

	mux.HandleFunc("/api/docs", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/api/docs/", http.StatusFound)
	})

	mux.HandleFunc("/api/docs/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		rel := strings.TrimPrefix(r.URL.Path, "/api/docs/")
		if rel == "" || rel == "index.html" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusOK)
				return
			}
			_, _ = w.Write(indexHTML)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/" + rel
		fileServer.ServeHTTP(w, r2)
	})
}
