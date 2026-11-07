package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

const (
	DefaultUserID     = "default-user"
	DefaultPlaylistID = "default-playlist"
)

var ErrConfirmationRequired = errors.New("confirmation required")

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
	FileName             string   `json:"fileName"`
	UploadedAt           string   `json:"uploadedAt"`
	AudioVersion         int64    `json:"audioVersion"`
	DeletedAt            *string  `json:"deletedAt,omitempty"`
	PurgeEligibleAt      *string  `json:"purgeEligibleAt,omitempty"`
	PurgeError           string   `json:"purgeError,omitempty"`
}

type Playlist struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	IsDefault bool    `json:"isDefault"`
	Tracks    []Track `json:"tracks"`
}

type NewTrack struct {
	ID, Title, Artist, Album, OriginalPath, PlayablePath, ContentType, FileName string
	SizeBytes                                                                   int64
	DurationSeconds                                                             float64
}

type RemoveResult struct {
	TrackDeleted bool `json:"trackDeleted"`
}

type PendingFile struct {
	Path, LastError string
	SizeBytes       int64
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
	rows, err := s.db.QueryContext(ctx, `SELECT t.id,t.title,t.artist,t.album,t.content_type,t.size_bytes,t.duration_seconds,t.base_random_weight,t.playback_start_seconds,t.playback_end_seconds,t.volume_ratio,t.lyrics,t.original_path,t.playable_path,t.audio_version,t.file_name,t.created_at
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
		if err = rows.Scan(&t.ID, &t.Title, &t.Artist, &t.Album, &t.ContentType, &t.SizeBytes, &t.DurationSeconds, &t.BaseRandomWeight, &t.PlaybackStartSeconds, &end, &t.VolumeRatio, &t.Lyrics, &t.OriginalPath, &t.PlayablePath, &t.AudioVersion, &t.FileName, &t.UploadedAt); err != nil {
			return nil, err
		}
		if end.Valid {
			t.PlaybackEndSeconds = &end.Float64
		}
		t.AudioURL = fmt.Sprintf("/api/tracks/%s/audio?v=%d", t.ID, t.AudioVersion)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) Track(ctx context.Context, id string) (Track, error) {
	var t Track
	var end sql.NullFloat64
	err := s.db.QueryRowContext(ctx, `SELECT id,title,artist,album,content_type,size_bytes,duration_seconds,base_random_weight,playback_start_seconds,playback_end_seconds,volume_ratio,lyrics,original_path,playable_path,audio_version,file_name,created_at FROM tracks WHERE id=? AND owner_user_id=? AND deleted_at IS NULL`, id, DefaultUserID).Scan(&t.ID, &t.Title, &t.Artist, &t.Album, &t.ContentType, &t.SizeBytes, &t.DurationSeconds, &t.BaseRandomWeight, &t.PlaybackStartSeconds, &end, &t.VolumeRatio, &t.Lyrics, &t.OriginalPath, &t.PlayablePath, &t.AudioVersion, &t.FileName, &t.UploadedAt)
	if end.Valid {
		t.PlaybackEndSeconds = &end.Float64
	}
	t.AudioURL = fmt.Sprintf("/api/tracks/%s/audio?v=%d", t.ID, t.AudioVersion)
	return t, err
}

func (s *Store) InsertTrack(ctx context.Context, n NewTrack) (Track, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Track{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO tracks(id,owner_user_id,title,artist,album,original_path,playable_path,content_type,size_bytes,duration_seconds,file_name,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, n.ID, DefaultUserID, n.Title, n.Artist, n.Album, n.OriginalPath, n.PlayablePath, n.ContentType, n.SizeBytes, n.DurationSeconds, n.FileName, now, now)
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

func (s *Store) UpdateTrack(ctx context.Context, id string, weight, start float64, end *float64, volume float64, lyrics string, identity ...*string) (Track, error) {
	if weight <= 0 || start < 0 || volume < 0 || volume > 1 {
		return Track{}, errors.New("invalid track settings")
	}
	var title, artist *string
	if len(identity) > 0 {
		title = identity[0]
	}
	if len(identity) > 1 {
		artist = identity[1]
	}
	if title != nil {
		trimmed := strings.TrimSpace(*title)
		if trimmed == "" {
			return Track{}, errors.New("title is required")
		}
		title = &trimmed
	}
	if artist != nil {
		trimmed := strings.TrimSpace(*artist)
		artist = &trimmed
	}
	t, err := s.Track(ctx, id)
	if err != nil {
		return Track{}, err
	}
	if title != nil && *title == t.Title {
		title = nil
	}
	if artist != nil && *artist == t.Artist {
		artist = nil
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
	_, err = s.db.ExecContext(ctx, `UPDATE tracks SET title=COALESCE(?,title),artist=COALESCE(?,artist),custom_title=CASE WHEN ? IS NULL THEN custom_title ELSE 1 END,custom_artist=CASE WHEN ? IS NULL THEN custom_artist ELSE 1 END,base_random_weight=?,playback_start_seconds=?,playback_end_seconds=?,volume_ratio=?,lyrics=?,updated_at=? WHERE id=? AND owner_user_id=?`, title, artist, title, artist, weight, start, endValue, volume, lyrics, time.Now().UTC().Format(time.RFC3339Nano), id, DefaultUserID)
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

func (s *Store) RenamePlaylist(ctx context.Context, id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("name is required")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE playlists SET name=?,updated_at=? WHERE id=? AND owner_user_id=? AND is_default=0`, name, nowText(), id, DefaultUserID)
	return requireOne(result, err)
}

