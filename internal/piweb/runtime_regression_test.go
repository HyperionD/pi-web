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
		case <-ctx.Done():
			return RPCResponse{}, ctx.Err()
		}
	}
	return RPCResponse{ID: command.ID, Type: "response", Command: command.Type, Success: true}, nil
}
func (r *cancelRegressionRuntime) Events() <-chan PiEvent      { return r.events }
func (r *cancelRegressionRuntime) IsRunning() bool             { return true }
func (r *cancelRegressionRuntime) Close(context.Context) error { return nil }

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
