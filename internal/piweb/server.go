package piweb

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"
)

type Config struct {
	Host              string
	Port              string
	AllowedOrigins    []string
	EnablePiExecution bool
	StaticFiles       fs.FS
}

type Server struct {
	store                   *Store
	broker                  *Broker
	runner                  *Runner
	mux                     *http.ServeMux
	config                  Config
	adminPasswordConfigured bool
	adminPasswordCheckError error
	adminPasswordStateMu    sync.RWMutex
}

func NewServer(config Config, store *Store, broker *Broker) *Server {
	if config.Host == "" {
		config.Host = "127.0.0.1"
	}
	if config.Port == "" {
		config.Port = "8732"
	}
	if len(config.AllowedOrigins) == 0 {
		config.AllowedOrigins = []string{
			"http://localhost:4321",
			"http://127.0.0.1:4321",
			"http://localhost:6006",
			"http://127.0.0.1:6006",
		}
	}
	adminPasswordConfigured, adminPasswordCheckError := AdminPasswordConfigured()
	s := &Server{
		store:                   store,
		broker:                  broker,
		runner:                  NewRunner(),
		mux:                     http.NewServeMux(),
		config:                  config,
		adminPasswordConfigured: adminPasswordConfigured,
		adminPasswordCheckError: adminPasswordCheckError,
	}
	s.routes()
	return s
}

