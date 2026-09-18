package piweb

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceSkillsEndpointListsAndPatchesSkill(t *testing.T) {
	root := t.TempDir()
	skillPath := filepath.Join(root, ".pi", "skills", "demo", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillPath, []byte("---\nname: demo\ndescription: Demo\n---\nBody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	installSkillPiFixture(t, skillPath, "project")

	store := NewWebStore(filepath.Join(t.TempDir(), "web.db"))
	workspace, err := store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{EnablePiExecution: true}, store, NewBroker())

	request := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspace.ID+"/skills", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", response.Code, response.Body.String())
	}
	rawBody := append([]byte(nil), response.Body.Bytes()...)
	var body SkillDiscoveryResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.Available || len(body.Skills) != 1 || !body.Skills[0].Enabled {
		t.Fatalf("unexpected GET body: %#v", body)
	}
	var raw struct {
		Skills []map[string]any `json:"skills"`
	}
	if err := json.Unmarshal(rawBody, &raw); err != nil {
		t.Fatal(err)
	}
	if raw.Skills[0]["sourceKind"] != "local" || raw.Skills[0]["sourceLabel"] != "项目自定义技能" {
		t.Fatalf("unexpected skill source: %#v", raw.Skills[0])
	}

	patchBody, err := json.Marshal(SkillPatchRequest{Path: skillPath, Enabled: boolPointer(false)})
	if err != nil {
		t.Fatal(err)
	}
	patch := httptest.NewRequest(http.MethodPatch, "/api/workspaces/"+workspace.ID+"/skills", bytes.NewReader(patchBody))
	patch.Header.Set("Content-Type", "application/json")
	patchResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(patchResponse, patch)
	if patchResponse.Code != http.StatusOK {
		t.Fatalf("PATCH status=%d body=%s", patchResponse.Code, patchResponse.Body.String())
	}
	var patched struct {
		Skill Skill `json:"skill"`
	}
	if err := json.NewDecoder(patchResponse.Body).Decode(&patched); err != nil {
		t.Fatal(err)
	}
	if patched.Skill.Enabled {
		t.Fatal("patched skill remained enabled")
	}
	content, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(content, []byte("disable-model-invocation: true")) {
		t.Fatalf("skill content was not disabled: %s", content)
	}
}

func TestWorkspaceSkillsEndpointReportsUnavailablePi(t *testing.T) {
	root := t.TempDir()
	store := NewWebStore(filepath.Join(t.TempDir(), "web.db"))
	workspace, err := store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{EnablePiExecution: false}, store, NewBroker())

	request := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspace.ID+"/skills", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body SkillDiscoveryResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Available || len(body.Skills) != 0 || body.Error == "" {
		t.Fatalf("unexpected unavailable response: %#v", body)
	}
}

func TestWorkspaceSkillPatchRejectsUnknownPath(t *testing.T) {
	root := t.TempDir()
	skillPath := filepath.Join(root, ".pi", "skills", "demo", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillPath, []byte("---\nname: demo\ndescription: Demo\n---\nBody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	installSkillPiFixture(t, skillPath, "project")
	store := NewWebStore(filepath.Join(t.TempDir(), "web.db"))
	workspace, err := store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{EnablePiExecution: true}, store, NewBroker())

	patchBody := fmt.Sprintf(`{"path":%q,"enabled":false}`, filepath.Join(root, "other", "SKILL.md"))
	request := httptest.NewRequest(http.MethodPatch, "/api/workspaces/"+workspace.ID+"/skills", bytes.NewBufferString(patchBody))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func installSkillPiFixture(t *testing.T, skillPath, scope string) {
	t.Helper()
	binDir := t.TempDir()
	script := filepath.Join(binDir, "pi")
	body := fmt.Sprintf(`#!/bin/sh
printf '{"id":"skills","type":"response","command":"get_commands","success":true,"data":{"commands":[{"name":"skill:demo","description":"Demo","source":"skill","sourceInfo":{"path":"%%s","scope":%q}}]}}\n' "$PI_WEB_TEST_SKILL_PATH"
`, scope)
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PI_WEB_TEST_SKILL_PATH", skillPath)
}

func boolPointer(value bool) *bool { return &value }
