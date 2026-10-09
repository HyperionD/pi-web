package piweb

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestCORSAllowsAdminPasswordHeader(t *testing.T) {
	server := NewServer(Config{UserManagement: true}, NewMockStore(), NewBroker())
	request := httptest.NewRequest(http.MethodOptions, "/api/prompt-presets", nil)
	request.Header.Set("Origin", "http://localhost:4321")
	request.Header.Set("Access-Control-Request-Headers", "X-Admin-Password")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d", response.Code)
	}
	if !strings.Contains(response.Header().Get("Access-Control-Allow-Headers"), "X-Admin-Password") {
		t.Fatalf("allowed headers = %q", response.Header().Get("Access-Control-Allow-Headers"))
	}
	if response.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("preflight did not allow credentials")
	}

	standalone := NewServer(Config{}, NewMockStore(), NewBroker())
	standaloneRequest := httptest.NewRequest(http.MethodOptions, "/api/prompt-presets", nil)
	standaloneRequest.Header.Set("Origin", "http://localhost:4321")
	standaloneResponse := httptest.NewRecorder()
	standalone.Handler().ServeHTTP(standaloneResponse, standaloneRequest)
	if standaloneResponse.Header().Get("Access-Control-Allow-Origin") != "http://localhost:4321" {
		t.Fatalf("standalone origin = %q", standaloneResponse.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestHealthEndpoint(t *testing.T) {
	server := NewServer(Config{}, NewMockStore(), NewBroker())
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	if !strings.Contains(res.Body.String(), `"ok":true`) {
		t.Fatalf("unexpected body: %s", res.Body.String())
	}
}

func TestServesWorkspaceFileAsset(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	asset := []byte("not really a png")
	if err := os.WriteFile(filepath.Join(root, "docs", "assets", "logo.png"), asset, 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewWebStore(filepath.Join(t.TempDir(), "web.db"))
	workspace, err := store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	saveAllowedRootsForTest(t, root)
	server := NewServer(Config{}, store, NewBroker())

	requestPath := "/api/workspaces/" + workspace.ID + "/files/raw?path=docs%2Fassets%2Flogo.png"
	request := httptest.NewRequest(http.MethodGet, requestPath, nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("content type = %q, want image/png", response.Header().Get("Content-Type"))
	}
	if response.Body.String() != string(asset) {
		t.Fatalf("asset body = %q, want %q", response.Body.String(), asset)
	}

	traversal := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspace.ID+"/files/raw?path=..%2Fsecret", nil)
	traversalResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(traversalResponse, traversal)
	if traversalResponse.Code == http.StatusOK {
		t.Fatal("path traversal unexpectedly served an asset")
	}

	outside := t.TempDir()
	outsideAsset := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideAsset, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideAsset, filepath.Join(root, "linked.txt")); err != nil {
		t.Fatal(err)
	}
	linked := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspace.ID+"/files/raw?path=linked.txt", nil)
	linkedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(linkedResponse, linked)
	if linkedResponse.Code != http.StatusForbidden {
		t.Fatalf("symlink escape got %d, want %d", linkedResponse.Code, http.StatusForbidden)
	}
}

func TestRemovedVersionEndpoints(t *testing.T) {
	server := NewServer(Config{}, NewMockStore(), NewBroker())
	for _, path := range []string{"/api/version", "/api/pi/version"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			res := httptest.NewRecorder()
			server.Handler().ServeHTTP(res, req)
			if res.Code != http.StatusNotFound {
				t.Fatalf("expected removed endpoint to return 404, got %d", res.Code)
			}
		})
	}
}

func TestServesStaticUI(t *testing.T) {
	files := fstest.MapFS{
		"index.html":    {Data: []byte("<html>app shell</html>")},
		"assets/app.js": {Data: []byte("console.log('app')")},
	}
	server := NewServer(Config{StaticFiles: files}, NewMockStore(), NewBroker())

	for _, tc := range []struct {
		path string
		want string
	}{
		{path: "/", want: "app shell"},
		{path: "/assets/app.js", want: "console.log('app')"},
		{path: "/workspaces/demo", want: "app shell"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			res := httptest.NewRecorder()
			server.Handler().ServeHTTP(res, req)
			if res.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", res.Code)
			}
			if !strings.Contains(res.Body.String(), tc.want) {
				t.Fatalf("unexpected body: %s", res.Body.String())
			}
		})
	}

	req := httptest.NewRequest(http.MethodGet, "/api/not-found", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("expected unknown API route to stay 404, got %d", res.Code)
	}
}

