-- +goose Up
ALTER TABLE upload_jobs ADD COLUMN playlist_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE upload_jobs DROP COLUMN playlist_id;
