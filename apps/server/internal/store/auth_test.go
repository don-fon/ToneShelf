package store

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestAccountSessionsPreserveLibraryAndSurviveRestart(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "music.db")
	s, err := Open(ctx, path, WithDataRoot(root))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.AuthenticateAccount(ctx, "default-user"); err == nil {
		t.Fatal("bootstrap identity allowed login")
	}
	track, err := s.InsertTrack(testContext(), NewTrack{ID: "existing", Title: "Existing", OriginalPath: filepath.Join(root, "media", "originals", "existing.mp3"), PlayablePath: filepath.Join(root, "media", "originals", "existing.mp3"), SizeBytes: 100, DurationSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetAccount(ctx, DefaultUserID, "", "my account", true); err != nil {
		t.Fatal(err)
	}
	id, proof, err := s.AuthenticateAccount(ctx, "my account")
	if err != nil || id != DefaultUserID {
		t.Fatalf("auth %s %v", id, err)
	}
	now := time.Now()
	token, err := s.CreateSession(ctx, id, proof, now)
	if err != nil {
		t.Fatal(err)
	}
	var verifier, storedToken []byte
	s.db.QueryRow(`SELECT verifier FROM user_credentials WHERE user_id=?`, id).Scan(&verifier)
	s.db.QueryRow(`SELECT token_hash FROM login_sessions WHERE user_id=?`, id).Scan(&storedToken)
	if bytes.Equal(verifier, []byte("my account")) || bytes.Equal(storedToken, []byte(token)) {
		t.Fatal("plaintext credential persisted")
	}
	if err = s.SetAccount(ctx, "duplicate", "", "my account", true); err == nil {
		t.Fatal("duplicate credential accepted")
	}
	users, _ := s.UserIDs(ctx)
	if len(users) != 1 {
		t.Fatal("failed account creation was not rolled back")
	}
	s.Close()
	s, err = Open(ctx, path, WithDataRoot(root))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, err := s.SessionUser(ctx, token, now); err != nil || got != id {
		t.Fatalf("restart lost session %s %v", got, err)
	}
	if _, err = s.SessionUser(ctx, token, now.Add(SessionLifetime)); err == nil {
		t.Fatal("expired session accepted")
	}
	saved, err := s.Track(testContext(), track.ID)
	if err != nil || saved.Title != track.Title || saved.OriginalPath != track.OriginalPath {
		t.Fatalf("library changed %+v %v", saved, err)
	}
	if err = s.SetAccount(ctx, id, "", "replacement", false); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SessionUser(ctx, token, now); err == nil {
		t.Fatal("reset retained session")
	}
	if _, err = s.CreateSession(ctx, id, proof, now); err == nil {
		t.Fatal("reset credential could create a session after authentication race")
	}
	if _, _, err = s.AuthenticateAccount(ctx, "my account"); err == nil {
		t.Fatal("old credential accepted")
	}
	id, proof, err = s.AuthenticateAccount(ctx, "replacement")
	if err != nil {
		t.Fatal(err)
	}
	token, err = s.CreateSession(ctx, id, proof, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DisableAccount(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SessionUser(ctx, token, now); err == nil {
		t.Fatal("disabled account session accepted")
	}
	if _, _, err = s.AuthenticateAccount(ctx, "replacement"); err == nil {
		t.Fatal("disabled account logged in")
	}
	if _, err = s.Track(testContext(), track.ID); err != nil {
		t.Fatal("disable removed media")
	}
}
