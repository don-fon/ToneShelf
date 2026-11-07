package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

const (
	DefaultUserID     = "default-user"
	DefaultPlaylistID = "default-playlist"
)

type Store struct{ db *sql.DB }

type Track struct {
	ID                   string   `json:"id"`
	Title                string   `json:"title"`
	Artist               string   `json:"artist"`
	Album                string   `json:"album"`
	ContentType          string   `json:"contentType"`
	SizeBytes            int64    `json:"sizeBytes"`
	DurationSeconds      float64  `json:"durationSeconds"`
	BaseRandomWeight     float64  `json:"baseRandomWeight"`
	PlaybackStartSeconds float64  `json:"playbackStartSeconds"`
	PlaybackEndSeconds   *float64 `json:"playbackEndSeconds"`
	VolumeRatio          float64  `json:"volumeRatio"`
	Lyrics               string   `json:"lyrics"`
	AudioURL             string   `json:"audioUrl"`
	OriginalPath         string   `json:"-"`
	PlayablePath         string   `json:"-"`
}

type Playlist struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	IsDefault bool    `json:"isDefault"`
	Tracks    []Track `json:"tracks"`
}

type NewTrack struct {
	ID, Title, Artist, Album, OriginalPath, PlayablePath, ContentType string
	SizeBytes                                                         int64
	DurationSeconds                                                   float64
}

func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{"PRAGMA foreign_keys=ON", "PRAGMA journal_mode=WAL", "PRAGMA busy_timeout=5000"} {
		if _, err = db.ExecContext(ctx, pragma); err != nil {
			db.Close()
			return nil, err
		}
	}
	s := &Store{db: db}
	if err = s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if err = s.seed(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return err
	}
	goose.SetBaseFS(sub)
	if err = goose.SetDialect("sqlite3"); err != nil {
		return err
	}
	return goose.Up(s.db, ".")
}

func (s *Store) seed(ctx context.Context) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO users(id,display_name,created_at) VALUES(?,?,?)`, DefaultUserID, "默认用户", now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO playlists(id,owner_user_id,name,is_default,created_at,updated_at) VALUES(?,?,?,?,?,?)`, DefaultPlaylistID, DefaultUserID, "默认歌单", 1, now, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Playlists(ctx context.Context) ([]Playlist, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,is_default FROM playlists WHERE owner_user_id=? ORDER BY is_default DESC, created_at`, DefaultUserID)
	if err != nil {
		return nil, err
	}
	out := make([]Playlist, 0)
	for rows.Next() {
		var p Playlist
		if err = rows.Scan(&p.ID, &p.Name, &p.IsDefault); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	// SQLite intentionally uses one connection. Close this result set before
	// issuing the per-playlist queries so the connection is available again.
	if err = rows.Close(); err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Tracks, err = s.tracksForPlaylist(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) tracksForPlaylist(ctx context.Context, playlistID string) ([]Track, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.id,t.title,t.artist,t.album,t.content_type,t.size_bytes,t.duration_seconds,t.base_random_weight,t.playback_start_seconds,t.playback_end_seconds,t.volume_ratio,t.lyrics,t.original_path,t.playable_path
		FROM playlist_items pi JOIN tracks t ON t.id=pi.track_id
		WHERE pi.playlist_id=? AND t.deleted_at IS NULL ORDER BY pi.position`, playlistID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Track, 0)
	for rows.Next() {
		var t Track
		var end sql.NullFloat64
		if err = rows.Scan(&t.ID, &t.Title, &t.Artist, &t.Album, &t.ContentType, &t.SizeBytes, &t.DurationSeconds, &t.BaseRandomWeight, &t.PlaybackStartSeconds, &end, &t.VolumeRatio, &t.Lyrics, &t.OriginalPath, &t.PlayablePath); err != nil {
			return nil, err
		}
		if end.Valid {
			t.PlaybackEndSeconds = &end.Float64
		}
		t.AudioURL = "/api/tracks/" + t.ID + "/audio"
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) Track(ctx context.Context, id string) (Track, error) {
	var t Track
	var end sql.NullFloat64
	err := s.db.QueryRowContext(ctx, `SELECT id,title,artist,album,content_type,size_bytes,duration_seconds,base_random_weight,playback_start_seconds,playback_end_seconds,volume_ratio,lyrics,original_path,playable_path FROM tracks WHERE id=? AND owner_user_id=? AND deleted_at IS NULL`, id, DefaultUserID).Scan(&t.ID, &t.Title, &t.Artist, &t.Album, &t.ContentType, &t.SizeBytes, &t.DurationSeconds, &t.BaseRandomWeight, &t.PlaybackStartSeconds, &end, &t.VolumeRatio, &t.Lyrics, &t.OriginalPath, &t.PlayablePath)
	if end.Valid {
		t.PlaybackEndSeconds = &end.Float64
	}
	t.AudioURL = "/api/tracks/" + t.ID + "/audio"
	return t, err
}

