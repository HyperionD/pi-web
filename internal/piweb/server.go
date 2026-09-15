package piweb

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

type Config struct {
	Host              string
	Port              string
	AllowedOrigins    []string
	EnablePiExecution bool
	UserManagement    bool
	StaticFiles       fs.FS
}

type serverLifecycle struct {
	mu     sync.Mutex
	state  uint8
	active int
	zero   chan struct{}
}

const (
	serverAccepting uint8 = iota
	serverClosing
	serverClosed
)

func newServerLifecycle() *serverLifecycle {
	zero := make(chan struct{})
	close(zero)
	return &serverLifecycle{zero: zero}
}

func (lifecycle *serverLifecycle) enter() bool {
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()
	if lifecycle.state != serverAccepting {
		return false
	}
	if lifecycle.active == 0 {
		lifecycle.zero = make(chan struct{})
	}
	lifecycle.active++
	return true
}

func (lifecycle *serverLifecycle) leave() {
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()
	lifecycle.active--
	if lifecycle.active == 0 {
		close(lifecycle.zero)
		if lifecycle.state == serverClosing {
			lifecycle.state = serverClosed
		}
	}
}

func (lifecycle *serverLifecycle) startShutdown() <-chan struct{} {
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()
	if lifecycle.state == serverAccepting {
		lifecycle.state = serverClosing
	}
	return lifecycle.zero
}

func (lifecycle *serverLifecycle) waitForZero(ctx context.Context, zero <-chan struct{}) error {
	select {
	case <-zero:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (lifecycle *serverLifecycle) beginShutdown(ctx context.Context) error {
	return lifecycle.waitForZero(ctx, lifecycle.startShutdown())
}

type Server struct {
	store                   *Store
	broker                  *Broker
	events                  SessionEventStore
	hub                     *SessionWakeHub
	commands                *SessionCommandService
	runner                  *Runner
	runtimeManager          *RuntimeManager
	mux                     *http.ServeMux
	config                  Config
	ctx                     context.Context
	cancel                  context.CancelFunc
	lifecycle               *serverLifecycle
	accounts                *accountStore
	workspaceAccess         *WorkspaceAccess
	adminPasswordConfigured bool
	adminPasswordCheckError error
	adminPasswordStateMu    sync.RWMutex
}

func NewServer(config Config, store *Store, broker *Broker) *Server {
	return NewServerWithRuntimeManager(config, store, broker, NewRuntimeManager(store))
}

func NewServerWithEventStore(config Config, store *Store, broker *Broker, events SessionEventStore, hub *SessionWakeHub) *Server {
	server := NewServer(config, store, broker)
	server.attachEventStore(store, events, hub)
	return server
}

func NewServerWithEventsAndRuntimeManager(config Config, store *Store, broker *Broker, manager *RuntimeManager, events SessionEventStore, hub *SessionWakeHub) *Server {
	server := NewServerWithRuntimeManager(config, store, broker, manager)
	server.attachEventStore(store, events, hub)
	return server
}

func (s *Server) attachEventStore(store *Store, events SessionEventStore, hub *SessionWakeHub) {
	s.events = events
	s.hub = hub
	s.commands = NewSessionCommandService(events, runnerPromptStarter{runner: s.runner, store: store})
}

type runnerPromptStarter struct {
	runner   *Runner
	store    *Store
	settings []ModelStartupSettings
}

func (s runnerPromptStarter) StartPrompt(ctx context.Context, sessionID, runID, text string, images []PromptAttachment, adapter *RuntimeEventAdapter) error {
	return s.runner.StartDurablePrompt(ctx, s.store, sessionID, runID, text, images, adapter, s.settings...)
}

func NewServerWithRuntimeManager(config Config, store *Store, broker *Broker, manager *RuntimeManager) *Server {
	if manager == nil {
		manager = NewRuntimeManager(store)
	}
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
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		store:                   store,
		broker:                  broker,
		runtimeManager:          manager,
		runner:                  NewRunnerWithRuntimeManager(manager),
		mux:                     http.NewServeMux(),
		config:                  config,
		ctx:                     ctx,
		cancel:                  cancel,
		lifecycle:               newServerLifecycle(),
		adminPasswordConfigured: adminPasswordConfigured,
		adminPasswordCheckError: adminPasswordCheckError,
	}
	if config.UserManagement {
		s.accounts = newAccountStore()
		s.accountRoutes()
	}
	s.workspaceAccess = NewWorkspaceAccess(store, s.accounts)
	s.routes()
	return s
}

func (s *Server) Addr() string {
	return s.config.Host + ":" + s.config.Port
}
func (s *Server) Handler() http.Handler {
	return s.withLogging(s.withShutdownGuard(s.withCORS(s.withAccount(s.mux))))
}

func (s *Server) withShutdownGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.lifecycle.enter() {
			writeError(w, http.StatusServiceUnavailable, errors.New("server is shutting down"))
			return
		}
		defer s.lifecycle.leave()
		next.ServeHTTP(w, r)
	})
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
	s.mux.HandleFunc("GET /api/admin/workspaces", s.adminWorkspaces)
	s.mux.HandleFunc("POST /api/workspaces/open", s.openWorkspace)
	s.mux.HandleFunc("POST /api/workspaces/clone", s.cloneWorkspace)
	s.mux.HandleFunc("DELETE /api/workspaces/{workspaceID}", s.requireWorkspaceAccess(s.closeWorkspace))
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
	s.mux.HandleFunc("POST /api/sessions/{sessionID}/compact", s.requireSessionAccess(s.compactSession))
	s.mux.HandleFunc("POST /api/sessions/{sessionID}/steer", s.requireSessionAccess(s.steerSession))
	s.mux.HandleFunc("POST /api/sessions/{sessionID}/cancel", s.requireSessionAccess(s.cancelSession))
	s.mux.HandleFunc("GET /api/sessions/{sessionID}/state", s.requireSessionAccess(s.sessionState))
	s.mux.HandleFunc("GET /api/sessions/{sessionID}/events-v2", s.requireSessionAccess(s.sessionEventsV2))
	s.mux.HandleFunc("GET /api/sessions/{sessionID}/items/{itemID}/content", s.requireSessionAccess(s.sessionItemContent))
	if s.config.StaticFiles != nil {
		s.mux.HandleFunc("GET /", s.staticFile)
	}
}
func (s *Server) context() context.Context {
	return s.ctx
}

