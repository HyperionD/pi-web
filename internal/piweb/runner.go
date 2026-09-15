package piweb

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const fallbackChoiceSystemPrompt = `Pi Web UI fallback choice protocol:
- You are running inside Pi Web UI.
- When you need the user to choose between options, or the user asks you to ask a choice question,
  output a fenced json block with top-level type "piweb_choice".
- Stop after emitting the fallback block and wait for the user's follow-up.
- When the user later sends:
  选项回复：
  id: <id>
  value: <value>
  continue using that id/value as the selected answer.
- Required schema:
  {
    "type":"piweb_choice",
    "id":"stable-choice-id",
    "question":"Question text",
    "options":[{"label":"Option A","value":"A","description":"What A means"}],
    "allowCustom":false
  }
- Keep id stable, short, and unique. Use at most 8 options. Use inert plain text only.`

type Runner struct {
	mu             sync.Mutex
	running        map[string]*activePiRun
	manager        *RuntimeManager
	nextGeneration atomic.Uint64

	cancelBeforeClaimHook func()
	steerBeforeSendHook   func()
	compactCompleteHook   func()
}

type commandGate struct {
	once  sync.Once
	ready chan struct{}
}

func (gate *commandGate) init() {
	gate.once.Do(func() {
		gate.ready = make(chan struct{}, 1)
		gate.ready <- struct{}{}
	})
}

func (gate *commandGate) acquire(ctx context.Context) bool {
	gate.init()
	select {
	case <-gate.ready:
		return true
	case <-ctx.Done():
		return false
	}
}

func (gate *commandGate) tryAcquire() bool {
	gate.init()
	select {
	case <-gate.ready:
		return true
	default:
		return false
	}
}

func (gate *commandGate) release() {
	gate.ready <- struct{}{}
}

type activePiRun struct {
	cancel               context.CancelFunc
	commandCancel        context.CancelFunc
	commandContext       context.Context
	stdin                io.WriteCloser
	runtime              PiRuntime
	ownership            runtimeOwnership
	generation           uint64
	compacting           bool
	eventsDone           chan struct{}
	runtimeReady         chan struct{}
	settled              chan struct{}
	settledOnce          sync.Once
	runtimeOnce          sync.Once
	commandMu            commandGate
	mu                   sync.Mutex
	commandKind          string
	steerPending         int
	cancelling           bool
	durableComplete      func(string) error
	durableSteer         func(string, []PromptAttachment) error
	durableTerminalOnce  sync.Once
	durableTerminalError error
}

func NewRunner() *Runner {
	return &Runner{running: map[string]*activePiRun{}}
}

func NewRunnerWithRuntimeManager(manager *RuntimeManager) *Runner {
	runner := NewRunner()
	runner.manager = manager
	return runner
}

func (r *Runner) runtimeManager(store *Store) (*RuntimeManager, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.manager == nil {
		r.manager = NewRuntimeManager(store)
	}
	return r.manager, nil
}

func (r *Runner) Steer(sessionID string, text string, images []PromptAttachment) error {
	return r.SteerContext(context.Background(), sessionID, text, images)
}

func (r *Runner) SteerContext(ctx context.Context, sessionID string, text string, images []PromptAttachment) error {
	r.mu.Lock()
	run, ok := r.running[sessionID]
	hook := r.steerBeforeSendHook
	r.mu.Unlock()
	if !ok {
		return errors.New("session is not running")
	}
	if hook != nil {
		hook()
	}
	run.adjustSteerPending(1)
	defer run.adjustSteerPending(-1)
	if !run.commandMu.acquire(ctx) {
		return ctx.Err()
	}
	defer run.commandMu.release()
	run.setCommandKind("steer")
	defer run.setCommandKind("")
	r.mu.Lock()
	current := r.running[sessionID] == run
	r.mu.Unlock()
	if !current || run.isCompacting() || run.isCancelling() {
		return errors.New("session is not running")
	}
	if run.durableSteer != nil {
		if err := run.durableSteer(text, images); err != nil {
			return err
		}
	}
	command := rpcPromptCommand(text, images, "steer")
	if run.getRuntime() == nil {
		return run.sendFallbackUnlocked(command)
	}
	payload, err := json.Marshal(command)
	if err != nil {
		return err
	}
	response, err := run.sendRPCUnlocked(ctx, RPCCommand{Type: "prompt", Payload: payload})
	if err != nil {
		return err
	}
	if !response.Success {
		if response.Error == "" {
			response.Error = "pi RPC command failed"
		}
		return errors.New(response.Error)
	}
	return nil
}

