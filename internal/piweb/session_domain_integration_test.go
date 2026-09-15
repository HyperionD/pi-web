package piweb

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type SessionSnapshot struct {
	Session              Session                   `json:"session"`
	Metadata             SessionMetadataProjection `json:"metadata"`
	Items                []ProjectedSessionItem    `json:"items"`
	Status               string                    `json:"status"`
	RunID                string                    `json:"runId"`
	Seq                  string                    `json:"seq"`
	Cursor               string                    `json:"cursor"`
	HasMore              bool                      `json:"hasMore"`
	Limit                int                       `json:"limit"`
	ProjectionSchema     int                       `json:"projectionSchemaVersion"`
	ProjectionGeneration string                    `json:"projectionGeneration"`
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
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(sessionPath, []byte(`{"type":"session","id":"8e7c-44ff"}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	store.sessionFiles[sessionID] = sessionPath
	store.sessionCWD[sessionID] = t.TempDir()
	hub := NewSessionWakeHub()
	events, err := OpenSQLiteSessionEventStore(filepath.Join(t.TempDir(), "events.db"), hub)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = events.Close(t.Context()) })
	seedEventStream(t, events, sessionID)
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
		Config{EnablePiExecution: true}, store, NewBroker(), manager, events, hub,
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
	response := postJSON(t, server, "/api/sessions/"+url.PathEscape(sessionID)+"/prompt", map[string]any{
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
	return requestJSON(t, server, http.MethodPost, path, body)
}

func requestJSON(t *testing.T, server *httptest.Server, method, path string, body any) *http.Response {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode JSON request: %v", err)
	}
	request, err := http.NewRequest(method, server.URL+path, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("create %s request: %v", method, err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		data, _ := io.ReadAll(response.Body)
		response.Body.Close()
		t.Fatalf("%s %s: status=%d body=%s", method, path, response.StatusCode, data)
	}
	return response
}

func getState(t *testing.T, server *httptest.Server, sessionID string) SessionSnapshot {
	t.Helper()
	response, err := http.Get(server.URL + "/api/sessions/" + url.PathEscape(sessionID) + "/state")
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
	response, err := http.Get(server.URL + "/api/sessions/" + url.PathEscape(sessionID))
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
		if len(batch.Events) == 0 || len(batch.Events) < 128 {
			return rows
		}
		after = batch.Events[len(batch.Events)-1].Seq
	}
}

func openSessionEventStream(t *testing.T, server *httptest.Server, sessionID string) (<-chan sessionEventEnvelope, func()) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, server.URL+"/api/sessions/"+url.PathEscape(sessionID)+"/events-v2?after=0", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("connect events-v2: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		t.Fatalf("connect events-v2: status=%d", response.StatusCode)
	}
	stream := make(chan sessionEventEnvelope, 32)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer close(stream)
		scanner := bufio.NewScanner(response.Body)
		scanner.Buffer(make([]byte, 1024), 1024*1024)
		var eventName, data string
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case line == "":
				if eventName == sseEventName && data != "" {
					var event sessionEventEnvelope
					if json.Unmarshal([]byte(data), &event) == nil {
						stream <- event
					}
				}
				eventName, data = "", ""
			case strings.HasPrefix(line, "event:"):
				eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				if data != "" {
					data += "\n"
				}
				data += strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
		}
	}()
	var closeOnce sync.Once
	closeStream := func() {
		closeOnce.Do(func() { _ = response.Body.Close() })
		<-done
	}
	return stream, closeStream
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
		t.Fatalf("event types = %v, want complete ordered sequence %v", actual, types)
	}
	for index, event := range events {
		wantSeq := int64(index + 1)
		if event.Seq != wantSeq {
			t.Fatalf("event seq at index %d = %d, want unique continuous seq %d", index, event.Seq, wantSeq)
		}
	}
}

func hasEventTypes(events []StoredSessionEvent, types ...string) bool {
	if len(events) != len(types) {
		return false
	}
	for index, event := range events {
		if event.Type != types[index] {
			return false
		}
	}
	return true
}

func TestSessionDomainControlCommandsShareDurableProjection(t *testing.T) {
	server, events, sessionID := newDurableIntegrationServer(t)
	rename := requestJSON(t, server, http.MethodPatch, "/api/sessions/"+url.PathEscape(sessionID), map[string]any{
		"title": "renamed", "clientRequestId": "rename-integration",
	})
	defer rename.Body.Close()
	if rename.StatusCode != http.StatusAccepted {
		t.Fatalf("rename status = %d, want %d", rename.StatusCode, http.StatusAccepted)
	}
	state := getState(t, server, sessionID)
	if state.Metadata.Title != "renamed" {
		t.Fatalf("projected title = %q", state.Metadata.Title)
	}

	deleted := requestJSON(t, server, http.MethodDelete, "/api/sessions/"+url.PathEscape(sessionID), map[string]any{})
	deleted.Body.Close()
	if deleted.StatusCode != http.StatusAccepted {
		t.Fatalf("delete status = %d, want %d", deleted.StatusCode, http.StatusAccepted)
	}
	state = getState(t, server, sessionID)
	if !state.Metadata.Deleted {
		t.Fatal("durable delete did not project tombstone")
	}
	requireTypes(t, mustReadEvents(t, events, sessionID), EventSessionRenamed, EventSessionRebased, EventSessionDeleted)
}

func TestSessionDomainCancelAppendsDurableRequest(t *testing.T) {
	server, events, sessionID := newDurableIntegrationServer(t)
	response := postJSON(t, server, "/api/sessions/"+url.PathEscape(sessionID)+"/cancel", map[string]any{})
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("cancel status = %d, want %d", response.StatusCode, http.StatusAccepted)
	}
	rows := mustReadEvents(t, events, sessionID)
	requireTypes(t, rows, EventRunCancelRequested)
}

func TestSessionDomainPromptProducesOneDurableSequence(t *testing.T) {
	server, events, sessionID := newDurableIntegrationServer(t)
	stream, closeStream := openSessionEventStream(t, server, sessionID)
	defer closeStream()
	receipt := postPromptAndDecodeReceipt(t, server, sessionID, "req-1", "hello")

	var streamed []sessionEventEnvelope
	eventually(t, 2*time.Second, func() bool {
		for {
			select {
			case event, ok := <-stream:
				if !ok {
					return false
				}
				streamed = append(streamed, event)
				if event.Type == EventRunFinished {
					return true
				}
			default:
				return false
			}
		}
	})

	rows := mustReadEvents(t, events, sessionID)
	expectedTypes := []string{
		EventSessionRenamed,
		EventUserMessageCreated,
		EventRunStarted,
		EventAssistantSegmentStarted,
		EventAssistantSegmentDelta,
		EventAssistantSegmentCommit,
		EventRunFinished,
	}
	requireTypes(t, rows, expectedTypes...)
	if len(streamed) != len(rows) {
		t.Fatalf("SSE event count = %d, durable event count = %d", len(streamed), len(rows))
	}
	for index, event := range streamed {
		if event.Seq != strconv.FormatInt(rows[index].Seq, 10) || event.Type != rows[index].Type {
			t.Fatalf("SSE event[%d] = (%s, %s), durable = (%d, %s)", index, event.Seq, event.Type, rows[index].Seq, rows[index].Type)
		}
	}

	snapshot := getState(t, server, sessionID)
	if snapshot.Status != SessionStatusIdle || snapshot.Seq != strconv.FormatInt(rows[len(rows)-1].Seq, 10) {
		t.Fatalf("projection status/seq = (%q, %q), want (idle, %d)", snapshot.Status, snapshot.Seq, rows[len(rows)-1].Seq)
	}
	if len(snapshot.Items) != 2 {
		t.Fatalf("projection items = %d, want user and assistant", len(snapshot.Items))
	}
	if user := snapshot.Items[0]; user.Kind != "user" || user.Text != "hello" || user.Status != "" {
		t.Fatalf("projected user item = %#v", user)
	}
	if assistant := snapshot.Items[1]; assistant.Kind != "pi" || assistant.Text != "hello" || assistant.Status != ItemStatusCommitted {
		t.Fatalf("projected assistant item = %#v", assistant)
	}
	if receipt.AcceptedSeq != rows[2].Seq {
		t.Fatalf("accepted seq = %d, run.started seq = %d (API receipt must expose acceptedSeq)", receipt.AcceptedSeq, rows[2].Seq)
	}
}
