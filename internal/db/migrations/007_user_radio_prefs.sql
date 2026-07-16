-- Per-user Radio (endless affinity) listening preferences (MariaDB).

CREATE TABLE IF NOT EXISTS user_radio_prefs (
    user_id          BIGINT   NOT NULL,
    radio_bridge     INTEGER  NOT NULL DEFAULT 1,
    artist_cooldown  INTEGER  NOT NULL DEFAULT 1,
    query_jitter     INTEGER  NOT NULL DEFAULT 1,
    artist_penalty   INTEGER  NOT NULL DEFAULT 1,
    bounded_random   INTEGER  NOT NULL DEFAULT 1,
    jitter_alpha     DOUBLE   NOT NULL DEFAULT 0.05,
    updated_at       VARCHAR(32) NOT NULL,
    PRIMARY KEY (user_id),
    CONSTRAINT fk_user_radio_prefs_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