func (s *Store) InsertTrack(ctx context.Context, n NewTrack) (Track, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Track{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO tracks(id,owner_user_id,title,artist,album,original_path,playable_path,content_type,size_bytes,duration_seconds,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, n.ID, DefaultUserID, n.Title, n.Artist, n.Album, n.OriginalPath, n.PlayablePath, n.ContentType, n.SizeBytes, n.DurationSeconds, now, now)
	if err != nil {
		return Track{}, err
	}
	var pos int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(position),-1)+1 FROM playlist_items WHERE playlist_id=?`, DefaultPlaylistID).Scan(&pos); err != nil {
		return Track{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO playlist_items(id,playlist_id,track_id,position,created_at) VALUES(?,?,?,?,?)`, n.ID+"-default", DefaultPlaylistID, n.ID, pos, now); err != nil {
		return Track{}, err
	}
	if err = tx.Commit(); err != nil {
		return Track{}, err
	}
	return s.Track(ctx, n.ID)
}

func (s *Store) UpdateTrack(ctx context.Context, id string, weight, start float64, end *float64, volume float64, lyrics string) (Track, error) {
	if weight <= 0 || start < 0 || volume < 0 || volume > 1 {
		return Track{}, errors.New("invalid track settings")
	}
	t, err := s.Track(ctx, id)
	if err != nil {
		return Track{}, err
	}
	endValue := any(nil)
	if end != nil {
		if *end <= start || *end > t.DurationSeconds {
			return Track{}, errors.New("invalid playback range")
		}
		endValue = *end
	} else if start >= t.DurationSeconds {
		return Track{}, errors.New("invalid playback range")
	}
	_, err = s.db.ExecContext(ctx, `UPDATE tracks SET base_random_weight=?,playback_start_seconds=?,playback_end_seconds=?,volume_ratio=?,lyrics=?,updated_at=? WHERE id=? AND owner_user_id=?`, weight, start, endValue, volume, lyrics, time.Now().UTC().Format(time.RFC3339Nano), id, DefaultUserID)
	if err != nil {
		return Track{}, err
	}
	return s.Track(ctx, id)
}

func (s *Store) CreatePlaylist(ctx context.Context, id, name string) (Playlist, error) {
	if name == "" {
		return Playlist{}, errors.New("name is required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `INSERT INTO playlists(id,owner_user_id,name,is_default,created_at,updated_at) VALUES(?,?,?,?,?,?)`, id, DefaultUserID, name, 0, now, now)
	return Playlist{ID: id, Name: name, Tracks: []Track{}}, err
}

func (s *Store) AddTrack(ctx context.Context, playlistID, trackID, itemID string) error {
	var pos int
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(position),-1)+1 FROM playlist_items WHERE playlist_id=?`, playlistID).Scan(&pos); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO playlist_items(id,playlist_id,track_id,position,created_at) SELECT ?,p.id,t.id,?,? FROM playlists p,tracks t WHERE p.id=? AND p.owner_user_id=? AND t.id=? AND t.owner_user_id=? AND t.deleted_at IS NULL`, itemID, pos, time.Now().UTC().Format(time.RFC3339Nano), playlistID, DefaultUserID, trackID, DefaultUserID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) Usage(ctx context.Context) (int64, error) {
	var n sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(size_bytes),0) FROM tracks`).Scan(&n)
	return n.Int64, err
}

func (s *Store) String() string { return fmt.Sprintf("Store(%p)", s.db) }
