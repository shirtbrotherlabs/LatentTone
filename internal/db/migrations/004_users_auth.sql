-- Phase 3: local users + opaque auth sessions (ADR-005) (MariaDB)
-- Username case-insensitive matching relies on the table default collation
-- (utf8mb4_unicode_ci) rather than SQLite's COLLATE NOCASE.

CREATE TABLE IF NOT EXISTS users (
    id              BIGINT       NOT NULL AUTO_INCREMENT,
    username        VARCHAR(255) NOT NULL,
    password_hash   VARCHAR(255) NOT NULL,
    created_at      VARCHAR(32)  NOT NULL,
    updated_at      VARCHAR(32)  NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_users_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Session ids are 43-char base64url opaque tokens (auth.NewOpaqueID); VARCHAR(64)
-- leaves headroom.
CREATE TABLE IF NOT EXISTS auth_sessions (
    id              VARCHAR(64)  NOT NULL,
    user_id         BIGINT       NOT NULL,
    created_at      VARCHAR(32)  NOT NULL,
    expires_at      VARCHAR(32)  NOT NULL,
    last_seen_at    VARCHAR(32)  NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT fk_auth_sessions_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX IF NOT EXISTS idx_auth_sessions_user ON auth_sessions (user_id);