func (r *Runner) IsRunning(sessionID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.running[sessionID]
	return ok
}

func (r *Runner) IsCompacting(sessionID string) bool {
	r.mu.Lock()
	run, ok := r.running[sessionID]
	r.mu.Unlock()
	return ok && run.isCompacting()
}

func (r *Runner) forgetRun(sessionID string, run *activePiRun) {
	r.mu.Lock()
	if r.running[sessionID] == run {
		delete(r.running, sessionID)
	}
	r.mu.Unlock()
}

func (r *Runner) discardRun(sessionID string, run *activePiRun) {
	r.mu.Lock()
	if r.running[sessionID] == run {
		run.mu.Lock()
		cancelling := run.cancelling
		run.mu.Unlock()
		if !cancelling {
			delete(r.running, sessionID)
		}
	}
	r.mu.Unlock()
}

func (run *activePiRun) completeDurableTerminal(eventType string) error {
	if run.durableComplete == nil {
		return nil
	}
	run.durableTerminalOnce.Do(func() {
		run.durableTerminalError = run.durableComplete(eventType)
	})
	return run.durableTerminalError
}

func (r *Runner) finishRun(sessionID string, run *activePiRun) {
	run.markSettled()
	if !run.commandMu.acquire(context.Background()) {
		return
	}
	defer run.commandMu.release()
	r.mu.Lock()
	if r.running[sessionID] == run {
		run.mu.Lock()
		cancelling := run.cancelling
		run.mu.Unlock()
		if !cancelling {
			delete(r.running, sessionID)
		}
	}
	r.mu.Unlock()
}

func (r *Runner) releaseRun(sessionID string, run *activePiRun) {
	r.mu.Lock()
	if r.running[sessionID] == run {
		delete(r.running, sessionID)
	}
	r.mu.Unlock()
}

func (r *Runner) claimCancellation(sessionID string, expected *activePiRun, useHook bool) (*activePiRun, bool) {
	r.mu.Lock()
	run, ok := r.running[sessionID]
	if expected != nil && run != expected {
		r.mu.Unlock()
		return nil, false
	}
	if useHook && r.cancelBeforeClaimHook != nil {
		hook := r.cancelBeforeClaimHook
		r.mu.Unlock()
		hook()
		r.mu.Lock()
		if r.running[sessionID] != run {
			r.mu.Unlock()
			return nil, false
		}
		ok = true
	}
	if !ok || run == nil || !run.beginCancel() {
		r.mu.Unlock()
		return nil, false
	}
	r.mu.Unlock()
	return run, true
}

func (run *activePiRun) attachRuntime(runtime PiRuntime, ownership runtimeOwnership) bool {
	run.mu.Lock()
	defer run.mu.Unlock()
	if run.cancelling {
		run.runtimeOnce.Do(func() { close(run.runtimeReady) })
		return false
	}
	run.runtime = runtime
	run.ownership = ownership
	run.runtimeOnce.Do(func() { close(run.runtimeReady) })
	return true
}

func (run *activePiRun) getRuntime() PiRuntime {
	run.mu.Lock()
	defer run.mu.Unlock()
	return run.runtime
}

func (run *activePiRun) isCompacting() bool {
	run.mu.Lock()
	defer run.mu.Unlock()
	return run.compacting
}

func (run *activePiRun) markSettled() {
	if run.settled != nil {
		run.settledOnce.Do(func() { close(run.settled) })
	}
}

func (run *activePiRun) beginCancel() bool {
	run.mu.Lock()
	defer run.mu.Unlock()
	if run.cancelling {
		return false
	}
	run.cancelling = true
	if run.commandCancel != nil {
		run.commandCancel()
	}
	return true
}

