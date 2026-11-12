package server

import (
	"encoding/json"
	"music-player/server/internal/store"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestTagEndpoints(t *testing.T) {
	s, err := store.Open(testContext(), filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.InsertTrack(testContext(), store.NewTrack{ID: "song", Title: "Song", OriginalPath: "a", PlayablePath: "b", SizeBytes: 1, DurationSeconds: 10}); err != nil {
		t.Fatal(err)
	}
	api := &API{Store: s, ResolveUser: testUserResolver}
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRecorder()
		api.Handler().ServeHTTP(r, httptest.NewRequest(method, path, strings.NewReader(body)))
		return r
	}
	if out := request("POST", "/api/tags", `{"name":"中文歌"}`); out.Code != 400 {
		t.Fatal("accepted more than two fullwidth characters", out.Code)
	}
	if out := request("POST", "/api/tags", `{"name":"ABCDE"}`); out.Code != 400 {
		t.Fatal(out.Code, out.Body.String())
	}
	if out := request("POST", "/api/tags", `{"name":"日语"}`); out.Code != 204 {
		t.Fatal(out.Code, out.Body.String())
	}
	var lib store.Library
	json.Unmarshal(request("GET", "/api/library", "").Body.Bytes(), &lib)
	if len(lib.Tags) != 1 || len(lib.Tracks) != 1 || len(lib.Groups) != 1 {
		t.Fatalf("%+v", lib)
	}
	tag := lib.Tags[0].ID
	if out := request("PATCH", "/api/track-tags", `{"trackIds":["song"],"tagIds":["`+tag+`"],"mode":"add"}`); out.Code != 204 {
		t.Fatal(out.Code, out.Body.String())
	}
	if out := request("POST", "/api/tag-groups", `{"name":"日语组","tagIds":["`+tag+`"]}`); out.Code != 204 {
		t.Fatal(out.Code, out.Body.String())
	}
	if out := request("POST", "/api/tag-groups", `{"name":"五个字的组","tagIds":[]}`); out.Code != 400 {
		t.Fatal("accepted oversized group name", out.Code)
	}
	json.Unmarshal(request("GET", "/api/library", "").Body.Bytes(), &lib)
	if len(lib.Tracks[0].TagIDs) != 1 || len(lib.Groups) != 2 {
		t.Fatalf("%+v", lib)
	}
	if out := request("DELETE", "/api/tag-groups/"+lib.Groups[0].ID, ""); out.Code != 400 {
		t.Fatal("deleted default", out.Code)
	}
	if out := request("DELETE", "/api/tags/"+tag, ""); out.Code != 204 {
		t.Fatal(out.Code)
	}
	for _, name := range []string{"🎵AB", "a/b?", "<>&\""} {
		body, _ := json.Marshal(map[string]string{"name": name})
		if out := request("POST", "/api/tags", string(body)); out.Code != 204 {
			t.Fatal(name, out.Code, out.Body.String())
		}
	}
	json.Unmarshal(request("GET", "/api/library", "").Body.Bytes(), &lib)
	if len(lib.Tags) != 3 {
		t.Fatalf("unicode tags did not round-trip: %+v", lib.Tags)
	}
	anonymous := httptest.NewRecorder()
	(&API{Store: s}).Handler().ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, "/api/library", nil))
	if anonymous.Code != 401 {
		t.Fatal("anonymous library", anonymous.Code)
	}
}
