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

func TestProcessRuntimeQuarantinesCanceledRequestID(t *testing.T) {
	root := t.TempDir()
	lateMarker := filepath.Join(root, "late-response-marker")
	writeFakePi(t, root, `#!/bin/sh
read first
sleep 0.2
printf late > '`+lateMarker+`'
printf '%s\n' '{"id":"reused","type":"response","command":"prompt","success":true}'
sleep 1
`)
	runtime, err := newPiRuntime(context.Background(), filepath.Join(root, "session.jsonl"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = runtime.Send(ctx, RPCCommand{ID: "reused", Type: "prompt"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("canceled request error = %v, want context deadline exceeded", err)
	}
	secondCtx, secondCancel := context.WithTimeout(context.Background(), time.Second)
	defer secondCancel()
	_, err = runtime.Send(secondCtx, RPCCommand{ID: "reused", Type: "prompt"})
	if err == nil || !strings.Contains(err.Error(), "quarantined") {
		t.Fatalf("reused request ID error = %v, want quarantine error", err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		if _, statErr := os.Stat(lateMarker); statErr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fake Pi did not produce the late response")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case event := <-runtime.Events():
		t.Fatalf("late response escaped quarantine as event: %+v", event)
	default:
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

func TestProcessRuntimeFailureIncludesRedactedStderrTail(t *testing.T) {
	runtime := &processPiRuntime{
		events:     make(chan PiEvent),
		done:       make(chan struct{}),
		stop:       make(chan struct{}),
		pending:    map[string]chan runtimeResponse{},
		stderrTail: []byte("api_key=secret-value\\n"),
	}
	runtime.finish(errors.New("pi process exited: exit status 7"))

	failure := runtime.Failure()
	if failure == nil || !strings.Contains(failure.Error(), "exit status 7") {
		t.Fatalf("failure = %v, want exit status", failure)
	}
	if strings.Contains(failure.Error(), "secret-value") || !strings.Contains(failure.Error(), "REDACTED") {
		t.Fatalf("failure leaked stderr secret: %v", failure)
	}
}

func TestProcessRuntimeDoesNotExposeSecretsAfterStderrTruncation(t *testing.T) {
	runtime := &processPiRuntime{
		events:     make(chan PiEvent),
		done:       make(chan struct{}),
		stop:       make(chan struct{}),
		pending:    map[string]chan runtimeResponse{},
		stderrTail: []byte("secret-value"), stderrTruncated: true,
	}
	runtime.finish(errors.New("pi process exited"))

	if failure := runtime.Failure(); failure == nil || strings.Contains(failure.Error(), "secret-value") {
		t.Fatalf("truncated stderr leaked: %v", failure)
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

func TestProcessRuntimeEventBackpressureWaitsForConsumer(t *testing.T) {
	runtime := &processPiRuntime{events: make(chan PiEvent, 1), done: make(chan struct{}), pending: map[string]chan runtimeResponse{}}
	runtime.events <- PiEvent{Type: "already-buffered"}
	done := make(chan struct{})
	go func() {
		if err := runtime.handleLine([]byte(`{"type":"message_update"}`)); err != nil {
			t.Errorf("handleLine returned error: %v", err)
		}
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("stdout reader discarded backpressure instead of waiting for consumer")
	case <-time.After(100 * time.Millisecond):
	}
	<-runtime.events
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stdout reader did not resume after consumer drained the channel")
	}
	select {
	case event := <-runtime.events:
		if event.Type != "message_update" {
			t.Fatalf("event type = %q, want message_update", event.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("backpressured event was not delivered")
	}
}

func TestRuntimeEntryEventBackpressureWaitsForConsumer(t *testing.T) {
	runtime := newFakePiRuntime()
	entry := &runtimeEntry{runtime: runtime, manager: &RuntimeManager{now: time.Now}, events: make(chan PiEvent, 1), stop: make(chan struct{}), activity: map[string]int{}, closeDone: make(chan struct{})}
	entry.events <- PiEvent{Type: "already-buffered"}
	defer entry.Close(context.Background())
	done := make(chan struct{})
	go func() {
		entry.enqueueEvent(PiEvent{Type: "message_update", Raw: json.RawMessage(`{"type":"message_update"}`)})
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("entry discarded backpressure instead of waiting for consumer")
	case <-time.After(100 * time.Millisecond):
	}
	if !entry.IsRunning() {
		t.Fatal("entry closed while waiting for consumer")
	}
	<-entry.events
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("entry did not resume after consumer drained the channel")
	}
	select {
	case event := <-entry.events:
		if event.Type != "message_update" {
			t.Fatalf("event type = %q, want message_update", event.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("backpressured event was not delivered")
	}
}

func TestProcessRuntimeRealProcessBackpressureWaitsForConsumer(t *testing.T) {
	root := t.TempDir()
	writeFakePi(t, root, `#!/bin/sh
read request
i=0
while [ "$i" -lt 65 ]; do
  printf '%s\n' '{"type":"message_update"}'
  i=$((i + 1))
done
printf '%s\n' '{"id":"request-1","type":"response","command":"prompt","success":true}'
`)
	runtimeValue, err := newPiRuntime(context.Background(), filepath.Join(root, "session.jsonl"), root)
	if err != nil {
		t.Fatal(err)
	}
	runtime := runtimeValue.(*processPiRuntime)
	defer runtime.Close(context.Background())
	result := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, sendErr := runtime.Send(ctx, RPCCommand{ID: "request-1", Type: "prompt"})
		result <- sendErr
	}()
	for index := 0; index < 65; index++ {
		select {
		case event := <-runtime.Events():
			if event.Type != "message_update" {
				t.Fatalf("event %d type = %q, want message_update", index, event.Type)
			}
		case <-time.After(time.Second):
			t.Fatalf("event %d was not delivered", index)
		}
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("prompt failed after consumer resumed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("prompt did not resume after consumer drained the channel")
	}
}

func TestProcessRuntimeNaturalExitUnblocksBackpressuredReader(t *testing.T) {
	root := t.TempDir()
	writeFakePi(t, root, `#!/bin/sh
read request
i=0
while [ "$i" -lt 65 ]; do
  printf '%s\n' '{"type":"message_update"}'
  i=$((i + 1))
done
exit 0
`)
	runtimeValue, err := newPiRuntime(context.Background(), filepath.Join(root, "session.jsonl"), root)
	if err != nil {
		t.Fatal(err)
	}
	runtime := runtimeValue.(*processPiRuntime)
	defer runtime.Close(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err = runtime.Send(ctx, RPCCommand{ID: "request-1", Type: "prompt"})
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("natural process exit did not unblock the backpressured reader")
	}
}

func TestProcessRuntimeClosesEventChannelAfterEOF(t *testing.T) {
	root := t.TempDir()
	writeFakePi(t, root, `#!/bin/sh
read request
exit 0
`)
	runtimeValue, err := newPiRuntime(context.Background(), filepath.Join(root, "session.jsonl"), root)
	if err != nil {
		t.Fatal(err)
	}
	runtime := runtimeValue.(*processPiRuntime)
	defer runtime.Close(context.Background())
	_, _ = runtime.Send(context.Background(), RPCCommand{ID: "request-1", Type: "prompt"})
	select {
	case _, ok := <-runtime.Events():
		if ok {
			t.Fatal("EOF left runtime event channel open with an event")
		}
	case <-time.After(time.Second):
		t.Fatal("EOF did not close runtime event channel")
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

func TestRuntimeEntryCloseTimeoutUnblocksBackpressure(t *testing.T) {
	runtime := newFakePiRuntime()
	entry := &runtimeEntry{runtime: runtime, manager: &RuntimeManager{now: time.Now}, events: make(chan PiEvent, 1), stop: make(chan struct{}), activity: map[string]int{}, closeDone: make(chan struct{})}
	entry.events <- PiEvent{Type: "already-buffered"}
	if !entry.steerGate.acquire(context.Background()) {
		t.Fatal("failed to hold entry command gate")
	}
	defer entry.steerGate.release()
	done := make(chan struct{})
	go func() {
		entry.enqueueEvent(PiEvent{Type: "message_update", Raw: json.RawMessage(`{"type":"message_update"}`)})
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("entry did not wait while event channel was full")
	case <-time.After(100 * time.Millisecond):
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := entry.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close error = %v, want context deadline exceeded", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("entry backpressure remained blocked after close timeout")
	}
}

func TestRuntimeEntryBackpressureUnblocksOnClose(t *testing.T) {
	runtime := newFakePiRuntime()
	entry := &runtimeEntry{runtime: runtime, manager: &RuntimeManager{now: time.Now}, events: make(chan PiEvent, 1), stop: make(chan struct{}), activity: map[string]int{}, closeDone: make(chan struct{})}
	entry.events <- PiEvent{Type: "already-buffered"}
	done := make(chan struct{})
	go func() {
		entry.enqueueEvent(PiEvent{Type: "message_update", Raw: json.RawMessage(`{"type":"message_update"}`)})
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("entry did not wait while event channel was full")
	case <-time.After(100 * time.Millisecond):
	}
	if err := entry.Close(context.Background()); err != nil {
		t.Fatalf("close entry: %v", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("entry backpressure did not unblock after close")
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

func TestProcessRuntimeEventBackpressureUnblocksOnClose(t *testing.T) {
	runtime := &processPiRuntime{
		events: make(chan PiEvent, 1), done: make(chan struct{}), stop: make(chan struct{}),
		pending: map[string]chan runtimeResponse{}, running: true,
	}
	runtime.events <- PiEvent{Type: "already-buffered"}
	done := make(chan struct{})
	go func() {
		if err := runtime.handleLine([]byte(`{"type":"message_update"}`)); err != nil {
			t.Errorf("handleLine returned error: %v", err)
		}
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("runtime did not wait while event channel was full")
	case <-time.After(100 * time.Millisecond):
	}
	runtime.stopEvents()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runtime backpressure did not unblock after close")
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

func TestProcessRuntimeForceCloseDoesNotWaitForeverForInheritedPipes(t *testing.T) {
	root := t.TempDir()
	writeFakePi(t, root, `#!/bin/sh
read request
setsid sh -c 'sleep 30' &
sleep 30
`)
	runtimeValue, err := newPiRuntime(context.Background(), filepath.Join(root, "session.jsonl"), root)
	if err != nil {
		t.Fatal(err)
	}
	runtime := runtimeValue.(*processPiRuntime)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	closeDone := make(chan error, 1)
	go func() { closeDone <- runtime.Close(ctx) }()
	select {
	case err := <-closeDone:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("forced close error = %v, want deadline exceeded", err)
		}
	case <-time.After(time.Second):
		t.Fatal("forced close waited forever for inherited pipes")
	}
	select {
	case <-runtime.done:
	case <-time.After(time.Second):
		t.Fatal("runtime cleanup did not finish after forced close")
	}
	select {
	case <-runtime.stdoutDone:
	case <-time.After(time.Second):
		t.Fatal("stdout pipe reader did not exit after forced close")
	}
	select {
	case <-runtime.stderrDone:
	case <-time.After(time.Second):
		t.Fatal("stderr pipe reader did not exit after forced close")
	}
	select {
	case _, ok := <-runtime.Events():
		if ok {
			t.Fatal("runtime event channel remained open after forced close")
		}
	case <-time.After(time.Second):
		t.Fatal("runtime event channel did not close after forced close")
	}
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
