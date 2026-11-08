-- +goose Up
ALTER TABLE tracks ADD COLUMN original_content_type TEXT NOT NULL DEFAULT '';
ALTER TABLE tracks ADD COLUMN playback_index_version INTEGER NOT NULL DEFAULT 0;
ALTER TABLE upload_jobs ADD COLUMN kind TEXT NOT NULL DEFAULT 'upload';

-- +goose Down
ALTER TABLE upload_jobs DROP COLUMN kind;
ALTER TABLE tracks DROP COLUMN playback_index_version;
ALTER TABLE tracks DROP COLUMN original_content_type;
