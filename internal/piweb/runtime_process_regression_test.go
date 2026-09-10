package piweb

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProcessRuntimeWaitsForPromptResponse(t *testing.T) {
	root := t.TempDir()
	writeFakePi(t, root, `#!/bin/sh
read request
sleep 0.1
printf '%s\n' '{"id":"request-1","type":"response","command":"prompt","success":true,"data":{}}'
`)
	runtime, err := newPiRuntime(context.Background(), filepath.Join(root, "session.jsonl"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())

	result := make(chan error, 1)
	go func() {
		_, err := runtime.Send(context.Background(), RPCCommand{ID: "request-1", Type: "prompt"})
		result <- err
	}()
	select {
	case err := <-result:
		t.Fatalf("prompt returned before its response: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("prompt failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("prompt did not receive response")
	}
}

func TestProcessRuntimeMatchesResponseRequestID(t *testing.T) {
	root := t.TempDir()
	writeFakePi(t, root, `#!/bin/sh
read request
printf '%s\n' '{"id":"other","type":"response","command":"prompt","success":true,"data":{}}'
`)
	runtime, err := newPiRuntime(context.Background(), filepath.Join(root, "session.jsonl"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = runtime.Send(ctx, RPCCommand{ID: "request-1", Type: "prompt"})
	if err == nil {
		t.Fatal("mismatched response must not satisfy the waiter")
	}
}

func TestProcessRuntimeReportsMalformedStdoutToWaiter(t *testing.T) {
	root := t.TempDir()
	writeFakePi(t, root, `#!/bin/sh
read request
printf '%s\n' 'not json'
sleep 10
`)
	runtime, err := newPiRuntime(context.Background(), filepath.Join(root, "session.jsonl"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = runtime.Send(ctx, RPCCommand{ID: "request-1", Type: "prompt"})
	if err == nil || !strings.Contains(err.Error(), "invalid pi RPC output") {
		t.Fatalf("malformed stdout error = %v", err)
	}
}

func TestProcessRuntimeEventBackpressureDoesNotBlockReader(t *testing.T) {
	runtime := &processPiRuntime{events: make(chan PiEvent, 1), done: make(chan struct{}), pending: map[string]chan runtimeResponse{}}
	runtime.events <- PiEvent{Type: "already-buffered"}
	done := make(chan struct{})
	go func() {
		runtime.handleLine([]byte(`{"type":"message_update"}`))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("stdout reader blocked on full event channel")
	}
}

func TestProcessRuntimeNormalStdoutEOFPoisonsPendingWaiter(t *testing.T) {
	root := t.TempDir()
	writeFakePi(t, root, `#!/bin/sh
read request
exit 0
`)
	runtime, err := newPiRuntime(context.Background(), filepath.Join(root, "session.jsonl"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = runtime.Send(ctx, RPCCommand{ID: "request-1", Type: "prompt"})
	if err == nil || strings.Contains(err.Error(), "context deadline") {
		t.Fatalf("normal stdout EOF error = %v, want immediate protocol failure", err)
	}
}

func TestRuntimeEntryBackpressurePoisonsEntry(t *testing.T) {
	runtime := newFakePiRuntime()
	entry := &runtimeEntry{runtime: runtime, manager: &RuntimeManager{now: time.Now}, events: make(chan PiEvent, 1), stop: make(chan struct{}), activity: map[string]int{}, closeDone: make(chan struct{})}
	entry.events <- PiEvent{Type: "already-buffered"}
	entry.enqueueEvent(PiEvent{Type: "message_update", Raw: json.RawMessage(`{"type":"message_update"}`)})
	if entry.IsRunning() {
		t.Fatal("entry remained usable after event backpressure")
	}
}

func TestProcessRuntimeCancelsBlockedStdinWrite(t *testing.T) {
	stdin := newBlockingWriteCloser()
	runtime := &processPiRuntime{
		stdin: stdin, events: make(chan PiEvent, 1), done: make(chan struct{}),
		pending: map[string]chan runtimeResponse{}, running: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := runtime.Send(ctx, RPCCommand{ID: "blocked", Type: "prompt"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked write error = %v, want deadline exceeded", err)
	}
	select {
	case <-stdin.closed:
	case <-time.After(time.Second):
		t.Fatal("canceled write did not close stdin")
	}
}

func TestProcessRuntimePoisonsOnEventBackpressure(t *testing.T) {
	runtime := &processPiRuntime{
		events: make(chan PiEvent, 1), done: make(chan struct{}),
		pending: map[string]chan runtimeResponse{}, running: true,
	}
	runtime.events <- PiEvent{Type: "already-buffered"}
	if err := runtime.handleLine([]byte(`{"type":"message_update"}`)); err != nil {
		t.Fatal(err)
	}
	if runtime.IsRunning() {
		t.Fatal("runtime remained usable after event backpressure")
	}
	if _, err := runtime.Send(context.Background(), RPCCommand{Type: "prompt"}); err == nil {
		t.Fatal("poisoned runtime accepted a new command")
	}
}

func TestProcessRuntimeReturnsRPCFailureResponse(t *testing.T) {
	root := t.TempDir()
	writeFakePi(t, root, `#!/bin/sh
read request
printf '%s\n' '{"id":"request-1","type":"response","command":"prompt","success":false,"error":"quota exceeded"}'
`)
	runtime, err := newPiRuntime(context.Background(), filepath.Join(root, "session.jsonl"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())
	response, err := runtime.Send(context.Background(), RPCCommand{ID: "request-1", Type: "prompt"})
	if err != nil || response.Success || response.Error != "quota exceeded" {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}

type blockingWriteCloser struct {
	closed chan struct{}
}

func newBlockingWriteCloser() *blockingWriteCloser {
	return &blockingWriteCloser{closed: make(chan struct{})}
}

func (w *blockingWriteCloser) Write([]byte) (int, error) {
	<-w.closed
	return 0, errors.New("stdin closed")
}

func (w *blockingWriteCloser) Close() error {
	select {
	case <-w.closed:
	default:
		close(w.closed)
	}
	return nil
}

func TestProcessRuntimeJoinsStderrBeforeClose(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "stderr-marker")
	writeFakePi(t, root, "#!/bin/sh\nread request\necho stderr >&2\ntouch '"+marker+"'\n")
	runtime, err := newPiRuntime(context.Background(), filepath.Join(root, "session.jsonl"), root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runtime.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("process did not exit cleanly: %v", err)
	}
}
