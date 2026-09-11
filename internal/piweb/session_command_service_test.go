package piweb

import (
	"context"
	"errors"
	"sync"
	"testing"
)

type fakePromptStarter struct {
	mu       sync.Mutex
	started  []string
	runtimes []*RuntimeEventAdapter
	err      error
	order    *[]string
}

func (s *fakePromptStarter) StartPrompt(_ context.Context, sessionID, runID string, adapter *RuntimeEventAdapter) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.started = append(s.started, sessionID+"/"+runID)
	s.runtimes = append(s.runtimes, adapter)
	if s.order != nil {
		*s.order = append(*s.order, "send")
	}
	return s.err
}

func newCommandServiceForTest(store SessionEventStore, starter RuntimePromptStarter, order *[]string) *SessionCommandService {
	service := NewSessionCommandService(store, starter)
	service.launch = func(task func()) { task() }
	service.newRunID = func() string { return "run-1" }
	_ = order
	return service
}

func TestStartPromptCommitsBeforeSendingRuntimeCommand(t *testing.T) {
	order := []string{}
	store := &orderingEventStore{inner: appendTestStore(t), order: &order}
	starter := &fakePromptStarter{order: &order}
	service := newCommandServiceForTest(store, starter, &order)
	if _, err := service.StartPrompt(t.Context(), StartPromptCommand{SessionID: "s1", ClientRequestID: "req-1", Text: "hello", DisplayText: "hello"}); err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "commit" || order[1] != "send" {
		t.Fatalf("order = %#v", order)
	}
}

func TestStartPromptDuplicateDoesNotStartRuntime(t *testing.T) {
	store := appendTestStore(t)
	starter := &fakePromptStarter{}
	service := newCommandServiceForTest(store, starter, nil)
	first, err := service.StartPrompt(t.Context(), StartPromptCommand{SessionID: "s1", ClientRequestID: "req-1", Text: "hello", DisplayText: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.StartPrompt(t.Context(), StartPromptCommand{SessionID: "s1", ClientRequestID: "req-1", Text: "hello", DisplayText: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Existing || second.RunID != first.RunID {
		t.Fatalf("second = %#v", second)
	}
	if len(starter.started) != 1 {
		t.Fatalf("runtime started %d times", len(starter.started))
	}
}

func TestStartPromptRejectsHashConflict(t *testing.T) {
	store := appendTestStore(t)
	service := newCommandServiceForTest(store, &fakePromptStarter{}, nil)
	if _, err := service.StartPrompt(t.Context(), StartPromptCommand{SessionID: "s1", ClientRequestID: "req-1", Text: "hello", DisplayText: "hello"}); err != nil {
		t.Fatal(err)
	}
	_, err := service.StartPrompt(t.Context(), StartPromptCommand{SessionID: "s1", ClientRequestID: "req-1", Text: "different", DisplayText: "different"})
	if !errors.Is(err, ErrCommandHashConflict) {
		t.Fatalf("error = %v", err)
	}
}

func TestStartPromptRuntimeFailureAppendsRunFailed(t *testing.T) {
	store := appendTestStore(t)
	starter := &fakePromptStarter{err: errors.New("runtime boom")}
	service := newCommandServiceForTest(store, starter, nil)
	receipt, err := service.StartPrompt(t.Context(), StartPromptCommand{SessionID: "s1", ClientRequestID: "req-1", Text: "hello", DisplayText: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.db.QueryRow(`SELECT count(*) FROM session_events WHERE session_id='s1' AND run_id=? AND event_type=?`, receipt.RunID, EventRunFailed).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("run.failed count = %d", count)
	}
}

type orderingEventStore struct {
	inner SessionEventStore
	order *[]string
}

func (s *orderingEventStore) StartRun(ctx context.Context, command StartRunCommand) (RunReceipt, error) {
	receipt, err := s.inner.StartRun(ctx, command)
	if err == nil && !receipt.Existing {
		*s.order = append(*s.order, "commit")
	}
	return receipt, err
}

func (s *orderingEventStore) Append(ctx context.Context, sessionID string, draft SessionEventDraft) (StoredSessionEvent, error) {
	return s.inner.Append(ctx, sessionID, draft)
}
func (s *orderingEventStore) Snapshot(ctx context.Context, sessionID string, request SessionPageRequest) (SessionStateSnapshot, error) {
	return s.inner.Snapshot(ctx, sessionID, request)
}
func (s *orderingEventStore) EventsAfter(ctx context.Context, sessionID string, after int64, limit EventReadLimit) (EventBatch, error) {
	return s.inner.EventsAfter(ctx, sessionID, after, limit)
}
func (s *orderingEventStore) ItemContent(ctx context.Context, sessionID, itemID string, version, start, end int64) ([]byte, int64, error) {
	return s.inner.ItemContent(ctx, sessionID, itemID, version, start, end)
}
func (s *orderingEventStore) OpenSession(ctx context.Context, source SessionSource) (ImportReport, error) {
	return s.inner.OpenSession(ctx, source)
}
func (s *orderingEventStore) RecoverInterrupted(ctx context.Context) error {
	return s.inner.RecoverInterrupted(ctx)
}
func (s *orderingEventStore) DeleteSession(ctx context.Context, sessionID string) error {
	return s.inner.DeleteSession(ctx, sessionID)
}
func (s *orderingEventStore) Close(ctx context.Context) error { return s.inner.Close(ctx) }