func (run *activePiRun) isSteering() bool {
	run.mu.Lock()
	defer run.mu.Unlock()
	return run.steerPending > 0 || run.commandKind == "steer"
}

func (run *activePiRun) adjustSteerPending(delta int) {
	run.mu.Lock()
	run.steerPending += delta
	run.mu.Unlock()
}

func (run *activePiRun) setCommandKind(kind string) {
	run.mu.Lock()
	run.commandKind = kind
	run.mu.Unlock()
}

func (run *activePiRun) waitCommand(timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if run.commandMu.acquire(ctx) {
		run.commandMu.release()
	}
}

func (run *activePiRun) isCancelling() bool {
	run.mu.Lock()
	defer run.mu.Unlock()
	return run.cancelling
}

func piRPCArgs(sessionFile string) []string {
	return []string{
		"--session", sessionFile,
		"--mode", "rpc",
		"--append-system-prompt", fallbackChoiceSystemPrompt,
	}
}

type rpcPrompt struct {
	Type              string     `json:"type"`
	Message           string     `json:"message"`
	Images            []rpcImage `json:"images,omitempty"`
	StreamingBehavior string     `json:"streamingBehavior,omitempty"`
}

type rpcImage struct {
	Type     string `json:"type"`
	Data     string `json:"data"`
	MIMEType string `json:"mimeType"`
}

func rpcPromptCommand(text string, images []PromptAttachment, streamingBehavior string) rpcPrompt {
	return rpcPrompt{Type: "prompt", Message: text, Images: rpcImages(images), StreamingBehavior: streamingBehavior}
}

func rpcImages(images []PromptAttachment) []rpcImage {
	result := make([]rpcImage, 0, len(images))
	for _, image := range images {
		data := strings.TrimSpace(image.DataURL)
		if data == "" {
			continue
		}
		if comma := strings.Index(data, ","); comma >= 0 {
			data = data[comma+1:]
		}
		mimeType := image.MIMEType
		if mimeType == "" {
			mimeType = "image/png"
		}
		result = append(result, rpcImage{Type: "image", Data: data, MIMEType: mimeType})
	}
	return result
}

func (r *activePiRun) send(command any) error {
	return r.sendContext(context.Background(), command)
}

func (r *activePiRun) sendContext(ctx context.Context, command any) error {
	payload, err := json.Marshal(command)
	if err != nil {
		return err
	}
	typeName := ""
	if value, ok := command.(rpcPrompt); ok {
		typeName = value.Type
	}
	if r.getRuntime() == nil {
		return r.sendFallback(command)
	}
	response, err := r.sendRPCContext(ctx, RPCCommand{Type: typeName, Payload: payload})
	if err != nil {
		return err
	}
	if !response.Success {
		if response.Error == "" {
			response.Error = "pi RPC command failed"
		}
		return errors.New(response.Error)
	}
	return nil
}

func (r *activePiRun) sendFallback(command any) error {
	if !r.commandMu.acquire(context.Background()) {
		return context.Canceled
	}
	defer r.commandMu.release()
	r.setCommandKind("fallback")
	defer r.setCommandKind("")
	return r.sendFallbackUnlocked(command)
}

func (r *activePiRun) sendFallbackUnlocked(command any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancelling {
		return errors.New("session is cancelling")
	}
	if r.stdin == nil {
		return errors.New("runtime is unavailable")
	}
	line, err := json.Marshal(command)
	if err != nil {
		return err
	}
	_, err = r.stdin.Write(append(line, '\n'))
	return err
}

func (r *activePiRun) sendRPCContext(ctx context.Context, command RPCCommand) (RPCResponse, error) {
	sendCtx, stop := context.WithCancel(ctx)
	commandCtx := r.commandContext
	if commandCtx == nil {
		commandCtx = context.Background()
	}
	stopCommand := context.AfterFunc(commandCtx, stop)
	defer stopCommand()
	if !r.commandMu.acquire(sendCtx) {
		return RPCResponse{}, sendCtx.Err()
	}
	defer r.commandMu.release()
	kind := command.Type
	if isSteerRPCCommand(command) {
		kind = "steer"
	}
	r.setCommandKind(kind)
	defer r.setCommandKind("")
	return r.sendRPCUnlocked(sendCtx, command)
}

