package piweb

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppConfigAPIWorksWithoutWorkspace(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", filepath.Join(root, "sessions"))
	server := NewServer(Config{}, NewWebStore(filepath.Join(root, "web.db")), NewBroker())
	if err := SetupAdminPassword("correct horse battery"); err != nil {
		t.Fatal(err)
	}

	get := httptest.NewRecorder()
	server.Handler().ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/app-config", nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"brand":"pi.dev / web"`) {
		t.Fatalf("GET %d %s", get.Code, get.Body.String())
	}

	patch := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/api/app-config", strings.NewReader(`{"brand":"pi-web / web"}`))
	request.Header.Set(adminPasswordHeader, "correct horse battery")
	server.Handler().ServeHTTP(patch, request)
	if patch.Code != http.StatusOK || !strings.Contains(patch.Body.String(), `"brand":"pi-web / web"`) {
		t.Fatalf("PATCH %d %s", patch.Code, patch.Body.String())
	}
}

func TestAppConfigAPIPatchRejectsMalformedJSON(t *testing.T) {
	server := newAppConfigAPIServer(t)
	request := httptest.NewRequest(http.MethodPatch, "/api/app-config", strings.NewReader(`{"brand":`))
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("PATCH status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAppConfigAPIPatchRejectsNullObject(t *testing.T) {
	server := newAppConfigAPIServer(t)
	request := httptest.NewRequest(http.MethodPatch, "/api/app-config", strings.NewReader(`null`))
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("PATCH status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAppConfigAPIPatchRejectsInvalidBrand(t *testing.T) {
	server := newAppConfigAPIServer(t)
	request := httptest.NewRequest(http.MethodPatch, "/api/app-config", strings.NewReader(`{"brand":42}`))
	request.Header.Set(adminPasswordHeader, "correct horse battery")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("PATCH status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAppConfigAPIPatchClassifiesAllowedRootsValidation(t *testing.T) {
	server := newAppConfigAPIServer(t)
	request := httptest.NewRequest(http.MethodPatch, "/api/app-config", strings.NewReader(
		`{"workspace":{"allowedRoots":["/path/that/does/not/exist"]}}`,
	))
	request.Header.Set(adminPasswordHeader, "correct horse battery")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("PATCH status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAppConfigAPIPatchKeepsStorageFailureAsServerError(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", filepath.Join(root, "sessions"))
	configPath := filepath.Join(root, "pi-web-config.json")
	if err := os.Mkdir(configPath, 0o700); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{}, NewWebStore(filepath.Join(root, "web.db")), NewBroker())
	request := httptest.NewRequest(http.MethodPatch, "/api/app-config", strings.NewReader(`{"brand":"new"}`))
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("PATCH status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAppConfigAPIReportsStorageFailure(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", filepath.Join(root, "sessions"))
	configPath := filepath.Join(root, "pi-web-config.json")
	if err := os.Mkdir(configPath, 0o700); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{}, NewWebStore(filepath.Join(root, "web.db")), NewBroker())
	request := httptest.NewRequest(http.MethodGet, "/api/app-config", nil)
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("GET status=%d body=%s", response.Code, response.Body.String())
	}
}

func newAppConfigAPIServer(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", filepath.Join(root, "sessions"))
	server := NewServer(Config{}, NewWebStore(filepath.Join(root, "web.db")), NewBroker())
	if err := SetupAdminPassword("correct horse battery"); err != nil {
		t.Fatal(err)
	}
	return server
}
