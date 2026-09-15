package piweb

import (
	"path/filepath"
	"testing"
)

func TestOpenServerRecoversInterruptedRunsBeforeServing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pi-web.db")
	seedStore, err := OpenSQLiteSessionEventStore(path, NewSessionWakeHub())
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := seedStore.StartRun(t.Context(), StartRunCommand{SessionID: "s1", ClientRequestID: "req-1", RequestHash: "h", RunID: "run-1", DisplayText: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seedStore.Append(t.Context(), "s1", appendToolStartedDraft(receipt.RunID, 1, "call-1")); err != nil {
		t.Fatal(err)
	}
	if err := seedStore.Close(t.Context()); err != nil {
		t.Fatal(err)
	}

	store := NewWebStore(path)
	server, err := OpenServer(Config{}, store, NewBroker())
	if err != nil {
		t.Fatal(err)
	}
	defer server.Shutdown(t.Context())
	if server.commands == nil {
		t.Fatal("OpenServer did not attach the durable command service")
	}

	snapshot, err := server.events.Snapshot(t.Context(), "s1", SessionPageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != SessionStatusInterrupted || snapshot.RunID != "" {
		t.Fatalf("snapshot status = %q run = %q", snapshot.Status, snapshot.RunID)
	}
	var tool *ProjectedSessionItem
	for index := range snapshot.Items {
		if snapshot.Items[index].Kind == "tool" {
			tool = &snapshot.Items[index]
		}
	}
	if tool == nil || tool.Status != ToolStatusInterrupted {
		t.Fatalf("tool = %#v", tool)
	}
}

func TestOpenServerRecoversPendingCancellationAsCancelled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pi-web.db")
	seedStore, err := OpenSQLiteSessionEventStore(path, NewSessionWakeHub())
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := seedStore.StartRun(t.Context(), StartRunCommand{SessionID: "s1", ClientRequestID: "req-1", RequestHash: "h", RunID: "run-1", DisplayText: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seedStore.AppendControl(t.Context(), "s1", "command:cancel", EventRunCancelRequested, []byte(`{"runId":"run-1"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := seedStore.Append(t.Context(), "s1", appendToolStartedDraft(receipt.RunID, 1, "call-1")); err != nil {
		t.Fatal(err)
	}
	if err := seedStore.Close(t.Context()); err != nil {
		t.Fatal(err)
	}

	store := NewWebStore(path)
	server, err := OpenServer(Config{}, store, NewBroker())
	if err != nil {
		t.Fatal(err)
	}
	defer server.Shutdown(t.Context())
	snapshot, err := server.events.Snapshot(t.Context(), "s1", SessionPageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != SessionStatusCancelled || snapshot.RunID != "" {
		t.Fatalf("snapshot status = %q run = %q", snapshot.Status, snapshot.RunID)
	}
}

func TestRecoverInterruptedIsIdempotent(t *testing.T) {
	store := appendTestStore(t)
	receipt := appendTestRun(t, store, "req-1")
	if _, err := store.Append(t.Context(), "s1", appendToolStartedDraft(receipt.RunID, 1, "call-1")); err != nil {
		t.Fatal(err)
	}
	if err := store.RecoverInterrupted(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := store.RecoverInterrupted(t.Context()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.db.QueryRow(`SELECT count(*) FROM session_events WHERE event_type=?`, EventRunInterrupted).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("run.interrupted count = %d", count)
	}
}

func TestOpenServerRejectsMissingDatabasePath(t *testing.T) {
	if _, err := OpenServer(Config{}, NewWebStore(""), NewBroker()); err == nil {
		t.Fatal("expected missing path error")
	}
	if _, err := OpenServer(Config{}, nil, NewBroker()); err == nil {
		t.Fatal("expected nil store error")
	}
}
