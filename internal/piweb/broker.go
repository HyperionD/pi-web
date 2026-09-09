package piweb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const defaultBrokerBacklog = 256

type Broker struct {
	mu          sync.RWMutex
	subscribers map[string]map[*brokerSubscriber]struct{}
	nextID      uint64
	buffer      int
	maxBacklog  int
	heartbeat   time.Duration
	history     map[string][]Event
	historySize int
	// Hooks are test barriers. publishAdmissionHook runs before mu is acquired;
	// the others run at their corresponding point in the locked/send pipeline.
	subscribeHook        func()
	publishAdmissionHook func(eventType string)
	publishHook          func()
	sendHook             func()
	sendCompleteHook     func()
}

type brokerSubscriber struct {
	sessionID    string
	events       chan Event
	after        uint64
	capacity     int
	backlogLimit int
	queue        []Event
	gapInFlight  bool
	wake         chan struct{}
	finished     chan struct{}
	ctx          context.Context
	cancel       context.CancelFunc
	sending      bool
	sendingEvent *Event
	closed       bool
}

func NewBroker() *Broker {
	return &Broker{
		subscribers: map[string]map[*brokerSubscriber]struct{}{},
		history:     map[string][]Event{},
		buffer:      32,
		heartbeat:   15 * time.Second,
		historySize: 256,
		maxBacklog:  defaultBrokerBacklog,
	}
}

func (b *Broker) Subscribe(sessionID string) (<-chan Event, func()) {
	subscription := b.SubscribeFrom(sessionID, 0)
	return subscription.Events, subscription.Close
}

func (b *Broker) SubscribeFrom(sessionID string, after uint64) EventSubscription {
	buffer := b.buffer
	if buffer < 1 {
		buffer = 1
	}
	backlogLimit := b.maxBacklog
	if backlogLimit < 2 {
		backlogLimit = 2
	}
	ctx, cancel := context.WithCancel(context.Background())
	subscriber := &brokerSubscriber{
		sessionID: sessionID, events: make(chan Event, buffer), after: after, capacity: buffer, backlogLimit: backlogLimit,
		wake: make(chan struct{}, 1), finished: make(chan struct{}), ctx: ctx, cancel: cancel,
	}
	b.mu.Lock()
	if b.subscribers[sessionID] == nil {
		b.subscribers[sessionID] = map[*brokerSubscriber]struct{}{}
	}
	b.subscribers[sessionID][subscriber] = struct{}{}
	replay := b.replayLocked(sessionID, after)
	if b.subscribeHook != nil {
		b.subscribeHook()
	}
	b.mu.Unlock()
	go b.forwardSubscriber(subscriber)

	var closeOnce sync.Once
	return EventSubscription{
		Events: subscriber.events,
		Replay: replay,
		Close: func() {
			closeOnce.Do(func() { b.removeSubscriber(sessionID, subscriber) })
		},
	}
}

func (b *Broker) removeSubscriber(sessionID string, subscriber *brokerSubscriber) {
	b.mu.Lock()
	b.terminateSubscriberLocked(sessionID, subscriber)
	b.mu.Unlock()
}

func (b *Broker) terminateSubscriberLocked(sessionID string, subscriber *brokerSubscriber) {
	if subscribers := b.subscribers[sessionID]; subscribers != nil {
		delete(subscribers, subscriber)
		if len(subscribers) == 0 {
			delete(b.subscribers, sessionID)
		}
	}
	subscriber.closed = true
	subscriber.cancel()
	b.signalSubscriberLocked(subscriber)
}

func (b *Broker) forwardSubscriber(subscriber *brokerSubscriber) {
	defer close(subscriber.events)
	defer close(subscriber.finished)
	for {
		b.mu.Lock()
		if len(subscriber.queue) == 0 {
			if subscriber.closed {
				b.mu.Unlock()
				return
			}
			wake := subscriber.wake
			ctx := subscriber.ctx
			b.mu.Unlock()
			select {
			case <-wake:
			case <-ctx.Done():
				return
			}
			continue
		}
		event := subscriber.queue[0]
		subscriber.queue = subscriber.queue[1:]
		subscriber.sending = true
		if event.Type == "broker_gap" {
			subscriber.gapInFlight = true
		}
		subscriber.sendingEvent = &event
		sendHook := b.sendHook
		sendCompleteHook := b.sendCompleteHook
		b.mu.Unlock()
		if sendHook != nil {
			sendHook()
		}
		select {
		case subscriber.events <- event:
		case <-subscriber.ctx.Done():
			return
		}
		if sendCompleteHook != nil {
			sendCompleteHook()
		}
		b.mu.Lock()
		subscriber.sending = false
		subscriber.sendingEvent = nil
		if event.Type != "broker_gap" {
			subscriber.gapInFlight = false
		}
		b.mu.Unlock()
	}
}

