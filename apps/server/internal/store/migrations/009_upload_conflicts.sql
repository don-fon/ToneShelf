-- +goose Up
ALTER TABLE upload_jobs ADD COLUMN pending_candidate TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE upload_jobs DROP COLUMN pending_candidate;
