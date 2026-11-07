-- +goose Up
ALTER TABLE tracks ADD COLUMN audio_version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE tracks ADD COLUMN purge_error TEXT NOT NULL DEFAULT '';

CREATE TABLE pending_file_deletions (
    path TEXT PRIMARY KEY,
    size_bytes INTEGER NOT NULL CHECK(size_bytes >= 0),
    created_at TEXT NOT NULL,
    last_error TEXT NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE pending_file_deletions;
ALTER TABLE tracks DROP COLUMN purge_error;
ALTER TABLE tracks DROP COLUMN audio_version;
