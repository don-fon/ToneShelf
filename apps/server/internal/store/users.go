package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"music-player/server/internal/currentuser"
)

// EnsureUser is an application-bootstrap operation, never an HTTP account API.
func (s *Store) EnsureUser(ctx context.Context, id, name, defaultPlaylist string) error {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(defaultPlaylist) == "" {
		return errors.New("user and default playlist are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := nowText()
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO users(id,display_name,created_at) VALUES(?,?,?)`, id, name, now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO playlists(id,owner_user_id,name,is_default,created_at,updated_at) SELECT ?,?, ?,1,?,? WHERE NOT EXISTS(SELECT 1 FROM playlists WHERE owner_user_id=? AND is_default=1)`, defaultPlaylist, id, "默认歌单", now, now, id); err != nil {
		return err
	}
	return tx.Commit()
}

// UserIDs enumerates owners for system maintenance, not for client responses.
func (s *Store) UserIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func defaultPlaylistTx(ctx context.Context, tx *sql.Tx) (string, error) {
	if err := currentuser.Require(ctx); err != nil {
		return "", err
	}
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM playlists WHERE owner_user_id=? AND is_default=1`, currentuser.ID(ctx)).Scan(&id)
	return id, err
}
