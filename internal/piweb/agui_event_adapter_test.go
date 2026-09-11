package piweb

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func durableEvent(seq int64, eventType, entityID string, payload any) StoredSessionEvent {
	data, _ := json.Marshal(payload)
	return StoredSessionEvent{Seq: seq, SessionEventDraft: SessionEventDraft{
		SessionID: "s1", RunID: "run-1", SourceID: "run-1:event:" + string(rune('0'+seq)),
		Type: eventType, EntityID: entityID, Payload: data,
	}}
}

func renderDurableAgui(t *testing.T, events ...StoredSessionEvent) string {
	t.Helper()
	res := httptest.NewRecorder()
	stream := newAguiEventStream(res, t.Context())
	state := newAguiDurableState("thread", "run", "s1")
	for _, event := range events {
		if _, err := state.Consume(event, stream); err != nil {
			t.Fatal(err)
		}
	}
	return res.Body.String()
}

func TestAguiDurableToolResultPrecedesToolEndAndCursorFollowsPayload(t *testing.T) {
	body := renderDurableAgui(t,
		durableEvent(1, EventToolStarted, "tool:run-1:call-1", map[string]any{"toolCallId": "call-1", "tool": "bash"}),
		durableEvent(2, EventToolOutput, "tool:run-1:call-1", map[string]any{"chunk": "final", "mode": "replace"}),
		durableEvent(3, EventToolFinished, "tool:run-1:call-1", map[string]any{"body": "final", "status": ToolStatusOK}),
	)
	result := strings.Index(body, `"type":"TOOL_CALL_RESULT"`)
	end := strings.Index(body, `"type":"TOOL_CALL_END"`)
	cursor := strings.LastIndex(body, `"name":"piweb.broker-cursor"`)
	if result < 0 || end < result {
		t.Fatalf("tool result must precede tool end:\n%s", body)
	}
	if cursor < end {
		t.Fatalf("cursor must follow payload:\n%s", body)
	}
	if !strings.Contains(body, `"eventId":"3"`) {
		t.Fatalf("cursor seq missing:\n%s", body)
	}
}

func TestAguiDurableAssistantSegmentsAndTerminal(t *testing.T) {
	body := renderDurableAgui(t,
		durableEvent(1, EventAssistantSegmentDelta, "assistant:run-1:1", map[string]any{"kind": "pi", "delta": "hello"}),
		durableEvent(2, EventAssistantSegmentDelta, "thinking:run-1:2", map[string]any{"kind": "think", "delta": "why"}),
		durableEvent(3, EventAssistantSegmentCommit, "assistant:run-1:1", map[string]any{"kind": "pi", "text": "hello"}),
		durableEvent(4, EventRunFinished, "", nil),
	)
	if !strings.Contains(body, `"type":"TEXT_MESSAGE_START"`) || !strings.Contains(body, "hello") {
		t.Fatalf("assistant text missing:\n%s", body)
	}
	if !strings.Contains(body, `"type":"REASONING_MESSAGE_START"`) || !strings.Contains(body, "why") {
		t.Fatalf("reasoning missing:\n%s", body)
	}
	if strings.Count(body, `"type":"TEXT_MESSAGE_END"`) != 1 {
		t.Fatalf("text end mismatch:\n%s", body)
	}
	if strings.Count(body, `"type":"REASONING_END"`) != 1 {
		t.Fatalf("reasoning end mismatch:\n%s", body)
	}
}

func TestAguiDurableRunFailureMapsErrorStatus(t *testing.T) {
	body := renderDurableAgui(t,
		durableEvent(1, EventToolStarted, "tool:run-1:call-1", map[string]any{"toolCallId": "call-1", "tool": "bash"}),
		durableEvent(2, EventToolFinished, "tool:run-1:call-1", map[string]any{"body": "boom", "status": ToolStatusErr}),
		durableEvent(3, EventRunFailed, "", map[string]any{"error": "boom"}),
	)
	if !strings.Contains(body, `"type":"TOOL_CALL_END"`) {
		t.Fatalf("tool end missing:\n%s", body)
	}
	if !strings.Contains(body, "boom") {
		t.Fatalf("tool error output missing:\n%s", body)
	}
}
