package piweb

import (
	"encoding/json"
	"errors"
	"testing"
)

func storedProjectionEvent(runID string, seq int64, eventType, entityID string, payload any) StoredSessionEvent {
	data, _ := json.Marshal(payload)
	return StoredSessionEvent{
		Seq: seq,
		SessionEventDraft: SessionEventDraft{
			SessionID: "s1", RunID: runID, SourceID: runID + ":event:" + string(rune('0'+seq)),
			SourceOrdinal: &seq, Type: eventType, EntityID: entityID, Payload: data,
		},
	}
}

func activeProjection(runID string) ProjectionView {
	return ProjectionView{Stream: SessionStreamState{SessionID: "s1", Status: SessionStatusRunning, CurrentRunID: runID}}
}

func activeProjectionWithTool(runID, entityID string) ProjectionView {
	view := activeProjection(runID)
	view.Items = map[string]ProjectedSessionItem{
		entityID: {ItemID: entityID, Kind: "tool", RunID: runID, EntityID: entityID, Status: ToolStatusRunning},
	}
	view.Order = []string{entityID}
	return view
}

func applyProjectionChange(view ProjectionView, change ProjectionChange) ProjectionView {
	view.Stream = change.Stream
	if view.Items == nil {
		view.Items = map[string]ProjectedSessionItem{}
	}
	for _, item := range change.Terminalize {
		view.Items[item.ItemID] = item
	}
	if change.Upsert != nil {
		view.Items[change.Upsert.ItemID] = *change.Upsert
		found := false
		for _, itemID := range view.Order {
			if itemID == change.Upsert.ItemID {
				found = true
				break
			}
		}
		if !found {
			view.Order = append(view.Order, change.Upsert.ItemID)
		}
	}
	return view
}

func TestReduceSessionEventKeepsToolIdentityAcrossRuns(t *testing.T) {
	first := storedProjectionEvent("run-1", 1, EventToolStarted, "tool:run-1:call-1", map[string]any{"toolCallId": "call-1"})
	second := storedProjectionEvent("run-2", 2, EventToolStarted, "tool:run-2:call-1", map[string]any{"toolCallId": "call-1"})
	firstChange, err := ReduceSessionEvent(activeProjection("run-1"), first)
	if err != nil {
		t.Fatal(err)
	}
	secondChange, err := ReduceSessionEvent(activeProjection("run-2"), second)
	if err != nil {
		t.Fatal(err)
	}
	if firstChange.Upsert.ItemID == secondChange.Upsert.ItemID {
		t.Fatalf("tool IDs collided: %q", firstChange.Upsert.ItemID)
	}
}

func TestReduceSessionEventRejectsSuccessfulFinishWithRunningTool(t *testing.T) {
	view := activeProjectionWithTool("run-1", "tool:run-1:call-1")
	_, err := ReduceSessionEvent(view, storedProjectionEvent("run-1", 3, EventRunFinished, "", nil))
	if !errors.Is(err, ErrActiveToolAtRunFinish) {
		t.Fatalf("error = %v", err)
	}
}

func TestReduceSessionEventBuildsAndUpdatesTool(t *testing.T) {
	view := activeProjection("run-1")
	start := storedProjectionEvent("run-1", 1, EventToolStarted, "tool:run-1:call-1", map[string]any{
		"toolCallId": "call-1", "tool": "bash", "args": "{\"command\":\"pwd\"}",
	})
	change, err := ReduceSessionEvent(view, start)
	if err != nil {
		t.Fatal(err)
	}
	view = applyProjectionChange(view, change)
	output := storedProjectionEvent("run-1", 2, EventToolOutput, "tool:run-1:call-1", map[string]any{"chunk": "first", "mode": "append"})
	change, err = ReduceSessionEvent(view, output)
	if err != nil {
		t.Fatal(err)
	}
	view = applyProjectionChange(view, change)
	finish := storedProjectionEvent("run-1", 3, EventToolFinished, "tool:run-1:call-1", map[string]any{
		"body": "final", "status": ToolStatusOK, "resultMeta": "done", "durationMs": 12,
	})
	change, err = ReduceSessionEvent(view, finish)
	if err != nil {
		t.Fatal(err)
	}
	if change.Upsert == nil || change.Upsert.Body != "final" || change.Upsert.Status != ToolStatusOK {
		t.Fatalf("finish change = %#v", change.Upsert)
	}
}

