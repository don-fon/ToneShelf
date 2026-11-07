-- +goose Up
ALTER TABLE tracks ADD COLUMN file_name TEXT NOT NULL DEFAULT '';
ALTER TABLE tracks ADD COLUMN custom_title INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tracks ADD COLUMN custom_artist INTEGER NOT NULL DEFAULT 0;
UPDATE tracks SET file_name=title;

-- +goose Down
ALTER TABLE tracks DROP COLUMN custom_artist;
ALTER TABLE tracks DROP COLUMN custom_title;
ALTER TABLE tracks DROP COLUMN file_name;
