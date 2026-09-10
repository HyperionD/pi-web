package piweb

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestAguiSessionRunStreamsProtocolEvents(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	store := NewMockStore()
	workspace, err := store.OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateSession(workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{EnablePiExecution: false}, store, NewBroker())
	body := `{"threadId":"` + session.ID + `","runId":"run-test","messages":[{"id":"m1","role":"user","content":"hello ag-ui"}],"forwardedProps":{"text":"hello ag-ui"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID+"/ag-ui", bytes.NewBufferString(body))
	res := httptest.NewRecorder()

	server.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	got := res.Body.String()
	for _, want := range []string{
		`"type":"RUN_STARTED"`,
		`"name":"piweb.broker-cursor"`,
		`"eventId":"`,
		`"type":"TEXT_MESSAGE_START"`,
		`"type":"TEXT_MESSAGE_CONTENT"`,
		`Mock backend received your prompt`,
		`"type":"TEXT_MESSAGE_END"`,
		`"type":"RUN_FINISHED"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in SSE body:\n%s", want, got)
		}
	}
}

func TestAguiStreamPreservesAssistantMessageBoundariesAroundTools(t *testing.T) {
	const sessionID = "8e7c-44ff"
	broker := NewBroker()
	store := NewMockStore()
	runnerState := &jsonStreamState{}

	handlePiJSONEvent(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"工具前说明"}}`, broker, store, sessionID, runnerState)
	handlePiJSONEvent(`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"工具前说明"},{"type":"toolCall","name":"bash"}]}}`, broker, store, sessionID, runnerState)
	handlePiJSONEvent(`{"type":"tool_execution_start","toolCallId":"call-1","toolName":"bash","args":{"command":"pwd"}}`, broker, store, sessionID, runnerState)
	handlePiJSONEvent(`{"type":"tool_execution_end","toolCallId":"call-1","toolName":"bash","result":"ok"}`, broker, store, sessionID, runnerState)
	handlePiJSONEvent(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"最终总结"}}`, broker, store, sessionID, runnerState)
	handlePiJSONEvent(`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"最终总结"}]}}`, broker, store, sessionID, runnerState)
	broker.Publish(sessionID, "session.status", map[string]string{"status": "idle"})

	res := httptest.NewRecorder()
	stream := newAguiEventStream(res, t.Context())
	aguiState := newAguiStreamState("thread", "run", sessionID)
	for _, event := range broker.Replay(sessionID, 0) {
		aguiState.consume(event, stream)
	}

	got := res.Body.String()
	if strings.Count(got, `"type":"TEXT_MESSAGE_START"`) != 2 ||
		strings.Count(got, `"type":"TEXT_MESSAGE_END"`) != 2 {
		t.Fatalf("expected two assistant text lifecycles:\n%s", got)
	}
	if strings.Count(got, "工具前说明") != 1 || strings.Count(got, "最终总结") != 1 {
		t.Fatalf("expected each assistant segment exactly once:\n%s", got)
	}
	messageIDs := regexp.MustCompile(`"type":"TEXT_MESSAGE_START"[^\n]*"messageId":"([^"]+)"`).FindAllStringSubmatch(got, -1)
	if len(messageIDs) != 2 || messageIDs[0][1] == messageIDs[1][1] {
		t.Fatalf("expected distinct assistant message IDs:\n%s", got)
	}
	firstEnd := strings.Index(got, `"type":"TEXT_MESSAGE_END"`)
	toolStart := strings.Index(got, `"type":"TOOL_CALL_START"`)
	secondStart := strings.LastIndex(got, `"type":"TEXT_MESSAGE_START"`)
	if firstEnd < 0 || toolStart < 0 || secondStart < 0 || !(firstEnd < toolStart && toolStart < secondStart) {
		t.Fatalf("assistant boundary must close before tool start:\n%s", got)
	}
	_, messages, err := store.Session(sessionID)
	var canonicalTexts []string
	for _, message := range messages {
		if message.Kind == "pi" && (message.Text == "工具前说明" || message.Text == "最终总结") {
			canonicalTexts = append(canonicalTexts, message.Text)
		}
	}
	if err != nil || strings.Join(canonicalTexts, "|") != "工具前说明|最终总结" {
		t.Fatalf("expected canonical messages in store: messages=%#v err=%v", messages, err)
	}
}

func TestAguiStreamPreservesWhitespaceOnlyTextDeltas(t *testing.T) {
	res := httptest.NewRecorder()
	stream := newAguiEventStream(res, t.Context())
	state := newAguiStreamState("thread", "run", "session")

	state.emitTextDelta("PR 올림: https://github.com/Epsilondelta-ai/pi-web/pull/62", stream)
	state.emitTextDelta("\n\n", stream)
	state.emitTextDelta("Checks:\n", stream)
	state.emitTextDelta("- Branch pushed\n- PR created\n- Working tree clean", stream)

	got := res.Body.String()
	if !strings.Contains(got, `"delta":"\n\n"`) {
		t.Fatalf("missing blank-line delta in SSE body:\n%s", got)
	}
	if !strings.Contains(got, `"delta":"Checks:\n"`) {
		t.Fatalf("missing line-break delta in SSE body:\n%s", got)
	}
}

