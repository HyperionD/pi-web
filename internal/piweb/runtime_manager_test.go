package piweb

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakePiRuntime struct {
	mu        sync.Mutex
	sends     []RPCCommand
	closeErr  error
	closeHook func()
	closed    int
	events    chan PiEvent
}

func newFakePiRuntime() *fakePiRuntime {
	return &fakePiRuntime{events: make(chan PiEvent, 16)}
}

func (r *fakePiRuntime) Send(_ context.Context, command RPCCommand) (RPCResponse, error) {
	r.mu.Lock()
	r.sends = append(r.sends, command)
	r.mu.Unlock()
	return RPCResponse{ID: command.ID, Type: "response", Command: command.Type, Success: true}, nil
}

func (r *fakePiRuntime) Events() <-chan PiEvent { return r.events }
func (r *fakePiRuntime) IsRunning() bool        { return true }
func (r *fakePiRuntime) Close(context.Context) error {
	r.mu.Lock()
	r.closed++
	hook := r.closeHook
	err := r.closeErr
	r.mu.Unlock()
	if hook != nil {
		hook()
	}
	return err
}

func runtimeTestStore() *Store {
	store := NewMockStore()
	store.sessionFiles["runtime-session"] = "/tmp/runtime-session.jsonl"
	store.sessionCWD["runtime-session"] = "/tmp"
	store.conversations["runtime-session"] = nil
	store.sessionFiles["other-session"] = "/tmp/other-session.jsonl"
	store.sessionCWD["other-session"] = "/tmp"
	store.conversations["other-session"] = nil
	return store
}

func TestRuntimeManagerInitializesModelBeforeReturningRuntime(t *testing.T) {
	store := runtimeTestStore()
	runtime := newFakePiRuntime()
	manager := NewRuntimeManagerWithFactory(store, func(context.Context, string, string) (PiRuntime, error) {
		return runtime, nil
	})
	defer manager.Shutdown(context.Background())

	settings := ModelStartupSettings{Provider: "zai", Model: "gpt-5.5", ThinkingLevel: "high"}
	if _, _, err := manager.EnsureOwnedWithInitializer(context.Background(), "runtime-session", 0, runtimeModelInitializer([]ModelStartupSettings{settings})); err != nil {
		t.Fatal(err)
	}
	if len(runtime.sends) != 2 || runtime.sends[0].Type != "set_model" || runtime.sends[1].Type != "set_thinking_level" {
		t.Fatalf("unexpected initialization commands: %+v", runtime.sends)
	}
}

func TestRuntimeManagerReusesProcessForSameSession(t *testing.T) {
	store := runtimeTestStore()
	var created atomic.Int32
	createdRuntime := newFakePiRuntime()
	manager := NewRuntimeManagerWithFactory(store, func(context.Context, string, string) (PiRuntime, error) {
		created.Add(1)
		return createdRuntime, nil
	})
	defer manager.Shutdown(context.Background())

	first, err := manager.Ensure(context.Background(), "runtime-session")
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Ensure(context.Background(), "runtime-session")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("same session did not reuse its runtime")
	}
	if created.Load() != 1 {
		t.Fatalf("created %d runtimes, want 1", created.Load())
	}

	other, err := manager.Ensure(context.Background(), "other-session")
	if err != nil {
		t.Fatal(err)
	}
	if other == first || created.Load() != 2 {
		t.Fatalf("different sessions unexpectedly shared runtime: created=%d", created.Load())
	}
}

func TestRuntimeManagerSharesConcurrentStartup(t *testing.T) {
	store := runtimeTestStore()
	started := make(chan struct{})
	release := make(chan struct{})
	var created atomic.Int32
	runtime := newFakePiRuntime()
	manager := NewRuntimeManagerWithFactory(store, func(ctx context.Context, _ string, _ string) (PiRuntime, error) {
		created.Add(1)
		close(started)
		select {
		case <-release:
			return runtime, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	defer manager.Shutdown(context.Background())

	results := make(chan PiRuntime, 2)
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			got, err := manager.Ensure(context.Background(), "runtime-session")
			results <- got
			errs <- err
		}()
	}
	<-started
	if created.Load() != 1 {
		t.Fatalf("startup factory called %d times before release", created.Load())
	}
	close(release)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if (<-results) != (<-results) {
		t.Fatal("concurrent Ensure calls did not share runtime")
	}
	if created.Load() != 1 {
		t.Fatalf("startup factory called %d times, want 1", created.Load())
	}
}

func TestRuntimeManagerEvictsIdleSessions(t *testing.T) {
	store := runtimeTestStore()
	clock := time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)
	runtime := newFakePiRuntime()
	manager := NewRuntimeManagerWithOptions(store, RuntimeManagerOptions{
		Factory:     func(context.Context, string, string) (PiRuntime, error) { return runtime, nil },
		IdleTimeout: time.Minute, CleanupInterval: time.Hour,
		Now: func() time.Time { return clock },
	})
	defer manager.Shutdown(context.Background())
	if _, err := manager.Ensure(context.Background(), "runtime-session"); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(2 * time.Minute)
	manager.evictIdle()
	runtime.mu.Lock()
	closed := runtime.closed
	runtime.mu.Unlock()
	if closed != 1 {
		t.Fatalf("idle runtime close count = %d, want 1", closed)
	}
}

