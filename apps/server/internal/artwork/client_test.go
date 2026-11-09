package artwork

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const releaseA = "11111111-1111-4111-8111-111111111111"
const releaseB = "22222222-2222-4222-8222-222222222222"

func TestSearchDeduplicatesArtAndDownloadsOnlyTrustedImages(t *testing.T) {
	var server *httptest.Server
	calls := 0
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("User-Agent"), "MusicPlayer") {
			t.Error("missing descriptive user agent")
		}
		switch {
		case strings.HasPrefix(r.URL.Path, "/ws/2/release/"):
			json.NewEncoder(w).Encode(map[string]any{"releases": []any{map[string]any{"id": releaseA, "title": "Album", "artist-credit": []any{map[string]any{"name": "Singer"}}}, map[string]any{"id": releaseB, "title": "Album", "artist-credit": []any{map[string]any{"name": "Singer"}}}}})
		case strings.HasPrefix(r.URL.Path, "/ws/2/recording/"):
			json.NewEncoder(w).Encode(map[string]any{"recordings": []any{map[string]any{"title": "Song", "length": 120000, "artist-credit": []any{map[string]any{"name": "Singer"}}, "releases": []any{map[string]any{"id": releaseA, "title": "Album"}}}}})
		case strings.HasPrefix(r.URL.Path, "/release/"):
			calls++
			json.NewEncoder(w).Encode(map[string]any{"images": []any{map[string]any{"front": true, "thumbnails": map[string]string{"500": server.URL + "/image"}}}})
		case r.URL.Path == "/image":
			w.Write([]byte("image bytes"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewWithEndpoints(server.URL, server.URL, 0)
	result, err := client.Search(context.Background(), Input{Title: "Song", Artist: "Singer", Album: "Album", DurationSeconds: 120})
	if err != nil || len(result.Candidates) != 1 || result.Candidates[0].ID != releaseA || result.Candidates[0].Score != 100 {
		t.Fatalf("result=%+v %v", result, err)
	}
	var image bytes.Buffer
	if err = client.Download(context.Background(), releaseA, &image); err != nil || image.String() != "image bytes" {
		t.Fatalf("download=%s %v", image.String(), err)
	}
	if calls != 2 {
		t.Fatalf("cover metadata not cached: calls=%d", calls)
	}
	if err = client.Download(context.Background(), "https://untrusted.example/cover", &image); err == nil {
		t.Fatal("arbitrary address accepted")
	}
}

func TestUntrustedImageAndRedirectAreRejected(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if redirect {
				http.Redirect(w, r, "http://127.0.0.1:1/private", 302)
			} else {
				json.NewEncoder(w).Encode(map[string]any{"images": []any{map[string]any{"front": true, "image": "http://127.0.0.1:1/private"}}})
			}
		}))
		client := NewWithEndpoints(server.URL, server.URL, 0)
		if err := client.Download(context.Background(), releaseA, &bytes.Buffer{}); err == nil {
			t.Fatal("untrusted host accepted")
		}
		server.Close()
	}
}

func TestServiceFailureIsNotReportedAsNoMatchesAndCancellationStopsRetry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := NewWithEndpoints(server.URL, server.URL, 0).Search(ctx, Input{Title: "Song", Artist: "Singer"}); err == nil {
		t.Fatal("503 treated as an empty result")
	}
}

func TestFilenameTermsAndArtistIdentityScore(t *testing.T) {
	title, _, hint := terms(Input{Title: "【字幕】OP「Stella」__SEKAI_NO_OWARI【无损】", Artist: "世终乐队"})
	if title != "Stella" || hint != "SEKAI NO OWARI" {
		t.Fatalf("terms=%q %q", title, hint)
	}
	title, _, hint = terms(Input{Title: "ryo _ 初音未来 - ワールドイズマイン (World is Mine)_H"})
	if title != "ワールドイズマイン" || hint != "ryo" {
		t.Fatalf("terms=%q %q", title, hint)
	}
	title, _, _ = terms(Input{Title: "MUKANJYO(TV size)"})
	if title != "MUKANJYO" {
		t.Fatal(title)
	}
	if similarity("さユり", "岩田さゆり") >= .5 {
		t.Fatal("different artist treated as exact identity")
	}
	_, _, hint = terms(Input{Title: "THERE IS A REASON", Artist: "鈴木このみ"})
	if hint != "" {
		t.Fatal("ordinary song title used as an artist hint")
	}
}