func TestAguiStreamForwardsConsumedBrokerCursor(t *testing.T) {
	res := httptest.NewRecorder()
	stream := newAguiEventStream(res, t.Context())
	state := newAguiStreamState("thread", "run", "session")

	state.consume(Event{ID: 9007199254740993, Type: "session.status", SessionID: "session", Payload: map[string]string{"status": "running"}}, stream)
	got := res.Body.String()
	if !strings.Contains(got, `"name":"piweb.broker-cursor"`) || !strings.Contains(got, `"eventId":"9007199254740993"`) {
		t.Fatalf("consumed Broker cursor was not forwarded as a string custom event:\n%s", got)
	}
}

func TestAguiStreamStopsWithRunErrorOnBrokerGap(t *testing.T) {
	res := httptest.NewRecorder()
	stream := newAguiEventStream(res, t.Context())
	state := newAguiStreamState("thread", "run", "session")

	if !state.consume(Event{Type: "broker_gap", SessionID: "session", Payload: map[string]any{
		"sessionId": "session", "after": uint64(3), "latestEventId": uint64(8),
	}}, stream) {
		t.Fatal("broker gap must terminate an AG-UI stream")
	}
	if !strings.Contains(res.Body.String(), `"type":"RUN_ERROR"`) ||
		!strings.Contains(res.Body.String(), "broker gap") {
		t.Fatalf("expected observable AG-UI run error:\n%s", res.Body.String())
	}
}

func TestAguiStreamPreservesToolCallIDs(t *testing.T) {
	res := httptest.NewRecorder()
	stream := newAguiEventStream(res, t.Context())
	state := newAguiStreamState("thread", "run", "session")

	state.consume(Event{Type: "tool.started", Payload: Message{ID: "call-1", Tool: "bash"}}, stream)
	state.consume(Event{Type: "tool.output", Payload: map[string]string{"id": "call-1", "tool": "bash", "chunk": "output"}}, stream)
	state.consume(Event{Type: "tool.finished", Payload: Message{ID: "call-1", Tool: "bash", Body: "output"}}, stream)

	got := res.Body.String()
	if !strings.Contains(got, `"name":"piweb.tool-output"`) || !strings.Contains(got, `"mode":"append"`) {
		t.Fatalf("expected append mode metadata for streamed tool output:\n%s", got)
	}
	if !strings.Contains(got, `"mode":"replace"`) {
		t.Fatalf("expected replace mode metadata for final tool output:\n%s", got)
	}
	if strings.Count(got, `"toolCallId":"call-1"`) < 3 {
		t.Fatalf("expected tool call id in AG-UI events:\n%s", got)
	}
	if !strings.Contains(got, `"type":"TOOL_CALL_RESULT"`) || strings.Contains(got, `"type":"TOOL_CALL_ARGS"`) {
		t.Fatalf("tool output must use AG-UI result events:\n%s", got)
	}
}

func TestAguiStreamForwardsCumulativeToolOutputSnapshots(t *testing.T) {
	res := httptest.NewRecorder()
	stream := newAguiEventStream(res, t.Context())
	state := newAguiStreamState("thread", "run", "session")

	for _, chunk := range []string{"first output", "latest output"} {
		state.consume(Event{Type: "tool.output", Payload: map[string]any{
			"id": "call-1", "tool": "bash", "chunk": chunk, "mode": "replace",
		}}, stream)
	}

	got := res.Body.String()
	if strings.Count(got, `"name":"piweb.tool-output"`) != 2 || strings.Count(got, `"mode":"replace"`) != 2 {
		t.Fatalf("expected replace mode metadata for each cumulative snapshot:\n%s", got)
	}
	if !strings.Contains(got, `"toolCallId":"call-1"`) || !strings.Contains(got, `"content":"latest output"`) {
		t.Fatalf("expected cumulative tool output snapshot in AG-UI events:\n%s", got)
	}
}

func TestAguiSessionRunRejectsMissingText(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", t.TempDir())
	store := NewMockStore()
	workspace, err := store.OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateSession(workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{EnablePiExecution: false}, store, NewBroker())
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID+"/ag-ui", strings.NewReader(`{"threadId":"t","runId":"r","messages":[]}`))
	res := httptest.NewRecorder()

	server.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "text is required") {
		t.Fatalf("unexpected body: %s", res.Body.String())
	}
}
