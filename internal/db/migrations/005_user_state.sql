-- Phase 3: user-state tables (SQLITE_SCHEMA §3 + listening sessions)

CREATE TABLE track_feedback (
    id              INTEGER PRIMARY KEY,
    user_id         INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    track_id        INTEGER NOT NULL REFERENCES tracks (id) ON DELETE CASCADE,
    signal          TEXT    NOT NULL,
    session_id      TEXT,
    created_at      TEXT    NOT NULL,
    UNIQUE (user_id, track_id, signal, created_at)
);

CREATE INDEX idx_feedback_user_track ON track_feedback (user_id, track_id);

CREATE TABLE playback_events (
    id              INTEGER PRIMARY KEY,
    user_id         INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    track_id        INTEGER NOT NULL REFERENCES tracks (id) ON DELETE CASCADE,
    session_id      TEXT,
    started_at      TEXT    NOT NULL,
    ended_at        TEXT,
    listened_ms     INTEGER,
    completed       INTEGER NOT NULL DEFAULT 0,
    skipped         INTEGER NOT NULL DEFAULT 0,
    skip_within_ms  INTEGER
);

CREATE INDEX idx_playback_user ON playback_events (user_id, started_at);

CREATE TABLE user_track_affinity (
    user_id         INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    track_id        INTEGER NOT NULL REFERENCES tracks (id) ON DELETE CASCADE,
    score           REAL    NOT NULL,
    updated_at      TEXT    NOT NULL,
    PRIMARY KEY (user_id, track_id)
);

CREATE TABLE user_track_skips (
    user_id         INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    track_id        INTEGER NOT NULL REFERENCES tracks (id) ON DELETE CASCADE,
    scope           TEXT    NOT NULL DEFAULT 'library',
    session_key     TEXT    NOT NULL DEFAULT '',
    created_at      TEXT    NOT NULL,
    PRIMARY KEY (user_id, track_id, scope, session_key)
);

CREATE TABLE listening_sessions (
    id              TEXT    PRIMARY KEY,
    user_id         INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    seed_track_id   INTEGER REFERENCES tracks (id),
    status          TEXT    NOT NULL,
    now_playing_id  INTEGER REFERENCES tracks (id),
    queue_json      TEXT,
    last_feedback   TEXT,
    error_message   TEXT,
    created_at      TEXT    NOT NULL,
    updated_at      TEXT    NOT NULL
);

CREATE INDEX idx_listening_sessions_user ON listening_sessions (user_id, updated_at);
