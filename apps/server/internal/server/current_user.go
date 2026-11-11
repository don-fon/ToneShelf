package server

import (
	"errors"
	"net/http"
	"time"

	"music-player/server/internal/currentuser"
	"music-player/server/internal/store"
)

// UserResolver is an optional trusted server adapter, never a client-supplied ID.
type UserResolver func(*http.Request) (string, error)

func (a *API) withCurrentUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		var id string
		var err error
		if a.ResolveUser != nil {
			id, err = a.ResolveUser(r)
		} else if a.Store != nil {
			cookie, cookieErr := r.Cookie(sessionCookie)
			if cookieErr == nil {
				id, err = a.Store.SessionUser(r.Context(), cookie.Value, time.Now())
				if err != nil && !errors.Is(err, store.ErrInvalidCredential) {
					writeError(w, http.StatusServiceUnavailable, errors.New("登录服务暂时不可用"))
					return
				}
			} else {
				err = store.ErrInvalidCredential
			}
		}
		if err != nil || id == "" {
			w.Header().Set("Cache-Control", "no-store")
			writeError(w, http.StatusUnauthorized, errors.New("请先登录"))
			return
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
