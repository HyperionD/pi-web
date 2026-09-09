package piweb

import (
	"context"
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
			entry.mu.Lock()
			entry.lastUsed = entry.manager.now()
			switch event.Type {
			case "agent_start", "turn_start", "tool_execution_start":
				entry.eventActive = true
			case "agent_end", "turn_end", "tool_execution_end":
				entry.eventActive = false
			}
			entry.mu.Unlock()
			select {
			case entry.events <- event:
			case <-entry.stop:
				return
			}
		case <-entry.stop:
			return
		}
	}
}

func (entry *runtimeEntry) IsRunning() bool { return !entry.isClosed() && entry.runtime.IsRunning() }
func (entry *runtimeEntry) Close(ctx context.Context) error {
	entry.mu.Lock()
	if entry.closed {
		entry.mu.Unlock()
		return nil
	}
	entry.closed = true
	entry.mu.Unlock()
	entry.stopOnce.Do(func() { close(entry.stop) })
	return entry.runtime.Close(ctx)
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
	if entry.closed || entry.active > 0 || entry.eventActive || entry.runtime.IsRunning() && runtimeIsActive(entry.runtime) {
		return false
	}
	lastUsed := entry.lastUsed
	if activity, ok := entry.runtime.(interface{ LastUsedAt() time.Time }); ok && activity.LastUsedAt().After(lastUsed) {
		lastUsed = activity.LastUsedAt()
	}
	return lastUsed.Before(cutoff)
}

func runtimeIsActive(runtime PiRuntime) bool {
	activity, ok := runtime.(interface{ Active() bool })
	return ok && activity.Active()
}
