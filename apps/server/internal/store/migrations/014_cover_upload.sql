-- +goose Up
ALTER TABLE upload_jobs ADD COLUMN cover_input_path TEXT NOT NULL DEFAULT '';
UPDATE upload_jobs SET state='failed',error='在线封面查找已移除，请上传图片',updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
WHERE kind='cover' AND cover_release_id<>'' AND state IN ('receiving','queued','processing');
ALTER TABLE upload_jobs DROP COLUMN cover_release_id;

-- +goose Down
ALTER TABLE upload_jobs ADD COLUMN cover_release_id TEXT NOT NULL DEFAULT '';
ALTER TABLE upload_jobs DROP COLUMN cover_input_path;
