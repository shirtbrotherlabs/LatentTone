-- Phase 3: user-state tables (SQLITE_SCHEMA §3 + listening sessions) (MariaDB)

CREATE TABLE IF NOT EXISTS track_feedback (
    id              BIGINT       NOT NULL AUTO_INCREMENT,
    user_id         BIGINT       NOT NULL,
    track_id        BIGINT       NOT NULL,
    `signal`        VARCHAR(32)  NOT NULL,
    session_id      VARCHAR(64),
    created_at      VARCHAR(32)  NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_track_feedback (user_id, track_id, `signal`, created_at),
    CONSTRAINT fk_track_feedback_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT fk_track_feedback_track FOREIGN KEY (track_id) REFERENCES tracks (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX IF NOT EXISTS idx_feedback_user_track ON track_feedback (user_id, track_id);

CREATE TABLE IF NOT EXISTS playback_events (
    id              BIGINT       NOT NULL AUTO_INCREMENT,
    user_id         BIGINT       NOT NULL,
    track_id        BIGINT       NOT NULL,
    session_id      VARCHAR(64),
    started_at      VARCHAR(32)  NOT NULL,
    ended_at        VARCHAR(32),
    listened_ms     BIGINT,
    completed       INTEGER      NOT NULL DEFAULT 0,
    skipped         INTEGER      NOT NULL DEFAULT 0,
    skip_within_ms  BIGINT,
    PRIMARY KEY (id),
    CONSTRAINT fk_playback_events_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT fk_playback_events_track FOREIGN KEY (track_id) REFERENCES tracks (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX IF NOT EXISTS idx_playback_user ON playback_events (user_id, started_at);

CREATE TABLE IF NOT EXISTS user_track_affinity (
    user_id         BIGINT      NOT NULL,
    track_id        BIGINT      NOT NULL,
    score           DOUBLE      NOT NULL,
    updated_at      VARCHAR(32) NOT NULL,
    PRIMARY KEY (user_id, track_id),
    CONSTRAINT fk_user_track_affinity_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT fk_user_track_affinity_track FOREIGN KEY (track_id) REFERENCES tracks (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS user_track_skips (
    user_id         BIGINT      NOT NULL,
    track_id        BIGINT      NOT NULL,
    scope           VARCHAR(32) NOT NULL DEFAULT 'library',
    session_key     VARCHAR(64) NOT NULL DEFAULT '',
    created_at      VARCHAR(32) NOT NULL,
    PRIMARY KEY (user_id, track_id, scope, session_key),
    CONSTRAINT fk_user_track_skips_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT fk_user_track_skips_track FOREIGN KEY (track_id) REFERENCES tracks (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Session ids are opaque tokens minted by internal/session (crypto/rand based);
-- VARCHAR(64) leaves headroom over the current ~36-43 char formats.
CREATE TABLE IF NOT EXISTS listening_sessions (
    id              VARCHAR(64)  NOT NULL,
    user_id         BIGINT       NOT NULL,
    seed_track_id   BIGINT,
    status          VARCHAR(32)  NOT NULL,
    now_playing_id  BIGINT,
    queue_json      LONGTEXT,
    last_feedback   LONGTEXT,
    error_message   TEXT,
    created_at      VARCHAR(32)  NOT NULL,
    updated_at      VARCHAR(32)  NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT fk_listening_sessions_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT fk_listening_sessions_seed FOREIGN KEY (seed_track_id) REFERENCES tracks (id),
    CONSTRAINT fk_listening_sessions_now_playing FOREIGN KEY (now_playing_id) REFERENCES tracks (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX IF NOT EXISTS idx_listening_sessions_user ON listening_sessions (user_id, updated_at);
