package piweb

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRunnerSendsSecondPromptToExistingRuntime(t *testing.T) {
	store := runtimeTestStore()
	runtime := newFakePiRuntime()
	runtimeFactory := func(context.Context, string, string) (PiRuntime, error) { return runtime, nil }
	manager := NewRuntimeManagerWithFactory(store, runtimeFactory)
	defer manager.Shutdown(context.Background())
	runner := NewRunnerWithRuntimeManager(manager)
	broker := NewBroker()

	for index := 0; index < 2; index++ {
		if err := runner.StartPiPrompt(context.Background(), broker, store, "runtime-session", "prompt", nil, "prompt"); err != nil {
			t.Fatalf("prompt %d: %v", index+1, err)
		}
		runtime.events <- PiEvent{Type: "agent_end", Raw: json.RawMessage(`{"type":"agent_end"}`)}
		deadline := time.After(time.Second)
		for runner.IsRunning("runtime-session") {
			select {
			case <-deadline:
				t.Fatal("prompt did not finish")
			default:
				time.Sleep(time.Millisecond)
			}
		}
	}
	runtime.mu.Lock()
	sends := append([]RPCCommand(nil), runtime.sends...)
	runtime.mu.Unlock()
	if len(sends) != 2 || sends[0].Type != "prompt" || sends[1].Type != "prompt" {
		t.Fatalf("unexpected runtime commands: %+v", sends)
	}
	if sends[0].ID == "" || sends[0].ID == sends[1].ID {
		t.Fatalf("prompt commands need unique IDs: %+v", sends)
	}
}

func TestHandlePiJSONEventPublishesAssistantErrorMessage(t *testing.T) {
	broker := NewBroker()
	store := NewMockStore()
	state := &jsonStreamState{}
	errEvent := `{"type":"message_end","message":{"role":"assistant","content":[],"stopReason":"error","errorMessage":"403 {\"error\":{\"type\":\"forbidden\",\"message\":\"Request not allowed\"}}"}}`
	handlePiJSONEvent(errEvent, broker, store, "8e7c-44ff", state)
	replay := broker.Replay("8e7c-44ff", 0)
	if len(replay) != 1 || replay[0].Type != "error" || !strings.Contains(replay[0].Payload.(map[string]string)["error"], "Request not allowed") {
		t.Fatalf("expected error event with message: %#v", replay)
	}
}

func TestHandlePiJSONEventReportsModelErrorOncePerPrompt(t *testing.T) {
	broker := NewBroker()
	store := NewMockStore()
	state := &jsonStreamState{}
	errMsg := `{"role":"assistant","content":[],"stopReason":"error","errorMessage":"429 rate limit"}`
	for _, raw := range []string{
		`{"type":"message_start","message":` + errMsg + `}`,
		`{"type":"message_end","message":` + errMsg + `}`,
		`{"type":"turn_end","message":` + errMsg + `}`,
	} {
		handlePiJSONEvent(raw, broker, store, "8e7c-44ff", state)
	}
	replay := broker.Replay("8e7c-44ff", 0)
	errors := 0
	for _, event := range replay {
		if event.Type == "error" {
			errors++
		}
	}
	if errors != 1 || !strings.Contains(replay[0].Payload.(map[string]string)["error"], "429 rate limit") {
		t.Fatalf("expected exactly one error event: %#v", replay)
	}
}

func TestPiTerminalNotificationOutputIsSuppressed(t *testing.T) {
	if !isPiTerminalNotification("\x1b]777;notify;on-pi;Done after 3 turn(s). Ready for input.\x1b\\") {
		t.Fatal("expected pi terminal notification to be suppressed")
	}
	if isPiTerminalNotification("ordinary pi output") {
		t.Fatal("ordinary pi output must remain visible")
	}
}

func TestStreamPipeHandlesLargeJSONLines(t *testing.T) {
	line := strings.Repeat("x", 1024*1024+1)
	done := make(chan struct{})
	var got string

	go streamPipe(strings.NewReader(line+"\n"), func(value string) {
		got = value
	}, done)
	<-done

	if got != line {
		t.Fatalf("expected large line to pass through, got %d bytes", len(got))
	}
}

func TestHandlePiJSONEventIgnoresToolCallDeltaAsText(t *testing.T) {
	broker := NewBroker()
	store := NewMockStore()
	state := &jsonStreamState{}
	ok := handlePiJSONEvent(`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_delta","delta":"{\"command\":\"pwd\"}"}}`, broker, store, "8e7c-44ff", state)
	if !ok {
		t.Fatal("expected json event to be handled")
	}
	if replay := broker.Replay("8e7c-44ff", 0); len(replay) != 0 {
		t.Fatalf("toolcall delta should not be published as chat text: %#v", replay)
	}
}

