package piweb

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestSessionWithoutAccountsDoesNotFallBackWhenPiHasNoModels(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	writeFakePi(t, t.TempDir(), "#!/bin/sh\nexit 1\n")
	s := NewServer(Config{EnablePiExecution: true}, runtimeTestStore(), NewBroker())
	_, err := s.modelSettingsForSession("8e7c-44ff")
	if err == nil {
		t.Fatal("missing catalog silently initialized a built-in model")
	}
}

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
	s.attachEventStore(s.store, appendTestStore(t), NewSessionWakeHub())
	response := accountRequest(s, http.MethodPost, "/api/sessions/"+session.ID+"/model",
		`{"provider":"builtin","model":"d"}`, alice)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("outside catalog was not rejected: %d %s", response.Code, response.Body.String())
	}
	response = accountRequest(s, http.MethodPost, "/api/sessions/"+session.ID+"/model",
		`{"thinkingLevel":"invalid"}`, alice)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid thinking level was not rejected: %d %s", response.Code, response.Body.String())
	}
}

func TestSessionModelSwitchRequiresDurableEventStore(t *testing.T) {
	s, admin := accountServer(t)
	root := t.TempDir()
	workspace, err := s.store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	setupAccountModelCatalog(t, s, root)
	alice := addAccount(t, s, admin, "model-needs-events", workspace.ID)
	session := createAccountSession(t, s, alice, workspace.ID)
	runtime := newFakePiRuntime()
	s.runtimeManager = NewRuntimeManagerWithFactory(s.store, func(context.Context, string, string) (PiRuntime, error) {
		return runtime, nil
	})
	defer s.runtimeManager.Shutdown(context.Background())
	response := accountRequest(s, http.MethodPost, "/api/sessions/"+session.ID+"/model",
		`{"provider":"local","model":"b"}`, alice)
	if response.Code != http.StatusServiceUnavailable || len(runtime.sends) != 0 {
		t.Fatalf("switch without durable events: %d %s sends=%+v", response.Code, response.Body.String(), runtime.sends)
	}
}

type failingModelEventStore struct{ SessionEventStore }

func (f failingModelEventStore) AppendControl(context.Context, string, string, string, []byte) (StoredSessionEvent, error) {
	return StoredSessionEvent{}, errors.New("event store unavailable")
}

func TestSessionModelSwitchRevertsRuntimeWhenEventAppendFails(t *testing.T) {
	s, admin := accountServer(t)
	root := t.TempDir()
	workspace, err := s.store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	setupAccountModelCatalog(t, s, root)
	alice := addAccount(t, s, admin, "model-append-failure", workspace.ID)
	session := createAccountSession(t, s, alice, workspace.ID)
	events := appendTestStore(t)
	s.events = events
	s.commands = NewSessionCommandService(failingModelEventStore{SessionEventStore: events},
		runnerPromptStarter{runner: s.runner, store: s.store})
	runtime := newFakePiRuntime()
	s.runtimeManager = NewRuntimeManagerWithFactory(s.store, func(context.Context, string, string) (PiRuntime, error) {
		return runtime, nil
	})
	defer s.runtimeManager.Shutdown(context.Background())
	response := accountRequest(s, http.MethodPost, "/api/sessions/"+session.ID+"/model",
		`{"provider":"local","model":"b"}`, alice)
	if response.Code < 400 || len(runtime.sends) < 2 ||
		string(runtime.sends[len(runtime.sends)-2].Payload) != `{"modelId":"a","provider":"local"}` {
		t.Fatalf("runtime not reverted after event failure: %d %s sends=%+v",
			response.Code, response.Body.String(), runtime.sends)
	}
}

func TestThinkingOnlySwitchInitializesAccountModel(t *testing.T) {
	s, admin := accountServer(t)
	root := t.TempDir()
	workspace, err := s.store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	setupAccountModelCatalog(t, s, root)
	alice := addAccount(t, s, admin, "thinking-only-initialize", workspace.ID)
	session := createAccountSession(t, s, alice, workspace.ID)
	s.attachEventStore(s.store, appendTestStore(t), NewSessionWakeHub())
	runtime := newFakePiRuntime()
	s.runtimeManager = NewRuntimeManagerWithFactory(s.store, func(context.Context, string, string) (PiRuntime, error) {
		return runtime, nil
	})
	defer s.runtimeManager.Shutdown(context.Background())
	response := accountRequest(s, http.MethodPost, "/api/sessions/"+session.ID+"/model",
		`{"thinkingLevel":"high"}`, alice)
	if response.Code != http.StatusAccepted || len(runtime.sends) != 2 || runtime.sends[0].Type != "set_model" ||
		runtime.sends[1].Type != "set_thinking_level" {
		t.Fatalf("thinking-only switch failed to initialize account model: %d %s sends=%+v",
			response.Code, response.Body.String(), runtime.sends)
	}
}