func (b *Broker) enqueueLocked(subscriber *brokerSubscriber, event Event) {
	if subscriber.closed {
		return
	}
	if len(subscriber.queue) == 0 && !subscriber.sending {
		select {
		case subscriber.events <- event:
			return
		default:
		}
	}
	if isCriticalBrokerEvent(event.Type) {
		if b.pendingLocked(subscriber) >= subscriber.capacity && !b.activeGapLocked(subscriber) {
			b.recordGapLocked(subscriber, event.SessionID, event.ID)
		}
		b.enqueueCriticalLocked(subscriber, event)
		return
	}
	if b.pendingLocked(subscriber) >= subscriber.capacity || len(subscriber.queue) >= subscriber.backlogLimit {
		b.recordGapLocked(subscriber, event.SessionID, event.ID)
		return
	}
	subscriber.queue = append(subscriber.queue, event)
	b.signalSubscriberLocked(subscriber)
}

func (b *Broker) pendingLocked(subscriber *brokerSubscriber) int {
	pending := len(subscriber.events) + len(subscriber.queue)
	if subscriber.sending {
		pending++
	}
	return pending
}

func (b *Broker) enqueueCriticalLocked(subscriber *brokerSubscriber, event Event) {
	if len(subscriber.queue) >= subscriber.backlogLimit {
		gap := b.newGapLocked(subscriber, event.SessionID, event.ID)
		subscriber.queue = []Event{*gap, event}
	} else {
		subscriber.queue = append(subscriber.queue, event)
	}
	b.signalSubscriberLocked(subscriber)
}

func (b *Broker) signalSubscriberLocked(subscriber *brokerSubscriber) {
	select {
	case subscriber.wake <- struct{}{}:
	default:
	}
}

func (b *Broker) pendingGapIndexLocked(subscriber *brokerSubscriber) int {
	for index := range subscriber.queue {
		if subscriber.queue[index].Type == "broker_gap" {
			return index
		}
	}
	return -1
}

func (b *Broker) pendingGapLocked(subscriber *brokerSubscriber) *Event {
	if index := b.pendingGapIndexLocked(subscriber); index >= 0 {
		return &subscriber.queue[index]
	}
	return nil
}

func (b *Broker) activeGapLocked(subscriber *brokerSubscriber) bool {
	return subscriber.gapInFlight || b.pendingGapLocked(subscriber) != nil
}

func (b *Broker) newGapLocked(subscriber *brokerSubscriber, sessionID string, latestEventID uint64) *Event {
	// A gap is synthetic and is not in history, so ID 0 must not advance the
	// client's Last-Event-ID cursor.
	return &Event{
		Type: "broker_gap", SessionID: sessionID,
		Payload: map[string]any{
			"sessionId": sessionID, "after": subscriber.after, "latestEventId": latestEventID,
		},
		At: time.Now().UTC(),
	}
}

func (b *Broker) recordGapLocked(subscriber *brokerSubscriber, sessionID string, latestEventID uint64) {
	gap := b.newGapLocked(subscriber, sessionID, latestEventID)
	if index := b.pendingGapIndexLocked(subscriber); index >= 0 {
		subscriber.queue[index] = *gap
		return
	}
	if len(subscriber.queue) < subscriber.backlogLimit {
		subscriber.queue = append(subscriber.queue, *gap)
	} else {
		// Replace the unrecoverable prefix with one gap, then terminate immediately.
		subscriber.queue = []Event{*gap}
		b.terminateSubscriberLocked(subscriber.sessionID, subscriber)
	}
	b.signalSubscriberLocked(subscriber)
}

