package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"music-player/server/internal/store"
)

func TestMoveTrackEndpoint(t *testing.T) {
	ctx := testContext()
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.CreatePlaylist(ctx, "target", "Target"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.InsertTrack(ctx, store.NewTrack{ID: "song", Title: "Song", OriginalPath: "original", PlayablePath: "playable", SizeBytes: 1, DurationSeconds: 10}); err != nil {
		t.Fatal(err)
	}
	api := &API{ResolveUser: testUserResolver, Store: s}
	for attempt := 0; attempt < 2; attempt++ {
		response := httptest.NewRecorder()
		api.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/playlists/"+store.DefaultPlaylistID+"/tracks/song/move", strings.NewReader(`{"targetPlaylistId":"target"}`)))
		if response.Code != http.StatusNoContent {
			t.Fatalf("move %d %s", response.Code, response.Body.String())
		}
	}
	lists, err := s.Playlists(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, list := range lists {
		want := 0
		if list.ID == "target" {
			want = 1
		}
		if len(list.Tracks) != want {
			t.Fatalf("unexpected relationships %#v", list)
		}
	}
}
