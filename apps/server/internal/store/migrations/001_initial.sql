-- +goose Up
PRAGMA foreign_keys = ON;

CREATE TABLE users (id TEXT PRIMARY KEY, display_name TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE tracks (
    id TEXT PRIMARY KEY,
    owner_user_id TEXT NOT NULL REFERENCES users(id),
    title TEXT NOT NULL,
    artist TEXT NOT NULL DEFAULT '',
    album TEXT NOT NULL DEFAULT '',
    original_path TEXT NOT NULL,
    playable_path TEXT NOT NULL,
    content_type TEXT NOT NULL DEFAULT 'audio/mpeg',
    size_bytes INTEGER NOT NULL CHECK(size_bytes >= 0),
    duration_seconds REAL NOT NULL CHECK(duration_seconds > 0),
    base_random_weight REAL NOT NULL DEFAULT 1 CHECK(base_random_weight > 0),
    playback_start_seconds REAL NOT NULL DEFAULT 0 CHECK(playback_start_seconds >= 0),
    playback_end_seconds REAL,
    volume_ratio REAL NOT NULL DEFAULT 0.8 CHECK(volume_ratio >= 0 AND volume_ratio <= 1),
    lyrics TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT,
    purge_eligible_at TEXT
);
CREATE TABLE playlists (
    id TEXT PRIMARY KEY,
    owner_user_id TEXT NOT NULL REFERENCES users(id),
    name TEXT NOT NULL,
    is_default INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX playlists_one_default_per_user ON playlists(owner_user_id) WHERE is_default = 1;
CREATE TABLE playlist_items (
    id TEXT PRIMARY KEY,
    playlist_id TEXT NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
    track_id TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    position INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(playlist_id, track_id)
);
CREATE INDEX playlist_items_order ON playlist_items(playlist_id, position);
CREATE INDEX tracks_owner_active ON tracks(owner_user_id, deleted_at);

-- +goose Down
DROP TABLE playlist_items;
DROP TABLE playlists;
DROP TABLE tracks;
DROP TABLE users;
