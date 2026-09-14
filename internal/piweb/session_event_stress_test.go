package piweb

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func TestSessionEventStressBoundedRetentionAndProjection(t *testing.T) {
	store := appendTestStore(t)
	store.SetRetentionLimits(RetentionLimits{ActiveRows: 256, ActiveBytes: 1 << 20, FinalRows: 64, FinalBytes: 1 << 20})
	receipt := appendTestRun(t, store, "req-1")
	if _, err := store.Append(t.Context(), "s1", appendToolStartedDraft(receipt.RunID, 1, "call-1")); err != nil {
		t.Fatal(err)
	}
	entityID := "tool:" + receipt.RunID + ":call-1"
	// Keep enough cycles to exercise retention repeatedly without making race runs time out.
	const eventCount = 8192
	for index := int64(0); index < eventCount; index++ {
		ordinal := index + 2
		payload, _ := json.Marshal(map[string]any{"chunk": strings.Repeat("x", 64), "mode": "replace"})
		if _, err := store.Append(t.Context(), "s1", SessionEventDraft{
			SessionID: "s1", RunID: receipt.RunID, SourceID: eventSourceID(receipt.RunID, ordinal),
			SourceOrdinal: &ordinal, Type: EventToolOutput, EntityID: entityID, Payload: payload,
		}); err != nil {
			t.Fatalf("append %d: %v", index, err)
		}
	}
	var rows int
	if err := store.db.QueryRow(`SELECT count(*) FROM session_events WHERE session_id='s1'`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows > 512 {
		t.Fatalf("retained rows = %d", rows)
	}
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
	if tool == nil || tool.Body != strings.Repeat("x", 64) {
		t.Fatalf("projection survived retention incorrectly: %#v", tool)
	}
	batch, err := store.EventsAfter(t.Context(), "s1", 0, EventReadLimit{})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Events) > defaultEventRows {
		t.Fatalf("batch rows = %d", len(batch.Events))
	}
}

func TestConcurrentSessionEventStressDoesNotStopAtAStableOrdinal(t *testing.T) {
	store := appendTestStore(t)
	const sessions = 4
	const eventsPerSession = 400
	for index := 0; index < sessions; index++ {
		sessionID := "stress-" + itoa(int64(index))
		if _, err := store.StartRun(t.Context(), StartRunCommand{
			SessionID: sessionID, ClientRequestID: "request-" + sessionID, RequestHash: "hash-" + sessionID,
			RunID: "run-" + sessionID, DisplayText: "stress",
		}); err != nil {
			t.Fatal(err)
		}
	}
	var group sync.WaitGroup
	errorsFound := make(chan error, sessions)
	for index := 0; index < sessions; index++ {
		index := index
		group.Add(1)
		go func() {
			defer group.Done()
			sessionID := "stress-" + itoa(int64(index))
			runID := "run-" + sessionID
			entityID := "assistant:" + runID + ":1"
			ordinal := int64(1)
			startedPayload, _ := json.Marshal(map[string]any{"kind": "pi"})
			if _, err := store.Append(t.Context(), sessionID, SessionEventDraft{
				SessionID: sessionID, RunID: runID, SourceID: eventSourceID(runID, ordinal), SourceOrdinal: &ordinal,
				Type: EventAssistantSegmentStarted, EntityID: entityID, Payload: startedPayload,
			}); err != nil {
				errorsFound <- err
				return
			}
			for eventIndex := 0; eventIndex < eventsPerSession; eventIndex++ {
				ordinal++
				payload, _ := json.Marshal(map[string]any{"kind": "pi", "delta": "1\\n"})
				if _, err := store.Append(t.Context(), sessionID, SessionEventDraft{
					SessionID: sessionID, RunID: runID, SourceID: eventSourceID(runID, ordinal), SourceOrdinal: &ordinal,
					Type: EventAssistantSegmentDelta, EntityID: entityID, Payload: payload,
				}); err != nil {
					errorsFound <- err
					return
				}
			}
		}()
	}
	group.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Fatal(err)
	}
	for index := 0; index < sessions; index++ {
		snapshot, err := store.Snapshot(t.Context(), "stress-"+itoa(int64(index)), SessionPageRequest{Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		// StartRun writes user + run.started, then the goroutine appends one
		// segment start plus eventsPerSession deltas.
		wantSeq := int64(eventsPerSession + 3)
		if snapshot.Seq != wantSeq {
			t.Fatalf("session %d seq = %d, want %d", index, snapshot.Seq, wantSeq)
		}
	}
}

func TestLargeToolOutputStressPreservesBytes(t *testing.T) {
	store := appendTestStore(t)
	receipt := appendTestRun(t, store, "req-1")
	if _, err := store.Append(t.Context(), "s1", appendToolStartedDraft(receipt.RunID, 1, "call-1")); err != nil {
		t.Fatal(err)
	}
	large := strings.Repeat("0123456789abcdef", 640*1024)
	payload, _ := json.Marshal(map[string]any{"chunk": large, "mode": "replace"})
	ordinal := int64(2)
	if _, err := store.Append(t.Context(), "s1", SessionEventDraft{
		SessionID: "s1", RunID: receipt.RunID, SourceID: eventSourceID(receipt.RunID, ordinal),
		SourceOrdinal: &ordinal, Type: EventToolOutput, EntityID: "tool:" + receipt.RunID + ":call-1", Payload: payload,
	}); err != nil {
		t.Fatal(err)
	}
	data, _, err := store.ItemContent(t.Context(), "s1", "tool:"+receipt.RunID+":call-1", 0, 0, -1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, []byte(large)) {
		t.Fatalf("content mismatch: got %d bytes want %d", len(data), len(large))
	}
	// A single state page must not inline more than the bounded preview.
	page, err := store.Snapshot(t.Context(), "s1", SessionPageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Seq != 4 {
		t.Fatalf("seq = %d, want 4", page.Seq)
	}
}

func eventSourceID(runID string, ordinal int64) string {
	return runID + ":event:" + itoa(ordinal)
}

func itoa(value int64) string {
	if value == 0 {
		return "0"
	}
	var digits []byte
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}
