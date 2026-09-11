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

const (
	defaultBrokerBacklog        = 256
	defaultOverflowDrainTimeout = time.Second
)

type SessionEventCursors struct {
	LatestEventID    uint64
	ActiveRunAfter   uint64
	ActiveRunRunning bool
}

type Broker struct {
	mu                   sync.RWMutex
	publishBarrier       sync.RWMutex
	subscribers          map[string]map[*brokerSubscriber]struct{}
	nextID               uint64
	buffer               int
	maxBacklog           int
	overflowDrainTimeout time.Duration
	heartbeat            time.Duration
	closed               bool
	history              map[string][]Event
	historySize          int
	running              map[string]bool
	activeRunAfter       map[string]uint64
	activeRunHistory     map[string][]Event
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
	after        atomic.Uint64
	capacity     int
	backlogLimit int
	queue        []Event
	gapInFlight  bool
	wake         chan struct{}
	finished     chan struct{}
	ctx          context.Context
	cancel       context.CancelFunc
	drainCtx     context.Context
	drainCancel  context.CancelFunc
	drainTimer   *time.Timer
	sending      bool
	sendingEvent *Event
	closed       bool
}

func NewBroker() *Broker {
	return &Broker{
		subscribers:          map[string]map[*brokerSubscriber]struct{}{},
		history:              map[string][]Event{},
		running:              map[string]bool{},
		activeRunAfter:       map[string]uint64{},
		activeRunHistory:     map[string][]Event{},
		buffer:               32,
		heartbeat:            15 * time.Second,
		historySize:          256,
		maxBacklog:           defaultBrokerBacklog,
		overflowDrainTimeout: defaultOverflowDrainTimeout,
	}
}

func (b *Broker) Subscribe(sessionID string) (<-chan Event, func()) {
	subscription := b.SubscribeFrom(sessionID, 0)
	return subscription.Events, subscription.Close
}

func (b *Broker) SubscribeFrom(sessionID string, after uint64) EventSubscription {
	return b.subscribeFrom(sessionID, after, 0, false)
}

func (b *Broker) SubscribeFromActive(sessionID string, after, snapshotAfter uint64) EventSubscription {
	return b.subscribeFrom(sessionID, after, snapshotAfter, true)
}

