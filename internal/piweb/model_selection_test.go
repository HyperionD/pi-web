package piweb

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func setupAccountModelCatalog(t *testing.T, s *Server, root string) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".pi", "agent", "models.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data := `{"providers":{"local":{"models":[{"id":"a"},{"id":"b"},{"id":"c"}]}}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	writeFakePi(t, root, "#!/bin/sh\necho 'local a'\necho 'local b'\necho 'local c'\necho 'builtin d'\n")
	s.config.EnablePiExecution = true
}

func TestAccountModelChoiceIsGlobalAndIgnoresLegacyProjectOverrides(t *testing.T) {
	s, admin := accountServer(t)
	root := t.TempDir()
	workspace, err := s.store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	setupAccountModelCatalog(t, s, root)
	alice := addAccount(t, s, admin, "alice-model-choice", workspace.ID)
	bob := addAccount(t, s, admin, "bob-model-choice", workspace.ID)
	url := "/api/workspaces/" + workspace.ID + "/settings"
	for _, test := range []struct {
		cookie *http.Cookie
		model  string
	}{{alice, "a"}, {bob, "b"}} {
		body := `{"scope":"personal","location":"global","settings":{"defaultProvider":"local","defaultModel":"` + test.model + `"}}`
		response := accountRequest(s, http.MethodPut, url, body, test.cookie)
		if response.Code != http.StatusOK {
			t.Fatalf("save choice: %d %s", response.Code, response.Body.String())
		}
	}
	aliceID := s.accounts.Users["alice-model-choice"].ID
	if err := savePersonalModelSettings(aliceID, "project", workspace.ID,
		map[string]any{"defaultProvider": "local", "defaultModel": "c"}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".pi", "settings.json"),
		[]byte(`{"defaultProvider":"local","defaultModel":"c"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		cookie *http.Cookie
		model  string
	}{{alice, "a"}, {bob, "b"}} {
		response := accountRequest(s, http.MethodGet, url, "", test.cookie)
		var body struct {
			Settings struct {
				Effective map[string]any `json:"effective"`
			} `json:"settings"`
		}
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &body) != nil ||
			body.Settings.Effective["defaultModel"] != test.model {
			t.Fatalf("wrong account choice: %d %s", response.Code, response.Body.String())
		}
	}
}

func TestRemovedAccountModelFallsBackToConfiguredDefault(t *testing.T) {
	s, admin := accountServer(t)
	root := t.TempDir()
	workspace, err := s.store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	setupAccountModelCatalog(t, s, root)
	alice := addAccount(t, s, admin, "alice-model-removed", workspace.ID)
	url := "/api/workspaces/" + workspace.ID + "/settings"
	if result := accountRequest(s, http.MethodPut, url,
		`{"scope":"personal","location":"global","settings":{"defaultProvider":"local","defaultModel":"c"}}`, alice); result.Code != 200 {
		t.Fatalf("save: %s", result.Body.String())
	}
	path, err := modelConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"providers":{"local":{"models":[{"id":"a"},{"id":"b"}]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	result := accountRequest(s, http.MethodGet, url, "", alice)
	var response struct {
		Settings struct {
			Effective      map[string]any `json:"effective"`
			ModelSelection struct {
				InvalidPersonal bool `json:"invalidPersonal"`
			} `json:"modelSelection"`
		} `json:"settings"`
	}
	if result.Code != 200 || json.Unmarshal(result.Body.Bytes(), &response) != nil ||
		response.Settings.Effective["defaultModel"] != "a" || !response.Settings.ModelSelection.InvalidPersonal {
		t.Fatalf("missing stale-model fallback: %d %s", result.Code, result.Body.String())
	}
}

func TestAccountModelSaveRejectsUnconfiguredOrProjectChoice(t *testing.T) {
	s, admin := accountServer(t)
	root := t.TempDir()
	workspace, err := s.store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	setupAccountModelCatalog(t, s, root)
	alice := addAccount(t, s, admin, "alice-model-validate", workspace.ID)
	url := "/api/workspaces/" + workspace.ID + "/settings"
	for _, body := range []string{
		`{"scope":"personal","location":"global","settings":{"defaultProvider":"builtin","defaultModel":"d"}}`,
		`{"scope":"personal","location":"global","settings":{"defaultModel":"a"}}`,
		`{"scope":"personal","location":"project","settings":{"defaultProvider":"local","defaultModel":"a"}}`,
	} {
		response := accountRequest(s, http.MethodPut, url, body, alice)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("accepted invalid choice: %d %s", response.Code, response.Body.String())
		}
	}
	response := accountRequest(s, http.MethodPut, url,
		`{"scope":"project","settings":{"defaultProvider":"local","defaultModel":"a"}}`, admin)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("accepted shared model patch: %d %s", response.Code, response.Body.String())
	}
}
