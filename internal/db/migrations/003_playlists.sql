-- Copyright (C) 2026 martinsah
-- SPDX-License-Identifier: GPL-3.0-only
-- Neighbor playlists generated from seed-track k-NN.

CREATE TABLE IF NOT EXISTS playlists (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT    NOT NULL,
    seed_track_id   INTEGER NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    kind            TEXT    NOT NULL DEFAULT 'neighbor',
    length          INTEGER NOT NULL,
    created_at      TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_playlists_seed ON playlists(seed_track_id);
CREATE INDEX IF NOT EXISTS idx_playlists_created ON playlists(created_at);

CREATE TABLE IF NOT EXISTS playlist_tracks (
    playlist_id     INTEGER NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
    position        INTEGER NOT NULL,
    track_id        INTEGER NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    score           REAL,
    PRIMARY KEY (playlist_id, position)
);

CREATE INDEX IF NOT EXISTS idx_playlist_tracks_track ON playlist_tracks(track_id);