func TestRuntimeManagerActivityPreventsIdleEviction(t *testing.T) {
	store := runtimeTestStore()
	clock := time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)
	runtime := newFakePiRuntime()
	manager := NewRuntimeManagerWithOptions(store, RuntimeManagerOptions{
		Factory:     func(context.Context, string, string) (PiRuntime, error) { return runtime, nil },
		IdleTimeout: time.Minute, CleanupInterval: time.Hour,
		Now: func() time.Time { return clock },
	})
	defer manager.Shutdown(context.Background())
	got, err := manager.Ensure(context.Background(), "runtime-session")
	if err != nil {
		t.Fatal(err)
	}
	events := got.Events()
	runtime.events <- PiEvent{Type: "tool_execution_start", Raw: json.RawMessage(`{"type":"tool_execution_start"}`)}
	select {
	case <-events:
	case <-time.After(time.Second):
		t.Fatal("runtime event was not forwarded")
	}
	clock = clock.Add(2 * time.Minute)
	manager.evictIdle()
	runtime.mu.Lock()
	closed := runtime.closed
	runtime.mu.Unlock()
	if closed != 0 {
		t.Fatal("active runtime was evicted")
	}
}

func TestRuntimeManagerEnsureWaitsForClosingEviction(t *testing.T) {
	store := runtimeTestStore()
	clock := time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)
	first := newFakePiRuntime()
	closeStarted := make(chan struct{})
	releaseClose := make(chan struct{})
	first.closeHook = func() {
		close(closeStarted)
		<-releaseClose
	}
	var created atomic.Int32
	manager := NewRuntimeManagerWithOptions(store, RuntimeManagerOptions{
		Factory: func(context.Context, string, string) (PiRuntime, error) {
			created.Add(1)
			if created.Load() == 1 {
				return first, nil
			}
			return newFakePiRuntime(), nil
		},
		IdleTimeout: time.Minute, CleanupInterval: time.Hour,
		Now: func() time.Time { return clock },
	})
	defer manager.Shutdown(context.Background())
	if _, err := manager.Ensure(context.Background(), "runtime-session"); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(2 * time.Minute)
	evictionDone := make(chan struct{})
	go func() {
		manager.evictIdle()
		close(evictionDone)
	}()
	<-closeStarted
	ensureDone := make(chan error, 1)
	go func() {
		_, err := manager.Ensure(context.Background(), "runtime-session")
		ensureDone <- err
	}()
	select {
	case err := <-ensureDone:
		t.Fatalf("Ensure returned while runtime was closing: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	if created.Load() != 1 {
		t.Fatalf("factory called %d times while closing, want one", created.Load())
	}
	close(releaseClose)
	if err := <-ensureDone; err != nil {
		t.Fatal(err)
	}
	<-evictionDone
	if created.Load() != 2 {
		t.Fatalf("factory called %d times after close, want two", created.Load())
	}
}

func TestRuntimeManagerRollbackSessionCloseAllowsEnsure(t *testing.T) {
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
	if err := manager.CloseSession(context.Background(), "runtime-session"); err != nil {
		t.Fatal(err)
	}
	if err := manager.RollbackSessionClose("runtime-session"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Ensure(context.Background(), "runtime-session"); err != nil {
		t.Fatal(err)
	}
	if created.Load() != 2 {
		t.Fatalf("factory called %d times, want two", created.Load())
	}
}

func TestRuntimeManagerClosesAllSessions(t *testing.T) {
	store := runtimeTestStore()
	first := newFakePiRuntime()
	second := newFakePiRuntime()
	var next atomic.Int32
	manager := NewRuntimeManagerWithFactory(store, func(context.Context, string, string) (PiRuntime, error) {
		if next.Add(1) == 1 {
			return first, nil
		}
		return second, nil
	})
	if _, err := manager.Ensure(context.Background(), "runtime-session"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Ensure(context.Background(), "other-session"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	first.mu.Lock()
	firstClosed := first.closed
	first.mu.Unlock()
	second.mu.Lock()
	secondClosed := second.closed
	second.mu.Unlock()
	if firstClosed != 1 || secondClosed != 1 {
		t.Fatalf("runtime close counts = %d, %d; want 1, 1", firstClosed, secondClosed)
	}
	if _, err := manager.Ensure(context.Background(), "runtime-session"); !errors.Is(err, errRuntimeManagerClosed) {
		t.Fatalf("Ensure after shutdown error = %v", err)
	}
}

var _ PiRuntime = (*fakePiRuntime)(nil)
