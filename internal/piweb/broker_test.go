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
	case <-time.After(time.Second):
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

func TestSubscribeFromAfterShutdownReturnsClosedSubscription(t *testing.T) {
	broker := NewBroker()
	if err := broker.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	subscription := broker.SubscribeFrom("s1", 0)
	defer subscription.Close()
	select {
	case _, ok := <-subscription.Events:
		if ok {
			t.Fatal("closed broker registered a live subscription")
		}
	case <-time.After(time.Second):
		t.Fatal("late subscription did not close immediately")
	}
	if len(subscription.Replay) != 0 {
		t.Fatalf("late subscription replayed events: %#v", subscription.Replay)
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
	if gap.ID != 0 {
		t.Fatalf("broker gap must not advance the event cursor: %#v", gap)
	}
	payload := gap.Payload.(map[string]any)
	if payload["latestEventId"] != idle.ID {
		t.Fatalf("gap did not identify the overflowing event: %#v", payload)
	}
}

func TestAutomaticOverflowDeliversGapBeforeTermination(t *testing.T) {
	broker := NewBroker()
	broker.buffer = 1
	broker.maxBacklog = 2
	broker.overflowDrainTimeout = 100 * time.Millisecond
	sendStarted := make(chan struct{})
	var sendOnce sync.Once
	broker.sendHook = func() { sendOnce.Do(func() { close(sendStarted) }) }
	subscription := broker.SubscribeFrom("s1", 0)
	defer subscription.Close()
	first := broker.Publish("s1", "one", nil)
	broker.Publish("s1", "two", nil)
	<-sendStarted
	broker.Publish("s1", "session.status", nil)
	broker.Publish("s1", "error", nil)
	overflow := broker.Publish("s1", "tool.output", nil)

	if event := <-subscription.Events; event.ID != first.ID {
		t.Fatalf("unexpected first event: %#v", event)
	}
	var events []Event
	deadline := time.After(time.Second)
	for {
		select {
		case event, ok := <-subscription.Events:
			if !ok {
				goto terminated
			}
			events = append(events, event)
		case <-deadline:
			t.Fatal("subscription did not terminate after broker gap")
		}
	}
terminated:
	if len(events) == 0 {
		t.Fatalf("subscription terminated without a gap: %#v", events)
	}
	gapIndex := -1
	for index, event := range events {
		payload, ok := event.Payload.(map[string]any)
		if event.Type == "broker_gap" && ok && payload["latestEventId"] == overflow.ID {
			gapIndex = index
			break
		}
	}
	if gapIndex < 0 {
		t.Fatalf("gap did not expose the overflow event: %#v", events)
	}
	for _, event := range events[gapIndex+1:] {
		if isCriticalBrokerEvent(event.Type) {
			return
		}
	}
	t.Fatalf("termination gap was not followed by a promised critical event: %#v", events)
}

func TestNonConsumingOverflowForwarderStopsAtFiniteDeadline(t *testing.T) {
	broker := NewBroker()
	broker.buffer = 1
	broker.maxBacklog = 2
	broker.overflowDrainTimeout = 50 * time.Millisecond
	sendStarted := make(chan struct{})
	var sendOnce sync.Once
	broker.sendHook = func() { sendOnce.Do(func() { close(sendStarted) }) }
	subscription := broker.SubscribeFrom("s1", 0)
	broker.Publish("s1", "one", nil)
	broker.Publish("s1", "two", nil)
	<-sendStarted
	broker.mu.RLock()
	var finished <-chan struct{}
	for subscriber := range broker.subscribers["s1"] {
		finished = subscriber.finished
		break
	}
	broker.mu.RUnlock()
	if finished == nil {
		t.Fatal("subscription was removed before overflow")
	}
	broker.Publish("s1", "session.status", nil)
	broker.Publish("s1", "error", nil)
	broker.Publish("s1", "tool.started", nil)

	broker.mu.RLock()
	var remaining int
	for range broker.subscribers["s1"] {
		remaining++
	}
	broker.mu.RUnlock()
	if remaining != 0 {
		t.Fatalf("overflow left subscriber in registry: %d", remaining)
	}
	select {
	case <-finished:
		t.Fatal("non-consuming forwarder stopped before its drain deadline")
	case <-time.After(10 * time.Millisecond):
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("non-consuming forwarder did not stop at its finite deadline")
	}
	subscription.Close()
}

func TestBacklogLimitCancelsAndRemovesBlockedForwarder(t *testing.T) {
	broker := NewBroker()
	broker.buffer = 1
	broker.maxBacklog = 2
	broker.overflowDrainTimeout = 50 * time.Millisecond
	sendStarted := make(chan struct{})
	var sendOnce sync.Once
	broker.sendHook = func() { sendOnce.Do(func() { close(sendStarted) }) }
	subscription := broker.SubscribeFrom("s1", 0)
	defer subscription.Close()
	broker.Publish("s1", "one", nil)
	broker.Publish("s1", "two", nil)
	<-sendStarted

	broker.mu.RLock()
	var finished <-chan struct{}
	for subscriber := range broker.subscribers["s1"] {
		finished = subscriber.finished
		break
	}
	broker.mu.RUnlock()
	if finished == nil {
		t.Fatal("subscription was removed before backlog overflow")
	}

	broker.Publish("s1", "session.status", nil)
	broker.Publish("s1", "error", nil)
	broker.Publish("s1", "tool.output", nil)

	broker.mu.RLock()
	remaining := len(broker.subscribers["s1"])
	broker.mu.RUnlock()
	if remaining != 0 {
		t.Fatalf("automatic termination left subscriber in registry: %d", remaining)
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("forwarder did not exit after automatic backlog termination")
	}
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-subscription.Events:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("subscription channel did not close after automatic termination")
		}
	}
}

