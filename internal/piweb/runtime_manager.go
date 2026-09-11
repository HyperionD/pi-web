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
type RuntimeInitializer func(context.Context, PiRuntime) error

type runtimeOwnership struct {
	sessionID  string
	generation uint64
	entry      *runtimeEntry
}

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
	closing   map[string]*runtimeClosing
	tombstone map[string]uint64
	ctx       context.Context
	cancel    context.CancelFunc
	stop      chan struct{}
	stopped   bool
	nextID    atomic.Uint64
}

type runtimeStartup struct {
	done        chan struct{}
	runtime     PiRuntime
	err         error
	discard     bool
	generation  uint64
	initializer RuntimeInitializer
}

type runtimeClosing struct {
	done    chan struct{}
	entry   *runtimeEntry
	err     error
	started bool
}

type runtimeEntry struct {
	manager             *RuntimeManager
	runtime             PiRuntime
	lastUsed            time.Time
	events              chan PiEvent
	eventsOnce          sync.Once
	stopOnce            sync.Once
	stop                chan struct{}
	mu                  sync.Mutex
	active              int
	activity            map[string]int
	dropUntilAgentStart bool
	closed              bool
	retiring            bool
	closeDone           chan struct{}
	closeErr            error
	ownerGeneration     uint64
	steerGate           commandGate
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
		sessions: map[string]*runtimeEntry{}, starting: map[string]*runtimeStartup{}, closing: map[string]*runtimeClosing{}, tombstone: map[string]uint64{},
		ctx: ctx, cancel: cancel, stop: make(chan struct{}),
	}
	go manager.evictLoop()
	return manager
}

func (m *RuntimeManager) Ensure(ctx context.Context, sessionID string) (PiRuntime, error) {
	runtime, _, err := m.EnsureOwned(ctx, sessionID, 0)
	return runtime, err
}

func (m *RuntimeManager) EnsureOwned(ctx context.Context, sessionID string, generation uint64) (PiRuntime, runtimeOwnership, error) {
	return m.ensureOwned(ctx, sessionID, generation, nil)
}

func (m *RuntimeManager) EnsureOwnedWithInitializer(ctx context.Context, sessionID string, generation uint64, initializer RuntimeInitializer) (PiRuntime, runtimeOwnership, error) {
	return m.ensureOwned(ctx, sessionID, generation, initializer)
}

func (m *RuntimeManager) ensureOwned(ctx context.Context, sessionID string, generation uint64, initializer RuntimeInitializer) (PiRuntime, runtimeOwnership, error) {
	if sessionID == "" {
		return nil, runtimeOwnership{}, fmt.Errorf("session ID is required")
	}
	for {
		m.mu.Lock()
		if m.stopped {
			m.mu.Unlock()
			return nil, runtimeOwnership{}, errRuntimeManagerClosed
		}
		if _, closed := m.tombstone[sessionID]; closed {
			m.mu.Unlock()
			return nil, runtimeOwnership{}, errRuntimeSessionClosed
		}
		if closing := m.closing[sessionID]; closing != nil {
			if !closing.entry.processRunning() {
				delete(m.closing, sessionID)
				m.mu.Unlock()
				continue
			}
			m.mu.Unlock()
			if err := waitRuntimeClosing(ctx, closing); err != nil {
				return nil, runtimeOwnership{}, err
			}
			continue
		}
		sessionFile, cwd, ok := m.store.SessionRuntime(sessionID)
		if !ok {
			m.mu.Unlock()
			return nil, runtimeOwnership{}, ErrNotFound
		}
		if entry := m.sessions[sessionID]; entry != nil {
			if entry.IsRunning() {
				if generation != 0 {
					entry.ownerGeneration = generation
				}
				entry.touch(m.now())
				ownership := runtimeOwnership{sessionID: sessionID, generation: entry.ownerGeneration, entry: entry}
				m.mu.Unlock()
				return entry, ownership, nil
			}
			delete(m.sessions, sessionID)
			closing := m.registerClosingLocked(sessionID, entry)
			m.mu.Unlock()
			if err := m.closeRegistered(ctx, sessionID, closing); err != nil {
				return nil, runtimeOwnership{}, err
			}
			continue
		}
		if startup := m.starting[sessionID]; startup != nil {
			m.mu.Unlock()
			return m.waitOwnedStartup(ctx, sessionID, startup, generation)
		}
		startup := &runtimeStartup{done: make(chan struct{}), generation: generation, initializer: initializer}
		m.starting[sessionID] = startup
		startCtx := m.ctx
		m.mu.Unlock()
		go m.startRuntime(sessionID, sessionFile, cwd, startup, startCtx)
		return m.waitOwnedStartup(ctx, sessionID, startup, generation)
	}
}