func (s *Store) DeletePlaylist(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT track_id FROM playlist_items WHERE playlist_id=?`, id)
	if err != nil {
		return err
	}
	var trackIDs []string
	for rows.Next() {
		var trackID string
		if err = rows.Scan(&trackID); err != nil {
			rows.Close()
			return err
		}
		trackIDs = append(trackIDs, trackID)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM playlists WHERE id=? AND owner_user_id=? AND is_default=0`, id, DefaultUserID)
	if err = requireOne(result, err); err != nil {
		return err
	}
	for _, trackID := range trackIDs {
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM playlist_items WHERE track_id=?`, trackID).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			if err = addTrackTx(ctx, tx, DefaultPlaylistID, trackID, trackID+"-restored-"+time.Now().Format("150405.000000000")); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *Store) RemoveTrackFromPlaylist(ctx context.Context, playlistID, trackID string, confirmDelete bool, now time.Time) (RemoveResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RemoveResult{}, err
	}
	defer tx.Rollback()
	var isDefault bool
	if err = tx.QueryRowContext(ctx, `SELECT is_default FROM playlists WHERE id=? AND owner_user_id=?`, playlistID, DefaultUserID).Scan(&isDefault); err != nil {
		return RemoveResult{}, err
	}
	var memberships int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM playlist_items WHERE track_id=?`, trackID).Scan(&memberships); err != nil {
		return RemoveResult{}, err
	}
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM playlist_items WHERE playlist_id=? AND track_id=?`, playlistID, trackID).Scan(&exists); err != nil || exists != 1 {
		if err == nil {
			err = sql.ErrNoRows
		}
		return RemoveResult{}, err
	}
	if isDefault && memberships == 1 {
		if !confirmDelete {
			return RemoveResult{}, ErrConfirmationRequired
		}
		if err = softDeleteTrackTx(ctx, tx, trackID, now); err != nil {
			return RemoveResult{}, err
		}
		if err = tx.Commit(); err != nil {
			return RemoveResult{}, err
		}
		return RemoveResult{TrackDeleted: true}, nil
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM playlist_items WHERE playlist_id=? AND track_id=?`, playlistID, trackID); err != nil {
		return RemoveResult{}, err
	}
	if memberships == 1 {
		if err = addTrackTx(ctx, tx, DefaultPlaylistID, trackID, trackID+"-fallback-"+time.Now().Format("150405.000000000")); err != nil {
			return RemoveResult{}, err
		}
	}
	if err = compactPlaylistTx(ctx, tx, playlistID); err != nil {
		return RemoveResult{}, err
	}
	return RemoveResult{}, tx.Commit()
}

