package piweb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrRunPoisoned = errors.New("runtime adapter run is poisoned")

const (
	runtimeCoalesceMaxBytes = 16 * 1024
	runtimeCoalesceMaxDelay = 20 * time.Millisecond
	runtimeMaxPayloadBytes  = 256 * 1024
)

type SessionEventSink interface {
	Append(ctx context.Context, sessionID string, draft SessionEventDraft) (StoredSessionEvent, error)
}

type RuntimeEventAdapter struct {
	mu              sync.Mutex
	sessionID       string
	runID           string
	sink            SessionEventSink
	now             func() time.Time
	ordinal         int64
	poisoned        error
	segments        map[string]string
	pending         *pendingRuntimeEvent
	toolArgs        map[string]string
	toolCounts      map[string]int
	runError        string
	flushTimer      *time.Timer
	flushGeneration uint64
}

type pendingRuntimeEvent struct {
	kind     string
	entityID string
	data     []byte
	started  time.Time
}

func NewRuntimeEventAdapter(sessionID, runID string, sink SessionEventSink) *RuntimeEventAdapter {
	return &RuntimeEventAdapter{
		sessionID: sessionID, runID: runID, sink: sink, now: time.Now,
		segments: map[string]string{}, toolArgs: map[string]string{}, toolCounts: map[string]int{},
	}
}

func (a *RuntimeEventAdapter) Consume(raw []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.poisoned != nil {
		return a.poisoned
	}
	var frame struct {
		Type                  string          `json:"type"`
		Message               json.RawMessage `json:"message"`
		ToolName              string          `json:"toolName"`
		ToolCallID            string          `json:"toolCallId"`
		Args                  json.RawMessage `json:"args"`
		PartialResult         json.RawMessage `json:"partialResult"`
		Result                json.RawMessage `json:"result"`
		IsError               bool            `json:"isError"`
		AssistantMessageEvent struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
			Text  string `json:"text"`
		} `json:"assistantMessageEvent"`
	}
	if err := json.Unmarshal(raw, &frame); err != nil {
		return a.poison(err)
	}
	switch frame.Type {
	case "message_start", "message_end", "turn_end":
		a.recordAssistantError(frame.Message)
		if frame.Type != "message_end" {
			return nil
		}
		return a.consumeAssistantCommit(frame.Message)
	case "message_update":
		return a.consumeAssistantDelta(frame.AssistantMessageEvent.Type, frame.AssistantMessageEvent.Delta, frame.AssistantMessageEvent.Text)
	case "tool_execution_start":
		return a.consumeToolStart(frame.ToolCallID, frame.ToolName, frame.Args)
	case "tool_execution_update":
		return a.consumeToolUpdate(frame.ToolCallID, frame.ToolName, frame.PartialResult)
	case "tool_execution_end":
		return a.consumeToolEnd(frame.ToolCallID, frame.ToolName, frame.Result, frame.IsError)
	default:
		return nil
	}
}

func (a *RuntimeEventAdapter) Flush() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.poisoned != nil {
		return a.poisoned
	}
	return a.flushPending()
}

func (a *RuntimeEventAdapter) AppendUserMessage(text string, attachments []PromptAttachment) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.poisoned != nil {
		return a.poisoned
	}
	if err := a.flushPending(); err != nil {
		return err
	}
	return a.emit(EventUserMessageCreated, "", map[string]any{"text": text, "attachments": attachments})
}

func (a *RuntimeEventAdapter) Complete(eventType string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.poisoned != nil {
		return a.poisoned
	}
	if err := a.flushPending(); err != nil {
		return err
	}
	if eventType == EventRunFinished && a.runError != "" {
		return a.emit(EventRunFailed, "", map[string]any{"error": a.runError})
	}
	return a.emit(eventType, "", map[string]any{})
}