func (m *RuntimeManager) waitOwnedStartup(ctx context.Context, sessionID string, startup *runtimeStartup, generation uint64) (PiRuntime, runtimeOwnership, error) {
	runtime, err := waitRuntimeStartup(ctx, startup)
	if err != nil {
		return nil, runtimeOwnership{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.sessions[sessionID]
	if !ok || entry != runtime {
		return runtime, runtimeOwnership{}, errRuntimeSessionClosed
	}
	if generation != 0 && entry.ownerGeneration == startup.generation {
		entry.ownerGeneration = generation
	}
	leaseGeneration := generation
	if leaseGeneration == 0 {
		leaseGeneration = entry.ownerGeneration
	}
	return entry, runtimeOwnership{sessionID: sessionID, generation: leaseGeneration, entry: entry}, nil
}

func (m *RuntimeManager) startRuntime(sessionID, sessionFile, cwd string, startup *runtimeStartup, ctx context.Context) {
	runtime, err := m.factory(ctx, sessionFile, cwd)
	if err == nil && runtime == nil {
		err = errors.New("runtime factory returned nil runtime")
	}
	if err == nil && startup.initializer != nil {
		initCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err = startup.initializer(initCtx, runtime)
		cancel()
	}
	closeRuntime := err != nil && runtime != nil
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
		entry := &runtimeEntry{manager: m, runtime: runtime, ownerGeneration: startup.generation, lastUsed: m.now(), events: make(chan PiEvent, 64), stop: make(chan struct{}), activity: map[string]int{}, closeDone: make(chan struct{})}
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
	_, err := m.CloseSessionToken(ctx, sessionID)
	return err
}

// CloseSessionToken marks a session closed and returns the generation that owns
// that tombstone. Callers performing a destructive store operation must use the
// token when rolling back or finalizing the deletion.
func (m *RuntimeManager) CloseSessionToken(ctx context.Context, sessionID string) (uint64, error) {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return 0, errRuntimeManagerClosed
	}
	token := m.nextID.Add(1)
	entry := m.sessions[sessionID]
	delete(m.sessions, sessionID)
	startup := m.starting[sessionID]
	closing := m.closing[sessionID]
	m.tombstone[sessionID] = token
	if startup != nil {
		startup.discard = true
	}
	if entry != nil {
		closing = m.registerClosingLocked(sessionID, entry)
	}
	m.mu.Unlock()
	if startup != nil {
		if _, err := waitRuntimeStartup(ctx, startup); err != nil && !errors.Is(err, errRuntimeManagerClosed) && !errors.Is(err, errRuntimeSessionClosed) {
			return token, err
		}
		return token, nil
	}
	if closing == nil {
		return token, nil
	}
	return token, m.closeRegistered(ctx, sessionID, closing)
}

// RollbackSessionClose releases a deletion tombstone when the backing store
// could not delete the session. The legacy form rolls back the current
// generation; destructive handlers use RollbackSessionCloseToken instead.
func (m *RuntimeManager) RollbackSessionClose(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return errRuntimeManagerClosed
	}
	delete(m.tombstone, sessionID)
	return nil
}

func (m *RuntimeManager) RollbackSessionCloseToken(sessionID string, token uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return errRuntimeManagerClosed
	}
	if current, ok := m.tombstone[sessionID]; ok && current == token {
		delete(m.tombstone, sessionID)
	}
	return nil
}

func (m *RuntimeManager) FinalizeSessionClose(sessionID string, token uint64) error {
	return m.RollbackSessionCloseToken(sessionID, token)
}

