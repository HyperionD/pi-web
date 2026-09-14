package piweb

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestSnapshotRejectsCursorFromOldProjectionGeneration(t *testing.T) {
	store := appendTestStore(t)
	receipt := appendTestRun(t, store, "req-1")
	for index := int64(1); index <= 3; index++ {
		appendAssistantProjectionEvent(t, store, receipt.RunID, index, "assistant:"+receipt.RunID+":"+string(rune('0'+index)), "text "+string(rune('0'+index)))
	}
	cursor, err := EncodeProjectionCursor(ProjectionCursor{Generation: 1, BeforeOrdinal: 3})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE session_streams SET projection_generation=2 WHERE session_id='s1'`); err != nil {
		t.Fatal(err)
	}
	_, err = store.Snapshot(t.Context(), "s1", SessionPageRequest{Limit: 2, Before: cursor})
	if !errors.Is(err, ErrProjectionCursorExpired) {
		t.Fatalf("error = %v", err)
	}
}

func TestSnapshotPaginatesInDisplayOrder(t *testing.T) {
	store := appendTestStore(t)
	receipt := appendTestRun(t, store, "req-1")
	for index := int64(1); index <= 3; index++ {
		appendAssistantProjectionEvent(t, store, receipt.RunID, index, "assistant:"+receipt.RunID+":"+string(rune('0'+index)), "text "+string(rune('0'+index)))
	}
	page, err := store.Snapshot(t.Context(), "s1", SessionPageRequest{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].Text != "text 2" || page.Items[1].Text != "text 3" || !page.HasMore {
		t.Fatalf("page = %#v", page)
	}
	decoded, err := DecodeProjectionCursor(page.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.BeforeOrdinal != page.Items[0].Ordinal {
		t.Fatalf("cursor = %#v, first item = %#v", decoded, page.Items[0])
	}
}

func TestEventsAfterReturnsResetWhenCursorExpired(t *testing.T) {
	store := appendTestStore(t)
	receipt := appendTestRun(t, store, "req-1")
	appendAssistantProjectionEvent(t, store, receipt.RunID, 1, "assistant:"+receipt.RunID+":1", "text")
	if _, err := store.db.Exec(`UPDATE session_streams SET min_retained_seq=3 WHERE session_id='s1'`); err != nil {
		t.Fatal(err)
	}
	batch, err := store.EventsAfter(t.Context(), "s1", 0, EventReadLimit{MaxRows: 128, MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if !batch.ResetRequired {
		t.Fatalf("batch = %#v", batch)
	}
}

func TestEventsAfterReturnsOversizedFirstEvent(t *testing.T) {
	store := appendTestStore(t)
	receipt := appendTestRun(t, store, "req-1")
	payload, _ := json.Marshal(map[string]any{"kind": "pi", "text": "0123456789"})
	ordinal := int64(1)
	if _, err := store.Append(t.Context(), "s1", SessionEventDraft{
		SessionID: "s1", RunID: receipt.RunID, SourceID: receipt.RunID + ":event:1", SourceOrdinal: &ordinal, Type: EventAssistantSegmentCommit,
		EntityID: "assistant:" + receipt.RunID + ":1", Payload: payload,
	}); err != nil {
		t.Fatal(err)
	}
	batch, err := store.EventsAfter(t.Context(), "s1", 2, EventReadLimit{MaxRows: 128, MaxBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Events) != 1 || string(batch.Events[0].Payload) != string(payload) {
		t.Fatalf("batch = %#v", batch)
	}
}

func TestItemContentReadsChunkedBodyAndRange(t *testing.T) {
	store := appendTestStore(t)
	receipt := appendTestRun(t, store, "req-1")
	callID := "call-1"
	start := appendToolStartedDraft(receipt.RunID, 1, callID)
	if _, err := store.Append(t.Context(), "s1", start); err != nil {
		t.Fatal(err)
	}
	body := "0123456789abcdefghijklmnopqrstuvwxyz"
	for index := int64(0); index < int64(len(body)); index += 8 {
		end := index + 8
		if end > int64(len(body)) {
			end = int64(len(body))
		}
		payload, _ := json.Marshal(map[string]any{"chunk": body[index:end], "mode": "append"})
		if _, err := store.Append(t.Context(), "s1", SessionEventDraft{
			SessionID: "s1", RunID: receipt.RunID, SourceID: receipt.RunID + ":event:" + string(rune('0'+index/8+2)), SourceOrdinal: &[]int64{index/8 + 2}[0], Type: EventToolOutput, EntityID: "tool:" + receipt.RunID + ":" + callID, Payload: payload,
		}); err != nil {
			t.Fatal(err)
		}
	}
	data, version, err := store.ItemContent(t.Context(), "s1", "tool:"+receipt.RunID+":"+callID, 0, 10, 20)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != body[10:20] || version == 0 {
		t.Fatalf("data=%q version=%d", data, version)
	}
}

func appendAssistantProjectionEvent(t *testing.T, store *SQLiteSessionEventStore, runID string, ordinal int64, entityID, text string) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"kind": "pi", "text": text})
	if _, err := store.Append(t.Context(), "s1", SessionEventDraft{
		SessionID: "s1", RunID: runID, SourceID: runID + ":assistant:" + string(rune('0'+ordinal)), SourceOrdinal: &ordinal, Type: EventAssistantSegmentCommit, EntityID: entityID, Payload: payload,
	}); err != nil {
		t.Fatal(err)
	}
}
