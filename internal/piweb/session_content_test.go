package piweb

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func appendToolOutputChunk(t *testing.T, store *SQLiteSessionEventStore, runID, callID string, ordinal int64, chunk, mode string) StoredSessionEvent {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"chunk": chunk, "mode": mode})
	stored, err := store.Append(t.Context(), "s1", SessionEventDraft{
		SessionID: "s1", RunID: runID, SourceID: runID + ":output:" + string(rune('a'+ordinal)),
		SourceOrdinal: &ordinal, Type: EventToolOutput, EntityID: "tool:" + runID + ":" + callID, Payload: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	return stored
}

func TestItemContentDetectsStaleVersionAndInvalidRange(t *testing.T) {
	store := appendTestStore(t)
	receipt := appendTestRun(t, store, "req-1")
	if _, err := store.Append(t.Context(), "s1", appendToolStartedDraft(receipt.RunID, 1, "call-1")); err != nil {
		t.Fatal(err)
	}
	appendToolOutputChunk(t, store, receipt.RunID, "call-1", 2, "hello world", "append")
	itemID := "tool:" + receipt.RunID + ":call-1"
	_, version, err := store.ItemContent(t.Context(), "s1", itemID, 0, 0, -1)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ItemContent(t.Context(), "s1", itemID, version-1, 0, -1); !errors.Is(err, ErrContentVersionConflict) {
		t.Fatalf("stale error = %v", err)
	}
	if _, _, err := store.ItemContent(t.Context(), "s1", itemID, version, 5, 2); !errors.Is(err, ErrInvalidContentRange) {
		t.Fatalf("range error = %v", err)
	}
	if _, _, err := store.ItemContent(t.Context(), "s1", "missing", 0, 0, -1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing error = %v", err)
	}
}

func TestItemContentPreservesTenMiBOutput(t *testing.T) {
	store := appendTestStore(t)
	receipt := appendTestRun(t, store, "req-1")
	if _, err := store.Append(t.Context(), "s1", appendToolStartedDraft(receipt.RunID, 1, "call-1")); err != nil {
		t.Fatal(err)
	}
	large := strings.Repeat("0123456789abcdef", 640*1024)
	appendToolOutputChunk(t, store, receipt.RunID, "call-1", 2, large, "replace")
	itemID := "tool:" + receipt.RunID + ":call-1"
	data, _, err := store.ItemContent(t.Context(), "s1", itemID, 0, 0, -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != len(large) || !bytes.Equal(data, []byte(large)) {
		t.Fatalf("content mismatch: len=%d want=%d", len(data), len(large))
	}
}

func TestSnapshotInlineBodyAndContentRef(t *testing.T) {
	store := appendTestStore(t)
	receipt := appendTestRun(t, store, "req-1")
	if _, err := store.Append(t.Context(), "s1", appendToolStartedDraft(receipt.RunID, 1, "call-1")); err != nil {
		t.Fatal(err)
	}
	appendToolOutputChunk(t, store, receipt.RunID, "call-1", 2, "inline body", "append")
	page, err := store.Snapshot(t.Context(), "s1", SessionPageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var tool *ProjectedSessionItem
	for index := range page.Items {
		if page.Items[index].Kind == "tool" {
			tool = &page.Items[index]
		}
	}
	if tool == nil || tool.ContentRef != tool.ItemID || tool.ContentVersion == 0 || tool.ContentLength != int64(len("inline body")) {
		t.Fatalf("tool content metadata = %#v", tool)
	}
}

func TestDeleteSessionCascadesEventStoreRows(t *testing.T) {
	store := appendTestStore(t)
	receipt := appendTestRun(t, store, "req-1")
	if _, err := store.Append(t.Context(), "s1", appendToolStartedDraft(receipt.RunID, 1, "call-1")); err != nil {
		t.Fatal(err)
	}
	appendToolOutputChunk(t, store, receipt.RunID, "call-1", 2, "body", "append")
	if err := store.DeleteSession(t.Context(), "s1"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"session_streams", "session_events", "session_items", "session_item_chunks", "session_commands"} {
		var count int
		if err := store.db.QueryRow(`SELECT count(*) FROM ` + table + ` WHERE session_id='s1'`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s retained %d rows", table, count)
		}
	}
}
