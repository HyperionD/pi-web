package piweb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunnerCompactUsesPiRPCAndPublishesSummary(t *testing.T) {
	root := t.TempDir()
	requestPath := filepath.Join(root, "compact-request.json")
	t.Setenv("PI_COMPACT_REQUEST", requestPath)
	writeFakePi(t, root, `#!/bin/sh
read request
printf '%s' "$request" > "$PI_COMPACT_REQUEST"
id=$(printf '%s' "$request" | sed 's/.*"id":"\([^"]*\)".*/\1/')
printf '%s\n' "{\"id\":\"$id\",\"type\":\"response\",\"command\":\"compact\",\"success\":true,\"data\":{\"summary\":\"kept the implementation plan\",\"tokensBefore\":120,\"estimatedTokensAfter\":40}}"
`)

	store := NewMockStore()
	workspace, err := store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateSession(workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	broker := NewBroker()
	result, err := NewRunner().Compact(context.Background(), broker, store, session.ID)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if result.TokensBefore != 120 || result.EstimatedTokensAfter != 40 {
		t.Fatalf("unexpected result: %+v", result)
	}
	request, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatal(err)
	}
	var command map[string]any
	if err := json.Unmarshal(request, &command); err != nil {
		t.Fatal(err)
	}
	if command["type"] != "compact" {
		t.Fatalf("unexpected RPC command: %s", request)
	}
	events := broker.Replay(session.ID, 0)
	if len(events) != 3 || events[0].Type != "session.status" || events[1].Type != "session.message" || events[2].Type != "session.status" {
		t.Fatalf("unexpected compact events: %#v", events)
	}
	if status := events[0].Payload.(map[string]string)["status"]; status != "compacting" {
		t.Fatalf("unexpected start status: %q", status)
	}
	if message := events[1].Payload.(Message).Text; !strings.Contains(message, "120") {
		t.Fatalf("unexpected compact message: %q", message)
	}
}

func TestRunnerCompactPropagatesPiRPCFailure(t *testing.T) {
	root := t.TempDir()
	writeFakePi(t, root, `#!/bin/sh
read request
id=$(printf '%s' "$request" | sed 's/.*"id":"\([^"]*\)".*/\1/')
printf '%s\n' "{\"id\":\"$id\",\"type\":\"response\",\"command\":\"compact\",\"success\":false,\"error\":\"quota exceeded\"}"
`)
	store := NewMockStore()
	workspace, err := store.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateSession(workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	broker := NewBroker()
	_, err = NewRunner().Compact(context.Background(), broker, store, session.ID)
	if err == nil || !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("expected quota error, got %v", err)
	}
	events := broker.Replay(session.ID, 0)
	if len(events) != 3 || events[1].Type != "error" {
		t.Fatalf("unexpected failure events: %#v", events)
	}
}

func TestRunnerReportsCompactingSessionSeparately(t *testing.T) {
	runner := NewRunner()
	runner.running["session"] = &activePiRun{compacting: true}
	if !runner.IsCompacting("session") {
		t.Fatal("expected session to be compacting")
	}
	if runner.IsCompacting("other") {
		t.Fatal("unexpected compacting state for another session")
	}
}

func TestRunnerCompactRejectsBusySession(t *testing.T) {
	store := NewMockStore()
	workspace, err := store.OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateSession(workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner()
	runner.running[session.ID] = &activePiRun{cancel: func() {}, stdin: nopWriteCloser{bytes.NewBufferString("")}}
	_, err = runner.Compact(context.Background(), NewBroker(), store, session.ID)
	if !errors.Is(err, errSessionBusy) {
		t.Fatalf("expected busy error, got %v", err)
	}
}
