-- +goose Up
ALTER TABLE tracks ADD COLUMN note TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE tracks DROP COLUMN note;
