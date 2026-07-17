// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"

	"github.com/shirtbrotherlabs/LatentTone/internal/db"
)

const (
	CookieName     = "lt_session"
	ctxUserKey     = ctxKey("user")
	ctxSessionKey  = ctxKey("authSession")
	AuthModeAuth   = "authenticated"
	AuthModeOpen   = "open"
)

type ctxKey string

// Params for argon2id.
type argonParams struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	saltLength  uint32
	keyLength   uint32
}

var defaultArgon = argonParams{
	memory:      64 * 1024,
	iterations:  3,
	parallelism: 2,
	saltLength:  16,
	keyLength:   32,
}

// Manager issues and validates opaque sessions.
type Manager struct {
	DB       *db.DB
	AuthMode string
	TTL      time.Duration
	Secure   bool // Secure cookie when TLS

	mu     sync.Mutex
	loginN map[string][]time.Time
}

// NewManager constructs an auth manager.
func NewManager(catalog *db.DB, authMode string, ttl time.Duration, secureCookie bool) *Manager {
	if authMode == "" {
		authMode = AuthModeAuth
	}
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	return &Manager{
		DB:       catalog,
		AuthMode: authMode,
		TTL:      ttl,
		Secure:   secureCookie,
		loginN:   make(map[string][]time.Time),
	}
}

// HashPassword returns an encoded argon2id hash.
func HashPassword(password string) (string, error) {
	p := defaultArgon
	salt := make([]byte, p.saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, p.iterations, p.memory, p.parallelism, p.keyLength)
	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.memory, p.iterations, p.parallelism, b64Salt, b64Hash), nil
}

// VerifyPassword checks password against encoded hash.
func VerifyPassword(encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	// "", "argon2id", "v=19", "m=...,t=...,p=...", salt, hash
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, fmt.Errorf("unsupported hash format")
	}
	var version int
	var memory, iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, err
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return false, err
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// NewOpaqueID returns a URL-safe random token.
func NewOpaqueID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Register creates a user and returns an issued session cookie value.
func (m *Manager) Register(username, password string) (*db.User, *db.AuthSession, error) {
	username = strings.TrimSpace(username)
	if len(username) < 2 || len(username) > 64 {
		return nil, nil, fmt.Errorf("username must be 2–64 characters")
	}
	if len(password) < 8 {
		return nil, nil, fmt.Errorf("password must be at least 8 characters")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, nil, err
	}
	u, err := m.DB.CreateUser(username, hash)
	if err != nil {
		return nil, nil, err
	}
	sess, err := m.issue(u.ID)
	return u, sess, err
}

// Login verifies credentials and issues a session.
func (m *Manager) Login(username, password, remoteKey string) (*db.User, *db.AuthSession, error) {
	if !m.allowLogin(remoteKey) {
		return nil, nil, fmt.Errorf("too many login attempts")
	}
	u, err := m.DB.GetUserByUsername(username)
	if err != nil {
		return nil, nil, err
	}
	if u == nil {
		return nil, nil, fmt.Errorf("invalid credentials")
	}
	ok, err := VerifyPassword(u.PasswordHash, password)
	if err != nil || !ok {
		return nil, nil, fmt.Errorf("invalid credentials")
	}
	sess, err := m.issue(u.ID)
	return u, sess, err
}

func (m *Manager) issue(userID int64) (*db.AuthSession, error) {
	id, err := NewOpaqueID()
	if err != nil {
		return nil, err
	}
	return m.DB.CreateAuthSession(id, userID, m.TTL)
}

// Logout deletes the opaque session.
func (m *Manager) Logout(token string) error {
	if token == "" {
		return nil
	}
	return m.DB.DeleteAuthSession(token)
}

// ResolveToken loads user for a valid opaque id.
func (m *Manager) ResolveToken(token string) (*db.User, *db.AuthSession, error) {
	if token == "" {
		return nil, nil, nil
	}
	sess, err := m.DB.GetAuthSession(token)
	if err != nil || sess == nil {
		return nil, nil, err
	}
	_ = m.DB.TouchAuthSession(token)
	u, err := m.DB.GetUserByID(sess.UserID)
	if err != nil || u == nil {
		return nil, nil, err
	}
	return u, sess, nil
}

