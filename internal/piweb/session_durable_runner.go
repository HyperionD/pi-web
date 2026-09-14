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
		durableComplete: adapter.Complete,
		durableSteer: func(text string, images []PromptAttachment) error {
			return adapter.AppendUserMessage(text, images)
		},
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
	runtimeEvents := runtime.Events()
	go r.consumeDurableRuntimeEvents(eventCtx, sessionID, runtimeEvents, activeRun, adapter)
	if err := activeRun.sendContext(eventCtx, rpcPromptCommand(text, images, "")); err != nil {
		_ = adapter.Fail(err)
		eventCancel()
		commandCancel()
		r.finishRun(sessionID, activeRun)
		return err
	}
	return nil
}

func (r *Runner) consumeDurableRuntimeEvents(ctx context.Context, sessionID string, runtimeEvents <-chan PiEvent, run *activePiRun, adapter *RuntimeEventAdapter) {
	if run.eventsDone != nil {
		defer close(run.eventsDone)
	}
	for {
		select {
		case <-ctx.Done():
			if err := run.completeDurableTerminal(EventRunInterrupted); err != nil {
				_ = adapter.Fail(err)
				r.abortDurableRun(sessionID, run)
				return
			}
			r.finishDurableRun(sessionID, run)
			return
		case event, ok := <-runtimeEvents:
			if !ok {
				_ = adapter.Fail(errors.New("pi runtime exited"))
				r.abortDurableRun(sessionID, run)
				return
			}
			if err := adapter.Consume(event.Raw); err != nil {
				_ = adapter.Fail(err)
				r.abortDurableRun(sessionID, run)
				return
			}
			if event.Type == "agent_end" {
				if err := run.completeDurableTerminal(EventRunFinished); err != nil {
					_ = adapter.Fail(err)
					r.abortDurableRun(sessionID, run)
					return
				}
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

func (r *Runner) abortDurableRun(sessionID string, run *activePiRun) {
	run.cancel()
	if run.commandCancel != nil {
		run.commandCancel()
	}
	if r.manager != nil {
		ctx, cancel := context.WithTimeout(context.Background(), cancelAbortTimeout)
		_ = r.manager.RetireOwned(ctx, run.ownership)
		cancel()
	}
	r.finishRun(sessionID, run)
}
