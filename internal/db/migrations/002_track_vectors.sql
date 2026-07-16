-- Phase 2: vector queue, feature payloads, embed runs (MariaDB)

CREATE TABLE IF NOT EXISTS track_vectors (
    track_id            BIGINT       NOT NULL,
    status              VARCHAR(32)  NOT NULL,
    extractor_set       VARCHAR(255) NOT NULL,
    model_versions      TEXT,
    lancedb_id          VARCHAR(64),
    vector_dim          INTEGER,
    embedding_blob      LONGBLOB,
    error_message       TEXT,
    audio_mtime_at_run  BIGINT,
    created_at          VARCHAR(32)  NOT NULL,
    updated_at          VARCHAR(32)  NOT NULL,
    PRIMARY KEY (track_id),
    CONSTRAINT fk_track_vectors_track FOREIGN KEY (track_id) REFERENCES tracks (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX IF NOT EXISTS idx_track_vectors_status ON track_vectors (status);

CREATE TABLE IF NOT EXISTS track_features (
    track_id        BIGINT       NOT NULL,
    extractor       VARCHAR(64)  NOT NULL,
    model_version   VARCHAR(64)  NOT NULL,
    features_json   LONGTEXT     NOT NULL,
    vector_dim      INTEGER,
    created_at      VARCHAR(32)  NOT NULL,
    updated_at      VARCHAR(32)  NOT NULL,
    PRIMARY KEY (track_id, extractor),
    CONSTRAINT fk_track_features_track FOREIGN KEY (track_id) REFERENCES tracks (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS embed_runs (
    id              BIGINT       NOT NULL AUTO_INCREMENT,
    started_at      VARCHAR(32)  NOT NULL,
    finished_at     VARCHAR(32),
    `trigger`       VARCHAR(32)  NOT NULL,
    sample_mode     VARCHAR(32),
    max_tracks      INTEGER,
    tracks_claimed  INTEGER,
    tracks_ok       INTEGER,
    tracks_error    INTEGER,
    status          VARCHAR(32)  NOT NULL,
    error_message   TEXT,
    PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
