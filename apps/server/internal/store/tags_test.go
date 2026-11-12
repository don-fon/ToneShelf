package store

import (
	"context"
	"encoding/json"
	"github.com/pressly/goose/v3"
	"music-player/server/internal/currentuser"
	"path/filepath"
	"testing"
	"time"
)

func TestTagNames(t *testing.T) {
	for _, name := range []string{"日语", "Rock", "カナ", "日AB", "1234", "a b", "日-", "🎵AB", "👩🏽‍💻AB", "🇯🇵AB", "éABC", "ｶﾅAB", "<>&\""} {
		if err := ValidateTagName(name); err != nil {
			t.Errorf("%q: %v", name, err)
		}
	}
	for _, name := range []string{"五个字标签", "中文歌", "日ABC", "ABCDE", "无", " 无 ", "", "   ", "🎵🎵🎵", "́"} {
		if ValidateTagName(name) == nil {
			t.Errorf("accepted %q", name)
		}
	}
}
func TestTagGroupNameLimits(t *testing.T) {
	s, err := Open(testContext(), filepath.Join(t.TempDir(), "groups.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i, name := range []string{"日语歌曲", "Rock", "🎵🎵🎵🎵"} {
		id := []string{"han", "latin", "unicode"}[i]
		if err := s.SaveTagGroup(testContext(), id, name, []string{UntaggedID}, true); err != nil {
			t.Fatalf("valid %q: %v", name, err)
		}
	}
	for _, name := range []string{"", "   ", "五个字的组", "ABCDE", "🎵🎵🎵🎵🎵"} {
		if err := s.SaveTagGroup(testContext(), "invalid", name, nil, true); err == nil {
			t.Fatalf("accepted %q", name)
		}
		if err := s.SaveTagGroup(testContext(), "han", name, nil, false); err == nil {
			t.Fatalf("updated to %q", name)
		}
	}
	lib, err := s.Library(testContext())
	if err != nil {
		t.Fatal(err)
	}
	if len(lib.Groups) != 4 {
		t.Fatalf("invalid creates changed library: %+v", lib.Groups)
	}
	for _, group := range lib.Groups {
		if group.ID == "han" && (group.Name != "日语歌曲" || len(group.TagIDs) != 1 || group.TagIDs[0] != UntaggedID) {
			t.Fatalf("rejected update changed existing group: %+v", group)
		}
	}
}
func TestTagLibraryIsolationAtomicityAndRecovery(t *testing.T) {
	ctx := testContext()
	path := filepath.Join(t.TempDir(), "tags.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	for _, id := range []string{"a", "b"} {
		if _, err = s.InsertTrack(ctx, NewTrack{ID: id, Title: id, OriginalPath: id, PlayablePath: id, DurationSeconds: 10, SizeBytes: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.SaveTag(ctx, "jp", "日语", true); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveTag(ctx, "rock", "Rock", true); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveTag(ctx, "duplicate", "rock", true); err == nil {
		t.Fatal("duplicate case-insensitive name accepted")
	}
	if err = s.SetTrackTags(ctx, []string{"a", "b"}, []string{"jp"}, "add"); err != nil {
		t.Fatal(err)
	}
	if err = s.SetTrackTags(ctx, []string{"a"}, []string{"rock"}, "add"); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveTagGroup(ctx, "mix", "混合", []string{"jp", UntaggedID}, true); err != nil {
		t.Fatal(err)
	}
	library, err := s.Library(ctx)
	if err != nil || len(library.Tracks) != 2 || len(library.Tracks[0].TagIDs) != 2 || len(library.Groups) != 2 {
		t.Fatalf("library=%+v err=%v", library, err)
	}
	if err = s.EnsureUser(context.Background(), "other", "Other", "other-default"); err != nil {
		t.Fatal(err)
	}
	other := currentuser.WithID(context.Background(), "other")
	if err = s.SaveTag(other, "secret", "私密", true); err != nil {
		t.Fatal(err)
	}
	if err = s.SetTrackTags(ctx, []string{"a"}, []string{"secret"}, "replace"); err == nil {
		t.Fatal("foreign tag accepted")
	}
	if err = s.SetTrackTags(other, []string{"a"}, []string{"secret"}, "replace"); err == nil {
		t.Fatal("foreign song accepted")
	}
	if err = s.SaveTagGroup(other, "mix", "覆写", nil, false); err == nil {
		t.Fatal("foreign group updated")
	}
	if err = s.DeleteTag(other, "jp"); err == nil {
		t.Fatal("foreign tag deleted")
	}
	if err = s.SetTrackTags(ctx, []string{"a", "missing"}, nil, "replace"); err == nil {
		t.Fatal("partial batch committed")
	}
	a, _ := s.Track(ctx, "a")
	if len(a.TagIDs) != 2 {
		t.Fatal("batch rollback lost tags")
	}
	invalid := `["missing"]`
	title := "changed"
	if _, err = s.UpdateTrack(ctx, "a", 1, 0, nil, .8, "", &title, nil, nil, nil, &invalid); err == nil {
		t.Fatal("invalid tags accepted in settings")
	}
	a, _ = s.Track(ctx, "a")
	if a.Title != "a" || len(a.TagIDs) != 2 {
		t.Fatal("settings and tags were not atomic")
	}
	valid := `["rock"]`
	if _, err = s.UpdateTrack(ctx, "a", 1, 0, nil, .8, "", &title, nil, nil, nil, &valid); err != nil {
		t.Fatal(err)
	}
	if err = s.SoftDeleteTrack(ctx, "a", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RestoreTrack(ctx, "a", "restored"); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	a, err = s.Track(ctx, "a")
	if err != nil || a.Title != title || len(a.TagIDs) != 1 || a.TagIDs[0] != "rock" {
		t.Fatalf("restore/restart lost data: %+v %v", a, err)
	}
	if err = s.DeleteTag(ctx, "jp"); err != nil {
		t.Fatal(err)
	}
	library, err = s.Library(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, track := range library.Tracks {
		if track.ID == "b" && len(track.TagIDs) != 0 {
			t.Fatal("tag deletion did not make song untagged")
		}
	}
	var group TagGroup
	for _, g := range library.Groups {
		if g.ID == "mix" {
			group = g
		}
		if g.IsDefault && len(g.TagIDs) != 2 {
			t.Fatal("default group did not update")
		}
	}
	if len(group.TagIDs) != 1 || group.TagIDs[0] != UntaggedID {
		t.Fatal("deleted tag remained in template")
	}
	if err = s.SaveTag(ctx, "new", "新", true); err != nil {
		t.Fatal(err)
	}
	library, _ = s.Library(ctx)
	for _, g := range library.Groups {
		if g.IsDefault && len(g.TagIDs) != 3 {
			t.Fatal("new tag missing from default group")
		}
		if g.ID == "mix" && len(g.TagIDs) != 1 {
			t.Fatal("new tag enabled in ordinary template")
		}
	}
}

func TestPlaylistTagMigration(t *testing.T) {
	ctx := testContext()
	path := filepath.Join(t.TempDir(), "migration.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err = goose.DownTo(s.db, ".", 15); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ExecContext(ctx, `INSERT INTO tracks(id,owner_user_id,title,original_path,playable_path,duration_seconds,size_bytes,created_at,updated_at,deleted_at) VALUES('a',?,'Original','a','b',10,2,?,?,?)`, DefaultUserID, nowText(), nowText(), nowText()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreatePlaylist(ctx, "long", "一个非常长的旧歌单名"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ExecContext(ctx, `INSERT INTO playlist_items(id,playlist_id,track_id,position,created_at) VALUES('member','long','a',0,?)`, nowText()); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	lib, err := s.Library(ctx)
	if err != nil || len(lib.Tags) != 1 || len(lib.Groups) != 2 || lib.Groups[1].Name != "一个非常长的旧歌单名" {
		t.Fatalf("migration: %+v %v", lib, err)
	}
	if ValidateTagName(lib.Tags[0].Name) != nil {
		t.Fatal("legacy name did not fit tag limit")
	}
	if _, err = s.RestoreTrack(ctx, "a", "restore"); err != nil {
		t.Fatal(err)
	}
	lib, _ = s.Library(ctx)
	if len(lib.Tracks) != 1 || lib.Tracks[0].Title != "Original" || len(lib.Tracks[0].TagIDs) != 1 {
		t.Fatal("trash membership lost")
	}
	before, _ := json.Marshal(lib)
	if err = s.migrate(ctx); err != nil {
		t.Fatal(err)
	}
	afterLib, _ := s.Library(ctx)
	after, _ := json.Marshal(afterLib)
	if string(before) != string(after) {
		t.Fatal("migration not idempotent")
	}
}

func TestTagMigrationFailureRollsBackAndCanRetry(t *testing.T) {
	ctx := testContext()
	s := openTestStore(t)
	if err := goose.DownTo(s.db, ".", 16); err != nil {
		t.Fatal(err)
	}
	// A preexisting conflicting ID makes the import fail after its transaction begins.
	if _, err := s.db.ExecContext(ctx, `INSERT INTO tag_groups(id,owner_user_id,name,created_at) VALUES(?,?,'conflict',?)`, "group:"+DefaultPlaylistID, DefaultUserID, nowText()); err != nil {
		t.Fatal(err)
	}
	if err := s.migrate(ctx); err == nil {
		t.Fatal("expected import failure")
	}
	version, err := goose.GetDBVersion(s.db)
	if err != nil || version != 16 {
		t.Fatalf("version=%d err=%v", version, err)
	}
	var count int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM tags`).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial tags survived rollback", err, count)
	}
	if _, err = s.db.Exec(`DELETE FROM tag_groups WHERE id=?`, "group:"+DefaultPlaylistID); err != nil {
		t.Fatal(err)
	}
	if err = s.migrate(ctx); err != nil {
		t.Fatal(err)
	}
	lib, err := s.Library(ctx)
	if err != nil || len(lib.Groups) != 1 || !lib.Groups[0].IsDefault {
		t.Fatalf("retry: %+v %v", lib, err)
	}
}
