// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-16

package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// User is a local account row.
type User struct {
	ID           int64
	Username     string
	PasswordHash string
	IsAdmin      bool
	CreatedAt    string
	UpdatedAt    string
}

// ErrUserExists is returned when username is taken.
var ErrUserExists = errors.New("username already exists")

// ErrNotFound is a generic missing-row error.
var ErrNotFound = errors.New("not found")

// CreateUser inserts a non-admin user; passwordHash must already be argon2id-encoded.
func (d *DB) CreateUser(username, passwordHash string) (*User, error) {
	return d.CreateUserRole(username, passwordHash, false)
}

// CreateUserRole inserts a user with an explicit admin flag.
func (d *DB) CreateUserRole(username, passwordHash string, isAdmin bool) (*User, error) {
	username = strings.TrimSpace(username)
	if username == "" || passwordHash == "" {
		return nil, fmt.Errorf("username and password_hash required")
	}
	now := Now()
	admin := 0
	if isAdmin {
		admin = 1
	}
	res, err := d.SQL.Exec(
		`INSERT INTO users (username, password_hash, is_admin, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		username, passwordHash, admin, now, now,
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, ErrUserExists
		}
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &User{
		ID: id, Username: username, PasswordHash: passwordHash,
		IsAdmin: isAdmin, CreatedAt: now, UpdatedAt: now,
	}, nil
}

// GetUserByUsername returns a user or nil.
func (d *DB) GetUserByUsername(username string) (*User, error) {
	row := d.SQL.QueryRow(
		`SELECT id, username, password_hash, COALESCE(is_admin, 0), created_at, updated_at
		 FROM users WHERE username = ? COLLATE NOCASE`,
		strings.TrimSpace(username),
	)
	return scanUser(row)
}

// GetUserByID returns a user or nil.
func (d *DB) GetUserByID(id int64) (*User, error) {
	row := d.SQL.QueryRow(
		`SELECT id, username, password_hash, COALESCE(is_admin, 0), created_at, updated_at
		 FROM users WHERE id = ?`, id,
	)
	return scanUser(row)
}

// UpdateUserPasswordHash sets a new argon2id hash for the user.
func (d *DB) UpdateUserPasswordHash(userID int64, passwordHash string) error {
	if userID <= 0 || passwordHash == "" {
		return fmt.Errorf("user id and password_hash required")
	}
	res, err := d.SQL.Exec(
		`UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`,
		passwordHash, Now(), userID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// CountUsers returns the number of registered users.
func (d *DB) CountUsers() (int64, error) {
	var n int64
	err := d.SQL.QueryRow(`SELECT COUNT(1) FROM users`).Scan(&n)
	return n, err
}

func scanUser(row scannable) (*User, error) {
	var u User
	var admin int
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &admin, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.IsAdmin = admin != 0
	return &u, nil
}

// AuthSession is an opaque login session.
type AuthSession struct {
	ID         string
	UserID     int64
	CreatedAt  string
	ExpiresAt  string
	LastSeenAt string
}

// CreateAuthSession stores a new opaque session.
func (d *DB) CreateAuthSession(id string, userID int64, ttl time.Duration) (*AuthSession, error) {
	if id == "" || userID == 0 {
		return nil, fmt.Errorf("id and user_id required")
	}
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	now := time.Now().UTC()
	created := now.Format(time.RFC3339)
	expires := now.Add(ttl).Format(time.RFC3339)
	_, err := d.SQL.Exec(
		`INSERT INTO auth_sessions (id, user_id, created_at, expires_at, last_seen_at) VALUES (?, ?, ?, ?, ?)`,
		id, userID, created, expires, created,
	)
	if err != nil {
		return nil, err
	}
	return &AuthSession{ID: id, UserID: userID, CreatedAt: created, ExpiresAt: expires, LastSeenAt: created}, nil
}

// GetAuthSession returns a non-expired session or nil.
func (d *DB) GetAuthSession(id string) (*AuthSession, error) {
	row := d.SQL.QueryRow(
		`SELECT id, user_id, created_at, expires_at, last_seen_at FROM auth_sessions WHERE id = ?`, id,
	)
	var s AuthSession
	err := row.Scan(&s.ID, &s.UserID, &s.CreatedAt, &s.ExpiresAt, &s.LastSeenAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	exp, err := time.Parse(time.RFC3339, s.ExpiresAt)
	if err != nil || time.Now().UTC().After(exp) {
		_ = d.DeleteAuthSession(id)
		return nil, nil
	}
	return &s, nil
}

// TouchAuthSession updates last_seen_at.
func (d *DB) TouchAuthSession(id string) error {
	_, err := d.SQL.Exec(`UPDATE auth_sessions SET last_seen_at = ? WHERE id = ?`, Now(), id)
	return err
}

// DeleteAuthSession removes a session row.
func (d *DB) DeleteAuthSession(id string) error {
	_, err := d.SQL.Exec(`DELETE FROM auth_sessions WHERE id = ?`, id)
	return err
}

// EnsureOpenModeUser creates or returns a synthetic solo-dev user for auth_mode=open.
func (d *DB) EnsureOpenModeUser() (*User, error) {
	const name = "open-mode"
	u, err := d.GetUserByUsername(name)
	if err != nil {
		return nil, err
	}
	if u != nil {
		return u, nil
	}
	// Placeholder hash — open mode never checks passwords.
	return d.CreateUser(name, "open-mode-no-password")
}
