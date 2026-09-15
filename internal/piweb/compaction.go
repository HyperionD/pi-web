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

func (r *Runner) CompactDurable(parent context.Context, events SessionControlEventStore, store *Store, sessionID, requestID string, startupSettings ...ModelStartupSettings) (CompactionResult, error) {
	return r.compactDurable(parent, events, store, sessionID, requestID, startupSettings...)
}

func (r *Runner) compactDurable(parent context.Context, events SessionControlEventStore, store *Store, sessionID, requestID string, startupSettings ...ModelStartupSettings) (CompactionResult, error) {
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
	if requestID == "" {
		requestID = "compact:" + createSessionID()
	}
	if err := appendCompactionEvent(parent, events, sessionID, requestID+":started", EventCompactionStarted, nil); err != nil {
		return CompactionResult{}, err
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
		return CompactionResult{}, r.reportDurableCompactionFailure(parent, events, sessionID, requestID, err)
	}
	var result CompactionResult
	if err := json.Unmarshal(response.Data, &result); err != nil {
		err = fmt.Errorf("decode pi compact response: %w", err)
		if run.isCancelling() {
			return CompactionResult{}, err
		}
		return CompactionResult{}, r.reportDurableCompactionFailure(parent, events, sessionID, requestID, err)
	}
	if run.isCancelling() {
		return CompactionResult{}, context.Canceled
	}
	if r.compactCompleteHook != nil {
		r.compactCompleteHook()
	}
	if err := appendCompactionEvent(parent, events, sessionID, requestID+":committed", EventCompactionCommitted, result); err != nil {
		return CompactionResult{}, err
	}
	if run.isCancelling() {
		return CompactionResult{}, context.Canceled
	}

	return result, nil
}

func (r *Runner) reportDurableCompactionFailure(ctx context.Context, events SessionControlEventStore, sessionID, requestID string, err error) error {
	_ = appendCompactionEvent(ctx, events, sessionID, requestID+":failed", EventCompactionFailed, map[string]string{"error": err.Error()})
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
