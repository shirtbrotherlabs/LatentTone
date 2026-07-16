// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package api

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOpenAPIYAMLSkeleton(t *testing.T) {
	var doc map[string]any
	if err := yaml.Unmarshal(OpenAPIYAML, &doc); err != nil {
		t.Fatalf("parse openapi.yaml: %v", err)
	}
	ver, _ := doc["openapi"].(string)
	if !strings.HasPrefix(ver, "3.") {
		t.Fatalf("openapi version want 3.x, got %q", ver)
	}
	info, _ := doc["info"].(map[string]any)
	if info == nil || info["title"] == nil {
		t.Fatal("missing info.title")
	}
	components, _ := doc["components"].(map[string]any)
	schemes, _ := components["securitySchemes"].(map[string]any)
	if schemes["bearerAuth"] == nil {
		t.Fatal("missing components.securitySchemes.bearerAuth")
	}
	if schemes["cookieAuth"] == nil {
		t.Fatal("missing components.securitySchemes.cookieAuth")
	}
	cookie, _ := schemes["cookieAuth"].(map[string]any)
	if cookie["name"] != "lt_session" {
		t.Fatalf("cookieAuth.name want lt_session, got %#v", cookie["name"])
	}

	paths, _ := doc["paths"].(map[string]any)
	required := []string{
		"/api/v1/auth/register",
		"/api/v1/auth/login",
		"/api/v1/auth/logout",
		"/api/v1/auth/me",
		"/api/v1/sessions",
		"/api/v1/sessions/{id}",
		"/api/v1/sessions/{id}/feedback",
		"/api/v1/sessions/{id}/hls/index.m3u8",
		"/api/v1/sessions/{id}/hls/{segment}",
		"/api/v1/tracks/{id}/stream",
		"/api/v1/playlists",
		"/api/v1/playlists/{id}",
		"/api/v1/me/playlists",
		"/api/v1/me/playlists/{id}",
		"/api/v1/me/playlists/{id}/tracks",
		"/api/v1/me/playlists/{id}/tracks/order",
		"/api/v1/me/playlists/{id}/tracks/{track_id}",
		"/api/v1/me/playlists/from-neighbor",
		"/api/v1/me/radio-prefs",
		"/api/v1/me/stream-prefs",
		"/api/v1/me/stations",
		"/api/v1/config",
		"/api/scan/status",
		"/api/scan/start",
		"/api/embed/start",
		"/api/embed/stop",
		"/api/embed/status",
	}
	for _, p := range required {
		if paths[p] == nil {
			t.Errorf("missing path %s", p)
		}
	}
}