func (m *RuntimeManager) Retire(ctx context.Context, sessionID string, runtime PiRuntime) error {
	m.mu.Lock()
	entry := m.sessions[sessionID]
	if entry == nil || (runtime != nil && entry != runtime) {
		m.mu.Unlock()
		return nil
	}
	ownership := runtimeOwnership{sessionID: sessionID, generation: entry.ownerGeneration, entry: entry}
	m.mu.Unlock()
	return m.RetireOwned(ctx, ownership)
}

func (m *RuntimeManager) RetireOwned(ctx context.Context, ownership runtimeOwnership) error {
	closing := m.detachOwned(ownership, false)
	if closing == nil {
		return nil
	}
	return m.closeRegistered(ctx, ownership.sessionID, closing)
}

func (m *RuntimeManager) RetireOwnedAsync(ownership runtimeOwnership) {
	closing := m.detachOwned(ownership, true)
	if closing == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), cancelAbortTimeout)
		defer cancel()
		_ = m.closeRegistered(ctx, ownership.sessionID, closing)
	}()
}

func (m *RuntimeManager) detachOwned(ownership runtimeOwnership, nonBlocking bool) *runtimeClosing {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.sessions[ownership.sessionID]
	if entry == nil || entry != ownership.entry || entry.ownerGeneration != ownership.generation {
		return nil
	}
	delete(m.sessions, ownership.sessionID)
	if nonBlocking {
		entry.markRetiring()
	}
	return m.registerClosingLocked(ownership.sessionID, entry)
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
	entries := make([]struct {
		sessionID string
		closing   *runtimeClosing
	}, 0, len(m.sessions)+len(m.closing))
	seen := make(map[*runtimeClosing]struct{}, len(m.sessions)+len(m.closing))
	for sessionID, entry := range m.sessions {
		closing := m.registerClosingLocked(sessionID, entry)
		entries = append(entries, struct {
			sessionID string
			closing   *runtimeClosing
		}{sessionID: sessionID, closing: closing})
		seen[closing] = struct{}{}
	}
	m.sessions = map[string]*runtimeEntry{}
	for sessionID, closing := range m.closing {
		if _, ok := seen[closing]; ok {
			continue
		}
		entries = append(entries, struct {
			sessionID string
			closing   *runtimeClosing
		}{sessionID: sessionID, closing: closing})
		seen[closing] = struct{}{}
	}
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
	for _, item := range entries {
		result = errors.Join(result, m.closeRegistered(ctx, item.sessionID, item.closing))
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
	type idleRuntime struct {
		sessionID string
		closing   *runtimeClosing
	}
	var idle []idleRuntime
	m.mu.Lock()
	for sessionID, entry := range m.sessions {
		if entry.idleBefore(cutoff) {
			delete(m.sessions, sessionID)
			idle = append(idle, idleRuntime{sessionID: sessionID, closing: m.registerClosingLocked(sessionID, entry)})
		}
	}
	m.mu.Unlock()
	for _, item := range idle {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = m.closeRegistered(closeCtx, item.sessionID, item.closing)
		cancel()
	}
}

func (m *RuntimeManager) registerClosingLocked(sessionID string, entry *runtimeEntry) *runtimeClosing {
	if closing := m.closing[sessionID]; closing != nil {
		return closing
	}
	closing := &runtimeClosing{done: make(chan struct{}), entry: entry}
	m.closing[sessionID] = closing
	return closing
}

func waitRuntimeClosing(ctx context.Context, closing *runtimeClosing) error {
	select {
	case <-closing.done:
		return closing.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *RuntimeManager) closeRegistered(ctx context.Context, sessionID string, closing *runtimeClosing) error {
	m.mu.Lock()
	isOwner := !closing.started
	if isOwner {
		closing.started = true
	}
	m.mu.Unlock()
	if isOwner {
		err := closing.entry.Close(ctx)
		m.mu.Lock()
		closing.err = err
		if m.closing[sessionID] == closing && (err == nil || !closing.entry.processRunning()) {
			delete(m.closing, sessionID)
		}
		close(closing.done)
		m.mu.Unlock()
		return err
	}
	return waitRuntimeClosing(ctx, closing)
}
