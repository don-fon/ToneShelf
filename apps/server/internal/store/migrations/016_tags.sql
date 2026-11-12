-- +goose Up
CREATE TABLE tags (
    id TEXT PRIMARY KEY,
    owner_user_id TEXT NOT NULL REFERENCES users(id),
    name TEXT NOT NULL COLLATE NOCASE,
    UNIQUE(owner_user_id, name)
);
CREATE TABLE track_tags (
    track_id TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    tag_id TEXT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY(track_id, tag_id)
);
CREATE INDEX track_tags_tag ON track_tags(tag_id);
CREATE TABLE tag_groups (
    id TEXT PRIMARY KEY,
    owner_user_id TEXT NOT NULL REFERENCES users(id),
    name TEXT NOT NULL,
    is_default INTEGER NOT NULL DEFAULT 0,
    include_untagged INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX tag_groups_default ON tag_groups(owner_user_id) WHERE is_default=1;
CREATE TABLE tag_group_tags (
    group_id TEXT NOT NULL REFERENCES tag_groups(id) ON DELETE CASCADE,
    tag_id TEXT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY(group_id, tag_id)
);

-- +goose Down
DROP TABLE tag_group_tags;
DROP TABLE tag_groups;
DROP TABLE track_tags;
DROP TABLE tags;