func (b *Broker) subscribeFrom(sessionID string, after, snapshotAfter uint64, active bool) EventSubscription {
	b.mu.Lock()
	if b.closed {
		events := make(chan Event)
		close(events)
		b.mu.Unlock()
		return EventSubscription{Events: events, Close: func() {}}
	}
	buffer := b.buffer
	if buffer < 1 {
		buffer = 1
	}
	backlogLimit := b.maxBacklog
	if backlogLimit < 2 {
		backlogLimit = 2
	}
	ctx, cancel := context.WithCancel(context.Background())
	drainCtx, drainCancel := context.WithCancel(context.Background())
	subscriber := &brokerSubscriber{
		sessionID: sessionID, events: make(chan Event, buffer), capacity: buffer, backlogLimit: backlogLimit,
		wake: make(chan struct{}, 1), finished: make(chan struct{}), ctx: ctx, cancel: cancel,
		drainCtx: drainCtx, drainCancel: drainCancel,
	}
	subscriber.after.Store(after)
	if b.subscribers[sessionID] == nil {
		b.subscribers[sessionID] = map[*brokerSubscriber]struct{}{}
	}
	b.subscribers[sessionID][subscriber] = struct{}{}
	replay := b.replayLocked(sessionID, after)
	activeHistoryAvailable := active && len(b.activeRunHistory[sessionID]) > 0
	activeRunMatchesSnapshot := snapshotAfter == 0 || snapshotAfter >= b.activeRunAfter[sessionID]
	if activeHistoryAvailable && activeRunMatchesSnapshot {
		replay = replayActiveRunAfter(b.activeRunHistory[sessionID], after, snapshotAfter)
	} else if gap := b.replayGapLocked(sessionID, after); gap != nil {
		replay = []Event{*gap}
	}
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

func (b *Broker) detachSubscriberLocked(sessionID string, subscriber *brokerSubscriber) {
	if subscribers := b.subscribers[sessionID]; subscribers != nil {
		delete(subscribers, subscriber)
		if len(subscribers) == 0 {
			delete(b.subscribers, sessionID)
		}
	}
}

func (b *Broker) terminateSubscriberLocked(sessionID string, subscriber *brokerSubscriber) {
	b.detachSubscriberLocked(sessionID, subscriber)
	subscriber.closed = true
	subscriber.cancel()
	subscriber.drainCancel()
	if subscriber.drainTimer != nil {
		subscriber.drainTimer.Stop()
		subscriber.drainTimer = nil
	}
	b.signalSubscriberLocked(subscriber)
}

func (b *Broker) beginAutomaticTerminationLocked(sessionID string, subscriber *brokerSubscriber) {
	if subscriber.closed {
		return
	}
	b.detachSubscriberLocked(sessionID, subscriber)
	subscriber.closed = true
	timeout := b.overflowDrainTimeout
	if timeout <= 0 {
		timeout = time.Nanosecond
	}
	subscriber.drainTimer = time.AfterFunc(timeout, subscriber.drainCancel)
	b.signalSubscriberLocked(subscriber)
}

func (b *Broker) forwardSubscriber(subscriber *brokerSubscriber) {
	defer func() {
		subscriber.drainCancel()
		b.mu.Lock()
		if subscriber.drainTimer != nil {
			subscriber.drainTimer.Stop()
			subscriber.drainTimer = nil
		}
		b.mu.Unlock()
		close(subscriber.events)
		close(subscriber.finished)
	}()
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
		if !sendSubscriberEvent(subscriber, event) {
			return
		}
		if event.Type != "broker_gap" && event.ID > 0 {
			// Publish may observe this cursor before the forwarder reacquires mu.
			subscriber.after.Store(event.ID)
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

func sendSubscriberEvent(subscriber *brokerSubscriber, event Event) bool {
	select {
	case <-subscriber.ctx.Done():
		return false
	default:
	}
	select {
	case subscriber.events <- event:
		return true
	default:
	}
	select {
	case subscriber.events <- event:
		return true
	case <-subscriber.ctx.Done():
		return false
	case <-subscriber.drainCtx.Done():
		return false
	}
}

func (b *Broker) enqueueLocked(subscriber *brokerSubscriber, event Event) {
	if subscriber.closed {
		return
	}
	if len(subscriber.queue) == 0 && !subscriber.sending {
		select {
		case subscriber.events <- event:
			if event.Type != "broker_gap" && event.ID > 0 {
				subscriber.after.Store(event.ID)
			}
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
		b.beginAutomaticTerminationLocked(subscriber.sessionID, subscriber)
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

func (b *Broker) newGapLocked(subscriber *brokerSubscriber, sessionID string, latestEventID uint64, after ...uint64) *Event {
	// A gap is synthetic and is not in history, so ID 0 must not advance the
	// client's Last-Event-ID cursor.
	cursor := uint64(0)
	if subscriber != nil {
		cursor = subscriber.after.Load()
	} else if len(after) > 0 {
		cursor = after[0]
	}
	return &Event{
		Type: "broker_gap", SessionID: sessionID,
		Payload: map[string]any{
			"sessionId": sessionID,
			"after":     strconv.FormatUint(cursor, 10), "latestEventId": strconv.FormatUint(latestEventID, 10),
		},
		At: time.Now().UTC(),
	}
}

func (b *Broker) recordGapLocked(subscriber *brokerSubscriber, sessionID string, latestEventID uint64) {
	gap := b.newGapLocked(subscriber, sessionID, latestEventID)
	if index := b.pendingGapIndexLocked(subscriber); index >= 0 {
		subscriber.queue[index] = *gap
		if len(subscriber.queue) >= subscriber.backlogLimit {
			b.beginAutomaticTerminationLocked(subscriber.sessionID, subscriber)
		}
		b.signalSubscriberLocked(subscriber)
		return
	}
	if len(subscriber.queue) < subscriber.backlogLimit {
		subscriber.queue = append(subscriber.queue, *gap)
	} else {
		preserved := make([]Event, 0, subscriber.backlogLimit-1)
		for _, queued := range subscriber.queue {
			if isCriticalBrokerEvent(queued.Type) && len(preserved) < subscriber.backlogLimit-1 {
				preserved = append(preserved, queued)
			}
		}
		subscriber.queue = append([]Event{*gap}, preserved...)
		b.beginAutomaticTerminationLocked(subscriber.sessionID, subscriber)
	}
	b.signalSubscriberLocked(subscriber)
}

func isActiveSessionStatus(status string) bool {
	return status == "running" || status == "thinking"
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
	b.publishBarrier.RLock()
	defer b.publishBarrier.RUnlock()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return Event{SessionID: sessionID, Type: eventType}
	}
	event := Event{
		ID:        b.nextEventIDLocked(),
		Type:      eventType,
		SessionID: sessionID,
		Payload:   RedactPayload(payload),
		At:        time.Now().UTC(),
	}
	runWasActive := b.running[sessionID]
	status := ""
	if event.Type == "session.status" {
		status = eventStatus(event)
		if isActiveSessionStatus(status) && !b.running[sessionID] {
			b.running[sessionID] = true
			b.activeRunHistory[sessionID] = nil
			b.activeRunAfter[sessionID] = event.ID
		}
	}
	b.appendHistoryLocked(sessionID, event)
	if b.running[sessionID] || (runWasActive && (status == "idle" || status == "cancelled")) {
		b.activeRunHistory[sessionID] = append(b.activeRunHistory[sessionID], event)
	}
	if status == "idle" || status == "cancelled" {
		b.running[sessionID] = false
	}
	if b.publishHook != nil {
		b.publishHook()
	}
	for subscriber := range b.subscribers[sessionID] {
		b.enqueueLocked(subscriber, event)
	}
	return event
}

func (b *Broker) Shutdown(ctx context.Context) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	var subscribers []*brokerSubscriber
	for sessionID, entries := range b.subscribers {
		for subscriber := range entries {
			subscribers = append(subscribers, subscriber)
			b.terminateSubscriberLocked(sessionID, subscriber)
		}
	}
	b.mu.Unlock()
	for _, subscriber := range subscribers {
		select {
		case <-subscriber.finished:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
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

func (b *Broker) IsRunning(sessionID string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.running[sessionID]
}

func (b *Broker) ActiveRunAfter(sessionID string) uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.activeRunAfter[sessionID]
}

func (b *Broker) LatestEventID(sessionID string) uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.latestEventIDLocked(sessionID)
}

func (b *Broker) latestEventIDLocked(sessionID string) uint64 {
	history := b.history[sessionID]
	if len(history) == 0 {
		return 0
	}
	return history[len(history)-1].ID
}

// WithSessionSnapshot blocks ordinary publishes while the caller reads the
// durable session state and captures the matching Broker cursor.
func (b *Broker) WithSessionSnapshot(sessionID string, read func() error) (SessionEventCursors, error) {
	b.publishBarrier.Lock()
	defer b.publishBarrier.Unlock()
	if err := read(); err != nil {
		return SessionEventCursors{}, err
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return SessionEventCursors{
		LatestEventID:    b.latestEventIDLocked(sessionID),
		ActiveRunAfter:   b.activeRunAfter[sessionID],
		ActiveRunRunning: b.running[sessionID],
	}, nil
}

func (b *Broker) Replay(sessionID string, after uint64) []Event {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.replayLocked(sessionID, after)
}

func (b *Broker) replayLocked(sessionID string, after uint64) []Event {
	return replayAfter(b.history[sessionID], after)
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

func replayActiveRunAfter(events []Event, after, snapshotAfter uint64) []Event {
	coveredDeltas := snapshotCoveredDeltaCursors(events, snapshotAfter)
	var replay []Event
	for _, event := range events {
		if event.ID <= after {
			continue
		}
		if event.ID <= snapshotAfter {
			if !isTransientRunEvent(event.Type) {
				continue
			}
			if event.Type == "session.delta" && event.ID <= coveredDeltas[eventPayloadKind(event.Payload)] {
				continue
			}
		}
		replay = append(replay, event)
	}
	return replay
}

func snapshotCoveredDeltaCursors(events []Event, snapshotAfter uint64) map[string]uint64 {
	covered := map[string]uint64{}
	for _, event := range events {
		if event.ID > snapshotAfter {
			break
		}
		if event.Type == "session.message" {
			kind := eventPayloadKind(event.Payload)
			if kind == "pi" || kind == "think" {
				covered[kind] = event.ID
			}
		}
	}
	return covered
}

func eventPayloadKind(payload any) string {
	switch value := payload.(type) {
	case Message:
		return value.Kind
	case map[string]string:
		return value["kind"]
	case map[string]any:
		kind, _ := value["kind"].(string)
		return kind
	default:
		return ""
	}
}

func isTransientRunEvent(eventType string) bool {
	switch eventType {
	case "session.delta", "tool.started", "tool.output", "error":
		return true
	default:
		return false
	}
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

func (b *Broker) replayGapLocked(sessionID string, after uint64) *Event {
	history := b.history[sessionID]
	if after == 0 || len(history) == 0 || after >= history[0].ID {
		return nil
	}
	return b.newGapLocked(nil, sessionID, history[len(history)-1].ID, after)
}

func (b *Broker) ServeSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	after, err := parseSessionEventCursor(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	snapshotAfter, err := parseEventCursor(r.URL.Query().Get("snapshot"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
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

	activeReplay := r.URL.Query().Get("replay") == "active"
	var subscription EventSubscription
	if activeReplay {
		subscription = b.SubscribeFromActive(sessionID, after, snapshotAfter)
	} else {
		subscription = b.SubscribeFrom(sessionID, after)
	}
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

func parseSessionEventCursor(r *http.Request) (uint64, error) {
	value := r.Header.Get("Last-Event-ID")
	if value == "" {
		value = r.URL.Query().Get("after")
	}
	return parseEventCursor(value)
}

func parseEventCursor(value string) (uint64, error) {
	if value == "" {
		return 0, nil
	}
	cursor, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid event cursor")
	}
	return cursor, nil
}

func shouldReplayHistory(r *http.Request, after uint64) bool {
	if r.URL.Query().Get("replay") == "false" {
		return false
	}
	return true
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
	_ = appendMessageAndPublish(store, b, sessionID, user)

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
				_ = appendMessageAndPublish(store, b, sessionID, msg)
				continue
			}
			b.Publish(sessionID, step.typeName, step.payload)
		}
	}
}
