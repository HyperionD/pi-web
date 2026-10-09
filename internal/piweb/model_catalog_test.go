package piweb

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfiguredModelsOnlyIncludesDeclaredAvailableModels(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".pi", "agent", "models.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data := `{"providers":{"local":{"api":"openai-completions","baseUrl":"http://127.0.0.1/v1","apiKey":"secret","models":[{"id":"b"},{"id":"a"}]}}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	writeFakePi(t, root, "#!/bin/sh\necho 'provider model context max-out thinking images'\necho 'local a 1M 64K yes no'\necho 'local b 1M 64K yes no'\necho 'builtin c 1M 64K yes no'\n")
	catalog, err := configuredModels(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Providers) != 1 || catalog.Providers[0].ID != "local" ||
		len(catalog.Providers[0].Models) != 2 || catalog.Providers[0].Models[0].ID != "a" ||
		catalog.Providers[0].Models[1].ID != "b" {
		t.Fatalf("wrong allowed models: %+v", catalog)
	}
	if catalogContains(catalog, "builtin", "c") || !catalogContains(catalog, "local", "a") {
		t.Fatal("catalog membership ignored administrator configuration")
	}
}

func TestModelCatalogPathIsOnlyVisibleToAdministrator(t *testing.T) {
	s, admin := accountServer(t)
	workspace, err := s.store.OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	user := addAccount(t, s, admin, "model-reader", workspace.ID)
	url := "/api/workspaces/" + workspace.ID + "/models"
	adminResponse := accountRequest(s, http.MethodGet, url, "", admin)
	userResponse := accountRequest(s, http.MethodGet, url, "", user)
	if adminResponse.Code != http.StatusOK || !strings.Contains(adminResponse.Body.String(), `"path"`) {
		t.Fatalf("admin cannot see catalog diagnostics: %d %s", adminResponse.Code, adminResponse.Body.String())
	}
	if userResponse.Code != http.StatusOK || strings.Contains(userResponse.Body.String(), `"path"`) {
		t.Fatalf("user saw server path: %d %s", userResponse.Code, userResponse.Body.String())
	}
}

func TestConfiguredModelsRejectsInvalidOrUnavailableDirectory(t *testing.T) {
	for _, test := range []struct {
		name, content, output string
	}{
		{"missing", "", "local a"},
		{"invalid json", "{", "local a"},
		{"empty", `{"providers":{}}`, "local a"},
		{"duplicate ids", `{"providers":{"local":{"models":[{"id":"a"},{"id":"a"}]}}}`, "local a"},
		{"unavailable", `{"providers":{"local":{"models":[{"id":"a"}]}}}`, "builtin c"},
	} {
		t.Run(test.name, func(t *testing.T) {
			home, root := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			if test.content != "" {
				path := filepath.Join(home, ".pi", "agent", "models.json")
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			writeFakePi(t, root, "#!/bin/sh\necho '"+test.output+"'\n")
			catalog, err := configuredModels(context.Background(), root)
			if err == nil || len(catalog.Providers) != 0 || strings.Contains(err.Error(), "secret") {
				t.Fatalf("expected safe empty catalog and error, got %+v, %v", catalog, err)
			}
		})
	}
}