func TestSessionManagementEndpointsRequireDurableEventStore(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	store := NewMockStore()
	workspace, err := store.OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{}, store, NewBroker())

	createReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspace.ID+"/sessions", nil)
	createRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", createRes.Code, createRes.Body.String())
	}

	var body struct {
		Session Session `json:"session"`
	}
	if err := json.NewDecoder(createRes.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	renameReq := httptest.NewRequest(http.MethodPatch, "/api/sessions/"+body.Session.ID, bytes.NewBufferString(`{"title":"renamed"}`))
	renameRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(renameRes, renameReq)
	if renameRes.Code != http.StatusServiceUnavailable {
		t.Fatalf("rename status = %d, want service unavailable", renameRes.Code)
	}
	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/sessions/"+body.Session.ID, nil)
	deleteRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(deleteRes, deleteReq)
	if deleteRes.Code != http.StatusServiceUnavailable {
		t.Fatalf("delete status = %d, want service unavailable", deleteRes.Code)
	}
}

type failingDeleteEventStore struct {
	SessionEventStore
	calls  int
	failAt int
}

func (s *failingDeleteEventStore) DeleteSession(ctx context.Context, sessionID string) error {
	s.calls++
	if s.calls == s.failAt {
		return errors.New("durable delete failed")
	}
	return s.SessionEventStore.DeleteSession(ctx, sessionID)
}

