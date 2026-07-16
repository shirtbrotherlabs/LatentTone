// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-16

package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shirtbrotherlabs/LatentTone/internal/config"
)

func writeCfg(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "scanner.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSecureCookieInferHTTPS(t *testing.T) {
	t.Setenv("SECURE_COOKIE", "")
	t.Setenv("LATENTTONE_SECURE_COOKIE", "")
	path := writeCfg(t, `
library_root: /music
database_dsn: "u:p@tcp(localhost:3306)/db"
public_base_url: https://example.test
`)
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !c.SecureCookie {
		t.Fatal("expected SecureCookie true for https public_base_url")
	}
}

func TestSecureCookieYAMLOverrideFalse(t *testing.T) {
	t.Setenv("SECURE_COOKIE", "")
	t.Setenv("LATENTTONE_SECURE_COOKIE", "")
	path := writeCfg(t, `
library_root: /music
database_dsn: "u:p@tcp(localhost:3306)/db"
public_base_url: https://example.test
secure_cookie: false
`)
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.SecureCookie {
		t.Fatal("expected SecureCookie false from YAML override")
	}
}

func TestSecureCookieEnvOverridesYAML(t *testing.T) {
	t.Setenv("SECURE_COOKIE", "true")
	t.Setenv("LATENTTONE_SECURE_COOKIE", "")
	path := writeCfg(t, `
library_root: /music
database_dsn: "u:p@tcp(localhost:3306)/db"
public_base_url: http://localhost:8080
secure_cookie: false
`)
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !c.SecureCookie {
		t.Fatal("expected SecureCookie true from SECURE_COOKIE env")
	}
}
