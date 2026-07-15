-- Phase 2: vector queue, feature payloads, embed runs

CREATE TABLE track_vectors (
    track_id            INTEGER PRIMARY KEY REFERENCES tracks (id) ON DELETE CASCADE,
    status              TEXT    NOT NULL,
    extractor_set       TEXT    NOT NULL,
    model_versions      TEXT,
    lancedb_id          TEXT,
    vector_dim          INTEGER,
    embedding_blob      BLOB,
    error_message       TEXT,
    audio_mtime_at_run  INTEGER,
    created_at          TEXT    NOT NULL,
    updated_at          TEXT    NOT NULL
);

CREATE INDEX idx_track_vectors_status ON track_vectors (status);

CREATE TABLE track_features (
    track_id        INTEGER NOT NULL REFERENCES tracks (id) ON DELETE CASCADE,
    extractor       TEXT    NOT NULL,
    model_version   TEXT    NOT NULL,
    features_json   TEXT    NOT NULL,
    vector_dim      INTEGER,
    created_at      TEXT    NOT NULL,
    updated_at      TEXT    NOT NULL,
    PRIMARY KEY (track_id, extractor)
);

CREATE TABLE embed_runs (
    id              INTEGER PRIMARY KEY,
    started_at      TEXT    NOT NULL,
    finished_at     TEXT,
    trigger         TEXT    NOT NULL,
    sample_mode     TEXT,
    max_tracks      INTEGER,
    tracks_claimed  INTEGER,
    tracks_ok       INTEGER,
    tracks_error    INTEGER,
    status          TEXT    NOT NULL,
    error_message   TEXT
);
