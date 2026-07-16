-- Per-user Radio (endless affinity) listening preferences.

CREATE TABLE user_radio_prefs (
    user_id          INTEGER PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    radio_bridge     INTEGER NOT NULL DEFAULT 1,
    artist_cooldown  INTEGER NOT NULL DEFAULT 1,
    query_jitter     INTEGER NOT NULL DEFAULT 1,
    artist_penalty   INTEGER NOT NULL DEFAULT 1,
    bounded_random   INTEGER NOT NULL DEFAULT 1,
    jitter_alpha     REAL    NOT NULL DEFAULT 0.05,
    updated_at       TEXT    NOT NULL
);
