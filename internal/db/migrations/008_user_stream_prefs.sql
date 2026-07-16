-- Per-user progressive / HLS stream encode preferences.

CREATE TABLE user_stream_prefs (
    user_id       INTEGER PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    stream_format TEXT    NOT NULL DEFAULT 'original',
    bitrate_kbps  INTEGER NOT NULL DEFAULT 192,
    updated_at    TEXT    NOT NULL
);
