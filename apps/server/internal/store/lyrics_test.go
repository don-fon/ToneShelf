package store

import (
	"path/filepath"
	"testing"
)

func TestEnsureLyricsOffset(t *testing.T) {
	for _, test := range []struct{ name, input, want string }{
		{"missing", "[00:01]line", "[offset:0]\n[00:01]line"},
		{"plain", "plain lyrics", "[offset:0]\nplain lyrics"},
		{"crlf and BOM", "\uFEFF[ti:name]\r\n[00:01]line\r\n", "\uFEFF[offset:0]\r\n[ti:name]\r\n[00:01]line\r\n"},
		{"cr", "[ti:name]\r[00:01]line", "[offset:0]\r[ti:name]\r[00:01]line"},
		{"manual advance", "[offset:+1250]\n[00:01]line", "[offset:+1250]\n[00:01]line"},
		{"manual delay", "\uFEFF \t[OFFSET:-500] \r\n[00:01]line", "\uFEFF \t[OFFSET:-500] \r\n[00:01]line"},
		{"duplicates preserved", "[offset:0]\n[offset:100]\n[00:01]line", "[offset:0]\n[offset:100]\n[00:01]line"},
		{"invalid", "[offset:no]\n[00:01]line", "[offset:0]\n[offset:no]\n[00:01]line"},
		{"unsafe", "[offset:9007199254740992]\n[00:01]line", "[offset:0]\n[offset:9007199254740992]\n[00:01]line"},
		{"overflow", "[offset:999999999999999999999]\nline", "[offset:0]\n[offset:999999999999999999999]\nline"},
		{"embedded text", "text [offset:1]", "[offset:0]\ntext [offset:1]"},
		{"empty", "", ""},
		{"whitespace", " \r\n\t", " \r\n\t"},
		{"empty BOM", "\uFEFF\r\n", "\uFEFF\r\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := ensureLyricsOffset(test.input)
			if got != test.want {
				t.Fatalf("got %q want %q", got, test.want)
			}
			if ensureLyricsOffset(got) != got {
				t.Fatal("repeated save inserted another tag")
			}
		})
	}
}

func TestSavedLyricsOffsetSurvivesRestartAndCanBeCleared(t *testing.T) {
	ctx := testContext()
	path := filepath.Join(t.TempDir(), "lyrics.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	old, err := s.InsertTrack(ctx, NewTrack{ID: "track", Title: "kept", DurationSeconds: 10})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.UpdateTrack(ctx, old.ID, 1, 0, nil, .8, "[00:01]line")
	if err != nil || got.Lyrics != "[offset:0]\n[00:01]line" || got.AudioVersion != old.AudioVersion {
		t.Fatalf("save=%#v %v", got, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	got, err = s.Track(ctx, old.ID)
	if err != nil || got.Lyrics != "[offset:0]\n[00:01]line" {
		t.Fatalf("restart=%#v %v", got, err)
	}
	manual := "[offset:-500]\r\n[00:01]line\r\n"
	got, err = s.UpdateTrack(ctx, old.ID, 1, 0, nil, .8, manual)
	if err != nil || got.Lyrics != manual {
		t.Fatalf("manual offset changed: %#v %v", got, err)
	}
	if _, err = s.UpdateTrack(ctx, old.ID, 0, 0, nil, .8, "replacement without tag"); err == nil {
		t.Fatal("invalid settings accepted")
	}
	got, _ = s.Track(ctx, old.ID)
	if got.Lyrics != manual {
		t.Fatal("failed save changed lyrics")
	}
	got, err = s.UpdateTrack(ctx, old.ID, 1, 0, nil, .8, "")
	if err != nil || got.Lyrics != "" {
		t.Fatalf("clear=%#v %v", got, err)
	}
}