func (r *activePiRun) sendRPCUnlocked(ctx context.Context, command RPCCommand) (RPCResponse, error) {
	r.mu.Lock()
	if r.cancelling {
		r.mu.Unlock()
		return RPCResponse{}, errors.New("session is cancelling")
	}
	runtime := r.runtime
	r.mu.Unlock()
	if runtime == nil {
		return RPCResponse{}, errors.New("runtime is unavailable")
	}
	return runtime.Send(ctx, command)
}

func isPiRPCAgentEnd(line string) bool {
	var event struct {
		Type string `json:"type"`
	}
	return json.Unmarshal([]byte(line), &event) == nil && event.Type == "agent_end"
}

func isPiTerminalNotification(line string) bool {
	return strings.Contains(line, "\x1b]777;notify;")
}

func imageExtension(mimeType string) string {
	subtype := strings.TrimPrefix(strings.ToLower(mimeType), "image/")
	subtype = strings.Split(subtype, ";")[0]
	switch subtype {
	case "jpeg":
		return ".jpg"
	case "png", "gif", "webp", "bmp":
		return "." + subtype
	case "svg+xml":
		return ".svg"
	default:
		return ".png"
	}
}

const (
	cancelAbortTimeout  = 5 * time.Second
	cancelSettleTimeout = time.Second
)

func (r *Runner) Cancel(sessionID string) bool {
	run, ok := r.claimCancellation(sessionID, nil, true)
	if !ok {
		return false
	}
	runManager := r.manager
	_ = run.completeDurableTerminal(EventRunCancelled)
	runtime := run.waitRuntime(cancelSettleTimeout)
	abortStarted := false
	if runtime != nil {
		abortStarted = run.tryAbort(runtime)
	}
	if !abortStarted {
		run.cancel()
		if run.isSteering() {
			run.waitCommand(cancelSettleTimeout)
		} else {
			run.markSettled()
		}
	} else if !waitForRun(run.settled, cancelSettleTimeout) {
		run.cancel()
		waitForRun(run.eventsDone, cancelSettleTimeout)
	}
	if runtime != nil {
		if abortStarted {
			closeCtx, closeCancel := context.WithTimeout(context.Background(), cancelAbortTimeout)
			if runManager != nil {
				_ = runManager.RetireOwned(closeCtx, run.ownership)
			} else {
				_ = runtime.Close(closeCtx)
			}
			closeCancel()
		} else if runManager != nil {
			runManager.RetireOwnedAsync(run.ownership)
		} else {
			go func() {
				closeCtx, closeCancel := context.WithTimeout(context.Background(), cancelAbortTimeout)
				defer closeCancel()
				_ = runtime.Close(closeCtx)
			}()
		}
	}
	run.markSettled()
	r.releaseRun(sessionID, run)
	return true
}

func (run *activePiRun) tryAbort(runtime PiRuntime) bool {
	if !run.commandMu.tryAcquire() {
		return false
	}
	defer run.commandMu.release()
	ctx, cancel := context.WithTimeout(context.Background(), cancelAbortTimeout)
	defer cancel()
	done := make(chan struct{})
	go func() {
		_, _ = runtime.Send(ctx, RPCCommand{Type: "abort"})
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
	return true
}

func (run *activePiRun) waitRuntime(timeout time.Duration) PiRuntime {
	select {
	case <-run.runtimeReady:
		return run.getRuntime()
	case <-time.After(timeout):
		return run.getRuntime()
	}
}

func waitForRun(done <-chan struct{}, timeout time.Duration) bool {
	if done == nil {
		return true
	}
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (r *Runner) cancelRunRuntime(sessionID string, run *activePiRun, manager *RuntimeManager) {
	runtime := run.getRuntime()
	if runtime == nil {
		return
	}
	run.tryAbort(runtime)
	closeCtx, closeCancel := context.WithTimeout(context.Background(), cancelAbortTimeout)
	defer closeCancel()
	if manager != nil {
		_ = manager.RetireOwned(closeCtx, run.ownership)
		return
	}
	_ = runtime.Close(closeCtx)
}
