// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package config

import "testing"

func TestNormalizePublicBaseURL(t *testing.T) {
	if got := NormalizePublicBaseURL(""); got != DefaultPublicBaseURL {
		t.Fatalf("empty: %q", got)
	}
	if got := NormalizePublicBaseURL("https://music.example.org/"); got != "https://music.example.org" {
		t.Fatalf("trim slash: %q", got)
	}
	if got := NormalizePublicBaseURL(" http://localhost:8080 "); got != "http://localhost:8080" {
		t.Fatalf("local: %q", got)
	}
}

func TestAbsoluteURL(t *testing.T) {
	c := &Config{PublicBaseURL: "https://music.example.org"}
	if got := c.AbsoluteURL("/covers/x.jpg"); got != "https://music.example.org/covers/x.jpg" {
		t.Fatalf("relative: %q", got)
	}
	if got := c.AbsoluteURL("https://cdn.example/a.png"); got != "https://cdn.example/a.png" {
		t.Fatalf("absolute: %q", got)
	}
}
