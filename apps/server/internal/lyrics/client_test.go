package lyrics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearchExpandsSparseArtistResults(t *testing.T) {
	for _, test := range []struct {
		name         string
		narrow       []record
		wantRequests int
		wantExpanded bool
		wantCount    int
	}{
		{"empty", nil, 2, true, 2},
		{"one", []record{{ID: 1, TrackName: "song", ArtistName: "original", Duration: 10, PlainLyrics: stringPointer("lyrics")}}, 2, true, 2},
		{"duplicates and unusable", []record{
			{ID: 1, TrackName: "song", ArtistName: "original", Duration: 10, PlainLyrics: stringPointer("lyrics")},
			{ID: 3, TrackName: "song", ArtistName: "original", Duration: 10, PlainLyrics: stringPointer("lyrics")},
			{ID: 4, TrackName: "song", ArtistName: "instrumental", Instrumental: true},
			{ID: 5, TrackName: "song", ArtistName: "no lyrics"},
		}, 2, true, 2},
		{"enough", []record{
			{ID: 1, TrackName: "song", ArtistName: "a", PlainLyrics: stringPointer("lyrics")},
			{ID: 2, TrackName: "song", ArtistName: "b", PlainLyrics: stringPointer("lyrics")},
			{ID: 3, TrackName: "song", ArtistName: "c", PlainLyrics: stringPointer("lyrics")},
		}, 1, false, 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.URL.Query().Get("track_name") != "song" {
					t.Error("lost title")
				}
				if requests == 1 {
					if r.URL.Query().Get("artist_name") != "original" {
						t.Error("lost artist")
					}
					_ = json.NewEncoder(w).Encode(test.narrow)
				} else {
					if _, present := r.URL.Query()["artist_name"]; present {
						t.Error("fallback still filters artist")
					}
					_ = json.NewEncoder(w).Encode([]record{
						{ID: 2, TrackName: "song", ArtistName: "cover", Duration: 10, PlainLyrics: stringPointer("lyrics")},
						{ID: 1, TrackName: "song", ArtistName: "original", Duration: 10, PlainLyrics: stringPointer("lyrics")},
					})
				}
			}))
			defer provider.Close()
			result, err := New(provider.URL).Search(context.Background(), SearchInput{Title: "song", Artist: "original", DurationSeconds: 10})
			if err != nil {
				t.Fatal(err)
			}
			if requests != test.wantRequests || result.ExpandedSearch != test.wantExpanded || len(result.Candidates) != test.wantCount {
				t.Fatalf("requests=%d result=%#v", requests, result)
			}
			if test.wantExpanded && (result.Candidates[0].ArtistName != "original" || result.QueryArtist != "original") {
				t.Fatalf("lost original artist scoring: %#v", result)
			}
		})
	}
}

func TestSearchFallbackFailurePreservesExistingCandidates(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "existing"}[existing], func(t *testing.T) {
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("artist_name") == "" {
					w.WriteHeader(http.StatusBadGateway)
					return
				}
				if existing {
					_ = json.NewEncoder(w).Encode([]record{{ID: 1, TrackName: "song", ArtistName: "artist", PlainLyrics: stringPointer("lyrics")}})
				} else {
					_, _ = w.Write([]byte("[]"))
				}
			}))
			defer provider.Close()
			result, err := New(provider.URL).Search(context.Background(), SearchInput{Title: "song", Artist: "artist"})
			if existing && (err != nil || len(result.Candidates) != 1) {
				t.Fatalf("lost existing result: %#v %v", result, err)
			}
			if !existing && err == nil {
				t.Fatal("failed fallback reported as empty success")
			}
		})
	}
}

func TestSearchWithoutArtistDoesNotRepeatQuery(t *testing.T) {
	requests := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++; _, _ = w.Write([]byte("[]")) }))
	defer provider.Close()
	result, err := New(provider.URL).Search(context.Background(), SearchInput{Title: "song"})
	if err != nil || requests != 1 || result.ExpandedSearch {
		t.Fatalf("requests=%d result=%#v err=%v", requests, result, err)
	}
}

func stringPointer(value string) *string { return &value }

func TestSearchParsesFilenameAndRanksCandidates(t *testing.T) {
	var gotTitle, gotArtist string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gotTitle == "" {
			gotTitle, gotArtist = r.URL.Query().Get("track_name"), r.URL.Query().Get("artist_name")
		}
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
