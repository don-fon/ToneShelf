package store

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestRelativePathsMigrateAndSurviveDataRootMove(t *testing.T) {
	ctx := testContext()
	base := t.TempDir()
	oldRoot := filepath.Join(base, "old")
	newRoot := filepath.Join(base, "new")
	if err := os.MkdirAll(filepath.Join(oldRoot, "data"), 0700); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(oldRoot, "data", "music.db")
	s, err := Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	if err = goose.DownTo(s.db, ".", 11); err != nil {
		t.Fatal(err)
	}
	mediaPath := func(name string) string { return filepath.Join(oldRoot, "media", name) }
	n := NewTrack{ID: "song", Title: "song", OriginalPath: mediaPath("originals/song.flac"), PlayablePath: mediaPath("playable/song.mp3"), CoverPath: mediaPath("playable/song.jpg"), SizeBytes: 30, DurationSeconds: 10}
	if _, err = insertV11Track(s, n); err != nil {
		t.Fatal(err)
	}
	job := UploadJob{ID: "upload", FileName: "candidate.flac", TargetTrackID: n.ID, SourcePath: mediaPath("staging/upload.flac"), OutputPath: mediaPath("playable/upload.mp3")}
	if err = createV11Job(s, job); err != nil {
		t.Fatal(err)
	}
	candidate, _ := json.Marshal(NewTrack{ID: job.ID, OriginalPath: job.SourcePath, PlayablePath: job.OutputPath, CoverPath: job.OutputPath + ".cover.jpg", DurationSeconds: 10})
	if _, err = s.db.ExecContext(ctx, `UPDATE upload_jobs SET state='queued',pending_candidate=? WHERE id=?`, string(candidate), job.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.QueueOrphan(ctx, mediaPath("playable/obsolete.mp3"), 4); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, database, WithDataRoot(oldRoot))
	if err != nil {
		t.Fatal(err)
	}
	var original, playable, cover, source, output, pending string
	if err = s.db.QueryRow(`SELECT original_path,playable_path,cover_path FROM tracks WHERE id='song'`).Scan(&original, &playable, &cover); err != nil {
		t.Fatal(err)
	}
	if original != "media/originals/song.flac" || playable != "media/playable/song.mp3" || cover != "media/playable/song.jpg" {
		t.Fatalf("persisted paths=%q %q %q", original, playable, cover)
	}
	if err = s.db.QueryRow(`SELECT source_path,output_path,pending_candidate FROM upload_jobs WHERE id='upload'`).Scan(&source, &output, &pending); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pending, oldRoot) || source != "media/staging/upload.flac" || output != "media/playable/upload.mp3" {
		t.Fatalf("job paths=%s %s %s", source, output, pending)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(oldRoot, newRoot); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, filepath.Join(newRoot, "data", "music.db"), WithDataRoot(newRoot))
	if err != nil {
		t.Fatal(err)
	}
	track, err := s.Track(ctx, n.ID)
	if err != nil || track.OriginalPath != filepath.Join(newRoot, "media", "originals", "song.flac") {
		t.Fatalf("moved track=%+v %v", track, err)
	}
	lists, err := s.Playlists(ctx)
	if err != nil || lists[0].Tracks[0].CoverPath != track.CoverPath {
		t.Fatalf("moved playlist=%+v %v", lists, err)
	}
	jobs, err := s.Jobs(ctx)
	if err != nil || len(jobs) != 1 || jobs[0].SourcePath != filepath.Join(newRoot, "media", "staging", "upload.flac") || jobs[0].Candidate.CoverPath != filepath.Join(newRoot, "media", "playable", "upload.mp3.cover.jpg") {
		t.Fatalf("moved jobs=%+v %v", jobs, err)
	}
	files, err := s.PendingFiles(ctx)
	if err != nil || len(files) != 1 || files[0].Path != filepath.Join(newRoot, "media", "playable", "obsolete.mp3") {
		t.Fatalf("moved cleanup=%+v %v", files, err)
	}
	if err = s.CompletePendingFile(ctx, files[0].Path); err != nil {
		t.Fatal(err)
	}
	replacement := NewTrack{OriginalPath: filepath.Join(newRoot, "media", "originals", "new.mp3"), PlayablePath: filepath.Join(newRoot, "media", "originals", "new.mp3"), DurationSeconds: 10, SizeBytes: 20}
	if _, err = s.ReplaceTrack(ctx, track.ID, replacement, []PendingFile{{Path: track.OriginalPath, SizeBytes: 10}, {Path: track.PlayablePath, SizeBytes: 10}, {Path: track.CoverPath, SizeBytes: 10}}); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow(`SELECT original_path FROM tracks WHERE id='song'`).Scan(&original); err != nil || original != "media/originals/new.mp3" {
		t.Fatalf("new write=%s %v", original, err)
	}
	if _, err = s.InsertTrack(ctx, NewTrack{ID: "escape", OriginalPath: filepath.Join(base, "outside.mp3"), DurationSeconds: 1}); err == nil {
		t.Fatal("outside-root write accepted")
	}
	known, err := s.KnownMediaPaths(ctx)
	if err != nil || !known[replacement.OriginalPath] {
		t.Fatalf("known=%+v %v", known, err)
	}
}

