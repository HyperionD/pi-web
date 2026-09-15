package piweb

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type SessionSnapshot struct {
	Session              Session                `json:"session"`
	Items                []ProjectedSessionItem `json:"items"`
	Status               string                 `json:"status"`
	RunID                string                 `json:"runId"`
	Seq                  string                 `json:"seq"`
	Cursor               string                 `json:"cursor"`
	HasMore              bool                   `json:"hasMore"`
	Limit                int                    `json:"limit"`
	ProjectionSchema     int                    `json:"projectionSchemaVersion"`
	ProjectionGeneration string                 `json:"projectionGeneration"`
}

type SessionResponse struct {
	Session       Session   `json:"session"`
	Messages      []Message `json:"messages"`
	LatestEventID string    `json:"latestEventId"`
	Status        string    `json:"status"`
}

func newDurableIntegrationServer(t *testing.T) (*httptest.Server, SessionEventStore, string) {
	t.Helper()
	const sessionID = "8e7c-44ff"
	store := NewMockStore()
	store.sessionFiles[sessionID] = "/tmp/8e7c-44ff.jsonl"
	store.sessionCWD[sessionID] = "/tmp"
	events := appendTestStore(t)
	runtime := newFakePiRuntime()
	runtime.sendHook = func() {
		runtime.mu.Lock()
		if len(runtime.sends) == 0 || runtime.sends[len(runtime.sends)-1].Type != "prompt" {
			runtime.mu.Unlock()
			return
		}
		runtime.mu.Unlock()
		runtime.events <- PiEvent{Type: "message_update", Raw: json.RawMessage(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"hello"}}`)}
		runtime.events <- PiEvent{Type: "message_end", Raw: json.RawMessage(`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"hello"}]}}`)}
		runtime.events <- PiEvent{Type: "agent_end", Raw: json.RawMessage(`{"type":"agent_end"}`)}
	}
	manager := NewRuntimeManagerWithFactory(store, func(context.Context, string, string) (PiRuntime, error) {
		return runtime, nil
	})
	server := NewServerWithEventsAndRuntimeManager(
		Config{EnablePiExecution: true}, store, NewBroker(), manager, events, NewSessionWakeHub(),
	)
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(func() {
		httpServer.Close()
		_ = manager.Shutdown(context.Background())
	})
	return httpServer, events, sessionID
}

func postPromptAndDecodeReceipt(t *testing.T, server *httptest.Server, sessionID, requestID, text string) RunReceipt {
	t.Helper()
	response := postJSON(t, server, "/api/sessions/"+sessionID+"/prompt", map[string]any{
		"clientRequestId": requestID,
		"text":            text,
	})
	defer response.Body.Close()
	var payload struct {
		RunID       string `json:"runId"`
		AcceptedSeq int64  `json:"acceptedSeq"`
		Existing    bool   `json:"existing"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode prompt receipt: %v", err)
	}
	return RunReceipt{RunID: payload.RunID, AcceptedSeq: payload.AcceptedSeq, Existing: payload.Existing}
}

func postJSON(t *testing.T, server *httptest.Server, path string, body any) *http.Response {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode JSON request: %v", err)
	}
	response, err := http.Post(server.URL+path, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		data, _ := io.ReadAll(response.Body)
		response.Body.Close()
		t.Fatalf("POST %s: status=%d body=%s", path, response.StatusCode, data)
	}
	return response
}

func getState(t *testing.T, server *httptest.Server, sessionID string) SessionSnapshot {
	t.Helper()
	response, err := http.Get(server.URL + "/api/sessions/" + sessionID + "/state")
	if err != nil {
		t.Fatalf("GET state: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET state: status=%d", response.StatusCode)
	}
	var snapshot SessionSnapshot
	if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	return snapshot
}

func getSession(t *testing.T, server *httptest.Server, sessionID string) SessionResponse {
	t.Helper()
	response, err := http.Get(server.URL + "/api/sessions/" + sessionID)
	if err != nil {
		t.Fatalf("GET session: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET session: status=%d", response.StatusCode)
	}
	var session SessionResponse
	if err := json.NewDecoder(response.Body).Decode(&session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	return session
}

func mustReadEvents(t *testing.T, events SessionEventStore, sessionID string) []StoredSessionEvent {
	t.Helper()
	var rows []StoredSessionEvent
	var after int64
	for {
		batch, err := events.EventsAfter(context.Background(), sessionID, after, EventReadLimit{MaxRows: 128, MaxBytes: 1024 * 1024})
		if err != nil {
			t.Fatalf("read events: %v", err)
		}
		if batch.ResetRequired {
			t.Fatal("event stream requires reset while reading test fixture")
		}
		rows = append(rows, batch.Events...)
		if len(batch.Events) == 0 {
			return rows
		}
		after = batch.Events[len(batch.Events)-1].Seq
		if len(batch.Events) < 128 {
			return rows
		}
	}
}

func eventually(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not satisfied before timeout")
}

func requireTypes(t *testing.T, events []StoredSessionEvent, types ...string) {
	t.Helper()
	if !hasEventTypes(events, types...) {
		actual := make([]string, 0, len(events))
		for _, event := range events {
			actual = append(actual, event.Type)
		}
		t.Fatalf("event types = %v, want ordered subsequence %v", actual, types)
	}
}

func hasEventTypes(events []StoredSessionEvent, types ...string) bool {
	if len(types) == 0 {
		return true
	}
	index := 0
	for _, event := range events {
		if event.Type != types[index] {
			continue
		}
		index++
		if index == len(types) {
			return true
		}
	}
	return false
}

func TestSessionDomainPromptProducesOneDurableSequence(t *testing.T) {
	server, events, sessionID := newDurableIntegrationServer(t)
	receipt := postPromptAndDecodeReceipt(t, server, sessionID, "req-1", "hello")

	eventually(t, 2*time.Second, func() bool {
		snapshot, err := events.Snapshot(context.Background(), sessionID, SessionPageRequest{Limit: 50})
		return err == nil && snapshot.Status == SessionStatusIdle
	})

	rows := mustReadEvents(t, events, sessionID)
	requireTypes(t, rows,
		EventUserMessageCreated,
		EventRunStarted,
		EventAssistantSegmentStarted,
		EventAssistantSegmentCommit,
		EventRunFinished,
	)
	if receipt.AcceptedSeq != rows[1].Seq {
		t.Fatalf("accepted seq = %d, run.started seq = %d", receipt.AcceptedSeq, rows[1].Seq)
	}
}
