package piweb

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWriteSSE(t *testing.T) {
	var b strings.Builder
	event := Event{ID: 7, Type: "tool.output", SessionID: "s1", Payload: map[string]string{"chunk": "ok"}, At: time.Unix(0, 0).UTC()}
	if err := WriteSSE(&b, event); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	for _, want := range []string{"event: tool.output\n", "id: 7\n", `"sessionId":"s1"`, `"chunk":"ok"`, "\n\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in %q", want, got)
		}
	}
}

func TestBrokerReplayAndRedaction(t *testing.T) {
	broker := NewBroker()
	event := broker.Publish("s1", "tool.output", map[string]string{"chunk": "api_key=secret-value"})
	replay := broker.Replay("s1", event.ID-1)
	if len(replay) != 1 {
		t.Fatalf("expected replay event: %#v", replay)
	}
	payload := replay[0].Payload.(map[string]string)
	if strings.Contains(payload["chunk"], "secret-value") || !strings.Contains(payload["chunk"], "[REDACTED]") {
		t.Fatalf("secret was not redacted: %#v", payload)
	}
}

func TestShouldReplayHistory(t *testing.T) {
	withoutReplay := httptest.NewRequest("GET", "/events?replay=false", nil)
	if shouldReplayHistory(withoutReplay, 0) {
		t.Fatal("initial loaded sessions should not replay broker history")
	}
	if !shouldReplayHistory(withoutReplay, 7) {
		t.Fatal("EventSource reconnects should replay missed events after Last-Event-ID")
	}
	withReplay := httptest.NewRequest("GET", "/events", nil)
	if !shouldReplayHistory(withReplay, 0) {
		t.Fatal("new live sessions should replay by default")
	}
}

