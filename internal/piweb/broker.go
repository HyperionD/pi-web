package piweb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type Broker struct {
	mu          sync.RWMutex
	subscribers map[string]map[*brokerSubscriber]struct{}
	nextID      atomic.Uint64
	buffer      int
	heartbeat   time.Duration
	history     map[string][]Event
	historySize int
}

type brokerSubscriber struct {
	events chan Event
	after  uint64
}

func NewBroker() *Broker {
	return &Broker{
		subscribers: map[string]map[*brokerSubscriber]struct{}{},
		history:     map[string][]Event{},
		buffer:      32,
		heartbeat:   15 * time.Second,
		historySize: 256,
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
	subscriber := &brokerSubscriber{events: make(chan Event, buffer), after: after}
	b.mu.Lock()
	if b.subscribers[sessionID] == nil {
		b.subscribers[sessionID] = map[*brokerSubscriber]struct{}{}
	}
	b.subscribers[sessionID][subscriber] = struct{}{}
	replay := b.replayLocked(sessionID, after)
	b.mu.Unlock()

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
	defer b.mu.Unlock()
	if subscribers := b.subscribers[sessionID]; subscribers != nil {
		delete(subscribers, subscriber)
		if len(subscribers) == 0 {
			delete(b.subscribers, sessionID)
		}
	}
}

func (b *Broker) Publish(sessionID, eventType string, payload any) Event {
	event := Event{
		ID:        b.nextID.Add(1),
		Type:      eventType,
		SessionID: sessionID,
		Payload:   RedactPayload(payload),
		At:        time.Now().UTC(),
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.appendHistoryLocked(sessionID, event)
	for subscriber := range b.subscribers[sessionID] {
		select {
		case subscriber.events <- event:
		default:
			b.publishGapLocked(sessionID, subscriber, event.ID)
		}
	}
	return event
}

func (b *Broker) appendHistoryLocked(sessionID string, event Event) {
	b.history[sessionID] = append(b.history[sessionID], event)
	if len(b.history[sessionID]) > b.historySize {
		b.history[sessionID] = b.history[sessionID][len(b.history[sessionID])-b.historySize:]
	}
}

func (b *Broker) publishGapLocked(sessionID string, subscriber *brokerSubscriber, latestEventID uint64) {
	for {
		select {
		case <-subscriber.events:
		default:
			gap := Event{
				ID:        b.nextID.Add(1),
				Type:      "broker_gap",
				SessionID: sessionID,
				Payload: map[string]any{
					"sessionId":     sessionID,
					"after":         subscriber.after,
					"latestEventId": latestEventID,
				},
				At: time.Now().UTC(),
			}
			subscriber.events <- gap
			return
		}
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
			event := Event{
				ID:        b.nextID.Add(1),
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
	if _, err := fmt.Fprintf(w, "id: %d\n", event.ID); err != nil {
		return err
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
