package piweb

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func appendTestStore(t *testing.T) *SQLiteSessionEventStore {
	t.Helper()
	store, err := OpenSQLiteSessionEventStore(filepath.Join(t.TempDir(), "events.db"), NewSessionWakeHub())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(t.Context()) })
	return store
}

func appendTestRun(t *testing.T, store *SQLiteSessionEventStore, requestID string) RunReceipt {
	t.Helper()
	receipt, err := store.StartRun(t.Context(), StartRunCommand{
		SessionID: "s1", ClientRequestID: requestID, RequestHash: "hash-" + requestID,
		RunID: "run-" + requestID, DisplayText: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}

func appendToolStartedDraft(runID string, ordinal int64, callID string) SessionEventDraft {
	payload, _ := json.Marshal(map[string]any{"toolCallId": callID, "tool": "bash", "args": "pwd"})
	return SessionEventDraft{
		SessionID: "s1", RunID: runID, SourceID: runID + ":event:" + string(rune('0'+ordinal)),
		SourceOrdinal: &ordinal, Type: EventToolStarted, EntityID: "tool:" + runID + ":" + callID, Payload: payload,
	}
}

func TestAppendRetriesTransientSQLiteBusy(t *testing.T) {
	store := appendTestStore(t)
	receipt := appendTestRun(t, store, "req-busy")
	attempts := 0
	store.eventStoreFaultHook = func(point EventStoreFaultPoint) error {
		if point == FaultBeforeCommit && attempts < 2 {
			attempts++
			return errors.New("database is locked (5) (SQLITE_BUSY)")
		}
		return nil
	}
	if _, err := store.Append(t.Context(), "s1", appendToolStartedDraft(receipt.RunID, 1, "call-1")); err != nil {
		t.Fatalf("append did not retry past a busy error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("retry attempts = %d, want 2", attempts)
	}
	var count int
	if err := store.db.QueryRow(`SELECT count(*) FROM session_events WHERE session_id='s1' AND event_type=?`, EventToolStarted).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("tool.started count = %d, want 1 (retry must not duplicate)", count)
	}
}

func TestStartRunCommitsUserAndRunAtomically(t *testing.T) {
	store := appendTestStore(t)
	receipt := appendTestRun(t, store, "req-1")
	if receipt.RunID != "run-req-1" || receipt.AcceptedSeq != 2 || receipt.Existing {
		t.Fatalf("receipt = %#v", receipt)
	}
	var count int
	if err := store.db.QueryRow(`SELECT count(*) FROM session_events WHERE session_id='s1'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("event count = %d, want 2", count)
	}
}

func TestStartRunIsIdempotentAndRejectsHashConflict(t *testing.T) {
	store := appendTestStore(t)
	first := appendTestRun(t, store, "req-1")
	second, err := store.StartRun(t.Context(), StartRunCommand{
		SessionID: "s1", ClientRequestID: "req-1", RequestHash: "hash-req-1", RunID: "other", DisplayText: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Existing || second.RunID != first.RunID || second.AcceptedSeq != first.AcceptedSeq {
		t.Fatalf("existing receipt = %#v", second)
	}
	_, err = store.StartRun(t.Context(), StartRunCommand{
		SessionID: "s1", ClientRequestID: "req-1", RequestHash: "different", RunID: "other", DisplayText: "hello",
	})
	if !errors.Is(err, ErrCommandHashConflict) {
		t.Fatalf("conflict error = %v", err)
	}
}

func TestAppendCommitsEventAndProjectionAtomically(t *testing.T) {
	store := appendTestStore(t)
	receipt := appendTestRun(t, store, "req-1")
	stored, err := store.Append(t.Context(), "s1", appendToolStartedDraft(receipt.RunID, 1, "call-1"))
	if err != nil {
		t.Fatal(err)
	}
	if stored.Seq != 3 {
		t.Fatalf("stored seq = %d, want 3", stored.Seq)
	}
	var status string
	if err := store.db.QueryRow(`SELECT json_extract(payload_json, '$.status') FROM session_items WHERE entity_id=?`, "tool:"+receipt.RunID+":call-1").Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != ToolStatusRunning {
		t.Fatalf("tool status = %q", status)
	}
}

func TestAppendLatestSourceRetryIsIdempotent(t *testing.T) {
	store := appendTestStore(t)
	receipt := appendTestRun(t, store, "req-1")
	draft := appendToolStartedDraft(receipt.RunID, 1, "call-1")
	first, err := store.Append(t.Context(), "s1", draft)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Append(t.Context(), "s1", draft)
	if err != nil {
		t.Fatal(err)
	}
	if second.Seq != first.Seq {
		t.Fatalf("retry seq = %d, first = %d", second.Seq, first.Seq)
	}
	var count int
	if err := store.db.QueryRow(`SELECT count(*) FROM session_events WHERE event_type=?`, EventToolStarted).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("tool event count = %d", count)
	}
}

func TestStartRunResetsRuntimeSourceFenceForNextRun(t *testing.T) {
	store := appendTestStore(t)
	first := appendTestRun(t, store, "req-1")
	ordinal := int64(1)
	if _, err := store.Append(t.Context(), "s1", SessionEventDraft{
		SessionID: "s1", RunID: first.RunID, SourceID: first.RunID + ":event:1",
		SourceOrdinal: &ordinal, Type: EventRunFinished, Payload: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
	second := appendTestRun(t, store, "req-2")
	if _, err := store.Append(t.Context(), "s1", appendToolStartedDraft(second.RunID, 1, "call-2")); err != nil {
		t.Fatalf("second run ordinal 1 was rejected: %v", err)
	}
}

func TestAppendRejectsSameOrdinalFromDifferentEventIdentity(t *testing.T) {
	store := appendTestStore(t)
	receipt := appendTestRun(t, store, "req-1")
	draft := appendToolStartedDraft(receipt.RunID, 1, "call-1")
	if _, err := store.Append(t.Context(), "s1", draft); err != nil {
		t.Fatal(err)
	}
	draft.SourceID = receipt.RunID + ":different-source"
	if _, err := store.Append(t.Context(), "s1", draft); !errors.Is(err, ErrInvalidSourceOrdinal) {
		t.Fatalf("different source retry error = %v", err)
	}
	draft = appendToolStartedDraft("old-run", 1, "call-1")
	if _, err := store.Append(t.Context(), "s1", draft); !errors.Is(err, ErrInvalidSourceOrdinal) {
		t.Fatalf("different run retry error = %v", err)
	}
}

func TestAppendRejectsStaleRunAndSourceGap(t *testing.T) {
	store := appendTestStore(t)
	receipt := appendTestRun(t, store, "req-1")
	stale := appendToolStartedDraft("old-run", 1, "call-1")
	if _, err := store.Append(t.Context(), "s1", stale); !errors.Is(err, ErrStaleSessionRun) {
		t.Fatalf("stale error = %v", err)
	}
	gap := appendToolStartedDraft(receipt.RunID, 3, "call-3")
	if _, err := store.Append(t.Context(), "s1", gap); !errors.Is(err, ErrInvalidSourceOrdinal) {
		t.Fatalf("gap error = %v", err)
	}
}
