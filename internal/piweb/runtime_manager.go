package piweb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type RPCCommand struct {
	ID      string
	Type    string
	Payload json.RawMessage
}

type RPCResponse struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Command string          `json:"command"`
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   string          `json:"error"`
}

type PiEvent struct {
	Type string
	Raw  json.RawMessage
}

type PiRuntime interface {
	Send(ctx context.Context, command RPCCommand) (RPCResponse, error)
	Events() <-chan PiEvent
	IsRunning() bool
	Close(ctx context.Context) error
}

type RuntimeProcessFactory func(ctx context.Context, sessionFile, cwd string) (PiRuntime, error)

type RuntimeManagerOptions struct {
	Factory         RuntimeProcessFactory
	IdleTimeout     time.Duration
	CleanupInterval time.Duration
	Now             func() time.Time
}

var (
	errRuntimeManagerClosed = errors.New("runtime manager is shut down")
	errRuntimeSessionClosed = errors.New("session runtime is closed")
)

const (
	defaultRuntimeIdleTimeout = 10 * time.Minute
	defaultRuntimeCleanup     = time.Minute
)

type RuntimeManager struct {
	store   *Store
	factory RuntimeProcessFactory
	now     func() time.Time
	idle    time.Duration
	cleanup time.Duration

	mu        sync.Mutex
	sessions  map[string]*runtimeEntry
	starting  map[string]*runtimeStartup
	tombstone map[string]struct{}
	ctx       context.Context
	cancel    context.CancelFunc
	stop      chan struct{}
	stopped   bool
	nextID    atomic.Uint64
}

type runtimeStartup struct {
	done    chan struct{}
	runtime PiRuntime
	err     error
	discard bool
}

type runtimeEntry struct {
	manager    *RuntimeManager
	runtime    PiRuntime
	lastUsed   time.Time
	events     chan PiEvent
	eventsOnce sync.Once
	stopOnce   sync.Once
	stop       chan struct{}
	mu         sync.Mutex
	active     int
	activity   map[string]int
	closed     bool
	closeDone  chan struct{}
	closeErr   error
}

func NewRuntimeManager(store *Store, factories ...RuntimeProcessFactory) *RuntimeManager {
	options := RuntimeManagerOptions{}
	if len(factories) > 0 {
		options.Factory = factories[0]
	}
	return NewRuntimeManagerWithOptions(store, options)
}

func NewRuntimeManagerWithFactory(store *Store, factory RuntimeProcessFactory) *RuntimeManager {
	return NewRuntimeManagerWithOptions(store, RuntimeManagerOptions{Factory: factory})
}

func NewRuntimeManagerWithOptions(store *Store, options RuntimeManagerOptions) *RuntimeManager {
	factory := options.Factory
	if factory == nil {
		factory = newPiRuntime
	}
	idle := options.IdleTimeout
	if idle <= 0 {
		idle = defaultRuntimeIdleTimeout
	}
	cleanup := options.CleanupInterval
	if cleanup <= 0 {
		cleanup = defaultRuntimeCleanup
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	ctx, cancel := context.WithCancel(context.Background())
	manager := &RuntimeManager{
		store: store, factory: factory, now: now, idle: idle, cleanup: cleanup,
		sessions: map[string]*runtimeEntry{}, starting: map[string]*runtimeStartup{}, tombstone: map[string]struct{}{},
		ctx: ctx, cancel: cancel, stop: make(chan struct{}),
	}
	go manager.evictLoop()
	return manager
}

func (m *RuntimeManager) Ensure(ctx context.Context, sessionID string) (PiRuntime, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("session ID is required")
	}
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return nil, errRuntimeManagerClosed
	}
	if _, closed := m.tombstone[sessionID]; closed {
		m.mu.Unlock()
		return nil, errRuntimeSessionClosed
	}
	sessionFile, cwd, ok := m.store.SessionRuntime(sessionID)
	if !ok {
		m.mu.Unlock()
		return nil, ErrNotFound
	}
	if entry := m.sessions[sessionID]; entry != nil && entry.IsRunning() {
		entry.touch(m.now())
		m.mu.Unlock()
		return entry, nil
	}
	if startup := m.starting[sessionID]; startup != nil {
		m.mu.Unlock()
		return waitRuntimeStartup(ctx, startup)
	}
	startup := &runtimeStartup{done: make(chan struct{})}
	m.starting[sessionID] = startup
	startCtx := m.ctx
	m.mu.Unlock()
	go m.startRuntime(sessionID, sessionFile, cwd, startup, startCtx)
	return waitRuntimeStartup(ctx, startup)
}

