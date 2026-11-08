package store

import (
	"context"
	"errors"
)

var ErrQuotaExceeded = errors.New("存储空间不足")

type UploadJob struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	FileName      string `json:"fileName"`
	RequestKey    string `json:"requestKey,omitempty"`
	TargetTrackID string `json:"targetTrackId,omitempty"`
	TrackID       string `json:"trackId,omitempty"`
	SourcePath    string `json:"-"`
	OutputPath    string `json:"-"`
	State         string `json:"state"`
	ReservedBytes int64  `json:"reservedBytes"`
	Error         string `json:"error,omitempty"`
	CreatedAt     string `json:"createdAt"`
}

// Includes all candidate files and reservations. A reservation is acquired
// before bytes reach disk, so concurrent writers cannot exceed the quota.
const usageSQL = `(SELECT COALESCE(SUM(size_bytes),0) FROM tracks)+(SELECT COALESCE(SUM(size_bytes),0) FROM pending_file_deletions)+(SELECT COALESCE(SUM(reserved_bytes),0) FROM upload_jobs)`

func (s *Store) CreateJob(ctx context.Context, job UploadJob) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO upload_jobs(id,owner_user_id,file_name,request_key,target_track_id,source_path,output_path,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,'receiving',?,?)`, job.ID, DefaultUserID, job.FileName, job.RequestKey, job.TargetTrackID, job.SourcePath, job.OutputPath, nowText(), nowText())
	return err
}

func (s *Store) Jobs(ctx context.Context) ([]UploadJob, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,kind,file_name,request_key,target_track_id,track_id,source_path,output_path,state,reserved_bytes,error,created_at FROM upload_jobs WHERE owner_user_id=? ORDER BY created_at DESC,id`, DefaultUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]UploadJob, 0)
	for rows.Next() {
		var job UploadJob
		if err = rows.Scan(&job.ID, &job.Kind, &job.FileName, &job.RequestKey, &job.TargetTrackID, &job.TrackID, &job.SourcePath, &job.OutputPath, &job.State, &job.ReservedBytes, &job.Error, &job.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (s *Store) ReserveJob(ctx context.Context, id string, bytes, limit int64) error {
	if bytes < 0 {
		return errors.New("invalid reservation")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE upload_jobs SET reserved_bytes=reserved_bytes+?,updated_at=? WHERE id=? AND owner_user_id=? AND state IN ('receiving','queued','processing') AND (`+usageSQL+`)<=?-?`, bytes, nowText(), id, DefaultUserID, limit, bytes)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrQuotaExceeded
	}
	return nil
}

// Grant a batch before writing. Near the quota boundary grant only what is
// still available, provided it covers the next write; never reject a small
// valid file merely because a full preferred batch no longer fits.
func (s *Store) ReserveUploadBatch(ctx context.Context, id string, minimum, preferred, limit int64) (int64, error) {
	if minimum <= 0 || preferred < minimum {
		return 0, errors.New("invalid reservation")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var used int64
	if err = tx.QueryRowContext(ctx, `SELECT `+usageSQL).Scan(&used); err != nil {
		return 0, err
	}
	available := limit - used
	if available < minimum {
		return 0, ErrQuotaExceeded
	}
	grant := min(preferred, available)
	result, err := tx.ExecContext(ctx, `UPDATE upload_jobs SET reserved_bytes=reserved_bytes+?,updated_at=? WHERE id=? AND owner_user_id=? AND state='receiving'`, grant, nowText(), id, DefaultUserID)
	if err = requireOne(result, err); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return grant, nil
}

// File reception is durable before this transition. Release unused headroom
// and expose the job to workers in the same transaction.
func (s *Store) QueueUpload(ctx context.Context, id string, size int64) error {
	if size < 0 {
		return errors.New("invalid upload size")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE upload_jobs SET state='queued',reserved_bytes=?,updated_at=? WHERE id=? AND owner_user_id=? AND state='receiving' AND reserved_bytes>=?`, size, nowText(), id, DefaultUserID, size)
	return requireOne(result, err)
}

func (s *Store) SetJobState(ctx context.Context, id, from, to, failure string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE upload_jobs SET state=?,error=?,updated_at=? WHERE id=? AND owner_user_id=? AND state=?`, to, failure, nowText(), id, DefaultUserID, from)
	return requireOne(result, err)
}

// Only call after physically removing the corresponding candidate bytes.
func (s *Store) SetJobReservation(ctx context.Context, id string, bytes int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE upload_jobs SET reserved_bytes=?,updated_at=? WHERE id=? AND owner_user_id=?`, bytes, nowText(), id, DefaultUserID)
	return err
}

func (s *Store) ReservedBytes(ctx context.Context) (int64, error) {
	var bytes int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(reserved_bytes),0) FROM upload_jobs`).Scan(&bytes)
	return bytes, err
}

func (s *Store) Quota(ctx context.Context) (used, reserved int64, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT (SELECT COALESCE(SUM(size_bytes),0) FROM tracks)+(SELECT COALESCE(SUM(size_bytes),0) FROM pending_file_deletions),(SELECT COALESCE(SUM(reserved_bytes),0) FROM upload_jobs)`).Scan(&used, &reserved)
	return
}

func (s *Store) KnownMediaPaths(ctx context.Context) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT original_path FROM tracks UNION SELECT playable_path FROM tracks UNION SELECT path FROM pending_file_deletions UNION SELECT source_path FROM upload_jobs UNION SELECT output_path FROM upload_jobs UNION SELECT output_path||'.stream' FROM upload_jobs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	known := make(map[string]bool)
	for rows.Next() {
		var path string
		if err = rows.Scan(&path); err != nil {
			return nil, err
		}
		known[path] = true
	}
	return known, rows.Err()
}

func (s *Store) QueueOrphan(ctx context.Context, path string, bytes int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO pending_file_deletions(path,size_bytes,created_at) VALUES(?,?,?)`, path, bytes, nowText())
	return err
}

// Activating files and exchanging reservations for committed usage must be one
// transaction. A crash before commit leaves a recoverable processing job.
func (s *Store) CompleteJob(ctx context.Context, job UploadJob, track NewTrack, oldFiles []PendingFile) (Track, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Track{}, err
	}
	defer tx.Rollback()
	var state string
	var reserved int64
	if err = tx.QueryRowContext(ctx, `SELECT state,reserved_bytes FROM upload_jobs WHERE id=? AND owner_user_id=?`, job.ID, DefaultUserID).Scan(&state, &reserved); err != nil {
		return Track{}, err
	}
	if state != "processing" || track.SizeBytes > reserved {
		return Track{}, errors.New("invalid job completion")
	}
	id := track.ID
	if job.TargetTrackID == "" {
		err = insertTrackTx(ctx, tx, track)
	} else {
		id = job.TargetTrackID
		err = replaceTrackTx(ctx, tx, id, track, oldFiles)
	}
	if err != nil {
		return Track{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE upload_jobs SET state='succeeded',track_id=?,reserved_bytes=0,error='',updated_at=? WHERE id=? AND state='processing'`, id, nowText(), job.ID)
	if err = requireOne(result, err); err != nil {
		return Track{}, err
	}
	if err = tx.Commit(); err != nil {
		return Track{}, err
	}
	return s.Track(ctx, id)
}