func TestDeleteWorkspaceSessionsPreservesStoreWhenDurableCleanupFails(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	store := NewMockStore()
	workspace, err := store.OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessions := make([]Session, 2)
	paths := make([]string, 2)
	for index := range sessions {
		sessions[index], err = store.CreateSession(workspace.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	hub := NewSessionWakeHub()
	events, err := OpenSQLiteSessionEventStore(filepath.Join(t.TempDir(), "events.db"), hub)
	if err != nil {
		t.Fatal(err)
	}
	defer events.Close(t.Context())
	for index, session := range sessions {
		path, _, ok := store.SessionRuntime(session.ID)
		if !ok {
			t.Fatalf("session runtime file was not created for %s", session.ID)
		}
		paths[index] = path
		if _, err := events.OpenSession(t.Context(), SessionSource{SessionID: session.ID, Path: path}); err != nil {
			t.Fatal(err)
		}
	}
	failing := &failingDeleteEventStore{SessionEventStore: events, failAt: 2}
	server := NewServerWithEventStore(Config{}, store, NewBroker(), failing, hub)
	request := httptest.NewRequest(http.MethodDelete, "/api/workspaces/"+workspace.ID+"/sessions", nil)
	request = request.WithContext(context.WithValue(request.Context(), accountContextKey{}, Account{Role: "admin"}))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("delete status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	for index, session := range sessions {
		if _, _, err := store.Session(session.ID); err != nil {
			t.Fatalf("session %s was removed after durable cleanup failure: %v", session.ID, err)
		}
		if _, err := os.Stat(paths[index]); err != nil {
			t.Fatalf("session JSONL %s was removed after durable cleanup failure: %v", session.ID, err)
		}
	}
}

func TestDeleteWorkspaceSessionsPurgesDurableEventStore(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	store := NewMockStore()
	workspace, err := store.OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateSession(workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	path, _, ok := store.SessionRuntime(session.ID)
	if !ok {
		t.Fatal("session runtime file was not created")
	}
	hub := NewSessionWakeHub()
	events, err := OpenSQLiteSessionEventStore(filepath.Join(t.TempDir(), "events.db"), hub)
	if err != nil {
		t.Fatal(err)
	}
	defer events.Close(t.Context())
	if _, err := events.OpenSession(t.Context(), SessionSource{SessionID: session.ID, Path: path}); err != nil {
		t.Fatal(err)
	}
	server := NewServerWithEventStore(Config{}, store, NewBroker(), events, hub)
	request := httptest.NewRequest(http.MethodDelete, "/api/workspaces/"+workspace.ID+"/sessions", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if _, err := events.Snapshot(t.Context(), session.ID, SessionPageRequest{Limit: 10}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("durable session snapshot error = %v, want sql.ErrNoRows", err)
	}
}

func TestDeleteWorkspaceSessionsEndpoint(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	store := NewMockStore()
	workspace, err := store.OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateSession(workspace.ID); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{}, store, NewBroker())

	req := httptest.NewRequest(http.MethodDelete, "/api/workspaces/"+workspace.ID+"/sessions", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `"deletedCount":1`) || !strings.Contains(res.Body.String(), `"sessions":[]`) {
		t.Fatalf("unexpected body: %s", res.Body.String())
	}
}

func TestSessionCommandsEndpointUsesCurrentRuntime(t *testing.T) {
	store := NewMockStore()
	store.sessionFiles["8e7c-44ff"] = "/tmp/session.jsonl"
	runtime := newFakePiRuntime()
	runtime.responseData = json.RawMessage(`{"commands":[{"name":"review","description":"Review","source":"prompt","sourceInfo":{"scope":"project"}}]}`)
	manager := NewRuntimeManagerWithFactory(store, func(context.Context, string, string) (PiRuntime, error) {
		return runtime, nil
	})
	defer manager.Shutdown(context.Background())
	server := NewServerWithRuntimeManager(Config{EnablePiExecution: true}, store, NewBroker(), manager)

	request := httptest.NewRequest(http.MethodGet, "/api/sessions/8e7c-44ff/commands", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"command":"/review"`) {
		t.Fatalf("unexpected body: %s", response.Body.String())
	}
	if len(runtime.sends) != 1 || runtime.sends[0].Type != "get_commands" {
		t.Fatalf("unexpected runtime calls: %+v", runtime.sends)
	}
}

func TestSessionCommandEndpointRejectsMutatingCommandsWhileRunning(t *testing.T) {
	store := NewMockStore()
	store.sessionFiles["8e7c-44ff"] = "/tmp/session.jsonl"
	runtime := newFakePiRuntime()
	manager := NewRuntimeManagerWithFactory(store, func(context.Context, string, string) (PiRuntime, error) {
		return runtime, nil
	})
	defer manager.Shutdown(context.Background())
	server := NewServerWithRuntimeManager(Config{EnablePiExecution: true}, store, NewBroker(), manager)
	server.runner.mu.Lock()
	server.runner.running["8e7c-44ff"] = &activePiRun{}
	server.runner.mu.Unlock()

	request := httptest.NewRequest(http.MethodPost, "/api/sessions/8e7c-44ff/command", strings.NewReader(`{"command":"clone"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("running clone status = %d, want %d", response.Code, http.StatusConflict)
	}
	if len(runtime.sends) != 0 {
		t.Fatalf("running clone sent runtime command: %+v", runtime.sends)
	}
}

func TestSessionCommandEndpointReloadRestartsCurrentRuntime(t *testing.T) {
	store := NewMockStore()
	store.sessionFiles["8e7c-44ff"] = "/tmp/session.jsonl"
	first := newFakePiRuntime()
	second := newFakePiRuntime()
	runtimes := []*fakePiRuntime{first, second}
	created := 0
	manager := NewRuntimeManagerWithFactory(store, func(context.Context, string, string) (PiRuntime, error) {
		runtime := runtimes[created]
		created++
		return runtime, nil
	})
	defer manager.Shutdown(context.Background())
	server := NewServerWithRuntimeManager(Config{EnablePiExecution: true}, store, NewBroker(), manager)

	request := httptest.NewRequest(http.MethodPost, "/api/sessions/8e7c-44ff/command", strings.NewReader(`{"command":"reload"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("reload status = %d: %s", response.Code, response.Body.String())
	}
	if created != 2 || first.closed != 1 {
		t.Fatalf("reload runtimes = created %d, first closed %d", created, first.closed)
	}
	if len(first.sends) != 0 || len(second.sends) != 0 {
		t.Fatalf("reload sent unsupported runtime command: first=%+v second=%+v", first.sends, second.sends)
	}
}

func TestSessionCommandEndpointAllowlistsRuntimeCommands(t *testing.T) {
	store := NewMockStore()
	store.sessionFiles["8e7c-44ff"] = "/tmp/session.jsonl"
	runtime := newFakePiRuntime()
	runtime.responseData = json.RawMessage(`{"text":"copied"}`)
	manager := NewRuntimeManagerWithFactory(store, func(context.Context, string, string) (PiRuntime, error) {
		return runtime, nil
	})
	defer manager.Shutdown(context.Background())
	server := NewServerWithRuntimeManager(Config{EnablePiExecution: true}, store, NewBroker(), manager)

	request := httptest.NewRequest(http.MethodPost, "/api/sessions/8e7c-44ff/command", strings.NewReader(`{"command":"copy"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Client-Request-ID", "copy-1")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"text":"copied"`) {
		t.Fatalf("copy response = %d %s", response.Code, response.Body.String())
	}
	if len(runtime.sends) != 1 || runtime.sends[0].Type != "get_last_assistant_text" || runtime.sends[0].ID != "command:copy-1" {
		t.Fatalf("unexpected runtime calls: %+v", runtime.sends)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/sessions/8e7c-44ff/command", strings.NewReader(`{"command":"delete_all"}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown command status = %d, want 400", response.Code)
	}
}

func TestWorkspaceCommandsEndpointDoesNotInventCommandsWhenPiDisabled(t *testing.T) {
	server := NewServer(Config{EnablePiExecution: false}, NewMockStore(), NewBroker())
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/pi-mono/commands", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	if res.Body.String() != "{\"commands\":[]}\n" {
		t.Fatalf("unexpected body: %s", res.Body.String())
	}
}

func TestWorkspaceCommandsEndpointReportsPiCommandFailureWithoutFakeCommands(t *testing.T) {
	root := t.TempDir()
	writeFakePi(t, root, "#!/bin/sh\nexit 0\n")
	store := NewMockStore()
	workspace, err := store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{EnablePiExecution: true}, store, NewBroker())
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspace.ID+"/commands", nil)
	res := httptest.NewRecorder()

	server.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 fallback, got %d: %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `"commands":[]`) || !strings.Contains(res.Body.String(), `"error"`) {
		t.Fatalf("unexpected body: %s", res.Body.String())
	}
}

func TestWorkspaceModelsEndpointUsesMockModelsWhenPiDisabled(t *testing.T) {
	server := NewServer(Config{EnablePiExecution: false}, NewMockStore(), NewBroker())
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/pi-mono/models", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `"id":"zai"`) || !strings.Contains(res.Body.String(), `"id":"gpt-5.5"`) {
		t.Fatalf("unexpected body: %s", res.Body.String())
	}
}

func TestWorkspaceModelsEndpointUsesNoExtensions(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	modelsPath := filepath.Join(home, ".pi", "agent", "models.json")
	if err := os.MkdirAll(filepath.Dir(modelsPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(modelsPath, []byte(`{"providers":{"zai":{"models":[{"id":"gpt-5.5"}],"apiKey":"hidden-secret"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(root, "args.txt")
	t.Setenv("PI_ARGS_FILE", argsFile)
	writeFakePi(t, root, `#!/bin/sh
echo "$@" > "$PI_ARGS_FILE"
echo 'provider  model  context  max-out  thinking  images' >&2
echo 'zai gpt-5.5 1M 64K yes no' >&2
`)
	store := NewMockStore()
	workspace, err := store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{EnablePiExecution: true}, store, NewBroker())
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspace.ID+"/models", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `"id":"gpt-5.5"`) || strings.Contains(res.Body.String(), "hidden-secret") {
		t.Fatalf("unexpected body: %s", res.Body.String())
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(args)) != "--no-extensions --list-models" {
		t.Fatalf("unexpected pi args: %q", string(args))
	}
}

func TestWorkspaceModelsEndpointFallsBackOnPiFailure(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(root, ".pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".pi", "settings.json"), []byte(`{"defaultProvider":"anthropic","defaultModel":"claude-test"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeFakePi(t, root, `#!/bin/sh
echo 'signal: killed' >&2
exit 137
`)
	store := NewMockStore()
	workspace, err := store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{EnablePiExecution: true}, store, NewBroker())
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspace.ID+"/models", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", res.Code, res.Body.String())
	}
	body := res.Body.String()
	if strings.Contains(body, `"id":"anthropic"`) || strings.Contains(body, `"id":"claude-test"`) || !strings.Contains(body, `"error"`) {
		t.Fatalf("unexpected error response: %s", body)
	}
}

func TestParseListModelsOutput(t *testing.T) {
	models := parseListModelsOutput("provider  model  context  max-out  thinking  images\nzai gpt-5.5 1M 64K yes no\nanthropic claude 200K 8K yes yes\n")
	if len(models.Providers) != 2 || models.Providers[0].ID != "anthropic" || models.Providers[1].Models[0].ID != "gpt-5.5" {
		t.Fatalf("unexpected models: %+v", models)
	}
}

func TestWorkspaceRuntimeStatusEndpointUsesMockStatusWhenPiDisabled(t *testing.T) {
	server := NewServer(Config{EnablePiExecution: false}, NewMockStore(), NewBroker())
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/pi-mono/runtime-status", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `"model":"GPT-5.5"`) || !strings.Contains(res.Body.String(), `"weeklyQuota":14`) {
		t.Fatalf("unexpected body: %s", res.Body.String())
	}
}

func TestWorkspaceSettingsEndpointReadsAndSavesSettings(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".pi", "agent"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	globalPath := filepath.Join(home, ".pi", "agent", "settings.json")
	projectPath := filepath.Join(root, ".pi", "settings.json")
	if err := os.WriteFile(globalPath, []byte(`{"theme":"dark","compaction":{"enabled":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectPath, []byte(`{"defaultModel":"gpt-5.5"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewMockStore()
	workspace, err := store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{}, store, NewBroker())

	getReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspace.ID+"/settings", nil)
	getRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", getRes.Code, getRes.Body.String())
	}
	if !strings.Contains(getRes.Body.String(), `"theme":"dark"`) ||
		!strings.Contains(getRes.Body.String(), `"defaultModel":"gpt-5.5"`) ||
		!strings.Contains(getRes.Body.String(), `"transport":"auto"`) ||
		!strings.Contains(getRes.Body.String(), `"imageWidthCells":60`) {
		t.Fatalf("unexpected settings body: %s", getRes.Body.String())
	}

	body := `{"scope":"project","settings":{"defaultModel":null,"transport":"sse","warnings":{"anthropicExtraUsage":null},"compaction":{"enabled":false}}}`
	putReq := httptest.NewRequest(http.MethodPut, "/api/workspaces/"+workspace.ID+"/settings", bytes.NewBufferString(body))
	putRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(putRes, putReq)
	if putRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", putRes.Code, putRes.Body.String())
	}
	saved, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "defaultModel") || strings.Contains(string(saved), "warnings") ||
		!strings.Contains(string(saved), `"transport": "sse"`) {
		t.Fatalf("unexpected saved settings: %s", string(saved))
	}
	if !strings.Contains(putRes.Body.String(), `"enabled":false`) {
		t.Fatalf("effective settings did not include project compaction override: %s", putRes.Body.String())
	}
}

func TestAuthEndpointsSaveListAndLogoutAPIKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	authPath := filepath.Join(home, ".pi", "agent", "auth.json")
	if err := os.MkdirAll(filepath.Dir(authPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authPath, []byte(`{"openai":{"type":"oauth","access":"keep"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{}, NewMockStore(), NewBroker())

	postBody := `{"provider":"anthropic","apiKey":"sk-test-secret"}`
	postReq := httptest.NewRequest(http.MethodPost, "/api/auth/api-key", bytes.NewBufferString(postBody))
	postRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(postRes, postReq)
	if postRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", postRes.Code, postRes.Body.String())
	}
	if strings.Contains(postRes.Body.String(), "sk-test-secret") || !strings.Contains(postRes.Body.String(), `"configured":true`) {
		t.Fatalf("unexpected API key response: %s", postRes.Body.String())
	}
	saved, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "sk-test-secret") || !strings.Contains(string(saved), `"type": "api_key"`) ||
		!strings.Contains(string(saved), `"access": "keep"`) {
		t.Fatalf("unexpected auth file: %s", string(saved))
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/auth/providers", nil)
	getRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusOK || !strings.Contains(getRes.Body.String(), `"id":"anthropic"`) ||
		!strings.Contains(getRes.Body.String(), `"source":"api_key"`) {
		t.Fatalf("unexpected providers response: %d %s", getRes.Code, getRes.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/auth/anthropic", nil)
	deleteRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(deleteRes, deleteReq)
	if deleteRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", deleteRes.Code, deleteRes.Body.String())
	}
	saved, err = os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "anthropic") || strings.Contains(string(saved), "sk-test-secret") {
		t.Fatalf("logout did not remove credential: %s", string(saved))
	}
}

func TestWorkspaceRuntimeSplitEndpointsUseMockStatusWhenPiDisabled(t *testing.T) {
	server := NewServer(Config{EnablePiExecution: false}, NewMockStore(), NewBroker())
	modelReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/pi-mono/runtime-model", nil)
	modelRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(modelRes, modelReq)
	if modelRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", modelRes.Code, modelRes.Body.String())
	}
	if !strings.Contains(modelRes.Body.String(), `"model":"GPT-5.5"`) || strings.Contains(modelRes.Body.String(), "weeklyQuota") {
		t.Fatalf("unexpected model body: %s", modelRes.Body.String())
	}

	quotaReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/pi-mono/runtime-quota?model=GPT-5.5", nil)
	quotaRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(quotaRes, quotaReq)
	if quotaRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", quotaRes.Code, quotaRes.Body.String())
	}
	if !strings.Contains(quotaRes.Body.String(), `"weeklyQuota":14`) || strings.Contains(quotaRes.Body.String(), "currentBranch") {
		t.Fatalf("unexpected quota body: %s", quotaRes.Body.String())
	}
}

func TestSessionContextUsageEndpointUsesMockWhenPiDisabled(t *testing.T) {
	server := NewServer(Config{EnablePiExecution: false}, NewMockStore(), NewBroker())
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/8e7c-44ff/context-usage", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	body := res.Body.String()
	if !strings.Contains(body, `"tokens":60000`) || !strings.Contains(body, `"contextWindow":200000`) || !strings.Contains(body, `"percent":30`) {
		t.Fatalf("unexpected context usage response: %s", body)
	}
}

func TestSessionCompactEndpointRequiresDurableEventStore(t *testing.T) {
	root := t.TempDir()
	writeFakePi(t, root, `#!/bin/sh
read request
id=$(printf '%s' "$request" | sed 's/.*"id":"\([^"]*\)".*/\1/')
printf '%s\n' "{\"id\":\"$id\",\"type\":\"response\",\"command\":\"compact\",\"success\":true,\"data\":{\"tokensBefore\":99,\"estimatedTokensAfter\":22}}"
`)
	store := NewMockStore()
	workspace, err := store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateSession(workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{EnablePiExecution: true}, store, NewBroker())
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID+"/compact", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusServiceUnavailable || !strings.Contains(res.Body.String(), "session event store operation is not ready") {
		t.Fatalf("unexpected compact response: %d %s", res.Code, res.Body.String())
	}
}

func TestSessionSetModelEndpointRejectsWhenPiDisabled(t *testing.T) {
	server := NewServer(Config{EnablePiExecution: false}, NewMockStore(), NewBroker())
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/8e7c-44ff/model",
		strings.NewReader(`{"provider":"openai-codex","model":"gpt-5.6-luna"}`))
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d: %s", res.Code, res.Body.String())
	}
}

func TestCreateSessionEndpoint(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	store := NewMockStore()
	workspace, err := store.OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{}, store, NewBroker())
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspace.ID+"/sessions", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `"title":"new session"`) {
		t.Fatalf("unexpected body: %s", res.Body.String())
	}
}

func TestSteerRequiresDurableEventStore(t *testing.T) {
	server := NewServer(Config{}, NewMockStore(), NewBroker())
	request := httptest.NewRequest(http.MethodPost, "/api/sessions/8e7c-44ff/steer", strings.NewReader(`{"text":"one more thing"}`))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", response.Code, response.Body.String())
	}
}
