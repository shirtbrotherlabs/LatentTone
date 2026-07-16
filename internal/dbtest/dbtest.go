// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-16

// Package dbtest provisions a throwaway MariaDB schema per test, mirroring
// the old "one temp SQLite file per test" isolation now that the catalog is
// backed by a shared MariaDB server. It requires a reachable MariaDB (see
// TEST_DATABASE_DSN below) — there is no more in-process/pure-Go fallback.
package dbtest

import (
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"

	"github.com/shirtbrotherlabs/LatentTone/internal/db"
)

// adminDSN returns a DSN with admin rights to create/drop test schemas.
// TEST_DATABASE_DSN (or DATABASE_DSN) takes precedence; its DBName is ignored
// (Open below always overrides it with a fresh per-test schema). Falls back
// to the local Compose dev convention (root / MARIADB_ROOT_PASSWORD on
// 127.0.0.1:3306) when unset.
func adminDSN() string {
	if v := strings.TrimSpace(os.Getenv("TEST_DATABASE_DSN")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("DATABASE_DSN")); v != "" {
		return v
	}
	pw := os.Getenv("MARIADB_ROOT_PASSWORD")
	if pw == "" {
		pw = "latenttone"
	}
	return fmt.Sprintf("root:%s@tcp(127.0.0.1:3306)/?parseTime=true&charset=utf8mb4&collation=utf8mb4_unicode_ci", pw)
}

// Open creates a uniquely-named MariaDB schema, opens it via db.Open (which
// applies migrations), and registers t.Cleanup to drop the schema and close
// the connection. Returns the catalog handle and the DSN used, so callers can
// wire the same DSN into config.Config / meta.Config test fixtures.
//
// Requires a reachable MariaDB server (see adminDSN); fails the test fast
// with a clear message otherwise rather than silently falling back to
// SQLite — there is no SQLite dialect support left in internal/db.
func Open(t testing.TB) (*db.DB, string) {
	t.Helper()

	base := adminDSN()
	cfg, err := mysql.ParseDSN(base)
	if err != nil {
		t.Fatalf("dbtest: parse admin dsn: %v", err)
	}

	adminCfg := *cfg
	adminCfg.DBName = ""
	adminDB, err := sql.Open("mysql", adminCfg.FormatDSN())
	if err != nil {
		t.Fatalf("dbtest: open admin conn: %v", err)
	}
	if err := adminDB.Ping(); err != nil {
		_ = adminDB.Close()
		t.Fatalf("dbtest: MariaDB not reachable at %s (set TEST_DATABASE_DSN, or run `docker compose up -d mariadb`): %v",
			adminCfg.Addr, err)
	}

	schema := fmt.Sprintf("latenttone_test_%d_%d", os.Getpid(), rand.Int63())
	if _, err := adminDB.Exec("CREATE DATABASE `" + schema + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		_ = adminDB.Close()
		t.Fatalf("dbtest: create schema %s: %v", schema, err)
	}

	testCfg := *cfg
	testCfg.DBName = schema
	dsn := testCfg.FormatDSN()

	catalog, err := db.Open(dsn)
	if err != nil {
		_, _ = adminDB.Exec("DROP DATABASE IF EXISTS `" + schema + "`")
		_ = adminDB.Close()
		t.Fatalf("dbtest: db.Open(%s): %v", schema, err)
	}

	t.Cleanup(func() {
		_ = catalog.Close()
		_, _ = adminDB.Exec("DROP DATABASE IF EXISTS `" + schema + "`")
		_ = adminDB.Close()
	})

	return catalog, dsn
}