// ExtractToken reads Bearer or cookie.
func ExtractToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	c, err := r.Cookie(CookieName)
	if err != nil || c == nil {
		return ""
	}
	return c.Value
}

// SetSessionCookie writes the HTTP-only session cookie.
func (m *Manager) SetSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   m.Secure,
		MaxAge:   int(m.TTL.Seconds()),
	})
}

// ClearSessionCookie expires the cookie.
func (m *Manager) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   m.Secure,
		MaxAge:   -1,
	})
}

// Middleware attaches user when authenticated; does not 401.
func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := ExtractToken(r)
		if m.AuthMode == AuthModeOpen && token == "" {
			u, err := m.DB.EnsureOpenModeUser()
			if err == nil && u != nil {
				ctx := context.WithValue(r.Context(), ctxUserKey, u)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}
		u, sess, err := m.ResolveToken(token)
		if err == nil && u != nil {
			ctx := context.WithValue(r.Context(), ctxUserKey, u)
			ctx = context.WithValue(ctx, ctxSessionKey, sess)
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

// RequireUser returns 403 unless a user is on the context (or open mode).
// Use 403 (not 401) for missing app sessions so browsers do not treat JSON API
// responses as HTTP Basic Auth challenges when nginx also protects the origin.
func RequireUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if UserFrom(r.Context()) == nil {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

// RequireAdmin returns 401 if unauthenticated and 403 if the user is not an admin.
func RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return RequireUser(func(w http.ResponseWriter, r *http.Request) {
		u := UserFrom(r.Context())
		if u == nil || !u.IsAdmin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin required"})
			return
		}
		next(w, r)
	})
}

// UserFrom returns the authenticated user or nil.
func UserFrom(ctx context.Context) *db.User {
	u, _ := ctx.Value(ctxUserKey).(*db.User)
	return u
}

// BootstrapAdmin creates the admin account when ADMIN_USERNAME / ADMIN_PASSWORD are set
// and no user with that username exists yet. Existing admins keep their password.
func BootstrapAdmin(catalog *db.DB, username, password string) error {
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if username == "" && password == "" {
		return nil
	}
	if username == "" || password == "" {
		return fmt.Errorf("ADMIN_USERNAME and ADMIN_PASSWORD must both be set (or both empty)")
	}
	if len(username) < 2 || len(username) > 64 {
		return fmt.Errorf("ADMIN_USERNAME must be 2–64 characters")
	}
	if len(password) < 8 {
		return fmt.Errorf("ADMIN_PASSWORD must be at least 8 characters")
	}
	existing, err := catalog.GetUserByUsername(username)
	if err != nil {
		return err
	}
	if existing != nil {
		if !existing.IsAdmin {
			return fmt.Errorf("user %q exists but is not admin; refuse to bootstrap", username)
		}
		return nil
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	_, err = catalog.CreateUserRole(username, hash, true)
	return err
}

// ChangePassword verifies the current password and stores a new hash.
func (m *Manager) ChangePassword(userID int64, currentPassword, newPassword string) error {
	if len(newPassword) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	u, err := m.DB.GetUserByID(userID)
	if err != nil {
		return err
	}
	if u == nil {
		return db.ErrNotFound
	}
	ok, err := VerifyPassword(u.PasswordHash, currentPassword)
	if err != nil || !ok {
		return fmt.Errorf("current password incorrect")
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	return m.DB.UpdateUserPasswordHash(userID, hash)
}

func (m *Manager) allowLogin(key string) bool {
	if key == "" {
		key = "unknown"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	cut := now.Add(-1 * time.Minute)
	var kept []time.Time
	for _, t := range m.loginN[key] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= 20 {
		m.loginN[key] = kept
		return false
	}
	kept = append(kept, now)
	m.loginN[key] = kept
	return true
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
