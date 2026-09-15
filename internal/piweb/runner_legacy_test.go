package piweb

import (
	"context"
	"time"
)

func (r *Runner) StartPiPrompt(
	parent context.Context,
	broker *Broker,
	store *Store,
	sessionID string,
	text string,
	images []PromptAttachment,
	displayText string,
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
	ctx := eventCtx

	user := Message{Kind: "user", Text: displayText, Attachments: images}
	if err := appendMessageAndPublish(store, broker, sessionID, user); err != nil {
		r.discardRun(sessionID, activeRun)
		eventCancel()
		commandCancel()
		return err
	}
	broker.Publish(sessionID, "session.status", map[string]string{"status": "running"})
	go r.consumeRuntimeEvents(ctx, broker, store, sessionID, runtime, activeRun)
	if err := activeRun.sendContext(ctx, rpcPromptCommand(text, images, "")); err != nil {
		r.finishPrompt(sessionID, activeRun, broker, err)
		return err
	}
	return nil
}

func (r *Runner) consumeRuntimeEvents(ctx context.Context, broker *Broker, store *Store, sessionID string, runtime PiRuntime, run *activePiRun) {
	if run.eventsDone != nil {
		defer close(run.eventsDone)
	}
	state := &jsonStreamState{}
	for {
		select {
		case <-ctx.Done():
			r.finishPrompt(sessionID, run, broker, ctx.Err())
			return
		case event, ok := <-runtime.Events():
			if !ok {
				r.finishPrompt(sessionID, run, broker, runtimeFailure(runtime))
				return
			}
			line := string(event.Raw)
			if !handlePiJSONEvent(line, broker, store, sessionID, state) && !isPiTerminalNotification(line) {
				broker.Publish(sessionID, "tool.output", map[string]string{"tool": "pi", "chunk": line})
			}
			if event.Type == "agent_end" {
				r.finishPrompt(sessionID, run, broker, nil)
				return
			}
		}
	}
}

func (r *Runner) finishPrompt(sessionID string, run *activePiRun, broker *Broker, runErr error) {
	run.markSettled()
	if run.isCancelling() {
		return
	}
	// Cleanup must not inherit the command context: parent cancellation is the
	// reason this path runs, and the gate still has to release the run claim.
	if !run.commandMu.acquire(context.Background()) {
		return
	}
	defer run.commandMu.release()
	r.mu.Lock()
	if r.running[sessionID] != run {
		r.mu.Unlock()
		return
	}
	run.mu.Lock()
	if run.cancelling {
		run.mu.Unlock()
		r.mu.Unlock()
		return
	}
	run.mu.Unlock()
	delete(r.running, sessionID)
	r.mu.Unlock()
	run.cancel()
	if run.commandCancel != nil {
		run.commandCancel()
	}
	if runErr != nil {
		broker.Publish(sessionID, "error", map[string]string{"error": runErr.Error()})
	}
	broker.Publish(sessionID, "session.status", map[string]string{
		"status": "idle", "finishedAt": time.Now().UTC().Format(time.RFC3339),
	})
}

func appendMessageAndPublish(store *Store, broker *Broker, sessionID string, msg Message) error {
	store.sessionMutationMu.Lock()
	defer store.sessionMutationMu.Unlock()
	if err := store.appendMessageLocked(sessionID, msg); err != nil {
		return err
	}
	broker.Publish(sessionID, eventTypeForMessage(msg), msg)
	return nil
}
