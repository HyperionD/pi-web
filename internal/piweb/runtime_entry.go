package piweb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

func (entry *runtimeEntry) Send(ctx context.Context, command RPCCommand) (RPCResponse, error) {
	if !entry.steerGate.acquire(ctx) {
		return RPCResponse{}, ctx.Err()
	}
	defer entry.steerGate.release()
	if command.ID == "" {
		command.ID = fmt.Sprintf("piweb-%d", entry.manager.nextID.Add(1))
	}
	entry.mu.Lock()
	if entry.closed {
		entry.mu.Unlock()
		return RPCResponse{}, errors.New("runtime is closed")
	}
	entry.active++
	entry.lastUsed = entry.manager.now()
	entry.mu.Unlock()
	response, err := entry.runtime.Send(ctx, command)
	entry.mu.Lock()
	entry.active--
	entry.lastUsed = entry.manager.now()
	entry.mu.Unlock()
	return response, err
}

func (entry *runtimeEntry) Events() <-chan PiEvent {
	entry.eventsOnce.Do(func() { go entry.forwardEvents() })
	return entry.events
}

func (entry *runtimeEntry) forwardEvents() {
	defer close(entry.events)
	for {
		select {
		case event, ok := <-entry.runtime.Events():
			if !ok {
				return
			}
			if !entry.acceptEvent(event) {
				continue
			}
			entry.recordEvent(event)
			entry.enqueueEvent(event)
		case <-entry.stop:
			return
		}
	}
}

func (entry *runtimeEntry) discardStaleEvents() {
	entry.mu.Lock()
	entry.dropUntilAgentStart = true
	entry.mu.Unlock()
}

func (entry *runtimeEntry) acceptEvent(event PiEvent) bool {
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if !entry.dropUntilAgentStart {
		return true
	}
	if event.Type != "agent_start" {
		return false
	}
	entry.dropUntilAgentStart = false
	return true
}

func (entry *runtimeEntry) enqueueEvent(event PiEvent) {
	select {
	case entry.events <- event:
	case <-entry.stop:
	}
}

func (entry *runtimeEntry) poison(err error) {
	closeDone, alreadyClosed := entry.beginClose()
	if alreadyClosed {
		return
	}
	entry.stopEvents()
	go func() {
		if !entry.steerGate.acquire(context.Background()) {
			return
		}
		defer entry.steerGate.release()
		entry.stopEvents()
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		closeErr := entry.runtime.Close(closeCtx)
		cancel()
		entry.finishClose(closeDone, errors.Join(err, closeErr))
	}()
}

func (entry *runtimeEntry) recordEvent(event PiEvent) {
	var envelope struct {
		ToolCallID string `json:"toolCallId"`
	}
	_ = json.Unmarshal(event.Raw, &envelope)
	key := eventActivityKey(event.Type, envelope.ToolCallID)
	if key == "" {
		return
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	entry.lastUsed = entry.manager.now()
	switch event.Type {
	case "agent_start", "turn_start", "tool_execution_start":
		entry.activity[key]++
	case "agent_end", "turn_end", "tool_execution_end":
		if entry.activity[key] > 0 {
			entry.activity[key]--
			if entry.activity[key] == 0 {
				delete(entry.activity, key)
			}
		}
	}
}

func isSteerRPCCommand(command RPCCommand) bool {
	if command.Type != "prompt" || len(command.Payload) == 0 {
		return false
	}
	var payload struct {
		StreamingBehavior string `json:"streamingBehavior"`
	}
	return json.Unmarshal(command.Payload, &payload) == nil && payload.StreamingBehavior == "steer"
}

func eventActivityKey(eventType, toolCallID string) string {
	switch eventType {
	case "agent_start", "agent_end":
		return "agent"
	case "turn_start", "turn_end":
		return "turn"
	case "tool_execution_start", "tool_execution_end":
		if toolCallID == "" {
			return "tool"
		}
		return "tool:" + toolCallID
	default:
		return ""
	}
}

func (entry *runtimeEntry) IsRunning() bool { return !entry.isClosed() && entry.runtime.IsRunning() }
func (entry *runtimeEntry) processRunning() bool {
	entry.mu.Lock()
	retiring := entry.retiring
	entry.mu.Unlock()
	return !retiring && entry.runtime.IsRunning()
}

func (entry *runtimeEntry) markRetiring() {
	entry.mu.Lock()
	entry.retiring = true
	entry.mu.Unlock()
}
func (entry *runtimeEntry) Close(ctx context.Context) error {
	done, alreadyClosed := entry.beginClose()
	if alreadyClosed {
		return entry.waitClose(ctx, done)
	}
	entry.stopEvents()
	if !entry.steerGate.acquire(ctx) {
		entry.finishClose(done, ctx.Err())
		return ctx.Err()
	}
	defer entry.steerGate.release()
	entry.stopEvents()
	err := entry.runtime.Close(ctx)
	entry.finishClose(done, err)
	return err
}

func (entry *runtimeEntry) stopEvents() {
	entry.stopOnce.Do(func() {
		if entry.stop != nil {
			close(entry.stop)
		}
	})
}

func (entry *runtimeEntry) beginClose() (chan struct{}, bool) {
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.closed {
		return entry.closeDone, true
	}
	entry.closed = true
	entry.closeDone = make(chan struct{})
	return entry.closeDone, false
}

func (entry *runtimeEntry) finishClose(done chan struct{}, err error) {
	entry.mu.Lock()
	entry.closeErr = err
	close(done)
	entry.mu.Unlock()
}

func (entry *runtimeEntry) waitClose(ctx context.Context, done chan struct{}) error {
	if done == nil {
		return nil
	}
	select {
	case <-done:
		entry.mu.Lock()
		err := entry.closeErr
		entry.mu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (entry *runtimeEntry) touch(at time.Time) {
	entry.mu.Lock()
	if at.After(entry.lastUsed) {
		entry.lastUsed = at
	}
	entry.mu.Unlock()
}
func (entry *runtimeEntry) isClosed() bool {
	entry.mu.Lock()
	defer entry.mu.Unlock()
	return entry.closed
}
func (entry *runtimeEntry) idleBefore(cutoff time.Time) bool {
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.closed {
		return true
	}
	if entry.active > 0 || len(entry.activity) > 0 {
		return false
	}
	return entry.lastUsed.Before(cutoff)
}