func TestThinkingOnlySwitchRequiresAvailableCatalog(t *testing.T) {
	s, admin := accountServer(t)
	root := t.TempDir()
	workspace, err := s.store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	setupAccountModelCatalog(t, s, root)
	alice := addAccount(t, s, admin, "thinking-only-catalog", workspace.ID)
	session := createAccountSession(t, s, alice, workspace.ID)
	s.attachEventStore(s.store, appendTestStore(t), NewSessionWakeHub())
	writeFakePi(t, t.TempDir(), "#!/bin/sh\nexit 1\n")
	runtime := newFakePiRuntime()
	s.runtimeManager = NewRuntimeManagerWithFactory(s.store, func(context.Context, string, string) (PiRuntime, error) {
		return runtime, nil
	})
	defer s.runtimeManager.Shutdown(context.Background())
	response := accountRequest(s, http.MethodPost, "/api/sessions/"+session.ID+"/model",
		`{"thinkingLevel":"high"}`, alice)
	if response.Code != http.StatusBadGateway || len(runtime.sends) != 0 {
		t.Fatalf("thinking-only switch bypassed missing catalog: %d %s sends=%+v",
			response.Code, response.Body.String(), runtime.sends)
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

func TestRemovedSessionModelRemainsVisibleButCannotRestart(t *testing.T) {
	s, admin := accountServer(t)
	root := t.TempDir()
	workspace, err := s.store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	setupAccountModelCatalog(t, s, root)
	alice := addAccount(t, s, admin, "removed-session-model", workspace.ID)
	session := createAccountSession(t, s, alice, workspace.ID)
	events := appendTestStore(t)
	if _, err := events.AppendControl(t.Context(), session.ID, "switch-to-b", EventSessionModelChanged,
		[]byte(`{"Provider":"local","Model":"b"}`)); err != nil {
		t.Fatal(err)
	}
	s.events = events
	writeFakePi(t, t.TempDir(), "#!/bin/sh\necho 'local a'\n")
	status, err := s.overlayPersonalRuntimeModel(RuntimeStatus{}, root, workspace.ID, session.ID)
	if err != nil || status.ModelProvider != "local" || status.Model != "b" {
		t.Fatalf("lost historical model status: %+v %v", status, err)
	}
	if _, err := s.modelSettingsForSession(session.ID); err == nil {
		t.Fatal("removed session model was restarted")
	}
}

func TestSessionThinkingOnlyEventUsesAccountDefaultModel(t *testing.T) {
	s, admin := accountServer(t)
	root := t.TempDir()
	workspace, err := s.store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	setupAccountModelCatalog(t, s, root)
	alice := addAccount(t, s, admin, "thinking-only-owner", workspace.ID)
	session := createAccountSession(t, s, alice, workspace.ID)
	events := appendTestStore(t)
	if _, err := events.AppendControl(t.Context(), session.ID, "thinking-only", EventSessionModelChanged,
		[]byte(`{"ThinkingLevel":"high"}`)); err != nil {
		t.Fatal(err)
	}
	s.events = events
	selected, err := s.modelSettingsForSession(session.ID)
	if err != nil || selected.Provider != "local" || selected.Model != "a" || selected.ThinkingLevel != "high" {
		t.Fatalf("lost thinking-only event: %+v %v", selected, err)
	}
}

func TestLatestSessionModelRestoresThinkingBeforeModelSwitch(t *testing.T) {
	events := appendTestStore(t)
	if _, err := events.AppendControl(t.Context(), "s1", "initial", EventSessionModelChanged,
		[]byte(`{"Provider":"local","Model":"a","ThinkingLevel":"high"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := events.AppendControl(t.Context(), "s1", "change", EventSessionModelChanged,
		[]byte(`{"Provider":"local","Model":"b"}`)); err != nil {
		t.Fatal(err)
	}
	selected, found, err := events.LatestSessionModel(t.Context(), "s1")
	if err != nil || !found || selected.Model != "b" || selected.ThinkingLevel != "high" {
		t.Fatalf("session thinking was lost: %+v found=%v err=%v", selected, found, err)
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
