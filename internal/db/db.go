// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-16

package db

import (
	"database/sql"
	"embed"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// DB wraps the catalog MariaDB connection.
type DB struct {
	SQL     *sql.DB
	writeMu sync.Mutex
}

// Open connects to MariaDB at dsn, applies migrations, and returns the handle.
//
// dsn is a github.com/go-sql-driver/mysql DSN, e.g.
//
//	latenttone:password@tcp(mariadb:3306)/latenttone?parseTime=true&charset=utf8mb4&collation=utf8mb4_unicode_ci
//
// multiStatements and parseTime are forced on regardless of the caller-supplied
// DSN: migrations apply several DDL statements per file in one Exec, and the
// catalog reads timestamps as RFC3339 text (parseTime only matters if a caller
// later introduces native DATETIME columns).
func Open(dsn string) (*DB, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database_dsn: %w", err)
	}
	cfg.MultiStatements = true
	cfg.ParseTime = true
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	if _, ok := cfg.Params["charset"]; !ok {
		cfg.Params["charset"] = "utf8mb4"
	}

	sqlDB, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("open mariadb: %w", err)
	}
	// Modest pool: browse/scan/embed are separate Compose services/processes,
	// each with their own *DB; MariaDB (unlike SQLite) serializes writers at
	// the row level, so this is about connection reuse, not write contention.
	sqlDB.SetMaxOpenConns(16)
	sqlDB.SetMaxIdleConns(16)
	sqlDB.SetConnMaxLifetime(time.Hour)

	if err := pingWithRetry(sqlDB, 10, 500*time.Millisecond); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("connect mariadb: %w", err)
	}

	d := &DB{SQL: sqlDB}
	if err := d.migrate(); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return d, nil
}

// pingWithRetry tolerates MariaDB briefly refusing connections right after its
// healthcheck reports healthy (Compose depends_on races on cold start).
func pingWithRetry(sqlDB *sql.DB, attempts int, delay time.Duration) error {
	var err error
	for i := 0; i < attempts; i++ {
		if err = sqlDB.Ping(); err == nil {
			return nil
		}
		time.Sleep(delay)
	}
	return err
}

// Close closes the database.
func (d *DB) Close() error {
	if d == nil || d.SQL == nil {
		return nil
	}
	return d.SQL.Close()
}

func (d *DB) migrate() error {
	if _, err := d.SQL.Exec(`
CREATE TABLE IF NOT EXISTS schema_migrations (
    version BIGINT NOT NULL,
    applied_at VARCHAR(32) NOT NULL,
    PRIMARY KEY (version)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`); err != nil {
		return fmt.Errorf("schema_migrations: %w", err)
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		var version int
		if _, err := fmt.Sscanf(e.Name(), "%d_", &version); err != nil {
			return fmt.Errorf("migration name %s: %w", e.Name(), err)
		}
		var exists int
		err := d.SQL.QueryRow(`SELECT COUNT(1) FROM schema_migrations WHERE version = ?`, version).Scan(&exists)
		if err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		// Note: DDL in MariaDB/InnoDB implicitly commits as each statement runs,
		// so this transaction does not make a whole migration file atomic the
		// way SQLite's did. Every statement in these migrations is written to be
		// safely re-run (IF NOT EXISTS / IF EXISTS) so a partial failure followed
		// by a retry does not error on already-applied DDL within the same file.
		tx, err := d.SQL.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply %s: %w", e.Name(), err)
		}
		if _, err := tx.Exec(
			`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
			version, Now(),
		); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Now returns RFC3339 UTC timestamp text.
func Now() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// NullString returns sql.NullString for empty → NULL.
func NullString(s string) sql.NullString {
	s = strings.TrimSpace(s)
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// NullInt64 returns sql.NullInt64 for nil pointer.
func NullInt64(p *int64) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *p, Valid: true}
}

// NullInt returns sql.NullInt64 for nil int pointer.
func NullInt(p *int) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*p), Valid: true}
}
