package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"music-player/server/internal/currentuser"
	"music-player/server/internal/tagwidth"
)

const UntaggedID = "_untagged"

func ensureDefaultTagGroupTx(ctx context.Context, tx *sql.Tx, user string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO tag_groups(id,owner_user_id,name,is_default,include_untagged,created_at) SELECT ?,?,'默认标签组',1,1,? WHERE NOT EXISTS(SELECT 1 FROM tag_groups WHERE owner_user_id=? AND is_default=1)`, "tag-default:"+user, user, nowText(), user)
	return err
}

type Tag struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type TagGroup struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	IsDefault bool     `json:"isDefault"`
	TagIDs    []string `json:"tagIds"`
}
type Library struct {
	Tracks []Track    `json:"tracks"`
	Tags   []Tag      `json:"tags"`
	Groups []TagGroup `json:"groups"`
}

func ValidateTagName(name string) error {
	name = strings.TrimSpace(name)
	if name == "无" {
		return errors.New("无为系统标签")
	}
	if units := tagwidth.Units(name); units < 1 || units > 4 {
		return errors.New("标签最多两个全角或四个半角字符")
	}
	return nil
}

func (s *Store) Library(ctx context.Context) (Library, error) {
	out := Library{Tracks: []Track{}, Tags: []Tag{}, Groups: []TagGroup{}}
	if err := currentuser.Require(ctx); err != nil {
		return out, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	tracks, err := s.queryTracksWith(ctx, tx, `SELECT t.id,t.title,t.artist,t.album,t.content_type,t.size_bytes,t.duration_seconds,t.base_random_weight,t.playback_start_seconds,t.playback_end_seconds,t.volume_ratio,t.lyrics,t.lyrics_layout,t.lyrics_primary_line,t.original_path,t.playable_path,t.audio_version,t.original_content_type,t.playback_index_version,t.duration_probe_version,t.file_name,t.created_at,t.cover_path,t.cover_version FROM tracks t WHERE t.owner_user_id=? AND t.deleted_at IS NULL ORDER BY t.created_at,t.id`, currentuser.ID(ctx))
	if err != nil {
		return out, err
	}
	out.Tracks = tracks
	rows, err := tx.QueryContext(ctx, `SELECT id,name FROM tags WHERE owner_user_id=? ORDER BY name COLLATE NOCASE,id`, currentuser.ID(ctx))
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var tag Tag
		if err = rows.Scan(&tag.ID, &tag.Name); err != nil {
			rows.Close()
			return out, err
		}
		out.Tags = append(out.Tags, tag)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT tt.track_id,tt.tag_id FROM track_tags tt JOIN tags tag ON tag.id=tt.tag_id JOIN tracks t ON t.id=tt.track_id WHERE t.owner_user_id=? AND tag.owner_user_id=? ORDER BY tt.tag_id`, currentuser.ID(ctx), currentuser.ID(ctx))
	if err != nil {
		return out, err
	}
	byTrack := map[string][]string{}
	for rows.Next() {
		var track, tag string
		if err = rows.Scan(&track, &tag); err != nil {
			rows.Close()
			return out, err
		}
		byTrack[track] = append(byTrack[track], tag)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	for i := range out.Tracks {
		out.Tracks[i].TagIDs = byTrack[out.Tracks[i].ID]
		if out.Tracks[i].TagIDs == nil {
			out.Tracks[i].TagIDs = []string{}
		}
	}
	rows, err = tx.QueryContext(ctx, `SELECT id,name,is_default,include_untagged FROM tag_groups WHERE owner_user_id=? ORDER BY is_default DESC,created_at,id`, currentuser.ID(ctx))
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var group TagGroup
		var untagged bool
		group.TagIDs = []string{}
		if err = rows.Scan(&group.ID, &group.Name, &group.IsDefault, &untagged); err != nil {
			rows.Close()
			return out, err
		}
		if untagged {
			group.TagIDs = append(group.TagIDs, UntaggedID)
		}
		out.Groups = append(out.Groups, group)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT gt.group_id,gt.tag_id FROM tag_group_tags gt JOIN tag_groups g ON g.id=gt.group_id JOIN tags t ON t.id=gt.tag_id WHERE g.owner_user_id=? AND t.owner_user_id=? ORDER BY gt.tag_id`, currentuser.ID(ctx), currentuser.ID(ctx))
	if err != nil {
		return out, err
	}
	byGroup := map[string][]string{}
	for rows.Next() {
		var group, tag string
		if err = rows.Scan(&group, &tag); err != nil {
			rows.Close()
			return out, err
		}
		byGroup[group] = append(byGroup[group], tag)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	for i := range out.Groups {
		if out.Groups[i].IsDefault {
			out.Groups[i].TagIDs = []string{UntaggedID}
			for _, tag := range out.Tags {
				out.Groups[i].TagIDs = append(out.Groups[i].TagIDs, tag.ID)
			}
		} else {
			out.Groups[i].TagIDs = append(out.Groups[i].TagIDs, byGroup[out.Groups[i].ID]...)
		}
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return out, nil
}

func (s *Store) SaveTag(ctx context.Context, id, name string, create bool) error {
	if err := currentuser.Require(ctx); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if err := ValidateTagName(name); err != nil {
		return err
	}
	var err error
	if create {
		_, err = s.db.ExecContext(ctx, `INSERT INTO tags(id,owner_user_id,name) VALUES(?,?,?)`, id, currentuser.ID(ctx), name)
	} else {
		var result sql.Result
		result, err = s.db.ExecContext(ctx, `UPDATE tags SET name=? WHERE id=? AND owner_user_id=?`, name, id, currentuser.ID(ctx))
		if err == nil {
			err = requireAffected(result)
		}
	}
	if err != nil {
		return errors.New("标签不存在或名称重复")
	}
	return nil
}
func requireAffected(result sql.Result) error {
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return sql.ErrNoRows
	}
	return nil
}
func (s *Store) DeleteTag(ctx context.Context, id string) error {
	if err := currentuser.Require(ctx); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM tags WHERE id=? AND owner_user_id=?`, id, currentuser.ID(ctx))
	if err != nil {
		return err
	}
	return requireAffected(result)
}
func validateTagsTx(ctx context.Context, tx *sql.Tx, ids []string, allowUntagged bool) error {
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			return errors.New("标签重复")
		}
		seen[id] = true
		if id == UntaggedID && allowUntagged {
			continue
		}
		var valid bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tags WHERE id=? AND owner_user_id=?)`, id, currentuser.ID(ctx)).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return errors.New("标签不存在")
		}
	}
	return nil
}
func replaceTrackTagsTx(ctx context.Context, tx *sql.Tx, track string, ids []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM track_tags WHERE track_id=?`, track); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `INSERT INTO track_tags(track_id,tag_id) VALUES(?,?)`, track, id); err != nil {
			return err
		}
	}
	return nil
}

