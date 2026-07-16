// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-16

package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

const defaultJSONBodyLimit = 1 << 20 // 1 MiB

// decodeJSONBody reads a JSON object into dst with a 1 MiB MaxBytesReader limit
// and DisallowUnknownFields. On error it writes a JSON error response and
// returns false.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	return decodeJSONBodyLimit(w, r, dst, defaultJSONBodyLimit)
}

func decodeJSONBodyLimit(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) bool {
	if maxBytes <= 0 {
		maxBytes = defaultJSONBodyLimit
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
			return false
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return false
	}
	// Reject trailing top-level values.
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return false
	}
	return true
}
