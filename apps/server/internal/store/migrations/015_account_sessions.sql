-- +goose Up
CREATE TABLE user_credentials (
    user_id TEXT NOT NULL REFERENCES users(id),
    kind TEXT NOT NULL,
    salt BLOB NOT NULL,
    verifier BLOB NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY(user_id, kind)
);
CREATE TABLE login_sessions (
    token_hash BLOB PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id),
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX login_sessions_user ON login_sessions(user_id);
CREATE INDEX login_sessions_expiry ON login_sessions(expires_at);

-- +goose Down
DROP TABLE login_sessions;
DROP TABLE user_credentials;
