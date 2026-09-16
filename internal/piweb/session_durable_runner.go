package piweb

import (
	"context"
	"encoding/json"
	"strings"
	"time"
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
		runID: runID, cancel: eventCancel, commandCancel: commandCancel, commandContext: commandCtx,
		eventsDone: make(chan struct{}), runtimeReady: make(chan struct{}), promptReady: make(chan struct{}), settled: make(chan struct{}),
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
	if activeRun.isCancelling() {
		err := activeRun.completeDurableTerminal(EventRunCancelled)
		eventCancel()
		commandCancel()
		activeRun.markSettled()
		r.releaseRun(sessionID, activeRun)
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
	go r.consumeDurableRuntimeEvents(eventCtx, sessionID, runtime, runtimeEvents, activeRun, adapter)
	response, err := activeRun.sendPromptContext(eventCtx, rpcPromptCommand(text, images, ""))
	if err != nil {
		activeRun.markPromptReady()
		cancelled := activeRun.failDurableStart(adapter, err)
		if !cancelled {
			eventCancel()
			commandCancel()
			r.finishRun(sessionID, activeRun)
		}
		return nil
	}
	activeRun.setPromptID(response.ID)
	activeRun.markPromptReady()
	if activeRun.promptFinishedWithoutAgent(eventCtx, response, text) {
		return r.completeDurableRun(sessionID, activeRun, adapter)
	}
	return nil
}

func (r *Runner) consumeDurableRuntimeEvents(ctx context.Context, sessionID string, runtime PiRuntime, runtimeEvents <-chan PiEvent, run *activePiRun, adapter *RuntimeEventAdapter) {
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
				_ = adapter.Fail(runtimeFailure(runtime))
				r.abortDurableRun(sessionID, run)
				return
			}
			if event.Type == "agent_start" {
				run.setAgentStarted()
			}
			if event.Type == "prompt_result" {
				if !run.waitForPromptReady(ctx) {
					return
				}
				matched, invoked, known := run.promptResult(event)
				if matched && known && invoked {
					run.setAgentStarted()
				}
				if matched && known && !invoked {
					if err := r.completeDurableRun(sessionID, run, adapter); err != nil {
						return
					}
					return
				}
			}
			if err := adapter.Consume(event.Raw); err != nil {
				_ = adapter.Fail(err)
				r.abortDurableRun(sessionID, run)
				return
			}
			if event.Type == "agent_end" {
				if !run.waitForPromptReady(ctx) {
					return
				}
				if err := r.completeDurableRun(sessionID, run, adapter); err != nil {
					return
				}
				return
			}
		}
	}
}

const (
	localPromptProbeCount    = 20
	localPromptProbeInterval = 25 * time.Millisecond
)

func (run *activePiRun) promptFinishedWithoutAgent(ctx context.Context, response RPCResponse, text string) bool {
	invoked, known := promptAgentInvocation(response.Data)
	if known {
		return !invoked
	}
	if !strings.HasPrefix(strings.TrimSpace(text), "/") {
		return false
	}
	for probe := 0; probe < localPromptProbeCount; probe++ {
		if run.agentWasStarted() {
			return false
		}
		streaming, ok := run.promptState(ctx)
		if !ok || streaming {
			return false
		}
		if probe+1 < localPromptProbeCount && !waitForPromptProbe(ctx) {
			return false
		}
	}
	return true
}

func (run *activePiRun) promptState(ctx context.Context) (bool, bool) {
	state, err := run.sendRPCContext(ctx, RPCCommand{Type: "get_state"})
	if err != nil || !state.Success {
		return false, false
	}
	var snapshot struct {
		IsStreaming bool `json:"isStreaming"`
	}
	if json.Unmarshal(state.Data, &snapshot) != nil {
		return false, false
	}
	return snapshot.IsStreaming, true
}

func waitForPromptProbe(ctx context.Context) bool {
	timer := time.NewTimer(localPromptProbeInterval)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func promptAgentInvocation(raw json.RawMessage) (bool, bool) {
	var result struct {
		AgentInvoked *bool `json:"agentInvoked"`
	}
	if json.Unmarshal(raw, &result) != nil || result.AgentInvoked == nil {
		return false, false
	}
	return *result.AgentInvoked, true
}

func (run *activePiRun) promptResult(event PiEvent) (bool, bool, bool) {
	if event.Type != "prompt_result" {
		return false, false, false
	}
	var result struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(event.Raw, &result) != nil || !run.promptResultMatches(result.ID) {
		return false, false, false
	}
	invoked, known := promptAgentInvocation(event.Raw)
	return true, invoked, known
}

func (r *Runner) completeDurableRun(sessionID string, run *activePiRun, adapter *RuntimeEventAdapter) error {
	eventType := EventRunFinished
	if run.isCancelling() {
		eventType = EventRunCancelled
	}
	if err := run.completeDurableTerminal(eventType); err != nil {
		_ = adapter.Fail(err)
		r.abortDurableRun(sessionID, run)
		return err
	}
	r.finishDurableRun(sessionID, run)
	return nil
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
