package piweb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRenameUsesDurableControlEventWhenConfigured(t *testing.T) {
	store := runtimeTestStore()
	events := appendTestStore(t)
	server := NewServerWithEventStore(Config{EnablePiExecution: true}, store, NewBroker(), events, NewSessionWakeHub())
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	request, err := http.NewRequest(http.MethodPatch, httpServer.URL+"/api/sessions/8e7c-44ff", bytes.NewBufferString(`{"title":"Renamed"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d", response.StatusCode)
	}
	rows, err := events.EventsAfter(t.Context(), "8e7c-44ff", 0, EventReadLimit{MaxRows: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.Events) != 1 || rows.Events[0].Type != EventSessionRenamed {
		t.Fatalf("events = %#v", rows.Events)
	}
	response, err = http.Get(httpServer.URL + "/api/workspaces")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("workspace status = %d", response.StatusCode)
	}
	var payload struct {
		Workspaces []Workspace `json:"workspaces"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, workspace := range payload.Workspaces {
		for _, session := range workspace.Sessions {
			if session.ID != "8e7c-44ff" {
				continue
			}
			found = true
			if session.Title != "Renamed" {
				t.Fatalf("workspace session title = %q", session.Title)
			}
		}
	}
	if !found {
		t.Fatal("renamed session missing from workspace list")
	}
}

func TestDurableSessionReadsUseEventProjection(t *testing.T) {
	store := runtimeTestStore()
	events := appendTestStore(t)
	if _, err := events.AppendControl(t.Context(), "8e7c-44ff", "rename-read", EventSessionRenamed, []byte(`{"title":"Projected"}`)); err != nil {
		t.Fatal(err)
	}
	server := NewServerWithEventStore(Config{EnablePiExecution: true}, store, NewBroker(), events, NewSessionWakeHub())
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	stateResponse, err := http.Get(httpServer.URL + "/api/sessions/8e7c-44ff/state")
	if err != nil {
		t.Fatal(err)
	}
	defer stateResponse.Body.Close()
	var state struct {
		Metadata struct {
			Title string `json:"title"`
		} `json:"metadata"`
		Seq string `json:"seq"`
	}
	if err := json.NewDecoder(stateResponse.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	if state.Metadata.Title != "Projected" || state.Seq != "1" {
		t.Fatalf("state = %#v", state)
	}

	legacyResponse, err := http.Get(httpServer.URL + "/api/sessions/8e7c-44ff")
	if err != nil {
		t.Fatal(err)
	}
	defer legacyResponse.Body.Close()
	var legacy struct {
		LatestEventID string `json:"latestEventId"`
	}
	if err := json.NewDecoder(legacyResponse.Body).Decode(&legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.LatestEventID != "1" {
		t.Fatalf("legacy cursor = %q", legacy.LatestEventID)
	}
}

func TestCreateSessionUsesDurableControlEventWhenConfigured(t *testing.T) {
	store := NewMockStore()
	events := appendTestStore(t)
	server := NewServerWithEventStore(Config{EnablePiExecution: true}, store, NewBroker(), events, NewSessionWakeHub())
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	request, err := http.NewRequest(http.MethodPost, httpServer.URL+"/api/workspaces/pi-mono/sessions", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var body struct {
		Session Session `json:"session"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	rows, err := events.EventsAfter(t.Context(), body.Session.ID, 0, EventReadLimit{MaxRows: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.Events) != 1 || rows.Events[0].Type != EventSessionCreated {
		t.Fatalf("events = %#v", rows.Events)
	}
}

func TestSetModelUsesDurableControlEventWhenConfigured(t *testing.T) {
	store := runtimeTestStore()
	store.sessionFiles["8e7c-44ff"] = "/tmp/8e7c.jsonl"
	store.sessionCWD["8e7c-44ff"] = "/tmp"
	events := appendTestStore(t)
	runtime := newFakePiRuntime()
	manager := NewRuntimeManagerWithFactory(store, func(context.Context, string, string) (PiRuntime, error) {
		return runtime, nil
	})
	server := NewServerWithEventsAndRuntimeManager(Config{EnablePiExecution: true}, store, NewBroker(), manager, events, NewSessionWakeHub())
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	request, err := http.NewRequest(http.MethodPost, httpServer.URL+"/api/sessions/8e7c-44ff/model", bytes.NewBufferString(`{"provider":"openai","model":"gpt"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d", response.StatusCode)
	}
	rows, err := events.EventsAfter(t.Context(), "8e7c-44ff", 0, EventReadLimit{MaxRows: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.Events) != 1 || rows.Events[0].Type != EventSessionModelChanged {
		t.Fatalf("events = %#v", rows.Events)
	}
	_ = manager.Shutdown(context.Background())
}

func TestDeleteUsesDurableControlEventWhenConfigured(t *testing.T) {
	store := runtimeTestStore()
	events := appendTestStore(t)
	server := NewServerWithEventStore(Config{EnablePiExecution: true}, store, NewBroker(), events, NewSessionWakeHub())
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	request, err := http.NewRequest(http.MethodDelete, httpServer.URL+"/api/sessions/8e7c-44ff", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d", response.StatusCode)
	}
	rows, err := events.EventsAfter(t.Context(), "8e7c-44ff", 0, EventReadLimit{MaxRows: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.Events) != 1 || rows.Events[0].Type != EventSessionDeleted {
		t.Fatalf("events = %#v", rows.Events)
	}
	if _, _, err := store.Session("8e7c-44ff"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted session remains in store: %v", err)
	}
}

func TestPromptUsesDurableCommandServiceWhenConfigured(t *testing.T) {
	store := runtimeTestStore()
	store.sessionFiles["8e7c-44ff"] = "/tmp/8e7c.jsonl"
	store.sessionCWD["8e7c-44ff"] = "/tmp"
	events := appendTestStore(t)
	hub := NewSessionWakeHub()
	runtime := newFakePiRuntime()
	manager := NewRuntimeManagerWithFactory(store, func(context.Context, string, string) (PiRuntime, error) {
		return runtime, nil
	})
	server := NewServerWithEventsAndRuntimeManager(Config{EnablePiExecution: true}, store, NewBroker(), manager, events, hub)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	body := `{"clientRequestId":"req-1","text":"hello"}`
	response, err := http.Post(httpServer.URL+"/api/sessions/8e7c-44ff/prompt", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var payload struct {
		RunID       string `json:"runId"`
		AcceptedSeq int64  `json:"acceptedSeq"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.RunID == "" {
		t.Fatal("durable prompt did not return a run id")
	}
	if payload.AcceptedSeq != 3 {
		t.Fatalf("durable prompt accepted seq = %d, want 3", payload.AcceptedSeq)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var count int
		if err := events.db.QueryRow(`SELECT count(*) FROM session_commands WHERE session_id='8e7c-44ff'`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	var commands int
	if err := events.db.QueryRow(`SELECT count(*) FROM session_commands WHERE session_id='8e7c-44ff'`).Scan(&commands); err != nil {
		t.Fatal(err)
	}
	if commands != 1 {
		t.Fatalf("command receipts = %d", commands)
	}
	_ = manager.Shutdown(context.Background())
}

func TestPromptWithoutClientRequestIdGeneratesOne(t *testing.T) {
	store := runtimeTestStore()
	store.sessionFiles["8e7c-44ff"] = "/tmp/8e7c.jsonl"
	store.sessionCWD["8e7c-44ff"] = "/tmp"
	events := appendTestStore(t)
	runtime := newFakePiRuntime()
	manager := NewRuntimeManagerWithFactory(store, func(context.Context, string, string) (PiRuntime, error) {
		return runtime, nil
	})
	server := NewServerWithEventsAndRuntimeManager(Config{EnablePiExecution: true}, store, NewBroker(), manager, events, NewSessionWakeHub())
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	response, err := http.Post(httpServer.URL+"/api/sessions/8e7c-44ff/prompt", "application/json", bytes.NewBufferString(`{"text":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d", response.StatusCode)
	}
	_ = manager.Shutdown(context.Background())
}
