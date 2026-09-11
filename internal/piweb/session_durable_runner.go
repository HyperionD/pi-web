package piweb

import (
	"context"
	"errors"
)

func (r *Runner) StartDurablePrompt(
	parent context.Context,
	store *Store,
	sessionID string,
	runID string,
	text string,
	images []PromptAttachment,
	adapter *RuntimeEventAdapter,
	startupSettings ...ModelStartupSettings,
) error {
	manager, err := r.runtimeManager(store)
	if err != nil {
		return err
	}
	eventCtx, eventCancel := context.WithCancel(parent)
	commandCtx, commandCancel := context.WithCancel(parent)
	activeRun := &activePiRun{
		cancel: eventCancel, commandCancel: commandCancel, commandContext: commandCtx,
		eventsDone: make(chan struct{}), runtimeReady: make(chan struct{}), settled: make(chan struct{}),
	}
	if err := r.claimRun(sessionID, activeRun); err != nil {
		eventCancel()
		commandCancel()
		return err
	}
	initializer := runtimeModelInitializer(startupSettings)
	runtime, ownership, err := manager.EnsureOwnedWithInitializer(parent, sessionID, activeRun.generation, initializer)
	if err != nil {
		r.discardRun(sessionID, activeRun)
		eventCancel()
		commandCancel()
		return err
	}
	if !activeRun.attachRuntime(runtime, ownership) {
		_ = manager.RetireOwned(context.Background(), ownership)
		r.discardRun(sessionID, activeRun)
		eventCancel()
		commandCancel()
		return errSessionBusy
	}
	go r.consumeDurableRuntimeEvents(eventCtx, sessionID, runtime, activeRun, adapter)
	if err := activeRun.sendContext(eventCtx, rpcPromptCommand(text, images, "")); err != nil {
		_ = adapter.Fail(err)
		eventCancel()
		commandCancel()
		r.finishRun(sessionID, activeRun)
		return err
	}
	return nil
}

func (r *Runner) consumeDurableRuntimeEvents(ctx context.Context, sessionID string, runtime PiRuntime, run *activePiRun, adapter *RuntimeEventAdapter) {
	if run.eventsDone != nil {
		defer close(run.eventsDone)
	}
	for {
		select {
		case <-ctx.Done():
			_ = adapter.Complete(EventRunInterrupted)
			r.finishDurableRun(sessionID, run)
			return
		case event, ok := <-runtime.Events():
			if !ok {
				_ = adapter.Fail(errors.New("pi runtime exited"))
				r.finishDurableRun(sessionID, run)
				return
			}
			_ = adapter.Consume(event.Raw)
			if event.Type == "agent_end" {
				_ = adapter.Complete(EventRunFinished)
				r.finishDurableRun(sessionID, run)
				return
			}
		}
	}
}

func (r *Runner) finishDurableRun(sessionID string, run *activePiRun) {
	run.cancel()
	if run.commandCancel != nil {
		run.commandCancel()
	}
	r.finishRun(sessionID, run)
}