func isCriticalBrokerEvent(eventType string) bool {
	switch eventType {
	case "session.message", "session.status", "tool.started", "tool.finished", "error":
		return true
	default:
		return false
	}
}

func (b *Broker) Publish(sessionID, eventType string, payload any) Event {
	if b.publishAdmissionHook != nil {
		b.publishAdmissionHook(eventType)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	event := Event{
		ID:        b.nextEventIDLocked(),
		Type:      eventType,
		SessionID: sessionID,
		Payload:   RedactPayload(payload),
		At:        time.Now().UTC(),
	}
	b.appendHistoryLocked(sessionID, event)
	if b.publishHook != nil {
		b.publishHook()
	}
	for subscriber := range b.subscribers[sessionID] {
		b.enqueueLocked(subscriber, event)
	}
	return event
}

func (b *Broker) nextEventIDLocked() uint64 {
	b.nextID++
	return b.nextID
}

func (b *Broker) appendHistoryLocked(sessionID string, event Event) {
	b.history[sessionID] = append(b.history[sessionID], event)
	if len(b.history[sessionID]) > b.historySize {
		b.history[sessionID] = b.history[sessionID][len(b.history[sessionID])-b.historySize:]
	}
}

func (b *Broker) Replay(sessionID string, after uint64) []Event {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.replayLocked(sessionID, after)
}

func (b *Broker) replayLocked(sessionID string, after uint64) []Event {
	var replay []Event
	for _, event := range b.history[sessionID] {
		if event.ID > after {
			replay = append(replay, event)
		}
	}
	return replay
}

func (b *Broker) ServeSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher.Flush()

	var after uint64
	if value := r.Header.Get("Last-Event-ID"); value != "" {
		after, _ = strconv.ParseUint(value, 10, 64)
	}
	subscription := b.SubscribeFrom(sessionID, after)
	defer subscription.Close()
	if shouldReplayHistory(r, after) {
		for _, event := range subscription.Replay {
			if err := WriteSSE(w, event); err != nil {
				return
			}
		}
		flusher.Flush()
	}

	events := subscription.Events
	ticker := time.NewTicker(b.heartbeat)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if err := WriteSSE(w, event); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			// Heartbeats keep the connection alive without changing replay state.
			event := Event{
				Type:      "heartbeat",
				SessionID: sessionID,
				Payload:   map[string]string{"status": "ok"},
				At:        time.Now().UTC(),
			}
			if err := WriteSSE(w, event); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func shouldReplayHistory(r *http.Request, after uint64) bool {
	return after > 0 || r.URL.Query().Get("replay") != "false"
}

func WriteSSE(w io.Writer, event Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", event.Type); err != nil {
		return err
	}
	if event.ID > 0 {
		if _, err := fmt.Fprintf(w, "id: %d\n", event.ID); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}

func (b *Broker) PublishMockPrompt(ctx context.Context, store *Store, sessionID, text string) {
	user := Message{Kind: "user", Text: text}
	_ = store.AppendMessage(sessionID, user)
	b.Publish(sessionID, "session.message", user)

	steps := []struct {
		delay    time.Duration
		typeName string
		payload  any
	}{
		{100 * time.Millisecond, "session.status", map[string]string{"status": "thinking"}},
		{150 * time.Millisecond, "tool.started", Message{
			Kind: "tool", Tool: "bash", Args: "$ pwd", Status: "running",
		}},
		{150 * time.Millisecond, "tool.output", map[string]string{
			"tool": "bash", "chunk": "/mock/workspace",
		}},
		{150 * time.Millisecond, "tool.finished", Message{
			Kind: "tool", Tool: "bash", Args: "$ pwd", Status: "ok",
			DurationMs: 42, ResultMeta: "done", Body: "/mock/workspace",
		}},
		{150 * time.Millisecond, "session.message", Message{
			Kind: "pi", Text: "Mock backend received your prompt and streamed this response over SSE.",
		}},
		{100 * time.Millisecond, "session.status", map[string]string{"status": "idle"}},
	}
	for _, step := range steps {
		select {
		case <-ctx.Done():
			return
		case <-time.After(step.delay):
			if msg, ok := step.payload.(Message); ok && step.typeName == "session.message" {
				_ = store.AppendMessage(sessionID, msg)
			}
			b.Publish(sessionID, step.typeName, step.payload)
		}
	}
}