// A whole batch is one transaction, including ownership and tag validation.
func (s *Store) SetTrackTags(ctx context.Context, tracks, tags []string, mode string) error {
	if err := currentuser.Require(ctx); err != nil {
		return err
	}
	if len(tracks) == 0 || len(tracks) > 10000 {
		return errors.New("请选择歌曲")
	}
	if mode != "replace" && mode != "add" && mode != "remove" {
		return errors.New("无效标签操作")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = validateTagsTx(ctx, tx, tags, false); err != nil {
		return err
	}
	for _, id := range tracks {
		var valid bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tracks WHERE id=? AND owner_user_id=? AND deleted_at IS NULL)`, id, currentuser.ID(ctx)).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return errors.New("歌曲不存在")
		}
		if mode == "replace" {
			if err = replaceTrackTagsTx(ctx, tx, id, tags); err != nil {
				return err
			}
			continue
		}
		for _, tag := range tags {
			if mode == "add" {
				_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO track_tags(track_id,tag_id) VALUES(?,?)`, id, tag)
			} else {
				_, err = tx.ExecContext(ctx, `DELETE FROM track_tags WHERE track_id=? AND tag_id=?`, id, tag)
			}
			if err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
func (s *Store) SaveTagGroup(ctx context.Context, id, name string, tags []string, create bool) error {
	if err := currentuser.Require(ctx); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 4 {
		return errors.New("标签组名称需为 1–4 字")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = validateTagsTx(ctx, tx, tags, true); err != nil {
		return err
	}
	untagged := false
	for _, tag := range tags {
		if tag == UntaggedID {
			untagged = true
		}
	}
	if create {
		_, err = tx.ExecContext(ctx, `INSERT INTO tag_groups(id,owner_user_id,name,include_untagged,created_at) VALUES(?,?,?,?,?)`, id, currentuser.ID(ctx), name, untagged, nowText())
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, `UPDATE tag_groups SET name=?,include_untagged=? WHERE id=? AND owner_user_id=? AND is_default=0`, name, untagged, id, currentuser.ID(ctx))
		if err == nil {
			err = requireAffected(result)
		}
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM tag_group_tags WHERE group_id=?`, id); err != nil {
		return err
	}
	for _, tag := range tags {
		if tag == UntaggedID {
			continue
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO tag_group_tags(group_id,tag_id) VALUES(?,?)`, id, tag); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) DeleteTagGroup(ctx context.Context, id string) error {
	if err := currentuser.Require(ctx); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM tag_groups WHERE id=? AND owner_user_id=? AND is_default=0`, id, currentuser.ID(ctx))
	if err != nil {
		return err
	}
	return requireAffected(result)
}
