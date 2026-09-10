package piweb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

func (entry *runtimeEntry) Send(ctx context.Context, command RPCCommand) (RPCResponse, error) {
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
		return
	default:
		entry.poison(errors.New("pi runtime entry event channel is full"))
	}
}

func (entry *runtimeEntry) poison(err error) {
	entry.mu.Lock()
	if entry.closed {
		entry.mu.Unlock()
		return
	}
	entry.closed = true
	if entry.closeDone == nil {
		entry.closeDone = make(chan struct{})
	}
	entry.mu.Unlock()
	entry.stopOnce.Do(func() { close(entry.stop) })
	go func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		closeErr := entry.runtime.Close(closeCtx)
		cancel()
		entry.mu.Lock()
		entry.closeErr = errors.Join(err, closeErr)
		close(entry.closeDone)
		entry.mu.Unlock()
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
func (entry *runtimeEntry) Close(ctx context.Context) error {
	entry.mu.Lock()
	if entry.closed {
		done := entry.closeDone
		entry.mu.Unlock()
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
	entry.closed = true
	done := entry.closeDone
	if done == nil {
		done = make(chan struct{})
		entry.closeDone = done
	}
	entry.mu.Unlock()
	entry.stopOnce.Do(func() { close(entry.stop) })
	err := entry.runtime.Close(ctx)
	entry.mu.Lock()
	entry.closeErr = err
	close(done)
	entry.mu.Unlock()
	return err
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
