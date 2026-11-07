-- +goose Up
CREATE TABLE upload_jobs (
    id TEXT PRIMARY KEY,
    owner_user_id TEXT NOT NULL REFERENCES users(id),
    file_name TEXT NOT NULL,
    request_key TEXT NOT NULL DEFAULT '',
    target_track_id TEXT NOT NULL DEFAULT '',
    track_id TEXT NOT NULL DEFAULT '',
    source_path TEXT NOT NULL,
    output_path TEXT NOT NULL,
    state TEXT NOT NULL CHECK(state IN ('receiving','queued','processing','succeeded','failed')),
    reserved_bytes INTEGER NOT NULL DEFAULT 0 CHECK(reserved_bytes >= 0),
    error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX upload_jobs_state ON upload_jobs(state,created_at);

-- +goose Down
DROP TABLE upload_jobs;
