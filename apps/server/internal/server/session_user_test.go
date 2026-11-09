package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"music-player/server/internal/store"
)

func TestCurrentUserForPlaybackSessionUsesTrustedIdentity(t *testing.T) {
	for _, id := range []string{store.DefaultUserID, "other-user"} {
		api := &API{ResolveUser: func(*http.Request) (string, error) { return id, nil }}
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/current-user?userId=spoof", nil)
		request.Header.Set("X-User-ID", "spoof")
		api.Handler().ServeHTTP(response, request)
		var user struct {
			ID        string `json:"id"`
			IsDefault bool   `json:"isDefault"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &user); err != nil {
			t.Fatal(err)
		}
		if response.Code != 200 || user.ID != id || user.IsDefault != (id == store.DefaultUserID) || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("identity=%+v status=%d", user, response.Code)
		}
	}
}
