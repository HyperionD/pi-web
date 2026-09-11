package piweb

import (
	"context"
	"sync"
)

type WakeSubscription struct {
	Wake  <-chan struct{}
	Close func()
}

type SessionWakeHub struct {
	mu          sync.Mutex
	subscribers map[string]map[chan struct{}]struct{}
	closed      bool
}

func NewSessionWakeHub() *SessionWakeHub {
	return &SessionWakeHub{subscribers: map[string]map[chan struct{}]struct{}{}}
}

func (h *SessionWakeHub) Subscribe(sessionID string) WakeSubscription {
	wake := make(chan struct{}, 1)
	h.mu.Lock()
	if h.closed {
		close(wake)
		h.mu.Unlock()
		return WakeSubscription{Wake: wake, Close: func() {}}
	}
	if h.subscribers[sessionID] == nil {
		h.subscribers[sessionID] = map[chan struct{}]struct{}{}
	}
	h.subscribers[sessionID][wake] = struct{}{}
	h.mu.Unlock()
	var once sync.Once
	return WakeSubscription{Wake: wake, Close: func() {
		once.Do(func() { h.remove(sessionID, wake) })
	}}
}

func (h *SessionWakeHub) remove(sessionID string, wake chan struct{}) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if subscribers := h.subscribers[sessionID]; subscribers != nil {
		delete(subscribers, wake)
		if len(subscribers) == 0 {
			delete(h.subscribers, sessionID)
		}
	}
}

func (h *SessionWakeHub) Notify(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	for wake := range h.subscribers[sessionID] {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
}

func (h *SessionWakeHub) Shutdown(context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	h.closed = true
	for sessionID, subscribers := range h.subscribers {
		for wake := range subscribers {
			close(wake)
		}
		delete(h.subscribers, sessionID)
	}
	return nil
}
