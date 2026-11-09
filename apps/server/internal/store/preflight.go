package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"music-player/server/internal/currentuser"
)

func duplicateTitle(ctx context.Context, tx *sql.Tx, title string) (string, error) {
	if err := currentuser.Require(ctx); err != nil {
		return "", err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,title FROM tracks WHERE owner_user_id=? AND deleted_at IS NULL ORDER BY created_at,id`, currentuser.ID(ctx))
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err = rows.Scan(&id, &name); err != nil {
			return "", err
		}
		if strings.EqualFold(strings.TrimSpace(title), strings.TrimSpace(name)) {
			return id, nil
		}
	}
	return "", rows.Err()
}

func (s *Store) PrepareUpload(ctx context.Context, id, title string, size int64) error {
	if err := currentuser.Require(ctx); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	target, err := duplicateTitle(ctx, tx, title)
	if err != nil {
		return err
	}
	candidate, err := json.Marshal(NewTrack{Title: title})
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE upload_jobs SET state='queued',target_track_id=?,pending_candidate=?,reserved_bytes=?,updated_at=? WHERE id=? AND owner_user_id=? AND kind='preflight' AND state='receiving' AND reserved_bytes>=?`, target, string(candidate), size, nowText(), id, currentuser.ID(ctx), size)
	if err = requireOne(result, err); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) StartPreparedUpload(ctx context.Context, id, key string) error {
	if err := currentuser.Require(ctx); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var target, pending string
	if err = tx.QueryRowContext(ctx, `SELECT target_track_id,pending_candidate FROM upload_jobs WHERE id=? AND owner_user_id=? AND kind='preflight' AND state='queued'`, id, currentuser.ID(ctx)).Scan(&target, &pending); err != nil {
		return err
	}
	var candidate NewTrack
	if err = json.Unmarshal([]byte(pending), &candidate); err != nil {
		return err
	}
	current, err := duplicateTitle(ctx, tx, candidate.Title)
	if err != nil {
		return err
	}
	if current != target {
		return errors.New("同名歌曲已变化，请取消后重新选择文件")
	}
	result, err := tx.ExecContext(ctx, `UPDATE upload_jobs SET kind='upload',pending_candidate='',request_key=?,updated_at=? WHERE id=? AND owner_user_id=? AND kind='preflight' AND state='queued'`, key, nowText(), id, currentuser.ID(ctx))
	if err = requireOne(result, err); err != nil {
		return err
	}
	return tx.Commit()
}
