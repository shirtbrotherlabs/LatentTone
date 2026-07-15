-- Phase 1 catalog schema

PRAGMA foreign_keys = ON;

CREATE TABLE artists (
    id              INTEGER PRIMARY KEY,
    name            TEXT    NOT NULL,
    name_sort       TEXT,
    mbid            TEXT,
    created_at      TEXT    NOT NULL,
    updated_at      TEXT    NOT NULL,
    UNIQUE (name COLLATE NOCASE)
);

CREATE UNIQUE INDEX idx_artists_mbid ON artists (mbid) WHERE mbid IS NOT NULL AND mbid != '';
CREATE INDEX idx_artists_name_sort ON artists (name_sort);

CREATE TABLE albums (
    id              INTEGER PRIMARY KEY,
    artist_id       INTEGER REFERENCES artists (id),
    title           TEXT    NOT NULL,
    title_sort      TEXT,
    year            INTEGER,
    mbid            TEXT,
    cover_path      TEXT,
    created_at      TEXT    NOT NULL,
    updated_at      TEXT    NOT NULL
);

CREATE UNIQUE INDEX idx_albums_mbid ON albums (mbid) WHERE mbid IS NOT NULL AND mbid != '';
CREATE INDEX idx_albums_artist ON albums (artist_id);
CREATE INDEX idx_albums_title ON albums (title COLLATE NOCASE);
CREATE UNIQUE INDEX idx_albums_artist_title ON albums (artist_id, title COLLATE NOCASE);

CREATE TABLE tracks (
    id              INTEGER PRIMARY KEY,
    album_id        INTEGER REFERENCES albums (id),
    path            TEXT    NOT NULL,
    path_hash       TEXT,
    file_mtime      INTEGER,
    file_size       INTEGER,
    title           TEXT    NOT NULL,
    track_number    INTEGER,
    disc_number     INTEGER DEFAULT 1,
    duration_ms     INTEGER,
    bitrate_kbps    INTEGER,
    sample_rate_hz  INTEGER,
    channels        INTEGER,
    format          TEXT,
    year            INTEGER,
    comment         TEXT,
    mbid            TEXT,
    catalogued_at   TEXT    NOT NULL,
    updated_at      TEXT    NOT NULL,
    missing_at      TEXT,
    UNIQUE (path)
);

CREATE UNIQUE INDEX idx_tracks_mbid ON tracks (mbid) WHERE mbid IS NOT NULL AND mbid != '';
CREATE INDEX idx_tracks_album ON tracks (album_id);
CREATE INDEX idx_tracks_title ON tracks (title COLLATE NOCASE);
CREATE INDEX idx_tracks_missing ON tracks (missing_at);
CREATE INDEX idx_tracks_mtime ON tracks (file_mtime);

CREATE TABLE track_artists (
    track_id        INTEGER NOT NULL REFERENCES tracks (id) ON DELETE CASCADE,
    artist_id       INTEGER NOT NULL REFERENCES artists (id) ON DELETE CASCADE,
    role            TEXT    NOT NULL DEFAULT 'primary',
    position        INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (track_id, artist_id, role)
);

CREATE TABLE genres (
    id              INTEGER PRIMARY KEY,
    name            TEXT    NOT NULL,
    parent_id       INTEGER REFERENCES genres (id),
    UNIQUE (name COLLATE NOCASE)
);

CREATE TABLE track_genres (
    track_id        INTEGER NOT NULL REFERENCES tracks (id) ON DELETE CASCADE,
    genre_id        INTEGER NOT NULL REFERENCES genres (id) ON DELETE CASCADE,
    source          TEXT,
    PRIMARY KEY (track_id, genre_id)
);

CREATE INDEX idx_genres_parent ON genres (parent_id);

CREATE TABLE scan_runs (
    id              INTEGER PRIMARY KEY,
    started_at      TEXT    NOT NULL,
    finished_at     TEXT,
    trigger         TEXT    NOT NULL,
    files_seen      INTEGER,
    files_upserted  INTEGER,
    files_missing   INTEGER,
    status          TEXT    NOT NULL,
    error_message   TEXT
);
