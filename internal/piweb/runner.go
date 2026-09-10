package piweb

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
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
	mu      sync.Mutex
	running map[string]*activePiRun
	manager *RuntimeManager
}

type activePiRun struct {
	cancel         context.CancelFunc
	commandCancel  context.CancelFunc
	commandContext context.Context
	stdin          io.WriteCloser
	runtime        PiRuntime
	compacting     bool
	done           chan struct{}
	eventsDone     chan struct{}
	runtimeReady   chan struct{}
	finishOnce     sync.Once
	settled        chan struct{}
	settledOnce    sync.Once
	runtimeOnce    sync.Once
	commandMu      sync.Mutex
	mu             sync.Mutex
	cancelling     bool
}

func NewRunner() *Runner {
	return &Runner{running: map[string]*activePiRun{}}
}

func NewRunnerWithRuntimeManager(manager *RuntimeManager) *Runner {
	runner := NewRunner()
	runner.manager = manager
	return runner
}

func (r *Runner) StartPiPrompt(
	parent context.Context,
	broker *Broker,
	store *Store,
	sessionID string,
	text string,
	images []PromptAttachment,
	displayText string,
) error {
	manager, err := r.runtimeManager(store)
	if err != nil {
		return err
	}
	eventCtx, eventCancel := context.WithCancel(parent)
	commandCtx, commandCancel := context.WithCancel(parent)
	activeRun := &activePiRun{
		cancel: eventCancel, commandCancel: commandCancel, commandContext: commandCtx, done: make(chan struct{}),
		eventsDone: make(chan struct{}), runtimeReady: make(chan struct{}), settled: make(chan struct{}),
	}
	if err := r.claimRun(sessionID, activeRun); err != nil {
		eventCancel()
		commandCancel()
		return err
	}
	runtime, err := manager.Ensure(parent, sessionID)
	if err != nil {
		r.forgetRun(sessionID, activeRun)
		eventCancel()
		commandCancel()
		return err
	}
	if !activeRun.attachRuntime(runtime) {
		_ = manager.Retire(context.Background(), sessionID, runtime)
		r.forgetRun(sessionID, activeRun)
		eventCancel()
		commandCancel()
		return errSessionBusy
	}
	ctx := eventCtx

	user := Message{Kind: "user", Text: displayText, Attachments: images}
	if err := store.AppendMessage(sessionID, user); err != nil {
		r.forgetRun(sessionID, activeRun)
		eventCancel()
		commandCancel()
		return err
	}
	broker.Publish(sessionID, "session.message", user)
	broker.Publish(sessionID, "session.status", map[string]string{"status": "running"})
	go r.consumeRuntimeEvents(ctx, broker, store, sessionID, runtime, activeRun)
	if err := activeRun.sendContext(ctx, rpcPromptCommand(text, images, "")); err != nil {
		r.finishPrompt(sessionID, activeRun, broker, err)
		return err
	}
	return nil
}

func (r *Runner) runtimeManager(store *Store) (*RuntimeManager, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.manager == nil {
		r.manager = NewRuntimeManager(store)
	}
	return r.manager, nil
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
				r.finishPrompt(sessionID, run, broker, errors.New("pi runtime exited"))
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
	if !run.complete() {
		return
	}
	r.forgetRun(sessionID, run)
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

func (r *Runner) Steer(sessionID string, text string, images []PromptAttachment) error {
	return r.SteerContext(context.Background(), sessionID, text, images)
}

func (r *Runner) SteerContext(ctx context.Context, sessionID string, text string, images []PromptAttachment) error {
	r.mu.Lock()
	run, ok := r.running[sessionID]
	r.mu.Unlock()
	if !ok || run.isCompacting() || run.isCancelling() {
		return errors.New("session is not running")
	}
	return run.sendContext(ctx, rpcPromptCommand(text, images, "steer"))
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

func (r *Runner) finishRun(sessionID string, run *activePiRun) {
	r.forgetRun(sessionID, run)
	run.markSettled()
	run.complete()
}

func (run *activePiRun) attachRuntime(runtime PiRuntime) bool {
	run.mu.Lock()
	defer run.mu.Unlock()
	if run.cancelling {
		return false
	}
	run.runtime = runtime
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

func (run *activePiRun) complete() bool {
	completed := false
	run.finishOnce.Do(func() {
		completed = true
		if run.done != nil {
			close(run.done)
		}
	})
	return completed
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

type staleEventDiscarder interface {
	discardStaleEvents()
}

func drainRuntimeEvents(runtime PiRuntime) {
	for {
		select {
		case _, ok := <-runtime.Events():
			if !ok {
				return
			}
		default:
			return
		}
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
	r.commandMu.Lock()
	defer r.commandMu.Unlock()
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
	r.commandMu.Lock()
	defer r.commandMu.Unlock()
	r.mu.Lock()
	if r.cancelling {
		r.mu.Unlock()
		return RPCResponse{}, errors.New("session is cancelling")
	}
	runtime := r.runtime
	commandCtx := r.commandContext
	if commandCtx == nil {
		commandCtx = context.Background()
	}
	r.mu.Unlock()
	if runtime == nil {
		return RPCResponse{}, errors.New("runtime is unavailable")
	}
	sendCtx, stop := context.WithCancel(ctx)
	stopCommand := context.AfterFunc(commandCtx, stop)
	defer stopCommand()
	return runtime.Send(sendCtx, command)
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
	r.mu.Lock()
	run, ok := r.running[sessionID]
	runManager := r.manager
	r.mu.Unlock()
	if !ok || !run.beginCancel() {
		return false
	}

	// Cancel the active RPC immediately (especially compact), then serialize the
	// abort behind any command that already acquired commandMu.
	runtime := run.waitRuntime(cancelSettleTimeout)
	run.commandMu.Lock()
	if runtime != nil {
		ctx, cancel := context.WithTimeout(context.Background(), cancelAbortTimeout)
		_, _ = runtime.Send(ctx, RPCCommand{Type: "abort"})
		cancel()
	}
	run.commandMu.Unlock()

	settled := waitForRun(run.settled, cancelSettleTimeout)
	if !settled {
		// Missing agent_end is a bounded protocol failure, not a reason to keep
		// the session busy forever. Stop the event consumer before retiring the
		// runtime so stale events cannot reach a future prompt.
		if run.cancel != nil {
			run.cancel()
		}
		waitForRun(run.eventsDone, cancelAbortTimeout)
	}
	if run.cancel != nil {
		run.cancel()
	}
	if trun := run.getRuntime(); trun != nil {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), cancelAbortTimeout)
		if runManager != nil {
			_ = runManager.Retire(closeCtx, sessionID, trun)
		} else {
			_ = trun.Close(closeCtx)
		}
		closeCancel()
	}
	r.finishRun(sessionID, run)
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
	run.commandMu.Lock()
	abortCtx, abortCancel := context.WithTimeout(context.Background(), cancelAbortTimeout)
	_, _ = runtime.Send(abortCtx, RPCCommand{Type: "abort"})
	abortCancel()
	run.commandMu.Unlock()
	closeCtx, closeCancel := context.WithTimeout(context.Background(), cancelAbortTimeout)
	defer closeCancel()
	if manager != nil {
		_ = manager.Retire(closeCtx, sessionID, runtime)
		return
	}
	_ = runtime.Close(closeCtx)
}
