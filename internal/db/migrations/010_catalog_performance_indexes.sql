-- Catalog browse performance indexes.

CREATE INDEX IF NOT EXISTS idx_tracks_album_missing ON tracks(album_id, missing_at);
CREATE INDEX IF NOT EXISTS idx_playback_events_track ON playback_events(track_id);