func TestHandlePiJSONEventPreservesToolCallID(t *testing.T) {
	broker := NewBroker()
	store := NewMockStore()
	state := &jsonStreamState{}
	start := `{"type":"tool_execution_start","toolCallId":"call-1","toolName":"bash","args":{"command":"pwd"}}`
	update := `{"type":"tool_execution_update","toolCallId":"call-1","toolName":"bash","partialResult":"ok"}`
	finish := `{"type":"tool_execution_end","toolCallId":"call-1","toolName":"bash","args":{"command":"pwd"},"result":"ok"}`

	handlePiJSONEvent(start, broker, store, "8e7c-44ff", state)
	handlePiJSONEvent(update, broker, store, "8e7c-44ff", state)
	handlePiJSONEvent(finish, broker, store, "8e7c-44ff", state)

	events := broker.Replay("8e7c-44ff", 0)
	if len(events) != 3 {
		t.Fatalf("expected start, output, and finish events: %#v", events)
	}
	if got := events[0].Payload.(Message).ID; got != "call-1" {
		t.Fatalf("expected tool start id, got %q", got)
	}
	if got := events[1].Payload.(map[string]any)["id"]; got != "call-1" {
		t.Fatalf("expected tool output id, got %q", got)
	}
	if got := events[1].Payload.(map[string]any)["mode"]; got != "replace" {
		t.Fatalf("expected cumulative tool output snapshot mode, got %q", got)
	}
	if got := events[2].Payload.(Message).ID; got != "call-1" {
		t.Fatalf("expected tool finish id, got %q", got)
	}
}

func TestHandlePiJSONEventRendersStructuredToolResultText(t *testing.T) {
	broker := NewBroker()
	store := NewMockStore()
	state := &jsonStreamState{}
	handlePiJSONEvent(`{"type":"tool_execution_start","toolCallId":"call-1","toolName":"bash","args":{"command":"ls"}}`, broker, store, "8e7c-44ff", state)
	handlePiJSONEvent(`{"type":"tool_execution_update","toolCallId":"call-1","toolName":"bash","partialResult":{"content":[{"type":"text","text":"partial output"}],"details":{"truncation":null}}}`, broker, store, "8e7c-44ff", state)
	handlePiJSONEvent(`{"type":"tool_execution_end","toolCallId":"call-1","toolName":"bash","result":{"content":[{"type":"text","text":"total output"}],"details":{}}}`, broker, store, "8e7c-44ff", state)

	events := broker.Replay("8e7c-44ff", 0)
	if len(events) != 3 {
		t.Fatalf("unexpected tool events: %#v", events)
	}
	if got := events[1].Payload.(map[string]any)["chunk"]; got != "partial output" {
		t.Fatalf("unexpected partial output: %q", got)
	}
	if got := events[1].Payload.(map[string]any)["mode"]; got != "replace" {
		t.Fatalf("expected structured partial output snapshot mode, got %q", got)
	}
	if got := events[2].Payload.(Message).Body; got != "total output" {
		t.Fatalf("unexpected final output: %q", got)
	}
}

func TestHandlePiJSONEventDoesNotDuplicateFinishedToolResult(t *testing.T) {
	broker := NewBroker()
	store := NewMockStore()
	state := &jsonStreamState{}
	start := `{"type":"tool_execution_start","toolCallId":"call-1","toolName":"bash","args":{"command":"pwd"}}`
	finish := `{"type":"tool_execution_end","toolCallId":"call-1","toolName":"bash","args":{"command":"pwd"},"result":"ok"}`
	messageEnd := `{"type":"message_end","message":{"role":"toolResult","toolCallId":"call-1","toolName":"bash","content":"ok"}}`

	handlePiJSONEvent(start, broker, store, "8e7c-44ff", state)
	handlePiJSONEvent(finish, broker, store, "8e7c-44ff", state)
	handlePiJSONEvent(messageEnd, broker, store, "8e7c-44ff", state)

	if events := broker.Replay("8e7c-44ff", 0); len(events) != 2 {
		t.Fatalf("expected no duplicate tool events: %#v", events)
	}
}

func TestHandlePiJSONEventDoesNotPublishUnpersistedFinishedTool(t *testing.T) {
	broker := NewBroker()
	store := NewMockStore()
	state := &jsonStreamState{}

	handlePiJSONEvent(`{"type":"tool_execution_end","toolCallId":"call-1","toolName":"bash","result":"orphan"}`, broker, store, "missing", state)

	events := broker.Replay("missing", 0)
	if len(events) != 1 || events[0].Type != "error" {
		t.Fatalf("expected persistence error without finished tool: %#v", events)
	}
	if state.toolWasFinished(Message{ID: "call-1", Tool: "bash"}) {
		t.Fatal("unpersisted tool result must remain eligible for message_end recovery")
	}
}