func (s *Server) Addr() string {
	return s.config.Host + ":" + s.config.Port
}
func (s *Server) Handler() http.Handler {
	return s.withLogging(s.withCORS(s.mux))
}
func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", s.health)
	s.mux.HandleFunc("GET /api/admin/password/status", s.adminPasswordStatus)
	s.mux.HandleFunc("POST /api/admin/password", s.setupAdminPassword)
	s.mux.HandleFunc("GET /api/app-config", s.appConfig)
	s.mux.HandleFunc("PATCH /api/app-config", s.patchAppConfig)
	s.mux.HandleFunc("GET /api/auth/providers", s.authProviders)
	s.mux.HandleFunc("GET /api/auth/oauth/providers", s.oauthProviders)
	s.mux.HandleFunc("POST /api/auth/oauth/start", s.startOAuthLogin)
	s.mux.HandleFunc("GET /api/auth/oauth/sessions/{sessionID}", s.oauthLoginSession)
	s.mux.HandleFunc("POST /api/auth/oauth/sessions/{sessionID}/input", s.oauthLoginInput)
	s.mux.HandleFunc("POST /api/auth/api-key", s.saveAPIKey)
	s.mux.HandleFunc("DELETE /api/auth/{provider}", s.logoutProvider)
	s.mux.HandleFunc("GET /api/system/folders", s.listFolders)
	s.mux.HandleFunc("GET /api/workspaces", s.workspaces)
	s.mux.HandleFunc("POST /api/workspaces/open", s.openWorkspace)
	s.mux.HandleFunc("POST /api/workspaces/clone", s.cloneWorkspace)
	s.mux.HandleFunc("DELETE /api/workspaces/{workspaceID}", s.requireWorkspaceAccess(s.deleteWorkspace))
	s.mux.HandleFunc("GET /api/workspaces/{workspaceID}/sessions", s.requireWorkspaceAccess(s.workspaceSessions))
	s.mux.HandleFunc("POST /api/workspaces/{workspaceID}/sessions", s.requireWorkspaceAccess(s.createSession))
	s.mux.HandleFunc("DELETE /api/workspaces/{workspaceID}/sessions", s.requireWorkspaceAccess(s.deleteWorkspaceSessions))
	s.mux.HandleFunc("GET /api/workspaces/{workspaceID}/files", s.requireWorkspaceAccess(s.workspaceFiles))
	s.mux.HandleFunc("GET /api/workspaces/{workspaceID}/files/raw", s.requireWorkspaceAccess(s.workspaceFileAsset))
	s.mux.HandleFunc("GET /api/workspaces/{workspaceID}/files/search", s.requireWorkspaceAccess(s.searchWorkspaceFiles))
	s.mux.HandleFunc("GET /api/workspaces/{workspaceID}/commands", s.requireWorkspaceAccess(s.workspaceCommands))
	s.mux.HandleFunc("GET /api/workspaces/{workspaceID}/models", s.requireWorkspaceAccess(s.workspaceModels))
	s.mux.HandleFunc("GET /api/workspaces/{workspaceID}/runtime-status", s.requireWorkspaceAccess(s.workspaceRuntimeStatus))
	s.mux.HandleFunc("GET /api/workspaces/{workspaceID}/runtime-model", s.requireWorkspaceAccess(s.workspaceRuntimeModel))
	s.mux.HandleFunc("GET /api/workspaces/{workspaceID}/runtime-quota", s.requireWorkspaceAccess(s.workspaceRuntimeQuota))
	s.mux.HandleFunc("GET /api/workspaces/{workspaceID}/settings", s.requireWorkspaceAccess(s.workspaceSettings))
	s.mux.HandleFunc("PUT /api/workspaces/{workspaceID}/settings", s.requireWorkspaceAccess(s.saveWorkspaceSettings))
	s.mux.HandleFunc("GET /api/workspaces/{workspaceID}/files/read", s.requireWorkspaceAccess(s.readWorkspaceFile))
	s.mux.HandleFunc("PUT /api/workspaces/{workspaceID}/files/write", s.requireWorkspaceAccess(s.writeWorkspaceFile))
	s.mux.HandleFunc("POST /api/workspaces/{workspaceID}/files/create", s.requireWorkspaceAccess(s.createWorkspaceFile))
	s.mux.HandleFunc("PATCH /api/workspaces/{workspaceID}/files/rename", s.requireWorkspaceAccess(s.renameWorkspaceFile))
	s.mux.HandleFunc("DELETE /api/workspaces/{workspaceID}/files/delete", s.requireWorkspaceAccess(s.deleteWorkspaceFile))
	s.mux.HandleFunc("POST /api/workspaces/{workspaceID}/files/upload", s.requireWorkspaceAccess(s.uploadWorkspaceFile))
	s.mux.HandleFunc("GET /api/workspaces/{workspaceID}/git/status", s.requireWorkspaceAccess(s.gitStatus))
	s.mux.HandleFunc("GET /api/workspaces/{workspaceID}/git/history", s.requireWorkspaceAccess(s.gitHistory))
	s.mux.HandleFunc("GET /api/workspaces/{workspaceID}/git/commit", s.requireWorkspaceAccess(s.gitCommit))
	s.mux.HandleFunc("POST /api/workspaces/{workspaceID}/shell", s.requireWorkspaceAccess(s.shellCommand))
	s.mux.HandleFunc("GET /api/sessions/{sessionID}", s.requireSessionAccess(s.session))
	s.mux.HandleFunc("GET /api/sessions/{sessionID}/context-usage", s.requireSessionAccess(s.handleSessionContextUsage))
	s.mux.HandleFunc("POST /api/sessions/{sessionID}/model", s.requireSessionAccess(s.handleSessionSetModel))
	s.mux.HandleFunc("PATCH /api/sessions/{sessionID}", s.requireSessionAccess(s.renameSession))
	s.mux.HandleFunc("DELETE /api/sessions/{sessionID}", s.requireSessionAccess(s.deleteSession))
	s.mux.HandleFunc("POST /api/sessions/{sessionID}/prompt", s.requireSessionAccess(s.prompt))
	s.mux.HandleFunc("POST /api/sessions/{sessionID}/ag-ui", s.requireSessionAccess(s.aguiSessionRun))
	s.mux.HandleFunc("POST /api/sessions/{sessionID}/compact", s.requireSessionAccess(s.compactSession))
	s.mux.HandleFunc("POST /api/sessions/{sessionID}/steer", s.requireSessionAccess(s.steerSession))
	s.mux.HandleFunc("POST /api/sessions/{sessionID}/cancel", s.requireSessionAccess(s.cancelSession))
	s.mux.HandleFunc("GET /api/sessions/{sessionID}/events", s.requireSessionAccess(s.sessionEvents))
	if s.config.StaticFiles != nil {
		s.mux.HandleFunc("GET /", s.staticFile)
	}
}
func (s *Server) context() context.Context {
	return context.Background()
}
func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Last-Event-ID")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start).String())
	})
}
func (s *Server) authProviders(w http.ResponseWriter, _ *http.Request) {
	providers, err := AuthProviders()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, providers)
}