func (s *Store) SoftDeleteTrack(ctx context.Context, id string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = softDeleteTrackTx(ctx, tx, id, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Trash(ctx context.Context) ([]Track, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,title,artist,album,content_type,size_bytes,duration_seconds,base_random_weight,playback_start_seconds,playback_end_seconds,volume_ratio,lyrics,original_path,playable_path,audio_version,file_name,created_at,deleted_at,purge_eligible_at,purge_error FROM tracks WHERE owner_user_id=? AND deleted_at IS NOT NULL ORDER BY deleted_at DESC`, DefaultUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Track, 0)
	for rows.Next() {
		track, scanErr := scanTrashTrack(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, track)
	}
	return out, rows.Err()
}

func (s *Store) RestoreTrack(ctx context.Context, id, itemID string) (Track, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Track{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE tracks SET deleted_at=NULL,purge_eligible_at=NULL,purge_error='',updated_at=? WHERE id=? AND owner_user_id=? AND deleted_at IS NOT NULL`, nowText(), id, DefaultUserID)
	if err = requireOne(result, err); err != nil {
		return Track{}, err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM playlist_items WHERE track_id=?`, id).Scan(&count); err != nil {
		return Track{}, err
	}
	if count == 0 {
		if err = addTrackTx(ctx, tx, DefaultPlaylistID, id, itemID); err != nil {
			return Track{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Track{}, err
	}
	return s.Track(ctx, id)
}

func (s *Store) ReplaceTrack(ctx context.Context, id string, replacement NewTrack, oldFiles []PendingFile) (Track, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Track{}, err
	}
	defer tx.Rollback()
	var oldOriginal, oldPlayable string
	var start float64
	var end sql.NullFloat64
	if err = tx.QueryRowContext(ctx, `SELECT original_path,playable_path,playback_start_seconds,playback_end_seconds FROM tracks WHERE id=? AND owner_user_id=? AND deleted_at IS NULL`, id, DefaultUserID).Scan(&oldOriginal, &oldPlayable, &start, &end); err != nil {
		return Track{}, err
	}
	endValue := any(nil)
	if start >= replacement.DurationSeconds {
		start = 0
	} else if end.Valid && end.Float64 <= replacement.DurationSeconds {
		endValue = end.Float64
	}
	for _, file := range oldFiles {
		if file.Path == "" || file.Path == replacement.OriginalPath || file.Path == replacement.PlayablePath {
			continue
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO pending_file_deletions(path,size_bytes,created_at) VALUES(?,?,?) ON CONFLICT(path) DO UPDATE SET size_bytes=excluded.size_bytes,last_error=''`, file.Path, file.SizeBytes, nowText()); err != nil {
			return Track{}, err
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE tracks SET title=CASE WHEN custom_title=1 THEN title ELSE ? END,artist=CASE WHEN custom_artist=1 THEN artist ELSE ? END,album=?,file_name=?,original_path=?,playable_path=?,content_type=?,size_bytes=?,duration_seconds=?,playback_start_seconds=?,playback_end_seconds=?,audio_version=audio_version+1,updated_at=? WHERE id=? AND owner_user_id=? AND original_path=? AND playable_path=?`, replacement.Title, replacement.Artist, replacement.Album, replacement.FileName, replacement.OriginalPath, replacement.PlayablePath, replacement.ContentType, replacement.SizeBytes, replacement.DurationSeconds, start, endValue, nowText(), id, DefaultUserID, oldOriginal, oldPlayable)
	if err = requireOne(result, err); err != nil {
		return Track{}, err
	}
	if err = tx.Commit(); err != nil {
		return Track{}, err
	}
	return s.Track(ctx, id)
}

func (s *Store) PendingFiles(ctx context.Context) ([]PendingFile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT path,size_bytes,last_error FROM pending_file_deletions ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingFile
	for rows.Next() {
		var file PendingFile
		if err = rows.Scan(&file.Path, &file.SizeBytes, &file.LastError); err != nil {
			return nil, err
		}
		out = append(out, file)
	}
	return out, rows.Err()
}

func (s *Store) CompletePendingFile(ctx context.Context, path string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM pending_file_deletions WHERE path=?`, path)
	return err
}

func (s *Store) FailPendingFile(ctx context.Context, path string, failure error) error {
	_, err := s.db.ExecContext(ctx, `UPDATE pending_file_deletions SET last_error=? WHERE path=?`, failure.Error(), path)
	return err
}

func (s *Store) PurgeCandidates(ctx context.Context, now time.Time) ([]Track, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,title,artist,album,content_type,size_bytes,duration_seconds,base_random_weight,playback_start_seconds,playback_end_seconds,volume_ratio,lyrics,original_path,playable_path,audio_version,file_name,created_at,deleted_at,purge_eligible_at,purge_error FROM tracks WHERE owner_user_id=? AND deleted_at IS NOT NULL AND purge_eligible_at<=? ORDER BY purge_eligible_at`, DefaultUserID, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Track
	for rows.Next() {
		track, scanErr := scanTrashTrack(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, track)
	}
	return out, rows.Err()
}

func (s *Store) PurgeTrack(ctx context.Context, id string, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM tracks WHERE id=? AND owner_user_id=? AND deleted_at IS NOT NULL AND purge_eligible_at<=?`, id, DefaultUserID, now.UTC().Format(time.RFC3339Nano))
	return requireOne(result, err)
}

