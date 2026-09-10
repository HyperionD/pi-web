package piweb

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRuntimeManagerCloseSessionTombstonesStartup(t *testing.T) {
	store := runtimeTestStore()
	started := make(chan struct{})
	var startedOnce sync.Once
	release := make(chan struct{})
	var created atomic.Int32
	runtime := newFakePiRuntime()
	manager := NewRuntimeManagerWithFactory(store, func(ctx context.Context, _, _ string) (PiRuntime, error) {
		created.Add(1)
		startedOnce.Do(func() { close(started) })
		select {
		case <-release:
			return runtime, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	defer manager.Shutdown(context.Background())
	startupDone := make(chan error, 1)
	go func() {
		_, err := manager.Ensure(context.Background(), "runtime-session")
		startupDone <- err
	}()
	<-started
	closeDone := make(chan error, 1)
	go func() { closeDone <- manager.CloseSession(context.Background(), "runtime-session") }()
	select {
	case err := <-closeDone:
		t.Fatalf("CloseSession returned before startup completed: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	if _, err := manager.Ensure(context.Background(), "runtime-session"); err == nil || err.Error() != "session runtime is closed" {
		t.Fatalf("Ensure during close error = %v", err)
	}
	close(release)
	if err := <-startupDone; err == nil || err.Error() != "session runtime is closed" {
		t.Fatalf("startup error = %v", err)
	}
	if err := <-closeDone; err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	if created.Load() != 1 {
		t.Fatalf("factory called %d times, want one", created.Load())
	}
}

func TestRuntimeManagerTracksOverlappingActivity(t *testing.T) {
	store := runtimeTestStore()
	clock := time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)
	runtime := newFakePiRuntime()
	manager := NewRuntimeManagerWithOptions(store, RuntimeManagerOptions{
		Factory:     func(context.Context, string, string) (PiRuntime, error) { return runtime, nil },
		IdleTimeout: time.Minute, CleanupInterval: time.Hour, Now: func() time.Time { return clock },
	})
	defer manager.Shutdown(context.Background())
	got, err := manager.Ensure(context.Background(), "runtime-session")
	if err != nil {
		t.Fatal(err)
	}
	events := got.Events()
	for _, event := range []string{
		`{"type":"tool_execution_start","toolCallId":"tool-1"}`,
		`{"type":"tool_execution_start","toolCallId":"tool-2"}`,
		`{"type":"agent_end"}`,
	} {
		runtime.events <- PiEvent{Type: eventType(event), Raw: json.RawMessage(event)}
		select {
		case <-events:
		case <-time.After(time.Second):
			t.Fatal("runtime event was not forwarded")
		}
	}
	clock = clock.Add(2 * time.Minute)
	manager.evictIdle()
	runtime.mu.Lock()
	closed := runtime.closed
	runtime.mu.Unlock()
	if closed != 0 {
		t.Fatal("runtime with an unfinished tool was evicted")
	}
}

func TestDeleteAccountSessionsClosesRuntimes(t *testing.T) {
	store := NewMockStore()
	workspace, err := store.OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateSession(workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	runtime := newFakePiRuntime()
	manager := NewRuntimeManagerWithFactory(store, func(context.Context, string, string) (PiRuntime, error) { return runtime, nil })
	defer manager.Shutdown(context.Background())
	if _, err := manager.Ensure(context.Background(), session.ID); err != nil {
		t.Fatal(err)
	}
	server := NewServerWithRuntimeManager(Config{}, store, NewBroker(), manager)
	req := httptest.NewRequest(http.MethodDelete, "/api/workspaces/"+workspace.ID+"/sessions", nil)
	req.SetPathValue("workspaceID", workspace.ID)
	res := httptest.NewRecorder()
	server.deleteAccountSessions(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("delete status = %d: %s", res.Code, res.Body.String())
	}
	runtime.mu.Lock()
	closed := runtime.closed
	runtime.mu.Unlock()
	if closed != 1 {
		t.Fatalf("runtime close count = %d, want 1", closed)
	}
}

func TestDeleteSessionRollsBackRuntimeTombstoneOnStoreFailure(t *testing.T) {
	store := runtimeTestStore()
	var created atomic.Int32
	manager := NewRuntimeManagerWithFactory(store, func(context.Context, string, string) (PiRuntime, error) {
		created.Add(1)
		return newFakePiRuntime(), nil
	})
	defer manager.Shutdown(context.Background())
	if _, err := manager.Ensure(context.Background(), "runtime-session"); err != nil {
		t.Fatal(err)
	}
	server := NewServerWithRuntimeManager(Config{}, store, NewBroker(), manager)
	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/runtime-session", nil)
	req.SetPathValue("sessionID", "runtime-session")
	res := httptest.NewRecorder()
	server.deleteSession(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("delete status = %d, want not found", res.Code)
	}
	if _, err := manager.Ensure(context.Background(), "runtime-session"); err != nil {
		t.Fatalf("Ensure after failed delete: %v", err)
	}
	if created.Load() != 2 {
		t.Fatalf("factory called %d times, want two", created.Load())
	}
}

func TestIndependentShutdownContextsDoNotShareCancellation(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	first, firstCancel := independentShutdownContext(parent)
	second, secondCancel := independentShutdownContext(parent)
	defer firstCancel()
	defer secondCancel()
	cancelParent()
	if first.Err() != nil || second.Err() != nil {
		t.Fatalf("independent contexts canceled with parent: %v, %v", first.Err(), second.Err())
	}
}

func TestIndependentShutdownContextAfterParentTimeoutHasFreshDeadline(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	<-parent.Done()
	started := time.Now()
	cleanup, cleanupCancel := independentShutdownContext(parent)
	defer cleanupCancel()
	if err := cleanup.Err(); err != nil {
		t.Fatalf("cleanup context already canceled: %v", err)
	}
	deadline, ok := cleanup.Deadline()
	if !ok || deadline.Before(started.Add(4*time.Second)) || deadline.After(started.Add(6*time.Second)) {
		t.Fatalf("cleanup deadline = %v, want a fresh five-second deadline", deadline)
	}
}

func TestServerShutdownCompletesRuntimeCleanupAfterParentTimeout(t *testing.T) {
	store := runtimeTestStore()
	runtime := newFakePiRuntime()
	cleanupDone := make(chan struct{})
	runtime.closeHook = func() { close(cleanupDone) }
	manager := NewRuntimeManagerWithFactory(store, func(context.Context, string, string) (PiRuntime, error) {
		return runtime, nil
	})
	server := NewServerWithRuntimeManager(Config{}, store, NewBroker(), manager)
	if _, err := manager.Ensure(context.Background(), "runtime-session"); err != nil {
		t.Fatal(err)
	}
	parent, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	<-parent.Done()
	_ = server.Shutdown(parent)
	select {
	case <-cleanupDone:
	case <-time.After(time.Second):
		t.Fatal("runtime cleanup did not complete after parent timeout")
	}
}

func TestServerShutdownDoesNotHoldLifecycleReadLock(t *testing.T) {
	server := NewServer(Config{}, NewMockStore(), NewBroker())
	entered := make(chan struct{})
	release := make(chan struct{})
	handler := server.withShutdownGuard(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(entered)
		<-release
	}))
	go handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- server.Shutdown(ctx) }()
	select {
	case err := <-shutdownDone:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("shutdown error = %v, want deadline exceeded", err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown blocked behind an active request")
	}
	close(release)
}

func TestServerShutdownGuardRejectsRequestsAfterShutdown(t *testing.T) {
	server := NewServer(Config{}, NewMockStore(), NewBroker())
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	called := false
	handler := server.withShutdownGuard(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/", nil))
	if res.Code != http.StatusServiceUnavailable || called {
		t.Fatalf("shutdown guard status=%d called=%v", res.Code, called)
	}
}

func eventType(raw string) string {
	var event struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal([]byte(raw), &event)
	return event.Type
}

type cancelRegressionRuntime struct {
	mu           sync.Mutex
	events       chan PiEvent
	commands     []RPCCommand
	abortStart   chan struct{}
	releaseAbort chan struct{}
	lateEvent    bool
}

func newCancelRegressionRuntime() *cancelRegressionRuntime {
	return &cancelRegressionRuntime{events: make(chan PiEvent, 8), abortStart: make(chan struct{}), releaseAbort: make(chan struct{})}
}
func (r *cancelRegressionRuntime) Send(ctx context.Context, command RPCCommand) (RPCResponse, error) {
	r.mu.Lock()
	r.commands = append(r.commands, command)
	r.mu.Unlock()
	if command.Type == "abort" {
		select {
		case <-r.abortStart:
		default:
			close(r.abortStart)
		}
		select {
		case <-r.releaseAbort:
			if r.lateEvent {
				go func() {
					time.Sleep(100 * time.Millisecond)
					r.events <- PiEvent{Type: "agent_end", Raw: json.RawMessage(`{"type":"agent_end"}`)}
				}()
			}
		case <-ctx.Done():
			return RPCResponse{}, ctx.Err()
		}
	}
	return RPCResponse{ID: command.ID, Type: "response", Command: command.Type, Success: true}, nil
}
func (r *cancelRegressionRuntime) Events() <-chan PiEvent      { return r.events }
func (r *cancelRegressionRuntime) IsRunning() bool             { return true }
func (r *cancelRegressionRuntime) Close(context.Context) error { return nil }

func TestRunnerCancelWaitsForSettledAgentAndDropsStaleEvents(t *testing.T) {
	store := runtimeTestStore()
	runtime := newCancelRegressionRuntime()
	runtime.lateEvent = true
	secondRuntime := newCancelRegressionRuntime()
	var created atomic.Int32
	manager := NewRuntimeManagerWithFactory(store, func(context.Context, string, string) (PiRuntime, error) {
		if created.Add(1) == 1 {
			return runtime, nil
		}
		return secondRuntime, nil
	})
	defer manager.Shutdown(context.Background())
	runner := NewRunnerWithRuntimeManager(manager)
	broker := NewBroker()
	if err := runner.StartPiPrompt(context.Background(), broker, store, "runtime-session", "first", nil, "first"); err != nil {
		t.Fatal(err)
	}
	cancelDone := make(chan bool, 1)
	go func() { cancelDone <- runner.Cancel("runtime-session") }()
	<-runtime.abortStart
	select {
	case <-cancelDone:
		t.Fatal("Cancel returned before abort response")
	case <-time.After(30 * time.Millisecond):
	}
	close(runtime.releaseAbort)
	select {
	case <-cancelDone:
		t.Fatal("Cancel returned before agent settled")
	case <-time.After(30 * time.Millisecond):
	}
	runtime.events <- PiEvent{Type: "agent_end", Raw: json.RawMessage(`{"type":"agent_end"}`)}
	if !<-cancelDone {
		t.Fatal("Cancel did not report an active run")
	}
	if err := runner.StartPiPrompt(context.Background(), broker, store, "runtime-session", "second", nil, "second"); err != nil {
		t.Fatal(err)
	}
	secondRuntime.events <- PiEvent{Type: "agent_start", Raw: json.RawMessage(`{"type":"agent_start"}`)}
	secondRuntime.events <- PiEvent{Type: "message_update", Raw: json.RawMessage(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"new"}}`)}
	time.Sleep(150 * time.Millisecond)
	if !runner.IsRunning("runtime-session") {
		t.Fatal("stale agent_end finished the new prompt")
	}
	secondRuntime.events <- PiEvent{Type: "agent_end", Raw: json.RawMessage(`{"type":"agent_end"}`)}
}

func TestRunnerCancelAbortsBeforeAcceptingNewPrompt(t *testing.T) {
	store := runtimeTestStore()
	runtime := newCancelRegressionRuntime()
	manager := NewRuntimeManagerWithFactory(store, func(context.Context, string, string) (PiRuntime, error) { return runtime, nil })
	defer manager.Shutdown(context.Background())
	runner := NewRunnerWithRuntimeManager(manager)
	broker := NewBroker()
	if err := runner.StartPiPrompt(context.Background(), broker, store, "runtime-session", "first", nil, "first"); err != nil {
		t.Fatal(err)
	}
	cancelDone := make(chan bool, 1)
	go func() { cancelDone <- runner.Cancel("runtime-session") }()
	<-runtime.abortStart
	if err := runner.StartPiPrompt(context.Background(), broker, store, "runtime-session", "second", nil, "second"); !errors.Is(err, errSessionBusy) {
		t.Fatalf("new prompt during abort error = %v", err)
	}
	close(runtime.releaseAbort)
	if !<-cancelDone {
		t.Fatal("Cancel did not report an active run")
	}
	if !runner.IsRunning("runtime-session") {
		t.Fatal("Cancel finished a run before agent_end settled")
	}
	if err := runner.StartPiPrompt(context.Background(), broker, store, "runtime-session", "second", nil, "second"); !errors.Is(err, errSessionBusy) {
		t.Fatalf("new prompt after unsettled cancel error = %v", err)
	}
	runtime.events <- PiEvent{Type: "agent_end", Raw: json.RawMessage(`{"type":"agent_end"}`)}
}

type compactCancelRuntime struct {
	events       chan PiEvent
	abortStarted chan struct{}
	releaseAbort chan struct{}
	mu           sync.Mutex
	commands     []RPCCommand
}

func newCompactCancelRuntime() *compactCancelRuntime {
	return &compactCancelRuntime{events: make(chan PiEvent, 8), abortStarted: make(chan struct{}), releaseAbort: make(chan struct{})}
}

func (r *compactCancelRuntime) Send(ctx context.Context, command RPCCommand) (RPCResponse, error) {
	r.mu.Lock()
	r.commands = append(r.commands, command)
	r.mu.Unlock()
	if command.Type == "compact" {
		<-ctx.Done()
		return RPCResponse{}, ctx.Err()
	}
	if command.Type == "abort" {
		close(r.abortStarted)
		select {
		case <-r.releaseAbort:
		case <-ctx.Done():
			return RPCResponse{}, ctx.Err()
		}
	}
	return RPCResponse{ID: command.ID, Type: "response", Command: command.Type, Success: true}, nil
}
func (r *compactCancelRuntime) Events() <-chan PiEvent      { return r.events }
func (r *compactCancelRuntime) IsRunning() bool             { return true }
func (r *compactCancelRuntime) Close(context.Context) error { return nil }

func TestRunnerCompactCancellationSettlesBeforeNewPrompt(t *testing.T) {
	store := runtimeTestStore()
	runtime := newCompactCancelRuntime()
	manager := NewRuntimeManagerWithFactory(store, func(context.Context, string, string) (PiRuntime, error) { return runtime, nil })
	defer manager.Shutdown(context.Background())
	runner := NewRunnerWithRuntimeManager(manager)
	resultDone := make(chan error, 1)
	compactCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	go func() {
		_, err := runner.Compact(compactCtx, NewBroker(), store, "runtime-session")
		resultDone <- err
	}()
	<-runtime.abortStarted
	if err := runner.StartPiPrompt(context.Background(), NewBroker(), store, "runtime-session", "new", nil, "new"); !errors.Is(err, errSessionBusy) {
		t.Fatalf("new prompt during compact abort error = %v", err)
	}
	close(runtime.releaseAbort)
	if err := <-resultDone; err == nil {
		t.Fatal("canceled compact unexpectedly succeeded")
	}
	if err := runner.StartPiPrompt(context.Background(), NewBroker(), store, "runtime-session", "new", nil, "new"); err != nil {
		t.Fatalf("new prompt after compact settlement: %v", err)
	}
}

func TestRuntimeManagerTombstoneRollbackUsesGeneration(t *testing.T) {
	store := runtimeTestStore()
	manager := NewRuntimeManagerWithFactory(store, func(context.Context, string, string) (PiRuntime, error) {
		return newFakePiRuntime(), nil
	})
	defer manager.Shutdown(context.Background())
	first, err := manager.CloseSessionToken(context.Background(), "runtime-session")
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.CloseSessionToken(context.Background(), "runtime-session")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("overlapping close requests reused a tombstone generation")
	}
	if err := manager.RollbackSessionCloseToken("runtime-session", first); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Ensure(context.Background(), "runtime-session"); !errors.Is(err, errRuntimeSessionClosed) {
		t.Fatalf("old rollback released new tombstone: %v", err)
	}
	if err := manager.RollbackSessionCloseToken("runtime-session", second); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Ensure(context.Background(), "runtime-session"); err != nil {
		t.Fatalf("current rollback did not release tombstone: %v", err)
	}
}

func TestWorkspaceSessionDeletionRollsBackTombstonesOnStoreFailure(t *testing.T) {
	store := NewMockStore()
	workspace, err := store.OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateSession(workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	var created atomic.Int32
	manager := NewRuntimeManagerWithFactory(store, func(context.Context, string, string) (PiRuntime, error) {
		created.Add(1)
		return newFakePiRuntime(), nil
	})
	defer manager.Shutdown(context.Background())
	if _, err := manager.Ensure(context.Background(), session.ID); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	store.refreshDisabledWorkspace[workspace.ID] = true
	store.workspacePath[workspace.ID] = "invalid\x00workspace"
	store.mu.Unlock()
	server := NewServerWithRuntimeManager(Config{}, store, NewBroker(), manager)
	req := httptest.NewRequest(http.MethodDelete, "/api/workspaces/"+workspace.ID+"/sessions", nil)
	req.SetPathValue("workspaceID", workspace.ID)
	res := httptest.NewRecorder()
	server.deleteWorkspaceSessions(res, req)
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("delete status = %d: %s", res.Code, res.Body.String())
	}
	if _, err := manager.Ensure(context.Background(), session.ID); err != nil {
		t.Fatalf("Ensure after failed batch delete: %v", err)
	}
	if created.Load() != 2 {
		t.Fatalf("factory called %d times, want runtime recreation after rollback", created.Load())
	}
}

func TestSuccessfulWorkspaceSessionDeletionCleansTombstones(t *testing.T) {
	store := NewMockStore()
	workspace, err := store.OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateSession(workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewRuntimeManagerWithFactory(store, func(context.Context, string, string) (PiRuntime, error) {
		return newFakePiRuntime(), nil
	})
	defer manager.Shutdown(context.Background())
	server := NewServerWithRuntimeManager(Config{}, store, NewBroker(), manager)
	req := httptest.NewRequest(http.MethodDelete, "/api/workspaces/"+workspace.ID+"/sessions", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("delete status = %d: %s", res.Code, res.Body.String())
	}
	manager.mu.Lock()
	_, tombstoned := manager.tombstone[session.ID]
	manager.mu.Unlock()
	if tombstoned {
		t.Fatal("successful deletion left an unbounded runtime tombstone")
	}
}

func TestDeleteWorkspaceSessionsClosesRuntimes(t *testing.T) {
	store := NewMockStore()
	workspace, err := store.OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateSession(workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	runtime := newFakePiRuntime()
	manager := NewRuntimeManagerWithFactory(store, func(context.Context, string, string) (PiRuntime, error) { return runtime, nil })
	defer manager.Shutdown(context.Background())
	if _, err := manager.Ensure(context.Background(), session.ID); err != nil {
		t.Fatal(err)
	}
	server := NewServerWithRuntimeManager(Config{}, store, NewBroker(), manager)
	req := httptest.NewRequest(http.MethodDelete, "/api/workspaces/"+workspace.ID+"/sessions", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("delete status = %d: %s", res.Code, res.Body.String())
	}
	runtime.mu.Lock()
	closed := runtime.closed
	runtime.mu.Unlock()
	if closed != 1 {
		t.Fatalf("runtime close count = %d, want 1", closed)
	}
}
