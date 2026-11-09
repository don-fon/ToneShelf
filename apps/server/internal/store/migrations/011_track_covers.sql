-- +goose Up
ALTER TABLE tracks ADD COLUMN cover_path TEXT NOT NULL DEFAULT '';
ALTER TABLE tracks ADD COLUMN cover_scan_version INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE tracks DROP COLUMN cover_scan_version;
ALTER TABLE tracks DROP COLUMN cover_path;
