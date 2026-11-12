package server

import (
	"encoding/json"
	"music-player/server/internal/store"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrackNotesAPI(t *testing.T) {
	ctx := testContext()
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "notes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.InsertTrack(ctx, store.NewTrack{ID: "song", Title: "song", OriginalPath: "a", PlayablePath: "a", DurationSeconds: 10}); err != nil {
		t.Fatal(err)
	}
	api := &API{Store: s, ResolveUser: testUserResolver}
	for _, note := range []any{"多行\n备注 🎵", nil, ""} {
		body := map[string]any{"baseRandomWeight": 1, "playbackStartSeconds": 0, "volumeRatio": .8, "lyrics": ""}
		if note != nil {
			body["note"] = note
		}
		raw, _ := json.Marshal(body)
		out := httptest.NewRecorder()
		api.Handler().ServeHTTP(out, httptest.NewRequest("PATCH", "/api/tracks/song", strings.NewReader(string(raw))))
		var got store.Track
		if err = json.Unmarshal(out.Body.Bytes(), &got); err != nil || out.Code != 200 {
			t.Fatalf("update: %d %s %v", out.Code, out.Body, err)
		}
		want := "多行\n备注 🎵"
		if note == "" {
			want = ""
		}
		if got.Note != want {
			t.Fatalf("note=%q want %q", got.Note, want)
		}
	}
}
