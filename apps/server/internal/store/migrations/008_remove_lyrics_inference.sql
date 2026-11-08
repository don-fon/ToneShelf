-- +goose Up
UPDATE tracks SET lyrics_layout='timestamp' WHERE lyrics_layout='end-timed';

-- +goose Down
-- Restoring an inferred layout would require guessing the previous user choice.
SELECT 1;