func (s *Store) RecordPurgeError(ctx context.Context, id string, failure error) error {
	_, err := s.db.ExecContext(ctx, `UPDATE tracks SET purge_error=? WHERE id=?`, failure.Error(), id)
	return err
}

func (s *Store) Usage(ctx context.Context) (int64, error) {
	var n sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT (SELECT COALESCE(SUM(size_bytes),0) FROM tracks)+(SELECT COALESCE(SUM(size_bytes),0) FROM pending_file_deletions)`).Scan(&n)
	return n.Int64, err
}

func softDeleteTrackTx(ctx context.Context, tx *sql.Tx, id string, now time.Time) error {
	deleted := now.UTC()
	result, err := tx.ExecContext(ctx, `UPDATE tracks SET deleted_at=?,purge_eligible_at=?,purge_error='',updated_at=? WHERE id=? AND owner_user_id=? AND deleted_at IS NULL`, deleted.Format(time.RFC3339Nano), deleted.Add(72*time.Hour).Format(time.RFC3339Nano), deleted.Format(time.RFC3339Nano), id, DefaultUserID)
	return requireOne(result, err)
}

func addTrackTx(ctx context.Context, tx *sql.Tx, playlistID, trackID, itemID string) error {
	var position int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(position),-1)+1 FROM playlist_items WHERE playlist_id=?`, playlistID).Scan(&position); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO playlist_items(id,playlist_id,track_id,position,created_at) VALUES(?,?,?,?,?)`, itemID, playlistID, trackID, position, nowText())
	return err
}

func compactPlaylistTx(ctx context.Context, tx *sql.Tx, playlistID string) error {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM playlist_items WHERE playlist_id=? ORDER BY position`, playlistID)
	if err != nil {
		return err
	}
	var itemIDs []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		itemIDs = append(itemIDs, id)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for position, id := range itemIDs {
		if _, err = tx.ExecContext(ctx, `UPDATE playlist_items SET position=? WHERE id=?`, position, id); err != nil {
			return err
		}
	}
	return nil
}

type scanner interface{ Scan(...any) error }

func scanTrashTrack(row scanner) (Track, error) {
	var track Track
	var end sql.NullFloat64
	var deleted, eligible sql.NullString
	err := row.Scan(&track.ID, &track.Title, &track.Artist, &track.Album, &track.ContentType, &track.SizeBytes, &track.DurationSeconds, &track.BaseRandomWeight, &track.PlaybackStartSeconds, &end, &track.VolumeRatio, &track.Lyrics, &track.OriginalPath, &track.PlayablePath, &track.AudioVersion, &track.FileName, &track.UploadedAt, &deleted, &eligible, &track.PurgeError)
	if end.Valid {
		track.PlaybackEndSeconds = &end.Float64
	}
	if deleted.Valid {
		track.DeletedAt = &deleted.String
	}
	if eligible.Valid {
		track.PurgeEligibleAt = &eligible.String
	}
	return track, err
}

func requireOne(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func nowText() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func (s *Store) String() string { return fmt.Sprintf("Store(%p)", s.db) }
