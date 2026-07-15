-- Phase 3: local users + opaque auth sessions (ADR-005)

CREATE TABLE users (
    id              INTEGER PRIMARY KEY,
    username        TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    password_hash   TEXT    NOT NULL,
    created_at      TEXT    NOT NULL,
    updated_at      TEXT    NOT NULL
);

CREATE TABLE auth_sessions (
    id              TEXT    PRIMARY KEY,
    user_id         INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at      TEXT    NOT NULL,
    expires_at      TEXT    NOT NULL,
    last_seen_at    TEXT    NOT NULL
);

CREATE INDEX idx_auth_sessions_user ON auth_sessions (user_id);