func TestClosingFullSubscriptionCancelsBlockedForwarder(t *testing.T) {
	broker := NewBroker()
	broker.buffer = 1
	sendAttempted := make(chan struct{})
	broker.sendHook = func() { close(sendAttempted) }
	subscription := broker.SubscribeFrom("s1", 0)
	broker.Publish("s1", "one", nil)
	broker.Publish("s1", "two", nil)
	<-sendAttempted

	broker.mu.RLock()
	var finished <-chan struct{}
	for subscriber := range broker.subscribers["s1"] {
		finished = subscriber.finished
		break
	}
	broker.mu.RUnlock()
	if finished == nil {
		t.Fatal("subscription was removed before Close")
	}

	closed := make(chan struct{})
	go func() {
		subscription.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close blocked with a full channel and no consumer")
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("forwarder did not exit after Close")
	}
}

func TestGapPayloadIsImmutableAfterExposure(t *testing.T) {
	broker := NewBroker()
	broker.buffer = 1
	sendCompleted := make(chan struct{})
	releaseStateCommit := make(chan struct{})
	var sendOnce sync.Once
	broker.sendCompleteHook = func() {
		sendOnce.Do(func() {
			close(sendCompleted)
			<-releaseStateCommit
		})
	}
	subscription := broker.SubscribeFrom("s1", 0)
	defer subscription.Close()
	seed := broker.Publish("s1", "seed", nil)
	dropped := broker.Publish("s1", "dropped", nil)
	if event := <-subscription.Events; event.ID != seed.ID {
		t.Fatalf("unexpected seed event: %#v", event)
	}
	<-sendCompleted
	latest := broker.Publish("s1", "latest", nil)
	close(releaseStateCommit)
	gap := <-subscription.Events
	if gap.Type != "broker_gap" {
		t.Fatalf("expected broker gap, got %#v", gap)
	}
	payload, ok := gap.Payload.(map[string]any)
	if !ok || payload["latestEventId"] != dropped.ID {
		t.Fatalf("exposed gap payload was mutated: payload=%#v dropped=%d latest=%d", gap.Payload, dropped.ID, latest.ID)
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

func TestBrokerForwarderKeepsInFlightEventBeforeNewDirectSend(t *testing.T) {
	broker := NewBroker()
	broker.buffer = 6
	sendStarted := make(chan struct{})
	releaseSend := make(chan struct{})
	var sendOnce sync.Once
	broker.sendHook = func() {
		sendOnce.Do(func() {
			close(sendStarted)
			<-releaseSend
		})
	}
	subscription := broker.SubscribeFrom("s1", 0)
	defer subscription.Close()
	first := broker.Publish("s1", "one", nil)
	second := broker.Publish("s1", "two", nil)
	third := broker.Publish("s1", "three", nil)
	fourth := broker.Publish("s1", "four", nil)
	fifth := broker.Publish("s1", "five", nil)
	sixth := broker.Publish("s1", "six", nil)
	seventh := broker.Publish("s1", "session.status", nil)
	for _, want := range []uint64{first.ID, second.ID, third.ID, fourth.ID, fifth.ID, sixth.ID} {
		if event := <-subscription.Events; event.ID != want {
			t.Fatalf("expected buffered event before forwarder backlog: %#v", event)
		}
	}
	<-sendStarted
	eighth := broker.Publish("s1", "eight", nil)
	close(releaseSend)
	wantTypes := []string{"broker_gap", "session.status", "eight"}
	for index, wantType := range wantTypes {
		select {
		case event := <-subscription.Events:
			if event.Type != wantType {
				t.Fatalf("event %d bypassed in-flight event: got=%#v want type=%s", index, event, wantType)
			}
			if index == 1 && event.ID != seventh.ID || index == 2 && event.ID != eighth.ID {
				t.Fatalf("event %d had wrong ID: got=%d", index, event.ID)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for event %d", index)
		}
	}
}

func TestBrokerCriticalBacklogIsBoundedAndGapIsVisible(t *testing.T) {
	broker := NewBroker()
	broker.buffer = 1
	broker.maxBacklog = 3
	sendStarted := make(chan struct{})
	releaseSend := make(chan struct{})
	var sendOnce sync.Once
	broker.sendHook = func() {
		sendOnce.Do(func() {
			close(sendStarted)
			<-releaseSend
		})
	}
	subscription := broker.SubscribeFrom("s1", 0)
	defer subscription.Close()
	seed := broker.Publish("s1", "seed", nil)
	broker.Publish("s1", "session.status", map[string]int{"index": 0})
	<-sendStarted
	var overflow Event
	for index := 1; index < 4; index++ {
		overflow = broker.Publish("s1", "session.status", map[string]int{"index": index})
	}
	broker.mu.RLock()
	pending := 0
	for subscriber := range broker.subscribers["s1"] {
		pending = len(subscriber.events) + len(subscriber.queue)
		if subscriber.sending {
			pending++
		}
		break
	}
	broker.mu.RUnlock()
	limit := broker.maxBacklog + broker.buffer + 1
	if pending > limit {
		t.Fatalf("critical backlog exceeded limit: pending=%d limit=%d", pending, limit)
	}
	if event := <-subscription.Events; event.ID != seed.ID {
		t.Fatalf("unexpected seed event: %#v", event)
	}
	close(releaseSend)
	var gap Event
	deadline := time.After(time.Second)
	for gap.Type == "" {
		select {
		case event := <-subscription.Events:
			if event.Type == "broker_gap" {
				payload, _ := event.Payload.(map[string]any)
				if payload["latestEventId"] == overflow.ID {
					gap = event
				}
			}
		case <-deadline:
			t.Fatalf("timed out waiting for bounded backlog recovery: gap=%#v overflow=%d", gap, overflow.ID)
		}
	}
	payload, ok := gap.Payload.(map[string]any)
	if !ok || payload["latestEventId"] != overflow.ID {
		t.Fatalf("gap did not expose the bounded overflow: %#v overflow=%d", gap.Payload, overflow.ID)
	}
}

func TestClosingSubscriptionCancelsQueuedAndInFlightEvents(t *testing.T) {
	broker := NewBroker()
	broker.buffer = 1
	sendStarted := make(chan struct{})
	releaseSend := make(chan struct{})
	var sendOnce sync.Once
	broker.sendHook = func() {
		sendOnce.Do(func() {
			close(sendStarted)
			<-releaseSend
		})
	}
	subscription := broker.SubscribeFrom("s1", 0)
	first := broker.Publish("s1", "one", nil)
	broker.Publish("s1", "session.status", nil)
	if event := <-subscription.Events; event.ID != first.ID {
		t.Fatalf("unexpected first event: %#v", event)
	}
	<-sendStarted
	closeDone := make(chan struct{})
	go func() {
		subscription.Close()
		close(closeDone)
	}()
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("Close blocked on an in-flight send")
	}
	close(releaseSend)
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-subscription.Events:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("subscription channel did not close after cancellation")
		}
	}
}

