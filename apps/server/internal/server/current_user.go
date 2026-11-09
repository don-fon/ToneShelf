package server

import (
	"net/http"

	"music-player/server/internal/currentuser"
	"music-player/server/internal/store"
)

// UserResolver will be supplied by a trusted authentication/session adapter.
// The initial app uses the bootstrap account and ignores client-supplied IDs.
type UserResolver func(*http.Request) (string, error)

func (a *API) withCurrentUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := store.DefaultUserID
		if a.ResolveUser != nil {
			var err error
			id, err = a.ResolveUser(r)
			if err != nil {
				writeError(w, http.StatusUnauthorized, err)
				return
			}
		}
		ctx := currentuser.WithID(r.Context(), id)
		if err := currentuser.Require(ctx); err != nil {
			writeError(w, http.StatusUnauthorized, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
func (a *API) currentUser(w http.ResponseWriter, r *http.Request) {
	id := currentuser.ID(r.Context())
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "isDefault": id == store.DefaultUserID})
}
