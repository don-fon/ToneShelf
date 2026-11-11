package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"music-player/server/internal/currentuser"
	"music-player/server/internal/store"
)

func TestAccountLoginProtectsAllResourcesAndLogoutRevokesSession(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	s, err := store.Open(ctx, filepath.Join(root, "music.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for id, account := range map[string]string{store.DefaultUserID: "first-account", "other": "second-account"} {
		if err = s.SetAccount(ctx, id, "", account, true); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, id+".mp3")
		os.WriteFile(path, []byte("private-audio"), 0600)
		if _, err = s.InsertTrack(currentuser.WithID(ctx, id), store.NewTrack{ID: id, Title: id, OriginalPath: path, PlayablePath: path, CoverPath: path, SizeBytes: 13, DurationSeconds: 10}); err != nil {
			t.Fatal(err)
		}
	}
	api := &API{Store: s}
	handler := api.Handler()
	request := func(method, path, body string, cookie *http.Cookie, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://music.test"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-User-ID", "other")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, r)
		return out
	}
	for _, path := range []string{"/api/current-user", "/api/playlists", "/api/jobs", "/api/quota", "/api/trash", "/api/tracks/default-user/audio", "/api/tracks/default-user/audio?source=original", "/api/tracks/default-user/cover"} {
		out := request("GET", path, "", nil, "")
		if out.Code != 401 || strings.Contains(out.Body.String(), "private-audio") {
			t.Fatalf("anonymous %s=%d", path, out.Code)
		}
	}
	if out := request("POST", "/api/playlists", `{"name":"bad"}`, nil, ""); out.Code != 401 {
		t.Fatal("anonymous write")
	}
	for _, path := range []string{"/api/register", "/api/auth/register", "/api/accounts"} {
		if out := request("POST", path, `{}`, nil, ""); out.Code < 400 {
			t.Fatal("registration available")
		}
	}
	if out := request("POST", "/api/auth/login", `{"account":"first-account"}`, nil, "http://evil.test"); out.Code != 403 {
		t.Fatal("cross-origin login")
	}
	if out := request("POST", "/api/auth/login", `{"account":"default-user"}`, nil, ""); out.Code != 401 {
		t.Fatal("user id is a login credential")
	}
	if out := request("POST", "/api/auth/login", `{"account":"wrong","userId":"default-user"}`, nil, ""); out.Code != 400 {
		t.Fatal("unrecognized identity accepted")
	}
	login := request("POST", "/api/auth/login", `{"account":"first-account"}`, nil, "http://music.test")
	if login.Code != 200 {
		t.Fatalf("login %d %s", login.Code, login.Body)
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing cookie")
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Secure || cookie.Path != "/" || cookie.Value == "first-account" {
		t.Fatalf("cookie flags %+v", cookie)
	}
	if strings.Contains(login.Body.String(), "first-account") || login.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("login credential leaked or cached")
	}
	var user map[string]any
	json.Unmarshal(request("GET", "/api/current-user?userId=other", "", cookie, "").Body.Bytes(), &user)
	if user["id"] != store.DefaultUserID {
		t.Fatal("client identity spoofing")
	}
	if out := request("GET", "/api/tracks/other/audio", "", cookie, ""); out.Code != 404 {
		t.Fatal("foreign audio exposed")
	}
	if out := request("GET", "/api/tracks/other/cover", "", cookie, ""); out.Code != 404 {
		t.Fatal("foreign cover exposed")
	}
	rangeRequest := httptest.NewRequest("GET", "http://music.test/api/tracks/default-user/audio", nil)
	rangeRequest.AddCookie(cookie)
	rangeRequest.Header.Set("Range", "bytes=0-6")
	audio := httptest.NewRecorder()
	handler.ServeHTTP(audio, rangeRequest)
	if audio.Code != 206 || !bytes.Equal(audio.Body.Bytes(), []byte("private")) || audio.Header().Get("Cache-Control") != "private, no-cache" {
		t.Fatalf("authenticated range %d %s", audio.Code, audio.Body)
	}
	if out := request("POST", "/api/playlists", `{"name":"bad"}`, cookie, "http://evil.test"); out.Code != 403 {
		t.Fatal("cross-origin write")
	}
	if out := request("POST", "/api/auth/logout", "", cookie, "http://evil.test"); out.Code != 403 {
		t.Fatal("cross-origin logout")
	}
	logout := request("POST", "/api/auth/logout", "", cookie, "http://music.test")
	if logout.Code != 204 || logout.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout failed")
	}
	if out := request("GET", "/api/playlists", "", cookie, ""); out.Code != 401 {
		t.Fatal("logout did not revoke session")
	}
	if out := request("GET", "/api/health", "", nil, ""); out.Code != 200 {
		t.Fatal("health requires account")
	}
}

func TestLoginLimitsAndCookieTransport(t *testing.T) {
	api := &API{}
	handler := api.Handler()
	for i := 0; i < 21; i++ {
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{}`)))
		if i == 20 && (out.Code != 429 || out.Header().Get("Retry-After") == "") {
			t.Fatalf("rate limit %d", out.Code)
		}
	}
	for _, remote := range []string{"127.0.0.1:1234", "[::1]:1234", "192.0.2.1:1234"} {
		r := httptest.NewRequest("GET", "http://music.test/", nil)
		r.RemoteAddr = remote
		r.Header.Set("X-Forwarded-Proto", "https")
		expected := remote != "192.0.2.1:1234"
		if cookieFor(r, "token", 1).Secure != expected {
			t.Fatalf("proxy trust %s", remote)
		}
	}
}

func TestAuthStorageFailureIsUnavailableRatherThanInvalidLogin(t *testing.T) {
	s, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "music.db"))
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	handler := (&API{Store: s}).Handler()
	for _, path := range []string{"/api/auth/login", "/api/playlists"} {
		method := "GET"
		if path == "/api/auth/login" {
			method = "POST"
		}
		request := httptest.NewRequest(method, path, strings.NewReader(`{"account":"account"}`))
		request.AddCookie(&http.Cookie{Name: sessionCookie, Value: strings.Repeat("a", 43)})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != 503 || strings.Contains(response.Body.String(), "database") {
			t.Fatalf("storage failure %d %s", response.Code, response.Body)
		}
	}
}