func TestConcurrentPublishIDsFollowHistoryAndLiveOrder(t *testing.T) {
	broker := NewBroker()
	blockedAdmission := make(chan struct{})
	secondPublished := make(chan struct{})
	releaseBlocked := make(chan struct{})
	releaseSecond := make(chan struct{})
	broker.publishAdmissionHook = func(eventType string) {
		if eventType != "blocked" {
			return
		}
		close(blockedAdmission)
		<-secondPublished
		<-releaseBlocked
	}
	broker.publishHook = func() {
		if len(broker.history["s1"]) == 0 || broker.history["s1"][len(broker.history["s1"])-1].Type != "second" {
			return
		}
		close(secondPublished)
		<-releaseSecond
	}
	subscription := broker.SubscribeFrom("s1", 0)
	defer subscription.Close()
	firstDone := make(chan Event, 1)
	go func() { firstDone <- broker.Publish("s1", "blocked", nil) }()
	<-blockedAdmission
	secondDone := make(chan Event, 1)
	go func() { secondDone <- broker.Publish("s1", "second", nil) }()
	<-secondPublished
	close(releaseBlocked)
	close(releaseSecond)
	second := <-secondDone
	first := <-firstDone
	history := broker.Replay("s1", 0)
	if len(history) != 2 || history[0].ID != second.ID || history[1].ID != first.ID || history[0].ID >= history[1].ID {
		t.Fatalf("history IDs do not follow lock order: history=%#v second=%#v first=%#v", history, second, first)
	}
	for index, want := range []uint64{second.ID, first.ID} {
		select {
		case event := <-subscription.Events:
			if event.ID != want {
				t.Fatalf("live ID %d out of order: got=%d want=%d", index, event.ID, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for live event %d", index)
		}
	}
}

func TestHeartbeatDoesNotAdvanceLastEventIDCursor(t *testing.T) {
	broker := NewBroker()
	broker.heartbeat = 10 * time.Millisecond
	first := broker.Publish("s1", "session.message", nil)
	writer := newTestSSEWriter()
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/events?replay=false", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		broker.ServeSession(writer, req, "s1")
		close(done)
	}()
	select {
	case <-writer.flushed:
	case <-time.After(time.Second):
		t.Fatal("ServeSession did not flush its initial response")
	}
	select {
	case <-writer.flushed:
	case <-time.After(time.Second):
		t.Fatal("ServeSession did not flush a heartbeat")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ServeSession did not stop after heartbeat test cancellation")
	}
	next := broker.Publish("s1", "session.status", nil)
	if next.ID != first.ID+1 {
		t.Fatalf("heartbeat advanced broker cursor: first=%d next=%d", first.ID, next.ID)
	}
	body := writer.String()
	heartbeatStart := strings.Index(body, "event: heartbeat\n")
	if heartbeatStart < 0 {
		t.Fatalf("heartbeat was not written: %q", body)
	}
	heartbeatBodyStart := heartbeatStart + len("event: heartbeat\n")
	if heartbeatBodyStart > len(body) {
		t.Fatalf("heartbeat start exceeded body length: %q", body)
	}
	heartbeatEnd := strings.Index(body[heartbeatBodyStart:], "\n\n")
	if heartbeatEnd < 0 {
		t.Fatalf("heartbeat event was incomplete: %q", body)
	}
	heartbeatBodyEnd := heartbeatBodyStart + heartbeatEnd
	if heartbeatBodyEnd > len(body) {
		t.Fatalf("heartbeat end exceeded body length: %q", body)
	}
	if strings.Contains(body[heartbeatBodyStart:heartbeatBodyEnd], "id:") {
		t.Fatalf("heartbeat must not advertise a Last-Event-ID cursor: %q", body)
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
	event := Event{ID: 0, Type: "broker_gap", SessionID: "s1", Payload: map[string]any{
		"sessionId": "s1", "after": uint64(3), "latestEventId": uint64(8),
	}}
	if err := WriteSSE(&body, event); err != nil {
		t.Fatal(err)
	}
	got := body.String()
	for _, want := range []string{
		"event: broker_gap\n", `"sessionId":"s1"`, `"after":3`, `"latestEventId":8`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in gap SSE %q", want, got)
		}
	}
	if strings.Contains(got, "id:") {
		t.Fatalf("broker gap must not advertise a Last-Event-ID cursor: %q", got)
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
