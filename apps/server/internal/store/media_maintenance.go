package store

import (
	"context"

	"music-player/server/internal/currentuser"
)

func (s *Store) MediaMaintenanceTracks(ctx context.Context) ([]Track, error) {
	if err := currentuser.Require(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,original_path,playable_path,duration_seconds,original_content_type,playback_index_version,duration_probe_version,audio_version FROM tracks WHERE owner_user_id=? AND deleted_at IS NULL AND (original_content_type='' OR (original_content_type='audio/aac' AND duration_probe_version<1) OR (original_path<>playable_path AND playback_index_version<1))`, currentuser.ID(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tracks []Track
	for rows.Next() {
		var track Track
		if err = rows.Scan(&track.ID, &track.OriginalPath, &track.PlayablePath, &track.DurationSeconds, &track.OriginalContentType, &track.PlaybackIndexVersion, &track.DurationProbeVersion, &track.AudioVersion); err != nil {
			return nil, err
		}
		tracks = append(tracks, track)
	}
	return tracks, rows.Err()
}

func (s *Store) SetOriginalProbe(ctx context.Context, track Track, mime string, duration float64) error {
	if err := currentuser.Require(ctx); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE tracks SET original_content_type=?,duration_seconds=?,duration_probe_version=1,
		playback_end_seconds=CASE WHEN playback_start_seconds>=? THEN NULL WHEN playback_end_seconds>? THEN ? ELSE playback_end_seconds END,
		playback_start_seconds=CASE WHEN playback_start_seconds>=? THEN 0 ELSE playback_start_seconds END,
		audio_version=audio_version+1,updated_at=?
		WHERE id=? AND owner_user_id=? AND original_path=? AND audio_version=? AND deleted_at IS NULL`,
		mime, duration, duration, duration, duration, duration, nowText(), track.ID, currentuser.ID(ctx), track.OriginalPath, track.AudioVersion)
	return requireOne(result, err)
}

func (s *Store) ScheduleIndexRepair(ctx context.Context, track Track, id, output string) error {
	if err := currentuser.Require(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO upload_jobs(id,owner_user_id,file_name,target_track_id,source_path,output_path,state,kind,created_at,updated_at)
		SELECT ?,owner_user_id,file_name,id,playable_path,?,'queued','repair',?,? FROM tracks t
		WHERE id=? AND owner_user_id=? AND playable_path=? AND original_path<>playable_path AND playback_index_version<1 AND deleted_at IS NULL
		AND NOT EXISTS(SELECT 1 FROM upload_jobs j WHERE j.target_track_id=t.id AND j.kind='repair' AND j.state IN ('queued','processing'))`,
		id, output, nowText(), nowText(), track.ID, currentuser.ID(ctx), track.PlayablePath)
	return err
}

func (s *Store) CompleteIndexRepair(ctx context.Context, job UploadJob, oldSize, newSize int64) error {
	if err := currentuser.Require(ctx); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE tracks SET playable_path=?,size_bytes=size_bytes-?+?,playback_index_version=1,audio_version=audio_version+1,updated_at=?
		WHERE id=? AND owner_user_id=? AND playable_path=? AND original_path<>playable_path AND deleted_at IS NULL
		AND EXISTS(SELECT 1 FROM upload_jobs WHERE id=? AND owner_user_id=? AND kind='repair' AND state='processing' AND reserved_bytes>=?)`,
		job.OutputPath, oldSize, newSize, nowText(), job.TargetTrackID, currentuser.ID(ctx), job.SourcePath, job.ID, currentuser.ID(ctx), newSize)
	if err = requireOne(result, err); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO pending_file_deletions(path,size_bytes,created_at)VALUES(?,?,?) ON CONFLICT(path)DO NOTHING`, job.SourcePath, oldSize, nowText()); err != nil {
		return err
	}
	result, err = tx.ExecContext(ctx, `UPDATE upload_jobs SET state='succeeded',track_id=?,reserved_bytes=0,error='',updated_at=? WHERE id=? AND owner_user_id=? AND kind='repair' AND state='processing'`, job.TargetTrackID, nowText(), job.ID, currentuser.ID(ctx))
	if err = requireOne(result, err); err != nil {
		return err
	}
	return tx.Commit()
}
