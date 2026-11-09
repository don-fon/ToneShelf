package store

import (
	"context"

	"music-player/server/internal/currentuser"
)

func (s *Store) CompleteUploadedCover(ctx context.Context, job UploadJob, old PendingFile, inputSize, size int64) error {
	if err := currentuser.Require(ctx); err != nil {
		return err
	}
	if err := s.paths(false, &job.SourcePath, &job.OutputPath, &old.Path, &job.CoverInputPath); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE tracks SET cover_path=?,cover_scan_version=1,cover_version=cover_version+1,size_bytes=size_bytes-?+?,updated_at=?
 WHERE id=? AND owner_user_id=? AND original_path=? AND cover_path=? AND deleted_at IS NULL
 AND EXISTS(SELECT 1 FROM upload_jobs WHERE id=? AND owner_user_id=? AND kind='cover' AND cover_input_path<>'' AND state='processing' AND reserved_bytes>=?)`, job.OutputPath, old.SizeBytes, size, nowText(), job.TargetTrackID, currentuser.ID(ctx), job.SourcePath, old.Path, job.ID, currentuser.ID(ctx), inputSize+size)
	if err = requireOne(result, err); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO pending_file_deletions(path,size_bytes,created_at)VALUES(?,?,?) ON CONFLICT(path)DO NOTHING`, job.CoverInputPath, inputSize, nowText()); err != nil {
		return err
	}
	if old.Path != "" {
		if _, err = tx.ExecContext(ctx, `INSERT INTO pending_file_deletions(path,size_bytes,created_at)VALUES(?,?,?) ON CONFLICT(path)DO NOTHING`, old.Path, old.SizeBytes, nowText()); err != nil {
			return err
		}
	}
	result, err = tx.ExecContext(ctx, `UPDATE upload_jobs SET state='succeeded',track_id=?,reserved_bytes=0,error='',updated_at=? WHERE id=? AND owner_user_id=? AND state='processing'`, job.TargetTrackID, nowText(), job.ID, currentuser.ID(ctx))
	if err = requireOne(result, err); err != nil {
		return err
	}
	return tx.Commit()
}
