package server

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"time"

	"music-player/server/internal/store"
)

const sessionCookie = "music_session"

func sameOriginWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
			origin := r.Header.Get("Origin")
			if origin != "" {
				u, err := url.Parse(origin)
				scheme := "http"
				if secureRequest(r) {
					scheme = "https"
				}
				if err != nil || u.Host != r.Host || u.Scheme != scheme || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
					writeError(w, 403, errors.New("不允许跨站操作"))
					return
				}
			}
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				writeError(w, 403, errors.New("不允许跨站操作"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func secureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	return err == nil && net.ParseIP(host).IsLoopback() && r.Header.Get("X-Forwarded-Proto") == "https"
}
func sessionValue(r *http.Request) string {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		return cookie.Value
	}
	return ""
}
func cookieFor(r *http.Request, value string, maxAge int) *http.Cookie {
	return &http.Cookie{Name: sessionCookie, Value: value, Path: "/", HttpOnly: true, Secure: secureRequest(r), SameSite: http.SameSiteStrictMode, MaxAge: maxAge}
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	a.authMu.Lock()
	now := time.Now()
	if now.Sub(a.loginWindow) >= time.Minute {
		a.loginWindow = now
		a.loginCount = 0
	}
	if a.loginCount >= 20 || a.activeLogins >= 2 {
		a.authMu.Unlock()
		w.Header().Set("Retry-After", "60")
		writeError(w, 429, errors.New("登录尝试过于频繁，请稍后重试"))
		return
	}
	a.loginCount++
	a.activeLogins++
	a.authMu.Unlock()
	defer func() { a.authMu.Lock(); a.activeLogins--; a.authMu.Unlock() }()
	var input struct {
		Account string `json:"account"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, 400, errors.New("无法读取账号"))
		return
	}
	if a.Store == nil {
		writeError(w, 503, errors.New("登录服务不可用"))
		return
	}
	id, proof, err := a.Store.AuthenticateAccount(r.Context(), input.Account)
	if err != nil {
		writeLoginError(w, err)
		return
	}
	token, err := a.Store.CreateSession(r.Context(), id, proof, time.Now())
	if err != nil {
		writeLoginError(w, err)
		return
	}
	if old := sessionValue(r); old != "" {
		if err = a.Store.DeleteSession(r.Context(), old); err != nil {
			writeError(w, 500, errors.New("无法更新登录会话"))
			return
		}
	}
	http.SetCookie(w, cookieFor(r, token, int(store.SessionLifetime.Seconds())))
	writeJSON(w, 200, map[string]any{"id": id, "isDefault": id == store.DefaultUserID})
}
func writeLoginError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrInvalidCredential) {
		writeError(w, http.StatusUnauthorized, store.ErrInvalidCredential)
		return
	}
	writeError(w, http.StatusServiceUnavailable, errors.New("登录服务暂时不可用，请稍后重试"))
}
func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if token := sessionValue(r); token != "" && a.Store != nil {
		if err := a.Store.DeleteSession(r.Context(), token); err != nil {
			writeError(w, 500, errors.New("退出登录失败"))
			return
		}
	}
	cookie := cookieFor(r, "", -1)
	cookie.Expires = time.Unix(1, 0)
	http.SetCookie(w, cookie)
	w.WriteHeader(204)
}
