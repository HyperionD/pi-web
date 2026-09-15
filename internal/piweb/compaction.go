package piweb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var errSessionBusy = errors.New("session already running")

type CompactionResult struct {
	Summary              string `json:"summary,omitempty"`
	FirstKeptEntryID     string `json:"firstKeptEntryId,omitempty"`
	TokensBefore         int    `json:"tokensBefore"`
	EstimatedTokensAfter int    `json:"estimatedTokensAfter"`
}

func (r *Runner) Compact(parent context.Context, broker *Broker, store *Store, sessionID string, startupSettings ...ModelStartupSettings) (CompactionResult, error) {
	return r.compact(parent, broker, store, nil, sessionID, "", startupSettings...)
}

func (r *Runner) CompactDurable(parent context.Context, events SessionControlEventStore, store *Store, sessionID, requestID string, startupSettings ...ModelStartupSettings) (CompactionResult, error) {
	return r.compact(parent, nil, store, events, sessionID, requestID, startupSettings...)
}

func (r *Runner) compact(parent context.Context, broker *Broker, store *Store, events SessionControlEventStore, sessionID, requestID string, startupSettings ...ModelStartupSettings) (CompactionResult, error) {
	manager, err := r.runtimeManager(store)
	if err != nil {
		return CompactionResult{}, err
	}
	_, eventCancel := context.WithCancel(parent)
	commandCtx, commandCancel := context.WithTimeout(parent, 5*time.Minute)
	run := &activePiRun{
		cancel: eventCancel, commandCancel: commandCancel, commandContext: commandCtx,
		compacting: true, eventsDone: make(chan struct{}),
		runtimeReady: make(chan struct{}), settled: make(chan struct{}),
	}
	if err := r.claimRun(sessionID, run); err != nil {
		eventCancel()
		commandCancel()
		return CompactionResult{}, err
	}
	runtime, ownership, err := manager.EnsureOwnedWithInitializer(parent, sessionID, run.generation, runtimeModelInitializer(startupSettings))
	if err != nil {
		r.discardRun(sessionID, run)
		eventCancel()
		commandCancel()
		return CompactionResult{}, err
	}
	if !run.attachRuntime(runtime, ownership) {
		_ = manager.RetireOwned(context.Background(), ownership)
		r.discardRun(sessionID, run)
		eventCancel()
		commandCancel()
		return CompactionResult{}, errSessionBusy
	}
	defer func() {
		eventCancel()
		commandCancel()
		if run.isCancelling() {
			run.markSettled()
			return
		}
		r.finishRun(sessionID, run)
	}()
	if events != nil {
		if requestID == "" {
			requestID = "compact:" + createSessionID()
		}
		if err := appendCompactionEvent(parent, events, sessionID, requestID+":started", EventCompactionStarted, nil); err != nil {
			return CompactionResult{}, err
		}
	} else {
		broker.Publish(sessionID, "session.status", map[string]string{"status": "compacting"})
	}

	response, err := run.sendRPCContext(commandCtx, RPCCommand{Type: "compact"})
	if err == nil && !response.Success {
		err = errors.New(response.Error)
		if response.Error == "" {
			err = errors.New("pi compact failed")
		}
	}
	if run.isCancelling() {
		if err == nil {
			err = context.Canceled
		}
		return CompactionResult{}, err
	}
	if err != nil {
		if commandCtx.Err() != nil {
			if _, claimed := r.claimCancellation(sessionID, run, false); claimed {
				r.cancelRunRuntime(sessionID, run, manager)
				r.finishRun(sessionID, run)
				r.releaseRun(sessionID, run)
			}
			return CompactionResult{}, err
		}
		return CompactionResult{}, r.reportCompactFailure(parent, events, broker, sessionID, requestID, err)
	}
	var result CompactionResult
	if err := json.Unmarshal(response.Data, &result); err != nil {
		err = fmt.Errorf("decode pi compact response: %w", err)
		if run.isCancelling() {
			return CompactionResult{}, err
		}
		return CompactionResult{}, r.reportCompactFailure(parent, events, broker, sessionID, requestID, err)
	}
	if run.isCancelling() {
		return CompactionResult{}, context.Canceled
	}
	if r.compactCompleteHook != nil {
		r.compactCompleteHook()
	}
	message := Message{Kind: "pi", Text: fmt.Sprintf("context summarized · %d tokens before compaction", result.TokensBefore)}
	if events != nil {
		if err := appendCompactionEvent(parent, events, sessionID, requestID+":committed", EventCompactionCommitted, result); err != nil {
			return CompactionResult{}, err
		}
	} else if err := appendMessageAndPublish(store, broker, sessionID, message); err != nil {
		if run.isCancelling() {
			return CompactionResult{}, err
		}
		return CompactionResult{}, r.reportCompactFailure(parent, nil, broker, sessionID, requestID, err)
	}
	if run.isCancelling() {
		return CompactionResult{}, context.Canceled
	}
	if events == nil {
		broker.Publish(sessionID, "session.status", map[string]string{
			"status":     "idle",
			"finishedAt": time.Now().UTC().Format(time.RFC3339),
		})
	}
	return result, nil
}

func (r *Runner) reportCompactFailure(ctx context.Context, events SessionControlEventStore, broker *Broker, sessionID, requestID string, err error) error {
	if events != nil {
		_ = appendCompactionEvent(ctx, events, sessionID, requestID+":failed", EventCompactionFailed, map[string]string{"error": err.Error()})
		return err
	}
	broker.Publish(sessionID, "error", map[string]string{"error": err.Error()})
	broker.Publish(sessionID, "session.status", map[string]string{"status": "idle"})
	return err
}

func appendCompactionEvent(ctx context.Context, events SessionControlEventStore, sessionID, sourceID, eventType string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = events.AppendControl(ctx, sessionID, sourceID, eventType, payload)
	return err
}

func (r *Runner) claimRun(sessionID string, run *activePiRun) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.running[sessionID]; exists {
		return errSessionBusy
	}
	run.generation = r.nextGeneration.Add(1)
	r.running[sessionID] = run
	return nil
}
