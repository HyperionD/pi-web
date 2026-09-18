package piweb

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

type accountContextKey struct{}

const accountCookieName = "pi_web_session"

func requestAccount(r *http.Request) (Account, bool) {
	user, ok := r.Context().Value(accountContextKey{}).(Account)
	return user, ok
}

func requestActor(r *http.Request) *Account {
	user, ok := requestAccount(r)
	if !ok {
		return nil
	}
	return &user
}

func (s *Server) originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	if parsed.Host == r.Host {
		return true
	}
	for _, allowed := range s.config.AllowedOrigins {
		if origin == allowed {
			return true
		}
	}
	return false
}

func (s *Server) withAccount(next http.Handler) http.Handler {
	if !s.config.UserManagement {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if s.accounts.dbErr != nil {
			writeError(w, 503, errors.New("account database is unavailable"))
			return
		}
		if !s.originAllowed(r) {
			writeError(w, 403, errors.New("request origin is not allowed"))
			return
		}
		if publicAccountRoute(r) {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie(accountCookieName)
		if err != nil {
			writeError(w, 401, errors.New("login required"))
			return
		}
		login, ok := s.accounts.authenticate(cookie.Value)
		if !ok {
			writeError(w, 401, errors.New("login expired"))
			return
		}
		if login.User.Role != "admin" && !memberAccountRoute(r) {
			writeError(w, 403, errAccountDenied)
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		stop := context.AfterFunc(login.Context, cancel)
		defer func() { stop(); cancel() }()
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, accountContextKey{}, login.User)))
	})
}

func publicAccountRoute(r *http.Request) bool {
	switch r.Method + " " + r.URL.Path {
	case "GET /api/health", "GET /api/account/status", "POST /api/account/login",
		"GET /api/admin/password/status", "POST /api/admin/password":
		return true
	}
	return false
}

func memberAccountRoute(r *http.Request) bool {
	path := r.URL.Path
	if r.Method == http.MethodPost && path == "/api/admin/users/batch-workspaces" {
		return false
	}
	if path == "/api/account/me" || path == "/api/account/logout" {
		return true
	}
	if r.Method == "GET" && (path == "/api/workspaces" || path == "/api/app-config") {
		return true
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 3 && parts[1] == "sessions" {
		return true
	}
	if len(parts) < 4 || parts[1] != "workspaces" {
		return false
	}
	// Global/provider configuration and workspace-wide deletion are administrator-only.
	if parts[3] == "settings" {
		return r.Method == http.MethodGet || r.Method == http.MethodPut
	}
	if len(parts) == 4 && parts[3] == "sessions" && r.Method == "DELETE" {
		return true
	}
	switch parts[3] {
	case "sessions", "files", "commands", "models", "runtime-status", "runtime-model", "runtime-quota", "skills", "git", "shell":
		return true
	}
	return false
}
