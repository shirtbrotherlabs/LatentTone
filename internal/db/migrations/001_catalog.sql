-- Phase 1 catalog schema (MariaDB)
-- Case-insensitive name/title matching relies on the table default collation
-- (utf8mb4_unicode_ci) rather than SQLite's COLLATE NOCASE.

CREATE TABLE IF NOT EXISTS artists (
    id              BIGINT       NOT NULL AUTO_INCREMENT,
    name            VARCHAR(255) NOT NULL,
    name_sort       VARCHAR(255),
    mbid            VARCHAR(36),
    created_at      VARCHAR(32)  NOT NULL,
    updated_at      VARCHAR(32)  NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_artists_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE UNIQUE INDEX IF NOT EXISTS idx_artists_mbid ON artists (mbid);
CREATE INDEX IF NOT EXISTS idx_artists_name_sort ON artists (name_sort);

CREATE TABLE IF NOT EXISTS albums (
    id              BIGINT       NOT NULL AUTO_INCREMENT,
    artist_id       BIGINT,
    title           VARCHAR(255) NOT NULL,
    title_sort      VARCHAR(255),
    year            INTEGER,
    mbid            VARCHAR(36),
    cover_path      TEXT,
    created_at      VARCHAR(32)  NOT NULL,
    updated_at      VARCHAR(32)  NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT fk_albums_artist FOREIGN KEY (artist_id) REFERENCES artists (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE UNIQUE INDEX IF NOT EXISTS idx_albums_mbid ON albums (mbid);
CREATE INDEX IF NOT EXISTS idx_albums_artist ON albums (artist_id);
CREATE INDEX IF NOT EXISTS idx_albums_title ON albums (title);
CREATE UNIQUE INDEX IF NOT EXISTS idx_albums_artist_title ON albums (artist_id, title);

-- path is indexed for uniqueness; 700 chars * 4 bytes (utf8mb4) stays under the
-- 3072-byte InnoDB key-prefix limit (DYNAMIC row format, MariaDB default).
CREATE TABLE IF NOT EXISTS tracks (
    id              BIGINT        NOT NULL AUTO_INCREMENT,
    album_id        BIGINT,
    path            VARCHAR(700)  NOT NULL,
    path_hash       VARCHAR(64),
    file_mtime      BIGINT,
    file_size       BIGINT,
    title           VARCHAR(500)  NOT NULL,
    track_number    INTEGER,
    disc_number     INTEGER DEFAULT 1,
    duration_ms     BIGINT,
    bitrate_kbps    INTEGER,
    sample_rate_hz  INTEGER,
    channels        INTEGER,
    format          VARCHAR(16),
    year            INTEGER,
    comment         TEXT,
    mbid            VARCHAR(36),
    catalogued_at   VARCHAR(32)   NOT NULL,
    updated_at      VARCHAR(32)   NOT NULL,
    missing_at      VARCHAR(32),
    PRIMARY KEY (id),
    UNIQUE KEY uq_tracks_path (path),
    CONSTRAINT fk_tracks_album FOREIGN KEY (album_id) REFERENCES albums (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE UNIQUE INDEX IF NOT EXISTS idx_tracks_mbid ON tracks (mbid);
CREATE INDEX IF NOT EXISTS idx_tracks_album ON tracks (album_id);
CREATE INDEX IF NOT EXISTS idx_tracks_title ON tracks (title);
CREATE INDEX IF NOT EXISTS idx_tracks_missing ON tracks (missing_at);
CREATE INDEX IF NOT EXISTS idx_tracks_mtime ON tracks (file_mtime);

CREATE TABLE IF NOT EXISTS track_artists (
    track_id        BIGINT      NOT NULL,
    artist_id       BIGINT      NOT NULL,
    role            VARCHAR(32) NOT NULL DEFAULT 'primary',
    position        INTEGER     NOT NULL DEFAULT 0,
    PRIMARY KEY (track_id, artist_id, role),
    CONSTRAINT fk_track_artists_track FOREIGN KEY (track_id) REFERENCES tracks (id) ON DELETE CASCADE,
    CONSTRAINT fk_track_artists_artist FOREIGN KEY (artist_id) REFERENCES artists (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS genres (
    id              BIGINT       NOT NULL AUTO_INCREMENT,
    name            VARCHAR(255) NOT NULL,
    parent_id       BIGINT,
    PRIMARY KEY (id),
    UNIQUE KEY uq_genres_name (name),
    CONSTRAINT fk_genres_parent FOREIGN KEY (parent_id) REFERENCES genres (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS track_genres (
    track_id        BIGINT      NOT NULL,
    genre_id        BIGINT      NOT NULL,
    source          VARCHAR(32),
    PRIMARY KEY (track_id, genre_id),
    CONSTRAINT fk_track_genres_track FOREIGN KEY (track_id) REFERENCES tracks (id) ON DELETE CASCADE,
    CONSTRAINT fk_track_genres_genre FOREIGN KEY (genre_id) REFERENCES genres (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX IF NOT EXISTS idx_genres_parent ON genres (parent_id);

CREATE TABLE IF NOT EXISTS scan_runs (
    id              BIGINT       NOT NULL AUTO_INCREMENT,
    started_at      VARCHAR(32)  NOT NULL,
    finished_at     VARCHAR(32),
    `trigger`       VARCHAR(32)  NOT NULL,
    files_seen      INTEGER,
    files_upserted  INTEGER,
    files_missing   INTEGER,
    status          VARCHAR(32)  NOT NULL,
    error_message   TEXT,
    PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
