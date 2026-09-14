package piweb

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type recordingSessionEventSink struct {
	mu     sync.Mutex
	events []StoredSessionEvent
	failAt int
}

func (s *recordingSessionEventSink) Append(_ context.Context, _ string, draft SessionEventDraft) (StoredSessionEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failAt > 0 && len(s.events)+1 >= s.failAt {
		return StoredSessionEvent{}, errors.New("injected sink failure")
	}
	event := StoredSessionEvent{Seq: int64(len(s.events) + 1), SessionEventDraft: draft}
	s.events = append(s.events, event)
	return event, nil
}

func (s *recordingSessionEventSink) Types() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	types := make([]string, 0, len(s.events))
	for _, event := range s.events {
		types = append(types, event.Type)
	}
	return types
}

func (s *recordingSessionEventSink) payloadString(index int, key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index >= len(s.events) {
		return ""
	}
	var value map[string]any
	if err := json.Unmarshal(s.events[index].Payload, &value); err != nil {
		return ""
	}
	text, _ := value[key].(string)
	return text
}

func consumeJSON(t *testing.T, adapter *RuntimeEventAdapter, raw string) {
	t.Helper()
	if err := adapter.Consume([]byte(raw)); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeEventAdapterAppendsSteeringUserMessage(t *testing.T) {
	sink := &recordingSessionEventSink{}
	adapter := NewRuntimeEventAdapter("s1", "run-1", sink)
	if err := adapter.AppendUserMessage("continue", []PromptAttachment{{Type: "image", Name: "shot.png"}}); err != nil {
		t.Fatal(err)
	}
	if got := sink.Types(); strings.Join(got, ",") != EventUserMessageCreated {
		t.Fatalf("types = %#v", got)
	}
	if got := sink.payloadString(0, "text"); got != "continue" {
		t.Fatalf("text = %q", got)
	}
}

func TestRuntimeEventAdapterFlushesFinalToolBodyBeforeFinish(t *testing.T) {
	sink := &recordingSessionEventSink{}
	adapter := NewRuntimeEventAdapter("s1", "run-1", sink)
	consumeJSON(t, adapter, `{"type":"tool_execution_start","toolCallId":"call-1","toolName":"bash","args":{"command":"pwd"}}`)
	consumeJSON(t, adapter, `{"type":"tool_execution_update","toolCallId":"call-1","toolName":"bash","partialResult":"partial"}`)
	consumeJSON(t, adapter, `{"type":"tool_execution_end","toolCallId":"call-1","toolName":"bash","result":"final"}`)
	if got := sink.Types(); strings.Join(got, ",") != EventToolStarted+","+EventToolOutput+","+EventToolFinished {
		t.Fatalf("types = %#v", got)
	}
	if sink.payloadString(1, "chunk") != "partial" || sink.payloadString(2, "body") != "final" {
		t.Fatalf("payloads = %q %q", sink.payloadString(1, "chunk"), sink.payloadString(2, "body"))
	}
	if entityID := sink.events[1].EntityID; entityID != "tool:run-1:call-1" {
		t.Fatalf("entity = %q", entityID)
	}
}

func TestRuntimeEventAdapterCoalescesAndFlushesDeltas(t *testing.T) {
	sink := &recordingSessionEventSink{}
	adapter := NewRuntimeEventAdapter("s1", "run-1", sink)
	consumeJSON(t, adapter, `{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"hello "}}`)
	consumeJSON(t, adapter, `{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"world"}}`)
	if len(sink.events) != 1 {
		t.Fatalf("delta flushed before boundary: %#v", sink.Types())
	}
	if err := adapter.Flush(); err != nil {
		t.Fatal(err)
	}
	if len(sink.events) != 2 {
		t.Fatalf("deltas were not coalesced: %#v", sink.Types())
	}
	if sink.payloadString(1, "delta") != "hello world" {
		t.Fatalf("delta = %q", sink.payloadString(1, "delta"))
	}
	consumeJSON(t, adapter, `{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","delta":"hmm"}}`)
	consumeJSON(t, adapter, `{"type":"tool_execution_start","toolCallId":"call-1","toolName":"bash"}`)
	types := sink.Types()
	if types[len(types)-1] != EventToolStarted || types[len(types)-2] != EventAssistantSegmentDelta {
		t.Fatalf("boundary did not flush pending: %#v", types)
	}
}

func TestRuntimeEventAdapterFlushesLargeDeltaImmediately(t *testing.T) {
	sink := &recordingSessionEventSink{}
	adapter := NewRuntimeEventAdapter("s1", "run-1", sink)
	large := strings.Repeat("x", runtimeCoalesceMaxBytes)
	payload, _ := json.Marshal(map[string]string{"type": "text_delta", "delta": large})
	consumeJSON(t, adapter, `{"type":"message_update","assistantMessageEvent":`+string(payload)+`}`)
	if len(sink.events) != 2 || sink.payloadString(1, "delta") != large {
		t.Fatalf("large delta missing: %#v", sink.Types())
	}
}

func TestRuntimeEventAdapterFlushesByClock(t *testing.T) {
	sink := &recordingSessionEventSink{}
	adapter := NewRuntimeEventAdapter("s1", "run-1", sink)
	current := time.Unix(0, 0)
	adapter.now = func() time.Time { return current }
	consumeJSON(t, adapter, `{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"a"}}`)
	current = current.Add(runtimeCoalesceMaxDelay)
	consumeJSON(t, adapter, `{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"b"}}`)
	if len(sink.events) != 2 {
		t.Fatalf("clock flush missing: %#v", sink.Types())
	}
}

func TestRuntimeEventAdapterCommitsAssistantSegments(t *testing.T) {
	sink := &recordingSessionEventSink{}
	adapter := NewRuntimeEventAdapter("s1", "run-1", sink)
	consumeJSON(t, adapter, `{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"hi"}}`)
	consumeJSON(t, adapter, `{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"hi"},{"type":"thinking","thinking":"why"}]}}`)
	types := sink.Types()
	if len(types) != 5 {
		t.Fatalf("types = %#v", types)
	}
	if types[2] != EventAssistantSegmentCommit || types[4] != EventAssistantSegmentCommit {
		t.Fatalf("commit types = %#v", types)
	}
	if sink.payloadString(2, "text") != "hi" || sink.payloadString(4, "text") != "why" {
		t.Fatalf("commit payloads = %q %q", sink.payloadString(2, "text"), sink.payloadString(4, "text"))
	}
}

func TestRuntimeEventAdapterPoisonsOnSinkFailure(t *testing.T) {
	sink := &recordingSessionEventSink{failAt: 2}
	adapter := NewRuntimeEventAdapter("s1", "run-1", sink)
	consumeJSON(t, adapter, `{"type":"tool_execution_start","toolCallId":"call-1","toolName":"bash"}`)
	if err := adapter.Consume([]byte(`{"type":"tool_execution_start","toolCallId":"call-2","toolName":"bash"}`)); err == nil {
		t.Fatal("expected sink failure")
	}
	if err := adapter.Consume([]byte(`{"type":"tool_execution_start","toolCallId":"call-3","toolName":"bash"}`)); err == nil {
		t.Fatal("expected poisoned run to reject further frames")
	}
}

func TestRuntimeEventAdapterRejectsOversizedPayload(t *testing.T) {
	sink := &recordingSessionEventSink{}
	adapter := NewRuntimeEventAdapter("s1", "run-1", sink)
	consumeJSON(t, adapter, `{"type":"tool_execution_start","toolCallId":"call-1","toolName":"bash"}`)
	err := adapter.consumeToolUpdate("call-1", "bash", json.RawMessage(`"`+strings.Repeat("y", runtimeMaxPayloadBytes+1)+`"`))
	if err == nil {
		t.Fatal("expected oversized payload rejection")
	}
}