func TestReduceSessionEventTerminalizesToolsAndSegments(t *testing.T) {
	view := activeProjectionWithTool("run-1", "tool:run-1:call-1")
	view.Items["assistant:run-1:1"] = ProjectedSessionItem{ItemID: "assistant:run-1:1", Kind: "pi", RunID: "run-1", EntityID: "assistant:run-1:1", Status: "streaming"}
	view.Order = append(view.Order, "assistant:run-1:1")
	change, err := ReduceSessionEvent(view, storedProjectionEvent("run-1", 4, EventRunInterrupted, "", map[string]any{"reason": "restart"}))
	if err != nil {
		t.Fatal(err)
	}
	if change.Stream.Status != SessionStatusInterrupted || len(change.Terminalize) != 2 {
		t.Fatalf("terminal change = %#v", change)
	}
	for _, item := range change.Terminalize {
		if item.Status != ToolStatusInterrupted && item.Status != "committed" {
			t.Fatalf("item was not terminalized: %#v", item)
		}
	}
}

func TestReduceSessionEventProjectsRunFailureError(t *testing.T) {
	view := activeProjection("run-1")
	view.Items = map[string]ProjectedSessionItem{
		"assistant:run-1:1": {ItemID: "assistant:run-1:1", Kind: "pi", RunID: "run-1", Status: ItemStatusStreaming, Ordinal: 1},
	}
	view.Order = []string{"assistant:run-1:1"}
	change, err := ReduceSessionEvent(view, storedProjectionEvent("run-1", 2, EventRunFailed, "", map[string]any{"error": "429 rate limit"}))
	if err != nil {
		t.Fatal(err)
	}
	if change.Stream.Status != SessionStatusError {
		t.Fatalf("status = %q", change.Stream.Status)
	}
	if change.Upsert == nil || change.Upsert.Kind != "error" || change.Upsert.Text != "429 rate limit" {
		t.Fatalf("error item = %#v", change.Upsert)
	}
	if change.Upsert.RunID != "run-1" || change.Upsert.Ordinal != 2 || change.Upsert.ItemID != "error:run-1" {
		t.Fatalf("error item metadata = %#v", change.Upsert)
	}
}

func TestReduceSessionEventSkipsRunFailureWithoutErrorText(t *testing.T) {
	view := activeProjection("run-1")
	change, err := ReduceSessionEvent(view, storedProjectionEvent("run-1", 2, EventRunFailed, "", map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	if change.Upsert != nil {
		t.Fatalf("unexpected error item = %#v", change.Upsert)
	}
}

func TestReduceSessionEventRejectsStaleRunAndInvalidEntity(t *testing.T) {
	stale := storedProjectionEvent("run-old", 1, EventToolOutput, "tool:run-old:call-1", map[string]any{"chunk": "x"})
	if _, err := ReduceSessionEvent(activeProjection("run-new"), stale); !errors.Is(err, ErrStaleSessionRun) {
		t.Fatalf("stale error = %v", err)
	}
	invalid := storedProjectionEvent("run-1", 1, EventToolStarted, "call-1", nil)
	if _, err := ReduceSessionEvent(activeProjection("run-1"), invalid); !errors.Is(err, ErrInvalidEntityID) {
		t.Fatalf("entity error = %v", err)
	}
}

func TestReduceSessionEventHandlesAssistantSegmentsAndControls(t *testing.T) {
	view := activeProjection("run-1")
	start := storedProjectionEvent("run-1", 1, EventAssistantSegmentStarted, "assistant:run-1:1", map[string]any{"kind": "pi"})
	change, err := ReduceSessionEvent(view, start)
	if err != nil {
		t.Fatal(err)
	}
	view = applyProjectionChange(view, change)
	delta := storedProjectionEvent("run-1", 2, EventAssistantSegmentDelta, "assistant:run-1:1", map[string]any{"delta": "hello"})
	change, err = ReduceSessionEvent(view, delta)
	if err != nil {
		t.Fatal(err)
	}
	if change.Upsert == nil || change.Upsert.Text != "hello" {
		t.Fatalf("delta change = %#v", change.Upsert)
	}
	control := storedProjectionEvent("", 3, EventSessionRebased, "", nil)
	control.SessionID = "s1"
	change, err = ReduceSessionEvent(view, control)
	if err != nil {
		t.Fatal(err)
	}
	if !change.RequireRebase {
		t.Fatal("session.rebased must require a snapshot")
	}
}
