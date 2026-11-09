package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"music-player/server/internal/store"
)

func TestLyricsDisplayAPIValidatesAndReturnsSavedChoice(t *testing.T) {
	ctx := testContext()
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = s.InsertTrack(ctx, store.NewTrack{ID: "track", Title: "kept", DurationSeconds: 10})
	if err != nil {
		t.Fatal(err)
	}
	api := &API{Store: s}
	handler := api.Handler()
	for _, test := range []struct {
		layout string
		status int
	}{{"separate", 200}, {"end-timed", 400}, {"bogus", 400}} {
		response := httptest.NewRecorder()
		body := `{"baseRandomWeight":1,"volumeRatio":0.8,"lyrics":"[00:01]First","lyricsLayout":"` + test.layout + `","lyricsPrimaryLine":"last"}`
		request := httptest.NewRequest(http.MethodPatch, "/api/tracks/track", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
		if test.status == 200 {
			var got store.Track
			if err = json.Unmarshal(response.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.LyricsLayout != "separate" || got.LyricsPrimaryLine != "last" || got.Lyrics != "[offset:0]\n[00:01]First" {
				t.Fatalf("response=%#v", got)
			}
		}
	}
	current, err := s.Track(ctx, "track")
	if err != nil || current.LyricsLayout != "separate" {
		t.Fatalf("invalid request overwrote choice: %#v %v", current, err)
	}
}
