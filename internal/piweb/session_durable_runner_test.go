package piweb

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStartDurablePromptPersistsRuntimeLifecycle(t *testing.T) {
	store := runtimeTestStore()
	events := appendTestStore(t)
	receipt, err := events.StartRun(t.Context(), StartRunCommand{
		SessionID: "runtime-session", ClientRequestID: "req-1", RequestHash: "h", RunID: "run-1", DisplayText: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := newFakePiRuntime()
	manager := NewRuntimeManagerWithFactory(store, func(context.Context, string, string) (PiRuntime, error) {
		return runtime, nil
	})
	runner := NewRunnerWithRuntimeManager(manager)
	adapter := NewRuntimeEventAdapter("runtime-session", receipt.RunID, events)
	if err := runner.StartDurablePrompt(t.Context(), store, "runtime-session", receipt.RunID, "hello", nil, adapter); err != nil {
		t.Fatal(err)
	}
	if err := runner.Steer("runtime-session", "continue", nil); err != nil {
		t.Fatalf("steer: %v", err)
	}
	var steeringMessages int
	if err := events.db.QueryRow(`SELECT count(*) FROM session_events WHERE event_type=? AND json_extract(payload_json, '$.text')=?`, EventUserMessageCreated, "continue").Scan(&steeringMessages); err != nil {
		t.Fatal(err)
	}
	if steeringMessages != 1 {
		t.Fatalf("steering user messages = %d", steeringMessages)
	}
	runtime.events <- PiEvent{Type: "tool_execution_start", Raw: json.RawMessage(`{"type":"tool_execution_start","toolCallId":"call-1","toolName":"bash","args":{"command":"pwd"}}`)}
	runtime.events <- PiEvent{Type: "tool_execution_end", Raw: json.RawMessage(`{"type":"tool_execution_end","toolCallId":"call-1","toolName":"bash","result":"ok"}`)}
	runtime.events <- PiEvent{Type: "agent_end", Raw: json.RawMessage(`{"type":"agent_end"}`)}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var count int
		if err := events.db.QueryRow(`SELECT count(*) FROM session_events WHERE event_type=?`, EventRunFinished).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	snapshot, err := events.Snapshot(t.Context(), "runtime-session", SessionPageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != SessionStatusIdle {
		t.Fatalf("status = %q", snapshot.Status)
	}
	var tool *ProjectedSessionItem
	for index := range snapshot.Items {
		if snapshot.Items[index].Kind == "tool" {
			tool = &snapshot.Items[index]
		}
	}
	if tool == nil || tool.Status != ToolStatusOK || tool.ToolCallID != "call-1" {
		t.Fatalf("tool = %#v", tool)
	}
	if runner.IsRunning("runtime-session") {
		t.Fatal("runner still holds the session after agent_end")
	}
	_ = manager.Shutdown(context.Background())
}

type failFirstAssistantSink struct {
	store  *SQLiteSessionEventStore
	failed bool
}

func (sink *failFirstAssistantSink) Append(ctx context.Context, sessionID string, draft SessionEventDraft) (StoredSessionEvent, error) {
	if draft.Type == EventAssistantSegmentStarted && !sink.failed {
		sink.failed = true
		return StoredSessionEvent{}, errors.New("injected assistant append failure")
	}
	return sink.store.Append(ctx, sessionID, draft)
}

type failedEventSink struct{}

func (*failedEventSink) Append(context.Context, string, SessionEventDraft) (StoredSessionEvent, error) {
	return StoredSessionEvent{}, errors.New("event store unavailable")
}

func TestDurablePersistenceFailureRetiresRuntimeForStartupRecovery(t *testing.T) {
	store := runtimeTestStore()
	events := appendTestStore(t)
	receipt, err := events.StartRun(t.Context(), StartRunCommand{
		SessionID: "runtime-session", ClientRequestID: "req-persistence", RequestHash: "h",
		RunID: "run-persistence", DisplayText: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := newFakePiRuntime()
	manager := NewRuntimeManagerWithFactory(store, func(context.Context, string, string) (PiRuntime, error) {
		return runtime, nil
	})
	runner := NewRunnerWithRuntimeManager(manager)
	adapter := NewRuntimeEventAdapter("runtime-session", receipt.RunID, &failedEventSink{})
	if err := runner.StartDurablePrompt(t.Context(), store, "runtime-session", receipt.RunID, "hello", nil, adapter); err != nil {
		t.Fatal(err)
	}
	runtime.events <- PiEvent{Type: "message_update", Raw: json.RawMessage(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"hello"}}`)}

	deadline := time.Now().Add(2 * time.Second)
	for runner.IsRunning("runtime-session") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	runtime.mu.Lock()
	closed := runtime.closed
	runtime.mu.Unlock()
	var status, runID string
	query := `SELECT status, coalesce(current_run_id, '') FROM session_streams WHERE session_id='runtime-session'`
	if err := events.db.QueryRow(query).Scan(&status, &runID); err != nil {
		t.Fatal(err)
	}
	if runner.IsRunning("runtime-session") || closed != 1 || status != SessionStatusRunning || runID != receipt.RunID {
		t.Fatalf("running=%v closed=%d status=%q run=%q", runner.IsRunning("runtime-session"), closed, status, runID)
	}
	if err := events.RecoverInterrupted(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := events.db.QueryRow(query).Scan(&status, &runID); err != nil {
		t.Fatal(err)
	}
	if status != SessionStatusInterrupted || runID != "" {
		t.Fatalf("recovered status=%q run=%q", status, runID)
	}
	_ = manager.Shutdown(context.Background())
}

func TestDurablePromptCapturesEventsEmittedDuringStartup(t *testing.T) {
	store := runtimeTestStore()
	events := appendTestStore(t)
	receipt, err := events.StartRun(t.Context(), StartRunCommand{
		SessionID: "runtime-session", ClientRequestID: "req-eager", RequestHash: "hash-eager",
		RunID: "run-eager", DisplayText: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := newFakePiRuntime()
	runtime.events = make(chan PiEvent)
	runtime.sendHook = func() {
		runtime.mu.Lock()
		subscribed := runtime.subscribed
		runtime.mu.Unlock()
		if !subscribed {
			return
		}
		runtime.events <- PiEvent{Type: "agent_start", Raw: json.RawMessage(`{"type":"agent_start"}`)}
		runtime.events <- PiEvent{Type: "agent_end", Raw: json.RawMessage(`{"type":"agent_end"}`)}
	}
	manager := NewRuntimeManagerWithFactory(store, func(context.Context, string, string) (PiRuntime, error) {
		return runtime, nil
	})
	runner := NewRunnerWithRuntimeManager(manager)
	adapter := NewRuntimeEventAdapter("runtime-session", receipt.RunID, events)
	if err := runner.StartDurablePrompt(t.Context(), store, "runtime-session", receipt.RunID, "hello", nil, adapter); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for runner.IsRunning("runtime-session") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	var count int
	if err := events.db.QueryRow(`SELECT count(*) FROM session_events WHERE session_id='runtime-session' AND event_type=?`, EventRunFinished).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 || runner.IsRunning("runtime-session") {
		t.Fatalf("run.finished count = %d running = %v", count, runner.IsRunning("runtime-session"))
	}
	_ = manager.Shutdown(context.Background())
}

func TestDurablePromptsForDifferentSessionsCompleteIndependently(t *testing.T) {
	store := runtimeTestStore()
	events := appendTestStore(t)
	receipts := make(map[string]RunReceipt)
	for _, sessionID := range []string{"runtime-session", "other-session"} {
		receipt, err := events.StartRun(t.Context(), StartRunCommand{
			SessionID: sessionID, ClientRequestID: "req-" + sessionID, RequestHash: "hash-" + sessionID,
			RunID: "run-" + sessionID, DisplayText: "hello",
		})
		if err != nil {
			t.Fatal(err)
		}
		receipts[sessionID] = receipt
	}
	runtimes := map[string]*fakePiRuntime{
		"runtime-session": newFakePiRuntime(),
		"other-session":   newFakePiRuntime(),
	}
	manager := NewRuntimeManagerWithFactory(store, func(_ context.Context, sessionFile, _ string) (PiRuntime, error) {
		for sessionID, runtime := range runtimes {
			if strings.Contains(sessionFile, sessionID) {
				return runtime, nil
			}
		}
		return nil, errors.New("unknown session runtime")
	})
	runner := NewRunnerWithRuntimeManager(manager)
	for sessionID, runtime := range runtimes {
		adapter := NewRuntimeEventAdapter(sessionID, receipts[sessionID].RunID, events)
		if err := runner.StartDurablePrompt(t.Context(), store, sessionID, receipts[sessionID].RunID, "hello", nil, adapter); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
		runtime.events <- PiEvent{Type: "agent_start", Raw: json.RawMessage(`{"type":"agent_start"}`)}
		runtime.events <- PiEvent{Type: "agent_end", Raw: json.RawMessage(`{"type":"agent_end"}`)}
	}
	deadline := time.Now().Add(2 * time.Second)
	for (runner.IsRunning("runtime-session") || runner.IsRunning("other-session")) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	for sessionID := range runtimes {
		var count int
		if err := events.db.QueryRow(`SELECT count(*) FROM session_events WHERE session_id=? AND event_type=?`, sessionID, EventRunFinished).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 || runner.IsRunning(sessionID) {
			t.Fatalf("session %q run.finished count = %d running = %v", sessionID, count, runner.IsRunning(sessionID))
		}
	}
	_ = manager.Shutdown(context.Background())
}

func TestDurableStreamingForDifferentSessionsInterleavesWithoutLoss(t *testing.T) {
	store := runtimeTestStore()
	events := appendTestStore(t)
	sessions := []string{"runtime-session", "other-session"}
	const deltasPerSession = 200
	receipts := make(map[string]RunReceipt)
	for _, sessionID := range sessions {
		receipt, err := events.StartRun(t.Context(), StartRunCommand{
			SessionID: sessionID, ClientRequestID: "req-" + sessionID, RequestHash: "hash-" + sessionID,
			RunID: "run-" + sessionID, DisplayText: "hello",
		})
		if err != nil {
			t.Fatal(err)
		}
		receipts[sessionID] = receipt
	}
	runtimes := map[string]*fakePiRuntime{
		sessions[0]: newFakePiRuntime(),
		sessions[1]: newFakePiRuntime(),
	}
	manager := NewRuntimeManagerWithFactory(store, func(_ context.Context, sessionFile, _ string) (PiRuntime, error) {
		for sessionID, runtime := range runtimes {
			if strings.Contains(sessionFile, sessionID) {
				return runtime, nil
			}
		}
		return nil, errors.New("unknown session runtime")
	})
	runner := NewRunnerWithRuntimeManager(manager)
	for _, sessionID := range sessions {
		adapter := NewRuntimeEventAdapter(sessionID, receipts[sessionID].RunID, events)
		if err := runner.StartDurablePrompt(t.Context(), store, sessionID, receipts[sessionID].RunID, "hello", nil, adapter); err != nil {
			t.Fatal(err)
		}
	}
	var group sync.WaitGroup
	for _, sessionID := range sessions {
		sessionID := sessionID
		runtime := runtimes[sessionID]
		group.Add(1)
		go func() {
			defer group.Done()
			runtime.events <- PiEvent{Type: "agent_start", Raw: json.RawMessage(`{"type":"agent_start"}`)}
			for index := 0; index < deltasPerSession; index++ {
				runtime.events <- PiEvent{Type: "message_update", Raw: json.RawMessage(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"x"}}`)}
			}
			runtime.events <- PiEvent{Type: "agent_end", Raw: json.RawMessage(`{"type":"agent_end"}`)}
		}()
	}
	group.Wait()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !runner.IsRunning(sessions[0]) && !runner.IsRunning(sessions[1]) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	for _, sessionID := range sessions {
		if runner.IsRunning(sessionID) {
			t.Fatalf("session %q never settled: multi-session streaming stalled", sessionID)
		}
		var finished int
		if err := events.db.QueryRow(`SELECT count(*) FROM session_events WHERE session_id=? AND event_type=?`, sessionID, EventRunFinished).Scan(&finished); err != nil {
			t.Fatal(err)
		}
		if finished != 1 {
			t.Fatalf("session %q run.finished count = %d, want 1", sessionID, finished)
		}
		snapshot, err := events.Snapshot(t.Context(), sessionID, SessionPageRequest{Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Status != SessionStatusIdle {
			t.Fatalf("session %q status = %q, want idle", sessionID, snapshot.Status)
		}
		var segment *ProjectedSessionItem
		for index := range snapshot.Items {
			if snapshot.Items[index].Kind == "pi" {
				segment = &snapshot.Items[index]
			}
		}
		streamed := 0
		if segment != nil {
			streamed = len(segment.Text)
		}
		if streamed != deltasPerSession {
			t.Fatalf("session %q streamed text = %d chars, want %d", sessionID, streamed, deltasPerSession)
		}
	}
	_ = manager.Shutdown(context.Background())
}

func TestCancelDurablePromptPersistsCancelledTerminalEvent(t *testing.T) {
	store := runtimeTestStore()
	events := appendTestStore(t)
	receipt, err := events.StartRun(t.Context(), StartRunCommand{
		SessionID: "runtime-session", ClientRequestID: "req-cancel", RequestHash: "hash-cancel",
		RunID: "run-cancel", DisplayText: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := newFakePiRuntime()
	manager := NewRuntimeManagerWithFactory(store, func(context.Context, string, string) (PiRuntime, error) {
		return runtime, nil
	})
	runner := NewRunnerWithRuntimeManager(manager)
	adapter := NewRuntimeEventAdapter("runtime-session", receipt.RunID, events)
	if err := runner.StartDurablePrompt(t.Context(), store, "runtime-session", receipt.RunID, "hello", nil, adapter); err != nil {
		t.Fatal(err)
	}
	if !runner.Cancel("runtime-session") {
		t.Fatal("durable run was not cancelled")
	}
	var count int
	if err := events.db.QueryRow(`SELECT count(*) FROM session_events WHERE session_id='runtime-session' AND event_type=?`, EventRunCancelled).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("run.cancelled count = %d", count)
	}
	_ = manager.Shutdown(context.Background())
}

func TestDurableRuntimeEventFailureTerminatesRunAsFailed(t *testing.T) {
	store := runtimeTestStore()
	events := appendTestStore(t)
	receipt, err := events.StartRun(t.Context(), StartRunCommand{
		SessionID: "runtime-session", ClientRequestID: "req-1", RequestHash: "h", RunID: "run-1", DisplayText: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := newFakePiRuntime()
	manager := NewRuntimeManagerWithFactory(store, func(context.Context, string, string) (PiRuntime, error) {
		return runtime, nil
	})
	runner := NewRunnerWithRuntimeManager(manager)
	adapter := NewRuntimeEventAdapter("runtime-session", receipt.RunID, &failFirstAssistantSink{store: events})
	if err := runner.StartDurablePrompt(t.Context(), store, "runtime-session", receipt.RunID, "hello", nil, adapter); err != nil {
		t.Fatal(err)
	}
	runtime.events <- PiEvent{Type: "message_update", Raw: json.RawMessage(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"hello"}}`)}

	deadline := time.Now().Add(2 * time.Second)
	for runner.IsRunning("runtime-session") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	var failed int
	if err := events.db.QueryRow(`SELECT count(*) FROM session_events WHERE event_type=?`, EventRunFailed).Scan(&failed); err != nil {
		t.Fatal(err)
	}
	runtime.mu.Lock()
	closed := runtime.closed
	runtime.mu.Unlock()
	if failed != 1 || runner.IsRunning("runtime-session") || closed != 1 {
		t.Fatalf("run.failed count = %d running = %v runtime closed = %d", failed, runner.IsRunning("runtime-session"), closed)
	}
	_ = manager.Shutdown(context.Background())
}

func TestStartDurablePromptFailureMarksRunFailed(t *testing.T) {
	store := runtimeTestStore()
	events := appendTestStore(t)
	receipt, err := events.StartRun(t.Context(), StartRunCommand{
		SessionID: "runtime-session", ClientRequestID: "req-1", RequestHash: "h", RunID: "run-1", DisplayText: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := newFakePiRuntime()
	runtime.closeErr = nil
	manager := NewRuntimeManagerWithFactory(store, func(context.Context, string, string) (PiRuntime, error) {
		return runtime, nil
	})
	runner := NewRunnerWithRuntimeManager(manager)
	adapter := NewRuntimeEventAdapter("runtime-session", receipt.RunID, events)
	if err := runner.StartDurablePrompt(t.Context(), store, "runtime-session", receipt.RunID, "hello", nil, adapter); err != nil {
		t.Fatal(err)
	}
	close(runtime.events)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var count int
		if err := events.db.QueryRow(`SELECT count(*) FROM session_events WHERE event_type=?`, EventRunFailed).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	var failed int
	if err := events.db.QueryRow(`SELECT count(*) FROM session_events WHERE event_type=?`, EventRunFailed).Scan(&failed); err != nil {
		t.Fatal(err)
	}
	if failed != 1 {
		t.Fatalf("run.failed count = %d", failed)
	}
	_ = manager.Shutdown(context.Background())
}
