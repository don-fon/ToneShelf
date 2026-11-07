package lyrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearchParsesFilenameAndRanksCandidates(t *testing.T) {
	var gotTitle, gotArtist string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTitle, gotArtist = r.URL.Query().Get("track_name"), r.URL.Query().Get("artist_name")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":1,"trackName":"ワールドイズマイン","artistName":"初音ミク/ryo (supercell)","albumName":"初音ミクベスト","duration":255,"plainLyrics":"plain"},
			{"id":3,"trackName":"ワールドイズマイン","artistName":"初音ミク/ryo (supercell)","albumName":"別の収録元","duration":255.4,"plainLyrics":"duplicate visible result"},
			{"id":2,"trackName":"ワールドイズマイン [World Is Mine]","artistName":"ryo","albumName":"supercell","duration":253,"plainLyrics":"plain","syncedLyrics":"[00:01.00] synced"}
		]`))
	}))
	defer provider.Close()

	client := New(provider.URL)
	result, err := client.Search(context.Background(), SearchInput{Title: "ryo _ 初音未来 - ワールドイズマイン (World is Mine)_H", DurationSeconds: 255.240726})
	if err != nil {
		t.Fatal(err)
	}
	if gotTitle != "ワールドイズマイン" || gotArtist != "ryo" {
		t.Fatalf("query title=%q artist=%q", gotTitle, gotArtist)
	}
	if result.QueryTitle != gotTitle || result.QueryArtist != gotArtist || len(result.Candidates) != 2 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.Candidates[0].ID != 3 || result.Candidates[1].ID != 2 || !result.Candidates[1].Synced {
		t.Fatalf("unexpected ranking: %#v", result.Candidates)
	}
}

func TestSearchRetriesOnceAfterRateLimit(t *testing.T) {
	requests := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer provider.Close()

	client := New(provider.URL)
	if _, err := client.Search(context.Background(), SearchInput{Title: "song"}); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests=%d", requests)
	}
}
