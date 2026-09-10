package piweb

import (
	"net/http/httptest"
	"strings"
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

func TestBrokerActiveRunReplay(t *testing.T) {
	broker := NewBroker()
	broker.Publish("s1", "session.message", Message{Kind: "pi", Text: "previous"})
	broker.Publish("s1", "session.status", map[string]string{"status": "idle"})
	broker.Publish("s1", "session.status", map[string]string{"status": "running"})
	current := broker.Publish("s1", "session.delta", map[string]string{"kind": "pi", "delta": "current"})

	for index := 0; index < broker.historySize+1; index++ {
		broker.Publish("s1", "session.delta", map[string]string{"kind": "pi", "delta": "more"})
	}

	replay := broker.ReplayActiveRun("s1")
	if len(replay) != broker.historySize+3 || replay[0].Type != "session.status" || replay[1].ID != current.ID {
		t.Fatalf("expected complete current run events, got %d events: %#v", len(replay), replay[:min(len(replay), 2)])
	}
}

func TestBrokerActiveReplaySkipsCompletedRun(t *testing.T) {
	broker := NewBroker()
	broker.Publish("s1", "session.status", map[string]string{"status": "running"})
	broker.Publish("s1", "session.status", map[string]string{"status": "idle"})

	_, unsubscribe, replay := broker.subscribeWithReplay("s1", 0, true, true)
	defer unsubscribe()
	if len(replay) != 0 {
		t.Fatalf("expected no completed run replay, got %#v", replay)
	}
}

func TestBrokerActiveReplaySubscribesBeforeReplaying(t *testing.T) {
	broker := NewBroker()
	broker.Publish("s1", "session.status", map[string]string{"status": "running"})
	first := broker.Publish("s1", "session.delta", map[string]string{"kind": "pi", "delta": "first"})

	events, unsubscribe, replay := broker.subscribeWithReplay("s1", 0, true, true)
	defer unsubscribe()
	second := broker.Publish("s1", "session.delta", map[string]string{"kind": "pi", "delta": "second"})

	if len(replay) != 2 || replay[1].ID != first.ID {
		t.Fatalf("unexpected replay: %#v", replay)
	}
	select {
	case event := <-events:
		if event.ID != second.ID {
			t.Fatalf("expected new event after replay, got %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event after replay")
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
	_, ok := <-ch
	if ok {
		t.Fatal("expected subscription channel to close")
	}
}