func (m *RuntimeManager) startRuntime(sessionID, sessionFile, cwd string, startup *runtimeStartup, ctx context.Context) {
	runtime, err := m.factory(ctx, sessionFile, cwd)
	if err == nil && runtime == nil {
		err = errors.New("runtime factory returned nil runtime")
	}
	closeRuntime := false
	m.mu.Lock()
	delete(m.starting, sessionID)
	_, tombstoned := m.tombstone[sessionID]
	if err == nil && (m.stopped || startup.discard || tombstoned) {
		closeRuntime = true
		err = errRuntimeSessionClosed
		if m.stopped {
			err = errRuntimeManagerClosed
		}
	}
	if err == nil {
		entry := &runtimeEntry{manager: m, runtime: runtime, lastUsed: m.now(), events: make(chan PiEvent, 64), stop: make(chan struct{}), activity: map[string]int{}, closeDone: make(chan struct{})}
		m.sessions[sessionID] = entry
		startup.runtime = entry
	} else {
		startup.runtime = runtime
	}
	startup.err = err
	m.mu.Unlock()
	if closeRuntime && runtime != nil {
		_ = runtime.Close(context.Background())
	}
	close(startup.done)
}

func waitRuntimeStartup(ctx context.Context, startup *runtimeStartup) (PiRuntime, error) {
	select {
	case <-startup.done:
		return startup.runtime, startup.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *RuntimeManager) CloseSession(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	entry := m.sessions[sessionID]
	delete(m.sessions, sessionID)
	startup := m.starting[sessionID]
	m.tombstone[sessionID] = struct{}{}
	if startup != nil {
		startup.discard = true
	}
	m.mu.Unlock()
	if startup != nil {
		if _, err := waitRuntimeStartup(ctx, startup); err != nil && !errors.Is(err, errRuntimeManagerClosed) && !errors.Is(err, errRuntimeSessionClosed) {
			return err
		}
		return nil
	}
	if entry == nil {
		return nil
	}
	return entry.Close(ctx)
}

func (m *RuntimeManager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return nil
	}
	m.stopped = true
	m.cancel()
	close(m.stop)
	entries := make([]*runtimeEntry, 0, len(m.sessions))
	for _, entry := range m.sessions {
		entries = append(entries, entry)
	}
	m.sessions = map[string]*runtimeEntry{}
	startups := make([]*runtimeStartup, 0, len(m.starting))
	for _, startup := range m.starting {
		startup.discard = true
		startups = append(startups, startup)
	}
	m.mu.Unlock()

	var result error
	for _, startup := range startups {
		select {
		case <-startup.done:
		case <-ctx.Done():
			result = errors.Join(result, ctx.Err())
		}
	}
	for _, entry := range entries {
		result = errors.Join(result, entry.Close(ctx))
	}
	return result
}

func (m *RuntimeManager) evictLoop() {
	ticker := time.NewTicker(m.cleanup)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.evictIdle()
		case <-m.stop:
			return
		}
	}
}

func (m *RuntimeManager) evictIdle() {
	cutoff := m.now().Add(-m.idle)
	var idle []*runtimeEntry
	m.mu.Lock()
	for sessionID, entry := range m.sessions {
		if entry.idleBefore(cutoff) {
			delete(m.sessions, sessionID)
			idle = append(idle, entry)
		}
	}
	m.mu.Unlock()
	for _, entry := range idle {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = entry.Close(closeCtx)
		cancel()
	}
}
