package piweb

import (
	"net/http"
	"testing"
)

func TestSessionRejectsModelsOutsideConfiguredCatalog(t *testing.T) {
	s, admin := accountServer(t)
	root := t.TempDir()
	workspace, err := s.store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	setupAccountModelCatalog(t, s, root)
	alice := addAccount(t, s, admin, "alice-session-model", workspace.ID)
	session := createAccountSession(t, s, alice, workspace.ID)
	response := accountRequest(s, http.MethodPost, "/api/sessions/"+session.ID+"/model",
		`{"provider":"builtin","model":"d"}`, alice)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("outside catalog was not rejected: %d %s", response.Code, response.Body.String())
	}
}

func TestSessionEventModelWinsOverAccountDefault(t *testing.T) {
	s, admin := accountServer(t)
	root := t.TempDir()
	workspace, err := s.store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	setupAccountModelCatalog(t, s, root)
	alice := addAccount(t, s, admin, "alice-session-event", workspace.ID)
	session := createAccountSession(t, s, alice, workspace.ID)
	if response := accountRequest(s, http.MethodPut, "/api/workspaces/"+workspace.ID+"/settings",
		`{"scope":"personal","location":"global","settings":{"defaultProvider":"local","defaultModel":"a"}}`, alice); response.Code != 200 {
		t.Fatalf("save account default: %d %s", response.Code, response.Body.String())
	}
	events := appendTestStore(t)
	if _, err := events.AppendControl(t.Context(), session.ID, "model-change", EventSessionModelChanged,
		[]byte(`{"Provider":"local","Model":"b"}`)); err != nil {
		t.Fatal(err)
	}
	s.events = events
	selected, err := s.modelSettingsForSession(session.ID)
	if err != nil || selected.Provider != "local" || selected.Model != "b" {
		t.Fatalf("session model was overwritten: %+v %v", selected, err)
	}
	status, err := s.overlayPersonalRuntimeModel(RuntimeStatus{}, root, workspace.ID, session.ID)
	if err != nil || status.ModelProvider != "local" || status.Model != "b" {
		t.Fatalf("status hid session model: %+v %v", status, err)
	}
}

func TestLatestSessionModelKeepsModelAcrossThinkingOnlyUpdates(t *testing.T) {
	events := appendTestStore(t)
	if _, err := events.AppendControl(t.Context(), "s1", "choose", EventSessionModelChanged,
		[]byte(`{"Provider":"local","Model":"b","ThinkingLevel":"high"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := events.AppendControl(t.Context(), "s1", "thinking", EventSessionModelChanged,
		[]byte(`{"ThinkingLevel":"low"}`)); err != nil {
		t.Fatal(err)
	}
	model, found, err := events.LatestSessionModel(t.Context(), "s1")
	if err != nil || !found || model.Provider != "local" || model.Model != "b" || model.ThinkingLevel != "low" {
		t.Fatalf("lost session model: %+v found=%v err=%v", model, found, err)
	}
}
