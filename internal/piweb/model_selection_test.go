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
	path, err := modelConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data := `{"providers":{"local":{"models":[{"id":"a"},{"id":"b"},{"id":"c"}]}}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	writeFakePi(t, root, "#!/bin/sh\necho 'local a'\necho 'local b'\necho 'local c'\n")
	s.config.EnablePiExecution = true
}

func TestPartialLegacyModelPreferenceIsReportedAsInvalid(t *testing.T) {
	catalog := WorkspaceModelsResponse{Providers: []ModelProvider{{ID: "local", Models: []ModelOption{{ID: "a", Provider: "local"}}}}}
	selected, invalid := resolveModelSelection(catalog, map[string]any{}, map[string]any{"defaultProvider": "local"})
	if !invalid || selected.Provider != "local" || selected.Model != "a" {
		t.Fatalf("partial legacy preference was not reported: %+v invalid=%v", selected, invalid)
	}
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

func TestAccountCanSelectPiConfiguredOpenAIWithoutModelsJSON(t *testing.T) {
	s, admin := accountServer(t)
	root := t.TempDir()
	workspace, err := s.store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	writeFakePi(t, root, "#!/bin/sh\necho 'openai gpt-offline'\n")
	s.config.EnablePiExecution = true
	alice := addAccount(t, s, admin, "openai-account", workspace.ID)
	url := "/api/workspaces/" + workspace.ID + "/settings"
	response := accountRequest(s, http.MethodPut, url,
		`{"scope":"personal","location":"global","settings":{"defaultProvider":"openai","defaultModel":"gpt-offline"}}`, alice)
	if response.Code != http.StatusOK {
		t.Fatalf("Pi-configured openai model was rejected: %d %s", response.Code, response.Body.String())
	}
	var result struct {
		Settings struct {
			Effective map[string]any `json:"effective"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil ||
		result.Settings.Effective["defaultProvider"] != "openai" ||
		result.Settings.Effective["defaultModel"] != "gpt-offline" {
		t.Fatalf("account openai selection not effective: %s %v", response.Body.String(), err)
	}
}

func TestAccountModelChoiceSurvivesWorkspaceChangeAndServerRestart(t *testing.T) {
	s, admin := accountServer(t)
	firstRoot, secondRoot := t.TempDir(), t.TempDir()
	first, err := s.store.OpenWorkspace(firstRoot)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.store.OpenWorkspace(secondRoot)
	if err != nil {
		t.Fatal(err)
	}
	setupAccountModelCatalog(t, s, firstRoot)
	url := "/api/workspaces/" + first.ID + "/settings"
	if response := accountRequest(s, http.MethodPut, url,
		`{"scope":"personal","location":"global","settings":{"defaultProvider":"local","defaultModel":"c"}}`, admin); response.Code != 200 {
		t.Fatalf("save model: %d %s", response.Code, response.Body.String())
	}
	checkChoice := func(server *Server, cookie *http.Cookie) {
		t.Helper()
		response := accountRequest(server, http.MethodGet, "/api/workspaces/"+second.ID+"/settings", "", cookie)
		var result struct {
			Settings struct {
				Effective map[string]any `json:"effective"`
			} `json:"settings"`
		}
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil ||
			result.Settings.Effective["defaultModel"] != "c" {
			t.Fatalf("account model was lost: %d %s", response.Code, response.Body.String())
		}
	}
	checkChoice(s, admin)
	restarted := NewServer(Config{UserManagement: true, EnablePiExecution: true}, s.store, NewBroker())
	login := accountRequest(restarted, http.MethodPost, "/api/account/login",
		`{"username":"admin","password":"admin-password"}`, nil)
	if login.Code != 200 || len(login.Result().Cookies()) == 0 {
		t.Fatalf("login after restart: %d %s", login.Code, login.Body.String())
	}
	checkChoice(restarted, login.Result().Cookies()[0])
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
	writeFakePi(t, t.TempDir(), "#!/bin/sh\necho 'local a'\necho 'local b'\n")
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
		`{"scope":"personal","location":"global","settings":{"defaultProvider":"unknown","defaultModel":"d"}}`,
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