func (s *Server) Shutdown(ctx context.Context) error {
	zero := s.lifecycle.startShutdown()
	s.cancel()
	runtimeCtx, runtimeCancel := independentShutdownContext(ctx)
	defer runtimeCancel()
	brokerCtx, brokerCancel := independentShutdownContext(ctx)
	defer brokerCancel()
	storeCtx, storeCancel := independentShutdownContext(ctx)
	defer storeCancel()

	// Closing subscriptions first lets long-lived SSE handlers leave before we
	// wait for the request count. Runtime and store cleanup follows admission
	// shutdown, so no new handler can recreate a resource while it closes.
	brokerErr := s.broker.Shutdown(brokerCtx)
	lifecycleErr := s.lifecycle.waitForZero(ctx, zero)
	var eventsErr error
	if s.events != nil {
		eventsErr = s.events.Close(storeCtx)
	}
	return errors.Join(
		lifecycleErr,
		brokerErr,
		s.runtimeManager.Shutdown(runtimeCtx),
		s.store.Shutdown(storeCtx),
		eventsErr,
	)
}

func independentShutdownContext(context.Context) (context.Context, context.CancelFunc) {
	// Cleanup must not inherit an already-expired request deadline. Every
	// shutdown phase gets a fresh, bounded five-second window.
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func (s *Server) workspacePolicy() (WorkspacePolicy, error) {
	if s.config.UserManagement {
		return WorkspacePolicy{}, nil
	}
	return LoadWorkspacePolicy()
}
func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.config.UserManagement {
			if !s.originAllowed(r) {
				writeError(w, http.StatusForbidden, errors.New("request origin is not allowed"))
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Add("Vary", "Origin")
			}
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
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
