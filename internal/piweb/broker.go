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
	subscribers map[string]map[chan Event]struct{}
	nextID      atomic.Uint64
	buffer      int
	heartbeat   time.Duration
	history     map[string][]Event
	historySize int
	runHistory  map[string][]Event
	activeRuns  map[string]bool
}

func NewBroker() *Broker {
	return &Broker{
		subscribers: map[string]map[chan Event]struct{}{},
		history:     map[string][]Event{},
		buffer:      32,
		heartbeat:   15 * time.Second,
		historySize: 256,
		runHistory:  map[string][]Event{},
		activeRuns:  map[string]bool{},
	}
}

func (b *Broker) Subscribe(sessionID string) (<-chan Event, func()) {
	b.mu.Lock()
	eventChannel := b.addSubscriberLocked(sessionID, b.buffer)
	b.mu.Unlock()
	return eventChannel, b.unsubscribe(sessionID, eventChannel)
}

func (b *Broker) subscribeWithReplay(sessionID string, after uint64, replay, active bool) (<-chan Event, func(), []Event) {
	b.mu.Lock()
	var replayEvents []Event
	if replay {
		if active && after == 0 {
			if b.activeRuns[sessionID] {
				replayEvents = append([]Event(nil), b.runHistory[sessionID]...)
			}
		} else {
			replayEvents = replayAfter(b.history[sessionID], after)
		}
	}
	capacity := b.buffer
	if required := len(replayEvents) + b.buffer; required > capacity {
		capacity = required
	}
	eventChannel := b.addSubscriberLocked(sessionID, capacity)
	b.mu.Unlock()
	return eventChannel, b.unsubscribe(sessionID, eventChannel), replayEvents
}

func (b *Broker) addSubscriberLocked(sessionID string, capacity int) chan Event {
	eventChannel := make(chan Event, capacity)
	if b.subscribers[sessionID] == nil {
		b.subscribers[sessionID] = map[chan Event]struct{}{}
	}
	b.subscribers[sessionID][eventChannel] = struct{}{}
	return eventChannel
}

func (b *Broker) unsubscribe(sessionID string, eventChannel chan Event) func() {
	return func() {
		b.mu.Lock()
		if subscribers := b.subscribers[sessionID]; subscribers != nil {
			if _, ok := subscribers[eventChannel]; ok {
				delete(subscribers, eventChannel)
				close(eventChannel)
			}
			if len(subscribers) == 0 {
				delete(b.subscribers, sessionID)
			}
		}
		b.mu.Unlock()
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
	if event.Type == "session.status" && eventStatus(event) == "running" {
		b.runHistory[sessionID] = nil
		b.activeRuns[sessionID] = true
	}
	if b.activeRuns[sessionID] {
		b.runHistory[sessionID] = append(b.runHistory[sessionID], event)
		if event.Type == "session.status" && (eventStatus(event) == "idle" || eventStatus(event) == "cancelled") {
			b.activeRuns[sessionID] = false
		}
	}
	b.history[sessionID] = append(b.history[sessionID], event)
	if len(b.history[sessionID]) > b.historySize {
		b.history[sessionID] = b.history[sessionID][len(b.history[sessionID])-b.historySize:]
	}
	for eventChannel := range b.subscribers[sessionID] {
		select {
		case eventChannel <- event:
		default:
		}
	}
	b.mu.Unlock()
	return event
}

func (b *Broker) Replay(sessionID string, after uint64) []Event {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return replayAfter(b.history[sessionID], after)
}

func (b *Broker) ReplayActiveRun(sessionID string) []Event {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return append([]Event(nil), b.runHistory[sessionID]...)
}

func (b *Broker) IsRunning(sessionID string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.activeRuns[sessionID]
}

func replayAfter(events []Event, after uint64) []Event {
	var replay []Event
	for _, event := range events {
		if event.ID > after {
			replay = append(replay, event)
		}
	}
	return replay
}

func eventStatus(event Event) string {
	switch payload := event.Payload.(type) {
	case map[string]string:
		return payload["status"]
	case map[string]any:
		status, _ := payload["status"].(string)
		return status
	default:
		return ""
	}
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
	replayEnabled := shouldReplayHistory(r, after)
	events, unsubscribe, replay := b.subscribeWithReplay(
		sessionID,
		after,
		replayEnabled,
		replayEnabled && after == 0 && r.URL.Query().Get("replay") == "active",
	)
	defer unsubscribe()
	for _, event := range replay {
		if err := WriteSSE(w, event); err != nil {
			return
		}
	}
	if len(replay) > 0 {
		flusher.Flush()
	}
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
	b.Publish(sessionID, "session.status", map[string]string{"status": "running"})

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
