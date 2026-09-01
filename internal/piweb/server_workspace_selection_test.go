package piweb

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func saveAllowedRootsForTest(t *testing.T, roots ...string) {
	t.Helper()
	values := make([]any, len(roots))
	for i, root := range roots {
		values[i] = root
	}
	if _, err := SaveAppConfig(map[string]any{
		"workspace": map[string]any{"allowedRoots": values},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceSelectionHonorsAllowedRoots(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	allowed := t.TempDir()
	outside := t.TempDir()
	server := NewServer(Config{}, NewMockStore(), NewBroker())
	saveAllowedRootsForTest(t, allowed)

	open := httptest.NewRecorder()
	server.Handler().ServeHTTP(open, httptest.NewRequest(http.MethodPost,
		"/api/workspaces/open", strings.NewReader(`{"path":"`+outside+`"}`)))
	if open.Code != http.StatusForbidden {
		t.Fatalf("open status = %d, body = %s", open.Code, open.Body.String())
	}

	folders := httptest.NewRecorder()
	server.Handler().ServeHTTP(folders, httptest.NewRequest(http.MethodGet,
		"/api/system/folders?path="+url.QueryEscape(outside), nil))
	if folders.Code != http.StatusForbidden {
		t.Fatalf("folders status = %d, body = %s", folders.Code, folders.Body.String())
	}
}

func TestWorkspaceSelectionAllowsChildInsideAllowedRoot(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	allowed := t.TempDir()
	child := filepath.Join(allowed, "child")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{}, NewMockStore(), NewBroker())
	saveAllowedRootsForTest(t, allowed)

	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost,
		"/api/workspaces/open", strings.NewReader(`{"path":"`+child+`"}`)))
	if response.Code != http.StatusOK {
		t.Fatalf("open status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestWorkspaceSelectionCloneHonorsAllowedRoots(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	allowed := t.TempDir()
	outside := t.TempDir()
	source := filepath.Join(t.TempDir(), "source.git")
	if output, err := exec.Command("git", "init", "--bare", source).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v: %s", err, output)
	}
	server := NewServer(Config{}, NewMockStore(), NewBroker())
	saveAllowedRootsForTest(t, allowed)

	inside := httptest.NewRecorder()
	server.Handler().ServeHTTP(inside, httptest.NewRequest(http.MethodPost,
		"/api/workspaces/clone", strings.NewReader(`{"parentPath":"`+allowed+`","gitUrl":"`+source+`","name":"inside"}`)))
	if inside.Code != http.StatusCreated {
		t.Fatalf("inside clone status = %d, body = %s", inside.Code, inside.Body.String())
	}

	outsideResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(outsideResponse, httptest.NewRequest(http.MethodPost,
		"/api/workspaces/clone", strings.NewReader(`{"parentPath":"`+outside+`","gitUrl":"`+source+`","name":"outside"}`)))
	if outsideResponse.Code != http.StatusForbidden {
		t.Fatalf("outside clone status = %d, body = %s", outsideResponse.Code, outsideResponse.Body.String())
	}
}

func TestFolderPurposeControlsBrowsingScope(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	allowed := t.TempDir()
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(outside, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{}, NewMockStore(), NewBroker())
	saveAllowedRootsForTest(t, allowed)

	ordinary := httptest.NewRecorder()
	server.Handler().ServeHTTP(ordinary, httptest.NewRequest(http.MethodGet,
		"/api/system/folders?path="+url.QueryEscape(outside), nil))
	if ordinary.Code != http.StatusForbidden {
		t.Fatalf("ordinary folder status = %d, body = %s", ordinary.Code, ordinary.Body.String())
	}

	allowedRoot := httptest.NewRecorder()
	server.Handler().ServeHTTP(allowedRoot, httptest.NewRequest(http.MethodGet,
		"/api/system/folders?path="+url.QueryEscape(outside)+"&purpose=allowed-root", nil))
	if allowedRoot.Code != http.StatusOK || !strings.Contains(allowedRoot.Body.String(), "child") {
		t.Fatalf("allowed-root folder status = %d, body = %s", allowedRoot.Code, allowedRoot.Body.String())
	}

	invalid := httptest.NewRecorder()
	server.Handler().ServeHTTP(invalid, httptest.NewRequest(http.MethodGet,
		"/api/system/folders?path="+url.QueryEscape(allowed)+"&purpose=other", nil))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid purpose status = %d, body = %s", invalid.Code, invalid.Body.String())
	}
}

func TestWorkspaceListFiltersForbiddenPaths(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	allowed := t.TempDir()
	outside := t.TempDir()
	store := emptyStore(filepath.Join(t.TempDir(), "web.db"))
	if _, err := store.OpenWorkspace(allowed); err != nil {
		t.Fatal(err)
	}
	if _, err := store.OpenWorkspace(outside); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{}, store, NewBroker())
	saveAllowedRootsForTest(t, allowed)

	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/workspaces", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("workspaces status = %d, body = %s", response.Code, response.Body.String())
	}
	var body struct {
		Workspaces []Workspace `json:"workspaces"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Workspaces) != 1 || body.Workspaces[0].Path != allowed {
		t.Fatalf("unexpected workspaces: %+v", body.Workspaces)
	}
}

func TestWorkspaceSelectionReportsMalformedPolicyAsServerError(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	configPath, err := AppConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`{"workspace":{"allowedRoots":[42]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{}, NewMockStore(), NewBroker())

	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost,
		"/api/workspaces/open", strings.NewReader(`{"path":"`+t.TempDir()+`"}`)))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("malformed policy status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestWorkspaceSelectionKeepsMalformedRequestAsBadRequest(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	server := NewServer(Config{}, NewMockStore(), NewBroker())

	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost,
		"/api/workspaces/open", strings.NewReader(`{"path":`)))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("malformed request status = %d, body = %s", response.Code, response.Body.String())
	}
}
