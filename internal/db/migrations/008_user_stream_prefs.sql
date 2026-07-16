-- Per-user progressive / HLS stream encode preferences (MariaDB).

CREATE TABLE IF NOT EXISTS user_stream_prefs (
    user_id       BIGINT      NOT NULL,
    stream_format VARCHAR(16) NOT NULL DEFAULT 'original',
    bitrate_kbps  INTEGER     NOT NULL DEFAULT 192,
    updated_at    VARCHAR(32) NOT NULL,
    PRIMARY KEY (user_id),
    CONSTRAINT fk_user_stream_prefs_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