func (a *RuntimeEventAdapter) recordAssistantError(raw json.RawMessage) {
	if a.runError != "" {
		return
	}
	if message := assistantErrorMessage(raw); message != "" {
		a.runError = message
	}
}

func (a *RuntimeEventAdapter) Fail(err error) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.poisoned != nil {
		err = a.poisoned
	}
	a.stopFlushTimer()
	a.pending = nil
	a.poisoned = err
	return a.emit(EventRunFailed, "", map[string]any{"error": err.Error()})
}

func (a *RuntimeEventAdapter) consumeAssistantDelta(eventType, delta, text string) error {
	if delta == "" {
		delta = text
	}
	if delta == "" {
		return nil
	}
	kind := "pi"
	switch eventType {
	case "thinking_delta", "reasoning_delta":
		kind = "think"
	case "text_delta":
		kind = "pi"
	default:
		return nil
	}
	entityID := a.segmentEntity(kind)
	if err := a.ensureSegmentStart(kind, entityID); err != nil {
		return err
	}
	return a.bufferDelta(kind, entityID, delta)
}

func (a *RuntimeEventAdapter) ensureSegmentStart(kind, entityID string) error {
	if a.segments[kind] == entityID {
		return nil
	}
	if err := a.flushPending(); err != nil {
		return err
	}
	a.segments[kind] = entityID
	return a.emit(EventAssistantSegmentStarted, entityID, map[string]any{"kind": kind})
}

func (a *RuntimeEventAdapter) segmentEntity(kind string) string {
	if a.segments[kind] != "" {
		return a.segments[kind]
	}
	namespaced := "assistant"
	if kind == "think" {
		namespaced = "thinking"
	}
	return fmt.Sprintf("%s:%s:%d", namespaced, a.runID, a.ordinal+1)
}

func (a *RuntimeEventAdapter) bufferDelta(kind, entityID, delta string) error {
	if a.pending != nil && (a.pending.kind != kind || a.pending.entityID != entityID) {
		if err := a.flushPending(); err != nil {
			return err
		}
	}
	if a.pending == nil {
		a.pending = &pendingRuntimeEvent{kind: kind, entityID: entityID, started: a.now()}
		a.flushGeneration++
		generation := a.flushGeneration
		a.flushTimer = time.AfterFunc(runtimeCoalesceMaxDelay, func() {
			a.flushPendingAsync(generation)
		})
	}
	a.pending.data = append(a.pending.data, delta...)
	if len(a.pending.data) >= runtimeCoalesceMaxBytes || a.now().Sub(a.pending.started) >= runtimeCoalesceMaxDelay {
		return a.flushPending()
	}
	return nil
}

func (a *RuntimeEventAdapter) flushPendingAsync(generation uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if generation != a.flushGeneration || a.poisoned != nil || a.pending == nil {
		return
	}
	_ = a.flushPending()
}

func (a *RuntimeEventAdapter) stopFlushTimer() {
	a.flushGeneration++
	if a.flushTimer == nil {
		return
	}
	a.flushTimer.Stop()
	a.flushTimer = nil
}

func (a *RuntimeEventAdapter) flushPending() error {
	a.stopFlushTimer()
	pending := a.pending
	a.pending = nil
	if pending == nil {
		return nil
	}
	return a.emitWithType(EventAssistantSegmentDelta, pending.entityID, map[string]any{"kind": pending.kind, "delta": string(pending.data)})
}

func (a *RuntimeEventAdapter) consumeAssistantCommit(raw json.RawMessage) error {
	if err := a.flushPending(); err != nil {
		return err
	}
	text, thinking := assistantFinalText(raw)
	for _, entry := range []struct {
		kind string
		text string
	}{{"pi", text}, {"think", thinking}} {
		if entry.text == "" {
			continue
		}
		entityID := a.segments[entry.kind]
		if entityID == "" {
			entityID = a.segmentEntity(entry.kind)
			if err := a.emit(EventAssistantSegmentStarted, entityID, map[string]any{"kind": entry.kind}); err != nil {
				return err
			}
		}
		if err := a.emit(EventAssistantSegmentCommit, entityID, map[string]any{"kind": entry.kind, "text": entry.text}); err != nil {
			return err
		}
	}
	a.segments = map[string]string{}
	return nil
}

