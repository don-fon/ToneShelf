-- +goose Up
ALTER TABLE tracks ADD COLUMN lyrics_layout TEXT NOT NULL DEFAULT 'timestamp' CHECK(lyrics_layout IN ('timestamp','end-timed','separate'));
ALTER TABLE tracks ADD COLUMN lyrics_primary_line TEXT NOT NULL DEFAULT 'first' CHECK(lyrics_primary_line IN ('first','last'));

-- +goose Down
ALTER TABLE tracks DROP COLUMN lyrics_primary_line;
ALTER TABLE tracks DROP COLUMN lyrics_layout;
