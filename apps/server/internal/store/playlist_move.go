package store

import (
	"context"
	"database/sql"
	"errors"

	"music-player/server/internal/currentuser"
)

func (s *Store) MoveTrack(ctx context.Context, sourceID, targetID, trackID, itemID string) error {
	if err := currentuser.Require(ctx); err != nil {
		return err
	}
	if sourceID == targetID || targetID == "" {
		return errors.New("请选择其他目标歌单")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM playlists WHERE owner_user_id=? AND id IN (?,?)`, currentuser.ID(ctx), sourceID, targetID).Scan(&count); err != nil {
		return err
	}
	if count != 2 {
		return sql.ErrNoRows
	}
	var owned string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM tracks WHERE id=? AND owner_user_id=? AND deleted_at IS NULL`, trackID, currentuser.ID(ctx)).Scan(&owned); err != nil {
		return err
	}
	var source, target int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM playlist_items WHERE playlist_id=? AND track_id=?`, sourceID, trackID).Scan(&source); err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM playlist_items WHERE playlist_id=? AND track_id=?`, targetID, trackID).Scan(&target); err != nil {
		return err
	}
	if source == 0 {
		if target > 0 {
			return nil
		}
		return sql.ErrNoRows
	}
	if target == 0 {
		if err = addTrackTx(ctx, tx, targetID, trackID, itemID); err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM playlist_items WHERE playlist_id=? AND track_id=?`, sourceID, trackID)
	if err = requireOne(result, err); err != nil {
		return err
	}
	if err = compactPlaylistTx(ctx, tx, sourceID); err != nil {
		return err
	}
	return tx.Commit()
}
