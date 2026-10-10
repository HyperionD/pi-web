package piweb

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPiAvailableModelsIncludesConfiguredOpenAIWithoutModelsJSON(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	root := t.TempDir()
	writeFakePi(t, root, "#!/bin/sh\necho 'provider model context max-out thinking images'\necho 'openai gpt-offline 1M 64K yes no'\necho 'local custom 1M 64K yes no'\n")
	catalog, err := configuredModels(context.Background(), root)
	if err != nil || len(catalog.Providers) != 2 ||
		!catalogContains(catalog, "openai", "gpt-offline") || !catalogContains(catalog, "local", "custom") {
		t.Fatalf("Pi-available models were filtered out: %+v %v", catalog, err)
	}
}

func TestPiModelListNeverParsesStderrAsModels(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	writeFakePi(t, root, "#!/bin/sh\necho 'openai gpt-safe'\necho 'apiKey super-secret' >&2\n")
	catalog, err := configuredModels(t.Context(), root)
	if err != nil || !catalogContains(catalog, "openai", "gpt-safe") ||
		catalogContains(catalog, "apiKey", "super-secret") {
		t.Fatalf("Pi stderr entered model list: %+v %v", catalog, err)
	}
}

func TestConfiguredModelsReturnsEveryPiAvailableProviderWithoutSecrets(t *testing.T) {
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
	if len(catalog.Providers) != 2 || catalog.Providers[0].ID != "builtin" ||
		catalog.Providers[1].ID != "local" || len(catalog.Providers[1].Models) != 2 ||
		catalog.Providers[1].Models[0].ID != "a" || catalog.Providers[1].Models[1].ID != "b" ||
		!catalogContains(catalog, "builtin", "c") || !catalogContains(catalog, "local", "a") {
		t.Fatalf("Pi-available models were filtered: %+v", catalog)
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

func TestModelConfigPathFollowsPiAgentDirectoryOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	agentDir := filepath.Join(t.TempDir(), "agent")
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)
	if err := os.MkdirAll(agentDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "models.json"),
		[]byte(`{"providers":{"local":{"models":[{"id":"a"}]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	path, err := modelConfigPath()
	if err != nil || path != filepath.Join(agentDir, "models.json") {
		t.Fatalf("agent directory override ignored: %s %v", path, err)
	}
}

func TestConfiguredModelsRejectsEmptyOrFailedPiList(t *testing.T) {
	for _, script := range []string{
		"#!/bin/sh\necho 'provider model'\n",
		"#!/bin/sh\necho 'secret-token' >&2\nexit 1\n",
	} {
		t.Run(strings.Split(script, "\n")[1], func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			root := t.TempDir()
			writeFakePi(t, root, script)
			catalog, err := configuredModels(t.Context(), root)
			if err == nil || len(catalog.Providers) != 0 || strings.Contains(err.Error(), "secret-token") {
				t.Fatalf("expected safe empty catalog: %+v %v", catalog, err)
			}
		})
	}
}

func TestConfiguredModelsIgnoresOptionalModelsFile(t *testing.T) {
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
			fields := strings.Fields(test.output)
			if err != nil || !catalogContains(catalog, fields[0], fields[1]) {
				t.Fatalf("Pi-available model was hidden by optional file: %+v, %v", catalog, err)
			}
		})
	}
}