func assistantFinalText(raw json.RawMessage) (string, string) {
	var message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &message); err != nil || message.Role != "assistant" {
		return "", ""
	}
	var blocks []contentBlock
	if err := json.Unmarshal(message.Content, &blocks); err != nil {
		return contentText(message.Content), ""
	}
	var text, thinking []string
	for _, block := range blocks {
		if block.Type == "text" && block.Text != "" {
			text = append(text, block.Text)
		}
		if block.Type == "thinking" && block.Thinking != "" {
			thinking = append(thinking, block.Thinking)
		}
	}
	return joinLines(text), joinLines(thinking)
}

func joinLines(values []string) string {
	if len(values) == 0 {
		return ""
	}
	out := values[0]
	for _, value := range values[1:] {
		out += "\n" + value
	}
	return out
}

func (a *RuntimeEventAdapter) consumeToolStart(callID, tool string, args json.RawMessage) error {
	if err := a.flushPending(); err != nil {
		return err
	}
	entityID := a.toolEntity(callID, tool)
	if err := a.emit(EventToolStarted, entityID, map[string]any{"toolCallId": callID, "tool": tool, "args": string(args)}); err != nil {
		return err
	}
	a.toolArgs[entityID] = string(args)
	return nil
}

func (a *RuntimeEventAdapter) consumeToolUpdate(callID, tool string, partial json.RawMessage) error {
	return a.emit(EventToolOutput, a.toolEntity(callID, tool), map[string]any{"chunk": jsonChunkText(partial), "mode": "replace"})
}

func (a *RuntimeEventAdapter) consumeToolEnd(callID, tool string, result json.RawMessage, isError bool) error {
	if err := a.flushPending(); err != nil {
		return err
	}
	status := ToolStatusOK
	if isError {
		status = ToolStatusErr
	}
	return a.emit(EventToolFinished, a.toolEntity(callID, tool), map[string]any{"body": jsonChunkText(result), "status": status})
}

func (a *RuntimeEventAdapter) toolEntity(callID, tool string) string {
	if callID != "" {
		return "tool:" + a.runID + ":" + callID
	}
	key := tool
	a.toolCounts[key]++
	return fmt.Sprintf("tool:%s:%s:%d", a.runID, key, a.toolCounts[key])
}

func jsonChunkText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var value struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &value); err == nil && len(value.Content) > 0 {
		return contentText(value.Content)
	}
	return string(raw)
}

func (a *RuntimeEventAdapter) emit(eventType, entityID string, payload any) error {
	return a.emitWithType(eventType, entityID, payload)
}

func (a *RuntimeEventAdapter) emitWithType(eventType, entityID string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return a.poison(err)
	}
	if len(data) > runtimeMaxPayloadBytes {
		return a.poison(fmt.Errorf("runtime payload exceeds %d bytes", runtimeMaxPayloadBytes))
	}
	ordinal := a.ordinal + 1
	stored, err := a.sink.Append(context.Background(), a.sessionID, SessionEventDraft{
		SessionID: a.sessionID, RunID: a.runID, SourceID: fmt.Sprintf("%s:event:%d", a.runID, ordinal),
		SourceOrdinal: &ordinal, Type: eventType, EntityID: entityID, Payload: data, OccurredAt: a.now(),
	})
	if err != nil {
		return a.poison(err)
	}
	a.ordinal = ordinal
	_ = stored
	return nil
}

func (a *RuntimeEventAdapter) poison(err error) error {
	a.stopFlushTimer()
	a.pending = nil
	a.poisoned = err
	return err
}
