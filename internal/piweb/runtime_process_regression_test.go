package piweb

import (
	"context"
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