func TestRelativePathMigrationRollsBackOnInvalidLegacyPath(t *testing.T) {
	ctx := testContext()
	root := t.TempDir()
	database := filepath.Join(root, "test.db")
	s, err := Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	if err = goose.DownTo(s.db, ".", 11); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(filepath.Dir(root), "outside.mp3")
	if _, err = insertV11Track(s, NewTrack{ID: "invalid", OriginalPath: outside, PlayablePath: outside, DurationSeconds: 1}); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(ctx, database, WithDataRoot(root)); err == nil {
		t.Fatal("unsafe migration accepted")
	}
	// Inspect without rerunning migrations: failed migration must not change paths or version.
	db, err := sql.Open("sqlite", database)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var got string
	var version int64
	if err = db.QueryRow(`SELECT original_path FROM tracks WHERE id='invalid'`).Scan(&got); err != nil || got != outside {
		t.Fatalf("rollback path=%s %v", got, err)
	}
	if err = db.QueryRow(`SELECT MAX(version_id) FROM goose_db_version WHERE is_applied=1`).Scan(&version); err != nil || version != 11 {
		t.Fatalf("rollback version=%d %v", version, err)
	}
}

func TestLegacyWorkingDirectoryPrefixAndWindowsSeparators(t *testing.T) {
	ctx := testContext()
	root := filepath.Join(t.TempDir(), "legacy-library")
	if err := os.MkdirAll(filepath.Join(root, "data"), 0700); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(root, "data", "music.db")
	s, err := Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	if err = goose.DownTo(s.db, ".", 11); err != nil {
		t.Fatal(err)
	}
	if _, err = insertV11Track(s, NewTrack{ID: "legacy", OriginalPath: `legacy-library\media\originals\song.mp3`, PlayablePath: "legacy-library/media/originals/song.mp3", DurationSeconds: 1}); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, database, WithDataRoot(root))
	if err != nil {
		t.Fatal(err)
	}
	track, err := s.Track(ctx, "legacy")
	if err != nil || track.OriginalPath != filepath.Join(root, "media", "originals", "song.mp3") || track.PlayablePath != track.OriginalPath {
		t.Fatalf("legacy prefix=%+v %v", track, err)
	}
}
func insertV11Track(s *Store, n NewTrack) (Track, error) {
	_, err := s.db.Exec(`INSERT INTO tracks(id,owner_user_id,title,original_path,playable_path,cover_path,size_bytes,duration_seconds,created_at,updated_at)VALUES(?,?,?,?,?,?,?,?,?,?)`, n.ID, DefaultUserID, n.Title, n.OriginalPath, n.PlayablePath, n.CoverPath, n.SizeBytes, n.DurationSeconds, nowText(), nowText())
	if err == nil {
		_, err = s.db.Exec(`INSERT INTO playlist_items(id,playlist_id,track_id,position,created_at)VALUES(?,?,?,0,?)`, n.ID+"-fixture", DefaultPlaylistID, n.ID, nowText())
	}
	return Track{}, err
}
func createV11Job(s *Store, j UploadJob) error {
	_, err := s.db.Exec(`INSERT INTO upload_jobs(id,owner_user_id,file_name,target_track_id,source_path,output_path,state,created_at,updated_at)VALUES(?,?,?,?,?,?,'receiving',?,?)`, j.ID, DefaultUserID, j.FileName, j.TargetTrackID, j.SourcePath, j.OutputPath, nowText(), nowText())
	return err
}
