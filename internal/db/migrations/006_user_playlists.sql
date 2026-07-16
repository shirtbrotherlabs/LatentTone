-- Copyright (C) 2026 martinsah
-- SPDX-License-Identifier: GPL-3.0-only
-- Phase 3C: user-owned playlists (kind=user) with ownership + nullable seed (MariaDB).
--
-- MariaDB supports in-place ALTER TABLE, so this skips SQLite's
-- rebuild-and-copy dance (playlist_tracks_bak / playlists_new) from the
-- original migration — existing rows are preserved automatically.

-- seed_track_id becomes optional for kind=user playlists; the original FK
-- (ON DELETE CASCADE) is replaced with ON DELETE SET NULL.
ALTER TABLE playlists DROP FOREIGN KEY fk_playlists_seed;
ALTER TABLE playlists MODIFY COLUMN seed_track_id BIGINT NULL;
ALTER TABLE playlists ADD CONSTRAINT fk_playlists_seed
  FOREIGN KEY (seed_track_id) REFERENCES tracks (id) ON DELETE SET NULL;

ALTER TABLE playlists ADD COLUMN IF NOT EXISTS user_id BIGINT NULL AFTER seed_track_id;
ALTER TABLE playlists ADD COLUMN IF NOT EXISTS updated_at VARCHAR(32) NULL AFTER created_at;
-- Backfill updated_at for pre-existing rows (idempotent; only touches NULLs).
UPDATE playlists SET updated_at = created_at WHERE updated_at IS NULL;
ALTER TABLE playlists MODIFY COLUMN updated_at VARCHAR(32) NOT NULL;
ALTER TABLE playlists MODIFY COLUMN length INTEGER NOT NULL DEFAULT 0;

ALTER TABLE playlists ADD CONSTRAINT fk_playlists_user
  FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE;
ALTER TABLE playlists ADD CONSTRAINT chk_playlists_user_kind
  CHECK (kind <> 'user' OR user_id IS NOT NULL);

CREATE INDEX IF NOT EXISTS idx_playlists_user_updated ON playlists (user_id, updated_at);
