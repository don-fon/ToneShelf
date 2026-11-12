package store

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddNamedMigrationContext("017_playlist_tags.go", migratePlaylistTags, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM tag_groups; DELETE FROM track_tags; DELETE FROM tags;`)
		return err
	})
}

// Keep the old tables as a migration source; new library membership comes only from tags.
func migratePlaylistTags(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT id,owner_user_id,name,is_default,created_at FROM playlists ORDER BY created_at,id`)
	if err != nil {
		return err
	}
	type oldPlaylist struct {
		id, owner, name, created string
		defaultGroup             bool
	}
	var old []oldPlaylist
	for rows.Next() {
		var p oldPlaylist
		if err = rows.Scan(&p.id, &p.owner, &p.name, &p.defaultGroup, &p.created); err != nil {
			rows.Close()
			return err
		}
		old = append(old, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, p := range old {
		groupName := p.name
		if p.defaultGroup {
			groupName = "默认标签组"
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO tag_groups(id,owner_user_id,name,is_default,include_untagged,created_at) VALUES(?,?,?,?,?,?)`, "group:"+p.id, p.owner, groupName, p.defaultGroup, p.defaultGroup, p.created); err != nil {
			return err
		}
		if p.defaultGroup {
			continue
		}
		tagName := p.name
		for index := 0; ; index++ {
			if ValidateTagName(tagName) != nil || index > 0 {
				tagName = "旧" + string(rune(0x4e00+index))
			}
			var exists bool
			if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tags WHERE owner_user_id=? AND name=? COLLATE NOCASE)`, p.owner, tagName).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				break
			}
			if index > 20000 {
				return fmt.Errorf("cannot assign legacy tag name")
			}
		}
		tagID := "legacy:" + p.id
		if _, err = tx.ExecContext(ctx, `INSERT INTO tags(id,owner_user_id,name) VALUES(?,?,?)`, tagID, p.owner, tagName); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO track_tags(track_id,tag_id) SELECT t.id,? FROM playlist_items pi JOIN tracks t ON t.id=pi.track_id WHERE pi.playlist_id=? AND t.owner_user_id=?`, tagID, p.id, p.owner); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO tag_group_tags(group_id,tag_id) VALUES(?,?)`, "group:"+p.id, tagID); err != nil {
			return err
		}
	}
	return nil
}
