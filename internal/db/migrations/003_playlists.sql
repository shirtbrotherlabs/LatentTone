-- Copyright (C) 2026 martinsah
-- SPDX-License-Identifier: GPL-3.0-only
-- Neighbor playlists generated from seed-track k-NN (MariaDB).

CREATE TABLE IF NOT EXISTS playlists (
    id              BIGINT       NOT NULL AUTO_INCREMENT,
    name            VARCHAR(255) NOT NULL,
    seed_track_id   BIGINT       NOT NULL,
    kind            VARCHAR(32)  NOT NULL DEFAULT 'neighbor',
    length          INTEGER      NOT NULL,
    created_at      VARCHAR(32)  NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT fk_playlists_seed FOREIGN KEY (seed_track_id) REFERENCES tracks (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX IF NOT EXISTS idx_playlists_seed ON playlists (seed_track_id);
CREATE INDEX IF NOT EXISTS idx_playlists_created ON playlists (created_at);

CREATE TABLE IF NOT EXISTS playlist_tracks (
    playlist_id     BIGINT   NOT NULL,
    position        INTEGER  NOT NULL,
    track_id        BIGINT   NOT NULL,
    score           DOUBLE,
    PRIMARY KEY (playlist_id, position),
    CONSTRAINT fk_playlist_tracks_playlist FOREIGN KEY (playlist_id) REFERENCES playlists (id) ON DELETE CASCADE,
    CONSTRAINT fk_playlist_tracks_track FOREIGN KEY (track_id) REFERENCES tracks (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX IF NOT EXISTS idx_playlist_tracks_track ON playlist_tracks (track_id);
