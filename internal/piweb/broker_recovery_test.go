package piweb

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBrokerRecoveryUsesLatestSessionEventIDAndAfter(t *testing.T) {
	broker := NewBroker()
	first := broker.Publish("s1", "session.message", map[string]string{"text": "first"})
	second := broker.Publish("s1", "session.status", map[string]string{"status": "running"})

	if got := broker.LatestEventID("s1"); got != second.ID {
		t.Fatalf("latest session event ID = %d, want %d", got, second.ID)
	}

	subscription := broker.SubscribeFrom("s1", first.ID)
	defer subscription.Close()
	if len(subscription.Replay) != 1 || subscription.Replay[0].ID != second.ID {
		t.Fatalf("after replay = %#v, want event %d", subscription.Replay, second.ID)
	}

	third := broker.Publish("s1", "session.message", map[string]string{"text": "third"})
	select {
	case event := <-subscription.Events:
		if event.ID != third.ID {
			t.Fatalf("live recovery ID = %d, want %d", event.ID, third.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for live recovery")
	}
}

func TestActiveRunReplaySurvivesHistoryWindow(t *testing.T) {
	broker := NewBroker()
	broker.historySize = 1
	running := broker.Publish("s1", "session.status", map[string]string{"status": "running"})
	firstDelta := broker.Publish("s1", "session.delta", map[string]string{"kind": "pi", "delta": "first"})
	durableTool := broker.Publish("s1", "tool.finished", Message{ID: "tool-1", Kind: "tool", Status: "ok"})
	secondDelta := broker.Publish("s1", "session.delta", map[string]string{"kind": "pi", "delta": "second"})

	if got := broker.ActiveRunAfter("s1"); got != running.ID {
		t.Fatalf("active run cursor = %d, want %d", got, running.ID)
	}
	subscription := broker.SubscribeFromActive("s1", running.ID, durableTool.ID)
	defer subscription.Close()
	if len(subscription.Replay) != 2 || subscription.Replay[0].ID != firstDelta.ID || subscription.Replay[1].ID != secondDelta.ID {
		t.Fatalf("active replay = %#v, want deltas %d and %d", subscription.Replay, firstDelta.ID, secondDelta.ID)
	}
}

func TestThinkingStatusStartsActiveRunRecovery(t *testing.T) {
	broker := NewBroker()
	thinking := broker.Publish("s1", "session.status", map[string]string{"status": "thinking"})
	broker.Publish("s1", "session.delta", map[string]string{"kind": "pi", "delta": "draft"})

	if !broker.IsRunning("s1") || broker.ActiveRunAfter("s1") != thinking.ID {
		t.Fatalf("thinking status did not start active recovery: running=%v after=%d", broker.IsRunning("s1"), broker.ActiveRunAfter("s1"))
	}
}

func TestSubscribeFromReportsHistoryGapForSnapshotRecovery(t *testing.T) {
	broker := NewBroker()
	broker.historySize = 1
	first := broker.Publish("s1", "session.message", map[string]string{"text": "first"})
	latest := broker.Publish("s1", "session.status", map[string]string{"status": "idle"})

	subscription := broker.SubscribeFrom("s1", first.ID)
	defer subscription.Close()
	if len(subscription.Replay) != 1 || subscription.Replay[0].Type != "broker_gap" {
		t.Fatalf("history gap replay = %#v, want broker_gap", subscription.Replay)
	}
	payload := subscription.Replay[0].Payload.(map[string]any)
	if payload["after"] != strconv.FormatUint(first.ID, 10) || payload["latestEventId"] != strconv.FormatUint(latest.ID, 10) {
		t.Fatalf("history gap payload = %#v", payload)
	}
}

func TestSessionSnapshotIncludesActiveRunCursor(t *testing.T) {
	broker := NewBroker()
	server := NewServer(Config{}, NewMockStore(), broker)
	running := broker.Publish("8e7c-44ff", "session.status", map[string]string{"status": "running"})
	broker.Publish("8e7c-44ff", "session.delta", map[string]string{"kind": "pi", "delta": "draft"})

	request := httptest.NewRequest(http.MethodGet, "/api/sessions/8e7c-44ff", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("snapshot status = %d: %s", response.Code, response.Body.String())
	}
	var payload struct {
		ActiveEventCursor string `json:"activeEventCursor"`
		ActiveRun         bool   `json:"activeRun"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ActiveEventCursor != strconv.FormatUint(running.ID, 10) || !payload.ActiveRun {
		t.Fatalf("active snapshot cursor = %q active=%v, want %d/true", payload.ActiveEventCursor, payload.ActiveRun, running.ID)
	}
}

func TestSessionSnapshotIncludesLatestEventID(t *testing.T) {
	broker := NewBroker()
	server := NewServer(Config{}, NewMockStore(), broker)
	first := broker.Publish("8e7c-44ff", "session.message", map[string]string{"text": "first"})
	latest := broker.Publish("8e7c-44ff", "session.status", map[string]string{"status": "idle"})

	request := httptest.NewRequest(http.MethodGet, "/api/sessions/8e7c-44ff", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("snapshot status = %d: %s", response.Code, response.Body.String())
	}
	var payload struct {
		LatestEventID string `json:"latestEventId"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	want := strconv.FormatUint(latest.ID, 10)
	if payload.LatestEventID != want || payload.LatestEventID == strconv.FormatUint(first.ID, 10) {
		t.Fatalf("snapshot latestEventId = %q, want %q", payload.LatestEventID, want)
	}
}

func TestServeSessionRecoversFromAfterQuery(t *testing.T) {
	broker := NewBroker()
	first := broker.Publish("s1", "session.message", map[string]string{"text": "first"})
	second := broker.Publish("s1", "session.status", map[string]string{"status": "running"})
	writer := newTestSSEWriter()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/events?after="+strconv.FormatUint(first.ID, 10), nil).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		broker.ServeSession(writer, request, "s1")
		close(done)
	}()
	select {
	case <-writer.flushed:
	case <-time.After(time.Second):
		t.Fatal("ServeSession did not flush initial response")
	}
	third := broker.Publish("s1", "session.message", map[string]string{"text": "third"})
	select {
	case <-writer.flushed:
	case <-time.After(time.Second):
		t.Fatal("ServeSession did not flush recovered events")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ServeSession did not stop after cancellation")
	}
	body := writer.String()
	secondIndex := strings.Index(body, "id: "+strconv.FormatUint(second.ID, 10)+"\n")
	thirdIndex := strings.Index(body, "id: "+strconv.FormatUint(third.ID, 10)+"\n")
	if secondIndex < 0 || thirdIndex < 0 || secondIndex > thirdIndex {
		t.Fatalf("after replay/live order is wrong: %q", body)
	}
}

func TestServeSessionReplaysActiveRunAfterCursor(t *testing.T) {
	broker := NewBroker()
	broker.historySize = 1
	running := broker.Publish("s1", "session.status", map[string]string{"status": "running"})
	delta := broker.Publish("s1", "session.delta", map[string]string{"kind": "pi", "delta": "draft"})
	writer := newTestSSEWriter()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/events?replay=active&after="+strconv.FormatUint(running.ID, 10)+"&snapshot="+strconv.FormatUint(running.ID, 10), nil).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		broker.ServeSession(writer, request, "s1")
		close(done)
	}()
	select {
	case <-writer.flushed:
	case <-time.After(time.Second):
		t.Fatal("ServeSession did not flush active replay")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ServeSession did not stop after cancellation")
	}
	if !strings.Contains(writer.String(), "id: "+strconv.FormatUint(delta.ID, 10)+"\n") {
		t.Fatalf("active delta was not replayed: %q", writer.String())
	}
}

func TestSessionSnapshotDoesNotAdvanceCursorAcrossOrdinaryPublish(t *testing.T) {
	store := NewMockStore()
	broker := NewBroker()
	snapshotStarted := make(chan struct{})
	releaseSnapshot := make(chan struct{})
	publishAdmitted := make(chan struct{})
	releasePublishAdmission := make(chan struct{})
	store.sessionSnapshotHook = func() {
		close(snapshotStarted)
		<-releaseSnapshot
	}
	broker.publishAdmissionHook = func(eventType string) {
		if eventType != "session.status" {
			return
		}
		close(publishAdmitted)
		<-releasePublishAdmission
	}
	server := NewServer(Config{}, store, broker)
	request := httptest.NewRequest(http.MethodGet, "/api/sessions/8e7c-44ff", nil)
	responseDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		responseDone <- response
	}()
	<-snapshotStarted

	publishDone := make(chan Event, 1)
	go func() {
		publishDone <- broker.Publish("8e7c-44ff", "session.status", map[string]string{"status": "running"})
	}()
	<-publishAdmitted
	close(releasePublishAdmission)
	close(releaseSnapshot)
	response := <-responseDone
	if response.Code != http.StatusOK {
		t.Fatalf("snapshot status = %d: %s", response.Code, response.Body.String())
	}
	var payload struct {
		LatestEventID string `json:"latestEventId"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.LatestEventID != "0" {
		t.Fatalf("snapshot cursor advanced before concurrent publish: %q", payload.LatestEventID)
	}
	published := <-publishDone
	if events := broker.Replay("8e7c-44ff", 0); len(events) != 1 || events[0].ID != published.ID {
		t.Fatalf("concurrent event was not published after snapshot: %#v", events)
	}
}

func TestBrokerGapAfterUsesMostRecentlyForwardedEvent(t *testing.T) {
	broker := NewBroker()
	broker.buffer = 1
	sendCompleted := make(chan struct{})
	releaseSendComplete := make(chan struct{})
	sendCompletions := 0
	broker.sendCompleteHook = func() {
		sendCompletions++
		if sendCompletions != 2 {
			return
		}
		close(sendCompleted)
		<-releaseSendComplete
	}
	subscription := broker.SubscribeFrom("s1", 0)
	defer subscription.Close()

	first := broker.Publish("s1", "one", nil)
	if event := <-subscription.Events; event.ID != first.ID {
		t.Fatalf("unexpected first event: %#v", event)
	}
	second := broker.Publish("s1", "session.status", nil)
	third := broker.Publish("s1", "error", nil)
	if event := <-subscription.Events; event.ID != second.ID {
		t.Fatalf("unexpected second event: %#v", event)
	}
	if gap := <-subscription.Events; gap.Type != "broker_gap" {
		t.Fatalf("expected initial broker gap, got %#v", gap)
	}
	<-sendCompleted

	fourth := broker.Publish("s1", "four", nil)
	close(releaseSendComplete)
	if event := <-subscription.Events; event.ID != third.ID {
		t.Fatalf("unexpected third event: %#v", event)
	}
	gap := <-subscription.Events
	if gap.Type != "broker_gap" {
		t.Fatalf("expected overflow broker gap, got %#v", gap)
	}
	payload, ok := gap.Payload.(map[string]any)
	if !ok || payload["after"] != strconv.FormatUint(third.ID, 10) {
		t.Fatalf("gap after cursor = %#v, want %d (fourth=%d)", gap.Payload, third.ID, fourth.ID)
	}
}

func TestSnapshotCursorSafelyBridgesPublishBeforeSubscribe(t *testing.T) {
	store := NewMockStore()
	broker := NewBroker()
	server := NewServer(Config{}, store, broker)
	first := broker.Publish("8e7c-44ff", "session.message", map[string]string{"text": "first"})
	snapshotRequest := httptest.NewRequest(http.MethodGet, "/api/sessions/8e7c-44ff", nil)
	snapshotResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(snapshotResponse, snapshotRequest)
	var snapshot struct {
		LatestEventID string `json:"latestEventId"`
	}
	if err := json.Unmarshal(snapshotResponse.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.LatestEventID != strconv.FormatUint(first.ID, 10) {
		t.Fatalf("snapshot cursor = %q, want %d", snapshot.LatestEventID, first.ID)
	}

	subscribeStarted := make(chan struct{})
	releaseSubscribe := make(chan struct{})
	publishAdmitted := make(chan struct{})
	releasePublishAdmission := make(chan struct{})
	broker.subscribeHook = func() {
		close(subscribeStarted)
		<-releaseSubscribe
	}
	broker.publishAdmissionHook = func(eventType string) {
		if eventType != "session.status" {
			return
		}
		close(publishAdmitted)
		<-releasePublishAdmission
	}
	writer := newTestSSEWriter()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/events?after="+snapshot.LatestEventID, nil).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		broker.ServeSession(writer, request, "8e7c-44ff")
		close(done)
	}()
	<-subscribeStarted
	publishDone := make(chan Event, 1)
	go func() {
		publishDone <- broker.Publish("8e7c-44ff", "session.status", map[string]string{"status": "idle"})
	}()
	<-publishAdmitted
	close(releaseSubscribe)
	close(releasePublishAdmission)
	live := <-publishDone
	select {
	case <-writer.flushed:
	case <-time.After(time.Second):
		t.Fatal("SSE did not flush event published before subscribe completed")
	}
	cancel()
	<-done
	envelopes := parseSSEEnvelopes(writer.String())
	if len(envelopes) != 1 || envelopes[0].ID != strconv.FormatUint(live.ID, 10) {
		t.Fatalf("snapshot-to-subscribe event was lost: %#v body=%q", envelopes, writer.String())
	}
}

func TestServeSessionRejectsInvalidEventCursor(t *testing.T) {
	broker := NewBroker()
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/events?after=not-a-number", nil),
		httptest.NewRequest(http.MethodGet, "/events", nil),
	} {
		if request.Header.Get("Last-Event-ID") == "" && request.URL.Query().Get("after") == "" {
			request.Header.Set("Last-Event-ID", "-1")
		}
		response := httptest.NewRecorder()
		broker.ServeSession(response, request, "s1")
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid cursor status = %d, body=%q", response.Code, response.Body.String())
		}
	}
}

func TestAguiCompletionReconnectDoesNotReplayRenderedEvents(t *testing.T) {
	store := NewMockStore()
	broker := NewBroker()
	server := NewServer(Config{}, store, broker)
	body := `{"threadId":"8e7c-44ff","runId":"run-reconnect","messages":[{"role":"user","content":"hello"}],"forwardedProps":{"text":"hello"}}`
	aguiRequest := httptest.NewRequest(http.MethodPost, "/api/sessions/8e7c-44ff/ag-ui", bytes.NewBufferString(body))
	aguiResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(aguiResponse, aguiRequest)
	if aguiResponse.Code != http.StatusOK || !strings.Contains(aguiResponse.Body.String(), `"type":"RUN_FINISHED"`) {
		t.Fatalf("AG-UI run did not complete: status=%d body=%q", aguiResponse.Code, aguiResponse.Body.String())
	}
	latest := broker.LatestEventID("8e7c-44ff")
	writer := newTestSSEWriter()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/events?replay=false&after="+strconv.FormatUint(latest, 10), nil).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		broker.ServeSession(writer, request, "8e7c-44ff")
		close(done)
	}()
	select {
	case <-writer.flushed:
	case <-time.After(time.Second):
		t.Fatal("reconnect did not flush initial response")
	}
	if events := parseSSEEnvelopes(writer.String()); len(events) != 0 {
		t.Fatalf("AG-UI rendered events were replayed: %#v", events)
	}
	live := broker.Publish("8e7c-44ff", "session.status", map[string]string{"status": "idle"})
	select {
	case <-writer.flushed:
	case <-time.After(time.Second):
		t.Fatal("reconnect did not receive live event")
	}
	cancel()
	<-done
	events := parseSSEEnvelopes(writer.String())
	if len(events) != 1 || events[0].ID != strconv.FormatUint(live.ID, 10) || events[0].Type != "session.status" {
		t.Fatalf("unexpected live SSE envelope: %#v", events)
	}
}

type parsedSSEEnvelope struct {
	ID   string
	Type string
	Data map[string]any
}

func parseSSEEnvelopes(body string) []parsedSSEEnvelope {
	var envelopes []parsedSSEEnvelope
	for _, block := range strings.Split(body, "\n\n") {
		var envelope parsedSSEEnvelope
		var data string
		for _, line := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(line, "id: "):
				envelope.ID = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				envelope.Type = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data = strings.TrimPrefix(line, "data: ")
			}
		}
		if data == "" || json.Unmarshal([]byte(data), &envelope.Data) != nil {
			continue
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes
}

func TestServeSessionGapEnvelopeIncludesRecoveryCursor(t *testing.T) {
	broker := NewBroker()
	broker.historySize = 1
	first := broker.Publish("s1", "session.message", map[string]string{"text": "first"})
	latest := broker.Publish("s1", "session.status", map[string]string{"status": "idle"})
	writer := newTestSSEWriter()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
	request.Header.Set("Last-Event-ID", strconv.FormatUint(first.ID, 10))
	done := make(chan struct{})
	go func() {
		broker.ServeSession(writer, request, "s1")
		close(done)
	}()
	select {
	case <-writer.flushed:
	case <-time.After(time.Second):
		t.Fatal("gap response did not flush")
	}
	cancel()
	<-done
	envelopes := parseSSEEnvelopes(writer.String())
	if len(envelopes) != 1 || envelopes[0].Type != "broker_gap" {
		t.Fatalf("unexpected gap SSE envelopes: %#v body=%q", envelopes, writer.String())
	}
	payload, ok := envelopes[0].Data["payload"].(map[string]any)
	if !ok || payload["after"] != strconv.FormatUint(first.ID, 10) || payload["latestEventId"] != strconv.FormatUint(latest.ID, 10) {
		t.Fatalf("gap cursor was not preserved in envelope: %#v", envelopes[0].Data)
	}
}

func TestServeSessionReplayFalseDoesNotReplayAfterCursor(t *testing.T) {
	broker := NewBroker()
	first := broker.Publish("s1", "session.message", map[string]string{"text": "first"})
	second := broker.Publish("s1", "session.message", map[string]string{"text": "second"})
	writer := newTestSSEWriter()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/events?replay=false&after="+strconv.FormatUint(first.ID, 10), nil).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		broker.ServeSession(writer, request, "s1")
		close(done)
	}()
	select {
	case <-writer.flushed:
	case <-time.After(time.Second):
		t.Fatal("ServeSession did not flush initial response")
	}
	body := writer.String()
	if strings.Contains(body, "id: "+strconv.FormatUint(second.ID, 10)) {
		t.Fatalf("replay=false replayed an AG-UI-consumed event: %q", body)
	}
	next := broker.Publish("s1", "session.status", map[string]string{"status": "idle"})
	select {
	case <-writer.flushed:
	case <-time.After(time.Second):
		t.Fatal("ServeSession did not flush live event")
	}
	cancel()
	<-done
	if !strings.Contains(writer.String(), "id: "+strconv.FormatUint(next.ID, 10)) {
		t.Fatalf("replay=false did not preserve live event: %q", writer.String())
	}
}

func TestRuntimeEntryPoisonWaitsForSteerExecutionGate(t *testing.T) {
	runtime := newBlockingSteerRuntime()
	entry := &runtimeEntry{
		runtime:  runtime,
		manager:  &RuntimeManager{now: time.Now},
		events:   make(chan PiEvent, 1),
		stop:     make(chan struct{}),
		activity: map[string]int{},
	}
	steerDone := make(chan struct{})
	go func() {
		_, _ = entry.Send(context.Background(), RPCCommand{
			Type:    "prompt",
			Payload: []byte(`{"streamingBehavior":"steer"}`),
		})
		close(steerDone)
	}()
	select {
	case <-runtime.sendStarted:
	case <-time.After(time.Second):
		t.Fatal("steer send did not start")
	}

	poisonDone := make(chan struct{})
	go func() {
		entry.poison(context.Canceled)
		close(poisonDone)
	}()
	select {
	case <-runtime.closeStarted:
		t.Fatal("poison closed runtime during in-flight steer")
	case <-time.After(20 * time.Millisecond):
	}
	close(runtime.releaseSend)
	<-steerDone
	select {
	case <-runtime.closeStarted:
	case <-time.After(time.Second):
		t.Fatal("poison did not close runtime after steer completed")
	}
	select {
	case <-poisonDone:
	case <-time.After(time.Second):
		t.Fatal("poison did not complete")
	}
}

func TestRuntimeEntryCloseWaitsForOrdinarySend(t *testing.T) {
	runtime := newBlockingSendRuntime()
	entry := &runtimeEntry{
		runtime:  runtime,
		manager:  &RuntimeManager{now: time.Now},
		events:   make(chan PiEvent, 1),
		stop:     make(chan struct{}),
		activity: map[string]int{},
	}
	sendDone := make(chan struct{})
	go func() {
		_, _ = entry.Send(context.Background(), RPCCommand{Type: "prompt"})
		close(sendDone)
	}()
	<-runtime.sendStarted
	closeDone := make(chan struct{})
	go func() {
		_ = entry.Close(context.Background())
		close(closeDone)
	}()
	select {
	case <-runtime.closeStarted:
		t.Fatal("close crossed ordinary send")
	case <-time.After(20 * time.Millisecond):
	}
	close(runtime.releaseSend)
	<-sendDone
	select {
	case <-runtime.closeStarted:
	case <-time.After(time.Second):
		t.Fatal("close did not start after ordinary send")
	}
	<-closeDone
}

type blockingSendRuntime struct {
	fakePiRuntime
	sendStarted  chan struct{}
	releaseSend  chan struct{}
	closeStarted chan struct{}
}

func newBlockingSendRuntime() *blockingSendRuntime {
	return &blockingSendRuntime{
		fakePiRuntime: *newFakePiRuntime(),
		sendStarted:   make(chan struct{}),
		releaseSend:   make(chan struct{}),
		closeStarted:  make(chan struct{}),
	}
}

func (r *blockingSendRuntime) Send(ctx context.Context, command RPCCommand) (RPCResponse, error) {
	select {
	case <-r.sendStarted:
	default:
		close(r.sendStarted)
	}
	select {
	case <-r.releaseSend:
	case <-ctx.Done():
		return RPCResponse{}, ctx.Err()
	}
	return RPCResponse{ID: command.ID, Type: "response", Command: command.Type, Success: true}, nil
}

func (r *blockingSendRuntime) Close(ctx context.Context) error {
	select {
	case <-r.closeStarted:
	default:
		close(r.closeStarted)
	}
	return nil
}

type blockingSteerRuntime struct {
	fakePiRuntime
	sendStarted  chan struct{}
	releaseSend  chan struct{}
	closeStarted chan struct{}
}

func newBlockingSteerRuntime() *blockingSteerRuntime {
	return &blockingSteerRuntime{
		fakePiRuntime: *newFakePiRuntime(),
		sendStarted:   make(chan struct{}),
		releaseSend:   make(chan struct{}),
		closeStarted:  make(chan struct{}),
	}
}

func (r *blockingSteerRuntime) Send(ctx context.Context, command RPCCommand) (RPCResponse, error) {
	if isSteerRPCCommand(command) {
		select {
		case <-r.sendStarted:
		default:
			close(r.sendStarted)
		}
		select {
		case <-r.releaseSend:
		case <-ctx.Done():
			return RPCResponse{}, ctx.Err()
		}
	}
	return RPCResponse{ID: command.ID, Type: "response", Command: command.Type, Success: true}, nil
}

func (r *blockingSteerRuntime) Close(ctx context.Context) error {
	select {
	case <-r.closeStarted:
	default:
		close(r.closeStarted)
	}
	return nil
}