func TestBrokerFanoutAndUnsubscribe(t *testing.T) {
	broker := NewBroker()
	ch, unsubscribe := broker.Subscribe("s1")
	broker.Publish("s1", "session.status", map[string]string{"status": "ok"})
	select {
	case event := <-ch:
		if event.Type != "session.status" || event.SessionID != "s1" {
			t.Fatalf("unexpected event: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
	unsubscribe()
	broker.Publish("s1", "session.status", map[string]string{"status": "closed"})
	select {
	case event, ok := <-ch:
		if ok {
			t.Fatalf("received event after unsubscribe: %#v", event)
		}
	case <-time.After(10 * time.Millisecond):
		t.Fatal("subscription channel did not close")
	}
}

func TestSubscribeFromDoesNotMissEventBetweenReplayAndLive(t *testing.T) {
	broker := NewBroker()
	first := broker.Publish("s1", "session.message", map[string]string{"text": "first"})
	second := broker.Publish("s1", "session.status", map[string]string{"status": "thinking"})
	registered := make(chan struct{})
	release := make(chan struct{})
	broker.subscribeHook = func() {
		close(registered)
		<-release
	}

	subscriptionReady := make(chan EventSubscription, 1)
	go func() { subscriptionReady <- broker.SubscribeFrom("s1", first.ID) }()
	<-registered
	liveReady := make(chan Event, 1)
	go func() { liveReady <- broker.Publish("s1", "tool.started", map[string]string{"tool": "bash"}) }()
	select {
	case <-liveReady:
		t.Fatal("publish crossed the replay registration barrier")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	subscription := <-subscriptionReady
	defer subscription.Close()
	third := <-liveReady

	events := append([]Event(nil), subscription.Replay...)
	select {
	case event := <-subscription.Events:
		events = append(events, event)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for live event")
	}
	if len(events) != 2 || events[0].ID != second.ID || events[1].ID != third.ID {
		t.Fatalf("subscription missed or reordered events: %#v", events)
	}
}

func TestBrokerGapIsObservableWhenSubscriberIsSlow(t *testing.T) {
	broker := NewBroker()
	broker.buffer = 1
	after := broker.Publish("s1", "session.message", map[string]string{"text": "before"})
	subscription := broker.SubscribeFrom("s1", after.ID)
	defer subscription.Close()
	queued := broker.Publish("s1", "session.message", map[string]string{"text": "queued"})
	dropped := broker.Publish("s1", "tool.output", map[string]string{"chunk": "dropped"})
	idle := broker.Publish("s1", "session.status", map[string]string{"status": "idle"})
	errorEvent := broker.Publish("s1", "error", map[string]string{"message": "boom"})
	started := broker.Publish("s1", "tool.started", map[string]string{"tool": "bash"})
	finished := broker.Publish("s1", "tool.finished", map[string]string{"tool": "bash"})

	var events []Event
	for len(events) < 6 {
		select {
		case event := <-subscription.Events:
			events = append(events, event)
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for event %d: %#v", len(events), events)
		}
	}
	expectedTypes := []string{"session.message", "broker_gap", "session.status", "error", "tool.started", "tool.finished"}
	for index, event := range events {
		if event.Type != expectedTypes[index] {
			t.Fatalf("event %d = %#v, want type %q", index, event, expectedTypes[index])
		}
	}
	gapPayload, ok := events[1].Payload.(map[string]any)
	if !ok || gapPayload["sessionId"] != "s1" || gapPayload["after"] != after.ID || gapPayload["latestEventId"] != dropped.ID {
		t.Fatalf("unexpected broker gap payload: %#v", events[1].Payload)
	}
	if events[0].ID != queued.ID || events[2].ID != idle.ID || events[3].ID != errorEvent.ID ||
		events[4].ID != started.ID || events[5].ID != finished.ID {
		t.Fatalf("critical events were reordered or lost: %#v", events)
	}
}

func TestBrokerGapPrecedesCriticalEventWhenBufferIsFull(t *testing.T) {
	broker := NewBroker()
	broker.buffer = 1
	subscription := broker.SubscribeFrom("s1", 0)
	defer subscription.Close()
	queued := broker.Publish("s1", "tool.output", map[string]string{"chunk": "queued"})
	idle := broker.Publish("s1", "session.status", map[string]string{"status": "idle"})
	first := <-subscription.Events
	gap := <-subscription.Events
	terminal := <-subscription.Events
	if first.ID != queued.ID || gap.Type != "broker_gap" || terminal.ID != idle.ID {
		t.Fatalf("full buffer lost ordering or terminal event: %#v, %#v, %#v", first, gap, terminal)
	}
	payload := gap.Payload.(map[string]any)
	if payload["latestEventId"] != idle.ID {
		t.Fatalf("gap did not identify the overflowing event: %#v", payload)
	}
}

func TestClosingSubscriptionConcurrentlyWithPublishDoesNotPanic(t *testing.T) {
	broker := NewBroker()
	for i := 0; i < 100; i++ {
		subscription := broker.SubscribeFrom("s1", 0)
		start := make(chan struct{})
		var waitGroup sync.WaitGroup
		waitGroup.Add(2)
		go func() {
			defer waitGroup.Done()
			<-start
			broker.Publish("s1", "error", map[string]string{"message": "boom"})
		}()
		go func() {
			defer waitGroup.Done()
			<-start
			subscription.Close()
		}()
		close(start)
		waitGroup.Wait()
		broker.mu.RLock()
		remaining := len(broker.subscribers["s1"])
		broker.mu.RUnlock()
		if remaining != 0 {
			t.Fatalf("subscription remained after concurrent close: remaining=%d", remaining)
		}
		deadline := time.After(time.Second)
		for {
			select {
			case _, ok := <-subscription.Events:
				if !ok {
					goto closed
				}
			case <-deadline:
				t.Fatal("subscription channel did not terminate")
			}
		}
	closed:
	}
}

func TestClosingSubscriptionDeliversPublishThatWinsTheLock(t *testing.T) {
	broker := NewBroker()
	published := make(chan struct{})
	release := make(chan struct{})
	broker.publishHook = func() {
		close(published)
		<-release
	}
	subscription := broker.SubscribeFrom("s1", 0)
	publishDone := make(chan Event, 1)
	go func() { publishDone <- broker.Publish("s1", "error", map[string]string{"message": "boom"}) }()
	<-published
	closeDone := make(chan struct{})
	go func() {
		subscription.Close()
		close(closeDone)
	}()
	select {
	case <-closeDone:
		t.Fatal("close crossed the publish barrier")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	<-publishDone
	<-closeDone
	select {
	case event, ok := <-subscription.Events:
		if !ok || event.Type != "error" {
			t.Fatalf("publish-winning event was not visible: %#v, open=%v", event, ok)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for publish-winning event")
	}
	if _, ok := <-subscription.Events; ok {
		t.Fatal("expected legacy subscription channel to close")
	}
}

func TestServeSessionReplaysBeforeLiveAfterLastEventID(t *testing.T) {
	broker := NewBroker()
	broker.heartbeat = time.Hour
	first := broker.Publish("s1", "session.message", map[string]string{"text": "first"})
	replayed := broker.Publish("s1", "session.status", map[string]string{"status": "thinking"})
	registered := make(chan struct{})
	release := make(chan struct{})
	broker.subscribeHook = func() {
		close(registered)
		<-release
	}
	writer := newTestSSEWriter()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
	req.Header.Set("Last-Event-ID", strconv.FormatUint(first.ID, 10))
	done := make(chan struct{})
	go func() {
		broker.ServeSession(writer, req, "s1")
		close(done)
	}()
	<-writer.flushed
	<-registered
	liveReady := make(chan Event, 1)
	go func() { liveReady <- broker.Publish("s1", "session.message", map[string]string{"text": "live"}) }()
	select {
	case <-liveReady:
		t.Fatal("live publish crossed subscription barrier")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	live := <-liveReady
	<-writer.flushed
	<-writer.flushed
	body := writer.String()
	replayedIndex := strings.Index(body, fmt.Sprintf("id: %d\n", replayed.ID))
	liveIndex := strings.Index(body, fmt.Sprintf("id: %d\n", live.ID))
	if replayedIndex < 0 || liveIndex < 0 || replayedIndex > liveIndex {
		t.Fatalf("replay/live order or Last-Event-ID recovery failed: %q", body)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ServeSession did not exit after request cancellation")
	}
}

func TestWriteSSEIncludesBrokerGapRecoveryFields(t *testing.T) {
	var body strings.Builder
	event := Event{ID: 9, Type: "broker_gap", SessionID: "s1", Payload: map[string]any{
		"sessionId": "s1", "after": uint64(3), "latestEventId": uint64(8),
	}}
	if err := WriteSSE(&body, event); err != nil {
		t.Fatal(err)
	}
	got := body.String()
	for _, want := range []string{
		"event: broker_gap\n", "id: 9\n", `"sessionId":"s1"`, `"after":3`, `"latestEventId":8`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in gap SSE %q", want, got)
		}
	}
}

type testSSEWriter struct {
	header  http.Header
	mu      sync.Mutex
	body    bytes.Buffer
	flushed chan struct{}
}

func newTestSSEWriter() *testSSEWriter {
	return &testSSEWriter{header: http.Header{}, flushed: make(chan struct{}, 8)}
}

func (w *testSSEWriter) Header() http.Header { return w.header }

func (w *testSSEWriter) WriteHeader(status int) {}

func (w *testSSEWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.Write(data)
}

func (w *testSSEWriter) Flush() { w.flushed <- struct{}{} }

func (w *testSSEWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.String()
}