func (s *Server) oauthProviders(w http.ResponseWriter, _ *http.Request) {
	providers, err := OAuthProviders()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, providers)
}

func (s *Server) startOAuthLogin(w http.ResponseWriter, r *http.Request) {
	var req StartOAuthRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	session, err := StartOAuthLogin(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": session})
}

func (s *Server) oauthLoginSession(w http.ResponseWriter, r *http.Request) {
	session, err := OAuthLoginSession(r.PathValue("sessionID"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": session})
}

func (s *Server) oauthLoginInput(w http.ResponseWriter, r *http.Request) {
	var req OAuthInputRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	session, err := SendOAuthLoginInput(r.PathValue("sessionID"), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": session})
}

func (s *Server) saveAPIKey(w http.ResponseWriter, r *http.Request) {
	var req SaveAPIKeyRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	provider, err := SaveAPIKey(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"provider": provider})
}

func (s *Server) logoutProvider(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	if err := LogoutProvider(provider); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"provider": provider, "configured": false})
}

const maxJSONBodyBytes int64 = 1 << 20

var errRequestBodyTooLarge = errors.New("request body is too large")

type limitedJSONReader struct {
	reader   io.Reader
	read     int64
	maxBytes int64
}

func (r *limitedJSONReader) Read(p []byte) (int, error) {
	remaining := r.maxBytes + 1 - r.read
	if remaining <= 0 {
		return 0, errRequestBodyTooLarge
	}
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := r.reader.Read(p)
	r.read += int64(n)
	return n, err
}

func readJSON(r *http.Request, v any) error {
	return readJSONWithLimit(r, v, maxJSONBodyBytes)
}

func readJSONWithLimit(r *http.Request, v any, maxBytes int64) error {
	defer r.Body.Close()
	if maxBytes <= 0 {
		maxBytes = maxJSONBodyBytes
	}
	limited := &limitedJSONReader{reader: r.Body, maxBytes: maxBytes}
	decoder := json.NewDecoder(limited)
	if err := decoder.Decode(v); err != nil {
		if limited.read > maxBytes {
			return errRequestBodyTooLarge
		}
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if limited.read > maxBytes {
			return errRequestBodyTooLarge
		}
		if err == nil {
			return errors.New("request body must contain only one JSON value")
		}
		return err
	}
	if limited.read > maxBytes {
		return errRequestBodyTooLarge
	}
	return nil
}
func writeUploadTooLarge(w http.ResponseWriter, maxBytes int64) {
	writeError(w, http.StatusRequestEntityTooLarge, &uploadTooLargeError{limit: maxBytes})
}

func maxUploadJSONBodyBytes(maxBytes int64) int64 {
	return maxBytes + ((maxBytes+2)/3)*4 + 4096
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, err error) {
	if errors.Is(err, errRequestBodyTooLarge) {
		status = http.StatusRequestEntityTooLarge
	}
	writeJSON(w, status, ErrorResponse{Error: err.Error()})
}
func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if errors.Is(err, ErrSafeUploadUnsupported) {
		writeError(w, http.StatusNotImplemented, err)
		return
	}
	writeError(w, http.StatusInternalServerError, err)
}

func writeWorkspaceSelectionError(w http.ResponseWriter, err error) {
	writeError(w, workspaceSelectionErrorStatus(err), err)
}

func workspaceSelectionErrorStatus(err error) int {
	if errors.Is(err, ErrWorkspaceForbidden) {
		return http.StatusForbidden
	}
	if errors.Is(err, ErrWorkspacePolicyInvalid) || isFilesystemError(err) {
		return http.StatusInternalServerError
	}
	return http.StatusBadRequest
}

func isFilesystemError(err error) bool {
	var pathError *os.PathError
	return errors.As(err, &pathError) || errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrInvalid)
}
