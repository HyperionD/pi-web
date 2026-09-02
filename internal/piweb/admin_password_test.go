package piweb

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdminPasswordSetupStoresOnlyHashAndVerifies(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", filepath.Join(root, "sessions"))

	if configured, err := AdminPasswordConfigured(); err != nil || configured {
		t.Fatalf("initial admin password status = %v, %v", configured, err)
	}
	if err := SetupAdminPassword("correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if configured, err := AdminPasswordConfigured(); err != nil || !configured {
		t.Fatalf("configured admin password status = %v, %v", configured, err)
	}
	if err := VerifyAdminPassword("correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if err := VerifyAdminPassword("wrong password"); err == nil {
		t.Fatal("expected wrong password to fail")
	}

	path, err := AppConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), "correct horse battery") || !strings.Contains(string(stored), "adminPasswordHash") {
		t.Fatalf("password was not stored as a hidden hash: %s", stored)
	}
}

func TestSetupAdminPasswordRejectsSecondSetupAndWeakPassword(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", filepath.Join(root, "sessions"))

	if err := SetupAdminPassword("short"); err == nil {
		t.Fatal("expected weak password to fail")
	}
	if err := SetupAdminPassword("correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if err := SetupAdminPassword("another correct password"); err == nil {
		t.Fatal("expected second setup to fail")
	}
}

func TestAppConfigResponseDoesNotExposeAdminPasswordHash(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", filepath.Join(root, "sessions"))
	if err := SetupAdminPassword("correct horse battery"); err != nil {
		t.Fatal(err)
	}

	response, err := LoadAppConfig()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := response.Config[adminPasswordHashKey]; ok {
		t.Fatalf("app config response exposed %q", adminPasswordHashKey)
	}
}

func TestAdminPasswordAPIRequiresSetupAndProtectsAppConfigPatch(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", filepath.Join(root, "sessions"))
	server := NewServer(Config{}, NewMockStore(), NewBroker())

	status := requestAdminAPI(t, server, http.MethodGet, "/api/admin/password/status", "", "")
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"configured":false`) {
		t.Fatalf("unexpected initial status: %d %s", status.Code, status.Body.String())
	}

	patch := requestAdminAPI(t, server, http.MethodPatch, "/api/app-config", `{"brand":"blocked"}`, "")
	if patch.Code != http.StatusUnauthorized {
		t.Fatalf("expected unconfigured patch to be unauthorized, got %d: %s", patch.Code, patch.Body.String())
	}

	setup := requestAdminAPI(t, server, http.MethodPost, "/api/admin/password", `{"password":"correct horse battery","confirmation":"correct horse battery"}`, "")
	if setup.Code != http.StatusCreated {
		t.Fatalf("expected setup 201, got %d: %s", setup.Code, setup.Body.String())
	}

	badPatch := requestAdminAPI(t, server, http.MethodPatch, "/api/app-config", `{"brand":"blocked"}`, "wrong password")
	if badPatch.Code != http.StatusUnauthorized {
		t.Fatalf("expected wrong password 401, got %d: %s", badPatch.Code, badPatch.Body.String())
	}
	goodPatch := requestAdminAPI(t, server, http.MethodPatch, "/api/app-config", `{"brand":"protected"}`, "correct horse battery")
	if goodPatch.Code != http.StatusOK || !strings.Contains(goodPatch.Body.String(), `"brand":"protected"`) {
		t.Fatalf("expected protected patch to succeed, got %d: %s", goodPatch.Code, goodPatch.Body.String())
	}
}

func TestAdminPasswordAPIRejectsMismatchedConfirmation(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", filepath.Join(root, "sessions"))
	server := NewServer(Config{}, NewMockStore(), NewBroker())

	response := requestAdminAPI(t, server, http.MethodPost, "/api/admin/password", `{"password":"correct horse battery","confirmation":"different password"}`, "")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected mismatched confirmation 400, got %d: %s", response.Code, response.Body.String())
	}
}

func requestAdminAPI(t *testing.T, server *Server, method, path, body, password string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if password != "" {
		request.Header.Set(adminPasswordHeader, password)
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}

func TestAdminPasswordStatusResponseIsJSON(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", filepath.Join(root, "sessions"))
	server := NewServer(Config{}, NewMockStore(), NewBroker())
	response := requestAdminAPI(t, server, http.MethodGet, "/api/admin/password/status", "", "")

	var body map[string]bool
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["configured"]; !ok {
		t.Fatalf("missing configured field: %s", response.Body.String())
	}
}
