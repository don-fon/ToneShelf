-- +goose Up
ALTER TABLE tracks ADD COLUMN cover_version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE upload_jobs ADD COLUMN cover_release_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE upload_jobs DROP COLUMN cover_release_id;
ALTER TABLE tracks DROP COLUMN cover_version;
