package store

import (
	"context"
	"music-player/server/internal/currentuser"
)

func (s *Store) CoverMaintenanceTracks(ctx context.Context) ([]Track, error) {
	if err := currentuser.Require(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,original_path FROM tracks WHERE owner_user_id=? AND deleted_at IS NULL AND cover_scan_version<1`, currentuser.ID(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tracks []Track
	for rows.Next() {
		var t Track
		if err = rows.Scan(&t.ID, &t.OriginalPath); err != nil {
			return nil, err
		}
		t, err = s.resolveTrack(t)
		if err != nil {
			return nil, err
		}
		tracks = append(tracks, t)
	}
	return tracks, rows.Err()
}

func (s *Store) ScheduleCover(ctx context.Context, track Track, id, output string) error {
	if err := s.paths(false, &track.OriginalPath, &output); err != nil {
		return err
	}
	if err := currentuser.Require(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO upload_jobs(id,owner_user_id,file_name,target_track_id,source_path,output_path,state,kind,created_at,updated_at)
 SELECT ?,owner_user_id,file_name,id,original_path,?,'queued','cover',?,? FROM tracks t
 WHERE id=? AND owner_user_id=? AND original_path=? AND deleted_at IS NULL AND cover_scan_version<1
 AND NOT EXISTS(SELECT 1 FROM upload_jobs j WHERE j.target_track_id=t.id AND j.kind='cover' AND j.state IN ('queued','processing'))`, id, output, nowText(), nowText(), track.ID, currentuser.ID(ctx), track.OriginalPath)
	return err
}

func (s *Store) CompleteCover(ctx context.Context, job UploadJob, path string, size int64) error {
	if err := s.paths(false, &job.SourcePath, &path); err != nil {
		return err
	}
	if err := currentuser.Require(ctx); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE tracks SET cover_path=?,cover_scan_version=1,size_bytes=size_bytes+? WHERE id=? AND owner_user_id=? AND original_path=? AND deleted_at IS NULL AND cover_scan_version<1
 AND EXISTS(SELECT 1 FROM upload_jobs WHERE id=? AND owner_user_id=? AND kind='cover' AND state='processing' AND reserved_bytes>=?)`, path, size, job.TargetTrackID, currentuser.ID(ctx), job.SourcePath, job.ID, currentuser.ID(ctx), size)
	if err = requireOne(result, err); err != nil {
		return err
	}
	result, err = tx.ExecContext(ctx, `UPDATE upload_jobs SET state='succeeded',track_id=?,reserved_bytes=0,error='',updated_at=? WHERE id=? AND owner_user_id=? AND kind='cover' AND state='processing'`, job.TargetTrackID, nowText(), job.ID, currentuser.ID(ctx))
	if err = requireOne(result, err); err != nil {
		return err
	}
	return tx.Commit()
}
