-- +goose Up
ALTER TABLE tracks ADD COLUMN duration_probe_version INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE tracks DROP COLUMN duration_probe_version;