func TestHandlePiJSONEventKeepsUnmatchedSameNameToolResult(t *testing.T) {
	broker := NewBroker()
	store := NewMockStore()
	state := &jsonStreamState{}
	handlePiJSONEvent(`{"type":"tool_execution_end","toolCallId":"call-1","toolName":"bash","result":"first"}`, broker, store, "8e7c-44ff", state)
	handlePiJSONEvent(`{"type":"message_end","message":{"role":"toolResult","toolCallId":"call-2","toolName":"bash","content":"second"}}`, broker, store, "8e7c-44ff", state)

	if events := broker.Replay("8e7c-44ff", 0); len(events) != 2 {
		t.Fatalf("expected unmatched same-name result to be preserved: %#v", events)
	}
}

func TestHandlePiJSONEventStreamsTextDelta(t *testing.T) {
	broker := NewBroker()
	store := NewMockStore()
	state := &jsonStreamState{}
	handlePiJSONEvent(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"hello"}}`, broker, store, "8e7c-44ff", state)
	replay := broker.Replay("8e7c-44ff", 0)
	if len(replay) != 1 || replay[0].Type != "session.delta" {
		t.Fatalf("expected text delta event: %#v", replay)
	}
}

func TestHandlePiJSONEventPublishesCanonicalMessageAfterTextDelta(t *testing.T) {
	broker := NewBroker()
	store := NewMockStore()
	state := &jsonStreamState{}

	handlePiJSONEvent(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"draft"}}`, broker, store, "8e7c-44ff", state)
	handlePiJSONEvent(`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"draft"}]}}`, broker, store, "8e7c-44ff", state)

	events := broker.Replay("8e7c-44ff", 0)
	if len(events) != 2 || events[0].Type != "session.delta" || events[1].Type != "session.message" {
		t.Fatalf("expected delta followed by canonical message: %#v", events)
	}
	if got := events[1].Payload.(Message).Text; got != "draft" {
		t.Fatalf("unexpected canonical text: %q", got)
	}
}

func TestHandlePiJSONEventDoesNotPublishUnpersistedCanonicalMessage(t *testing.T) {
	broker := NewBroker()
	store := NewMockStore()
	state := &jsonStreamState{}

	handlePiJSONEvent(`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"orphan"}]}}`, broker, store, "missing", state)

	events := broker.Replay("missing", 0)
	if len(events) != 1 || events[0].Type != "error" {
		t.Fatalf("expected persistence error without canonical message: %#v", events)
	}
}

func TestHandlePiJSONEventPublishesFinalFallbackChoiceAfterStreaming(t *testing.T) {
	broker := NewBroker()
	store := NewMockStore()
	state := &jsonStreamState{}
	choice := "```json\n{\"type\":\"piweb_choice\",\"id\":\"test\",\"question\":\"Pick?\",\"options\":[{\"label\":\"A\",\"value\":\"a\"}],\"allowCustom\":false}\n```"
	encoded, _ := json.Marshal(choice)
	handlePiJSONEvent(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":`+string(encoded)+`}}`, broker, store, "8e7c-44ff", state)
	handlePiJSONEvent(`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":`+string(encoded)+`}]}}`, broker, store, "8e7c-44ff", state)
	replay := broker.Replay("8e7c-44ff", 0)
	if len(replay) != 2 || replay[0].Type != "session.delta" || replay[1].Type != "session.message" {
		t.Fatalf("expected final fallback choice message after delta: %#v", replay)
	}
}

func TestHandlePiJSONEventSkipsFinalToolCallPlaceholders(t *testing.T) {
	broker := NewBroker()
	store := NewMockStore()
	state := &jsonStreamState{}
	handlePiJSONEvent(`{"type":"message_end","message":{"role":"assistant","content":[{"type":"toolCall","name":"bash","arguments":{"command":"pwd"}}]}}`, broker, store, "8e7c-44ff", state)
	if replay := broker.Replay("8e7c-44ff", 0); len(replay) != 0 {
		t.Fatalf("assistant tool call placeholders should not be replayed as running tools: %#v", replay)
	}
	_, messages, _ := store.Session("8e7c-44ff")
	if len(messages) != 5 {
		t.Fatalf("assistant tool call placeholders should not be stored: %#v", messages)
	}
}
