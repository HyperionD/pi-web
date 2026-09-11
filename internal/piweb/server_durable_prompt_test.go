package piweb

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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
		RunID string `json:"runId"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.RunID == "" {
		t.Fatal("durable prompt did not return a run id")
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
