package piweb

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func serverWithAllowedAndForbiddenWorkspace(t *testing.T) (*Server, Workspace, Workspace) {
	t.Helper()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	allowedRoot := t.TempDir()
	outsideRoot := t.TempDir()
	store := NewMockStore()
	allowed, err := store.OpenWorkspace(allowedRoot)
	if err != nil {
		t.Fatal(err)
	}
	outside, err := store.OpenWorkspace(outsideRoot)
	if err != nil {
		t.Fatal(err)
	}
	saveAllowedRootsForTest(t, allowedRoot)
	return NewServer(Config{}, store, NewBroker()), allowed, outside
}

func TestWorkspaceRoutesRejectForbiddenWorkspace(t *testing.T) {
	server, workspace, outside := serverWithAllowedAndForbiddenWorkspace(t)
	cases := []struct {
		method string
		suffix string
	}{
		{http.MethodDelete, ""},
		{http.MethodGet, "/sessions"},
		{http.MethodPost, "/sessions"},
		{http.MethodDelete, "/sessions"},
		{http.MethodGet, "/files"},
		{http.MethodGet, "/files/raw"},
		{http.MethodGet, "/files/search"},
		{http.MethodGet, "/commands"},
		{http.MethodGet, "/models"},
		{http.MethodGet, "/runtime-status"},
		{http.MethodGet, "/runtime-model"},
		{http.MethodGet, "/runtime-quota"},
		{http.MethodGet, "/settings"},
		{http.MethodPut, "/settings"},
		{http.MethodGet, "/files/read"},
		{http.MethodPut, "/files/write"},
		{http.MethodPost, "/files/create"},
		{http.MethodPatch, "/files/rename"},
		{http.MethodDelete, "/files/delete"},
		{http.MethodPost, "/files/upload"},
		{http.MethodGet, "/git/status"},
		{http.MethodGet, "/git/history"},
		{http.MethodGet, "/git/commit"},
		{http.MethodPost, "/shell"},
	}
	for _, tc := range cases {
		requestPath := "/api/workspaces/" + outside.ID + tc.suffix
		response := serveRequest(server, tc.method, requestPath, nil)
		if response.Code != http.StatusForbidden {
			t.Errorf("%s %s: got %d, want %d", tc.method, requestPath, response.Code, http.StatusForbidden)
		}
	}

	allowedResponse := serveRequest(server, http.MethodGet, "/api/workspaces/"+workspace.ID+"/files", nil)
	if allowedResponse.Code == http.StatusForbidden {
		t.Fatalf("allowed workspace was forbidden: %s", allowedResponse.Body.String())
	}
	missingResponse := serveRequest(server, http.MethodGet, "/api/workspaces/missing/files", nil)
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("missing workspace: got %d, want %d", missingResponse.Code, http.StatusNotFound)
	}
}

func TestSessionRoutesRejectForbiddenWorkspace(t *testing.T) {
	server, allowed, outside := serverWithAllowedAndForbiddenWorkspace(t)
	store := server.store
	outsideSession, err := store.CreateSession(outside.ID)
	if err != nil {
		t.Fatal(err)
	}
	allowedSession, err := store.CreateSession(allowed.ID)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		method string
		suffix string
	}{
		{http.MethodGet, ""},
		{http.MethodGet, "/context-usage"},
		{http.MethodPost, "/model"},
		{http.MethodPatch, ""},
		{http.MethodDelete, ""},
		{http.MethodPost, "/prompt"},
		{http.MethodPost, "/ag-ui"},
		{http.MethodPost, "/compact"},
		{http.MethodPost, "/steer"},
		{http.MethodPost, "/cancel"},
		{http.MethodGet, "/events"},
	}
	for _, tc := range cases {
		requestPath := "/api/sessions/" + outsideSession.ID + tc.suffix
		response := serveRequest(server, tc.method, requestPath, nil)
		if response.Code != http.StatusForbidden {
			t.Errorf("%s %s: got %d, want %d", tc.method, requestPath, response.Code, http.StatusForbidden)
		}
	}

	allowedResponse := serveRequest(server, http.MethodGet, "/api/sessions/"+allowedSession.ID, nil)
	if allowedResponse.Code == http.StatusForbidden {
		t.Fatalf("allowed session was forbidden: %s", allowedResponse.Body.String())
	}
	missingResponse := serveRequest(server, http.MethodGet, "/api/sessions/missing", nil)
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("missing session: got %d, want %d", missingResponse.Code, http.StatusNotFound)
	}
}

func TestAuthorizedSessionStatusRoutesRemainReachable(t *testing.T) {
	server, allowed, _ := serverWithAllowedAndForbiddenWorkspace(t)
	session, err := server.store.CreateSession(allowed.ID)
	if err != nil {
		t.Fatal(err)
	}
	contextResponse := serveRequest(server, http.MethodGet, "/api/sessions/"+session.ID+"/context-usage", nil)
	if contextResponse.Code != http.StatusOK {
		t.Fatalf("context usage: got %d, want %d: %s", contextResponse.Code, http.StatusOK, contextResponse.Body.String())
	}
	modelResponse := serveRequest(server, http.MethodPost, "/api/sessions/"+session.ID+"/model", nil)
	if modelResponse.Code != http.StatusNotImplemented {
		t.Fatalf("model: got %d, want %d: %s", modelResponse.Code, http.StatusNotImplemented, modelResponse.Body.String())
	}
}

func TestSessionStatusEndpointRemainsUnregistered(t *testing.T) {
	server := NewServer(Config{}, NewMockStore(), NewBroker())
	response := serveRequest(server, http.MethodGet, "/api/sessions/8e7c-44ff/status", nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status endpoint: got %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestSessionRoutesReturnInternalServerErrorWhenWorkspacePolicyLoadFails(t *testing.T) {
	server, allowed, _ := serverWithAllowedAndForbiddenWorkspace(t)
	session, err := server.store.CreateSession(allowed.ID)
	if err != nil {
		t.Fatal(err)
	}
	configPath, err := AppConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`{"workspace":{"allowedRoots":[42]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadWorkspacePolicy(); err == nil {
		t.Fatal("LoadWorkspacePolicy succeeded with invalid allowedRoots")
	}

	cases := []struct {
		method string
		suffix string
	}{
		{http.MethodGet, "/context-usage"},
		{http.MethodPost, "/model"},
	}
	for _, tc := range cases {
		requestPath := "/api/sessions/" + session.ID + tc.suffix
		response := serveRequest(server, tc.method, requestPath, nil)
		if response.Code != http.StatusInternalServerError {
			t.Errorf("%s %s: got %d, want %d", tc.method, requestPath, response.Code, http.StatusInternalServerError)
		}
	}
}

func serveRequest(server *Server, method, path string, body io.Reader) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, body)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}
