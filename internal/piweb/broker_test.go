package piweb

import (
	"net/http/httptest"
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
	case event := <-ch:
		t.Fatalf("received event after unsubscribe: %#v", event)
	case <-time.After(10 * time.Millisecond):
	}
}

func TestSubscribeFromDoesNotMissEventBetweenReplayAndLive(t *testing.T) {
	broker := NewBroker()
	first := broker.Publish("s1", "session.message", map[string]string{"text": "first"})
	second := broker.Publish("s1", "session.status", map[string]string{"status": "thinking"})

	subscription := broker.SubscribeFrom("s1", first.ID)
	defer subscription.Close()
	third := broker.Publish("s1", "tool.started", map[string]string{"tool": "bash"})

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
	broker.Publish("s1", "session.message", map[string]string{"text": "queued"})
	latest := broker.Publish("s1", "session.status", map[string]string{"status": "idle"})

	select {
	case event := <-subscription.Events:
		if event.Type != "broker_gap" {
			t.Fatalf("expected broker_gap, got %#v", event)
		}
		payload, ok := event.Payload.(map[string]any)
		if !ok || payload["sessionId"] != "s1" || payload["after"] != after.ID || payload["latestEventId"] != latest.ID {
			t.Fatalf("unexpected broker gap payload: %#v", event.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broker gap")
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
	}
}
