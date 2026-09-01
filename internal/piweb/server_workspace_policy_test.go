package piweb

import (
	"io"
	"net/http"
	"net/http/httptest"
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

func serveRequest(server *Server, method, path string, body io.Reader) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, body)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}
