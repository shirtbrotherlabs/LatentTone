-- Copyright (C) 2026 martinsah
-- SPDX-License-Identifier: GPL-3.0-only
-- Phase 3C: user-owned playlists (kind=user) with ownership + nullable seed.

-- Preserve membership rows across playlists rebuild (seed_track_id → nullable).
CREATE TABLE playlist_tracks_bak AS SELECT * FROM playlist_tracks;

DROP TABLE playlist_tracks;

CREATE TABLE playlists_new (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT    NOT NULL,
    seed_track_id   INTEGER REFERENCES tracks(id) ON DELETE SET NULL,
    user_id         INTEGER REFERENCES users(id) ON DELETE CASCADE,
    kind            TEXT    NOT NULL DEFAULT 'neighbor',
    length          INTEGER NOT NULL DEFAULT 0,
    created_at      TEXT    NOT NULL,
    updated_at      TEXT    NOT NULL,
    CHECK (kind <> 'user' OR user_id IS NOT NULL)
);

INSERT INTO playlists_new (id, name, seed_track_id, user_id, kind, length, created_at, updated_at)
SELECT id, name, seed_track_id, NULL, kind, length, created_at, created_at FROM playlists;

DROP TABLE playlists;
ALTER TABLE playlists_new RENAME TO playlists;

CREATE TABLE playlist_tracks (
    playlist_id     INTEGER NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
    position        INTEGER NOT NULL,
    track_id        INTEGER NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    score           REAL,
    PRIMARY KEY (playlist_id, position)
);

INSERT INTO playlist_tracks (playlist_id, position, track_id, score)
SELECT playlist_id, position, track_id, score FROM playlist_tracks_bak;

DROP TABLE playlist_tracks_bak;

CREATE INDEX IF NOT EXISTS idx_playlists_seed ON playlists(seed_track_id);
CREATE INDEX IF NOT EXISTS idx_playlists_created ON playlists(created_at);
CREATE INDEX IF NOT EXISTS idx_playlists_user_updated ON playlists(user_id, updated_at);
CREATE INDEX IF NOT EXISTS idx_playlist_tracks_track ON playlist_tracks(track_id);
